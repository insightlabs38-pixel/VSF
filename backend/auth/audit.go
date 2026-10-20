package auth

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

// Event is the closed set of things the audit log records.
type Event string

const (
	EventIssued      Event = "grant_issued"
	EventRefused     Event = "use_refused"
	EventRevoked     Event = "grant_revoked"
	EventPaused      Event = "reauth_required"
	EventResumed     Event = "reauth_completed"
	EventSignedIn    Event = "signed_in"
	EventSignedOut   Event = "signed_out"
	EventLease       Event = "credential_leased"
	EventRevealed    Event = "credential_revealed"
	EventCancelled   Event = "dependent_cancelled"
	EventProfile     Event = "profile_changed"
	EventPlanStale   Event = "plan_invalidated"
	EventDelegated   Event = "delegation_given"
	EventUndelegated Event = "delegation_withdrawn"
	EventWipeFailed  Event = "secret_delete_failed"
	EventRotated     Event = "audit_rotated"
	EventRefreshed   Event = "token_refreshed"
)

var events = map[Event]bool{EventIssued: true, EventRefused: true, EventRevoked: true, EventPaused: true, EventResumed: true, EventSignedIn: true,
	EventSignedOut: true, EventLease: true, EventRevealed: true, EventCancelled: true, EventProfile: true, EventPlanStale: true, EventDelegated: true, EventUndelegated: true, EventWipeFailed: true, EventRotated: true, EventRefreshed: true}

var reasons = map[Reason]bool{}

func init() {
	for _, r := range []Reason{ReasonUnknownGrant, ReasonOwner, ReasonSource, ReasonAudience, ReasonScope, ReasonExpired, ReasonRevoked, ReasonPaused, ReasonOK, ReasonEpoch, ReasonIdentity, ReasonRuntime, ReasonReplay, ReasonWipe, ReasonProvider, ReasonAmbiguous} {
		reasons[r] = true
	}
}

// AddReasons lets a later file in this package extend the closed set at start-up.
func addReasons(rs ...Reason) {
	for _, r := range rs {
		reasons[r] = true
	}
}

// AuditRecord is what a caller supplies. There is no free text: every field is an identifier, an origin or a member of a closed set, so a record cannot carry a secret or a person's data.
type AuditRecord struct {
	At      time.Time
	Event   Event
	Grant   string
	Account string
	Actor   string
	Origin  string
	Reason  Reason
	// Ref is a short digest, such as the start of a profile digest; it identifies without describing.
	Ref string
}

type auditLine struct {
	Seq     uint64 `json:"seq"`
	At      int64  `json:"at"`
	Event   Event  `json:"event"`
	Grant   string `json:"grant,omitempty"`
	Account string `json:"account,omitempty"`
	Actor   string `json:"actor,omitempty"`
	Origin  string `json:"origin,omitempty"`
	Reason  Reason `json:"reason,omitempty"`
	Ref     string `json:"ref,omitempty"`
	Prev    string `json:"prev"`
	Hash    string `json:"hash"`
}

var ErrAuditTampered = errors.New("auth.audit_tampered")

// Audit is an append-only, hash-chained log: removing, reordering or editing any line breaks the chain from there on. Durability: when the writer can Sync (a file), every record is synced before Append returns, so a nil error means the line reached stable storage. A write that fails before any byte is written can be retried; a partial write leaves the log needing recovery (OpenAuditFile truncates the torn tail), and until then every Append fails. Dropping the tail of a valid log is undetectable without an external anchor, such as the last hash kept elsewhere.
type Audit struct {
	mu     sync.Mutex
	w      io.Writer
	seq    uint64
	prev   string
	broken bool
	file   *os.File
	path   string
}

const genesis = "0000000000000000000000000000000000000000000000000000000000000000"

func NewAudit(w io.Writer) *Audit { return &Audit{w: w, prev: genesis} }

func hashLine(l auditLine) string {
	l.Hash = ""
	b, _ := json.Marshal(l)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func (r AuditRecord) valid() bool {
	ok := func(s string) bool { return s == "" || idRe.MatchString(s) }
	return events[r.Event] && ok(r.Grant) && ok(r.Account) && ok(r.Actor) && (r.Origin == "" || originRe.MatchString(r.Origin)) && (r.Reason == "" || reasons[r.Reason]) && (r.Ref == "" || refRe.MatchString(r.Ref))
}

var refRe = regexp.MustCompile(`^[a-f0-9]{8,64}$`)

// Append writes one record; a record that does not fit the closed vocabulary is refused rather than written.
func (a *Audit) Append(r AuditRecord) error {
	if !r.valid() {
		return ErrInvalid
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.appendLocked(r)
}

func (a *Audit) appendLocked(r AuditRecord) error {
	if a.broken {
		return ErrAudit
	}
	l := auditLine{Seq: a.seq + 1, At: r.At.UnixMilli(), Event: r.Event, Grant: r.Grant, Account: r.Account, Actor: r.Actor, Origin: r.Origin, Reason: r.Reason, Ref: r.Ref, Prev: a.prev}
	l.Hash = hashLine(l)
	b, _ := json.Marshal(l)
	b = append(b, '\n')
	n, err := a.w.Write(b)
	if err != nil || n != len(b) {
		if n > 0 {
			a.broken = true
		}
		return errors.Join(ErrAudit, err)
	}
	a.seq, a.prev = l.Seq, l.Hash
	if s, ok := a.w.(interface{ Sync() error }); ok {
		if err := s.Sync(); err != nil {
			a.broken = true
			return errors.Join(ErrAudit, err)
		}
	}
	return nil
}

// Head returns the sequence number and hash of the last record, for anchoring outside the log.
func (a *Audit) Head() (uint64, string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.seq, a.prev
}

// Recovery says what opening an audit file found.
type Recovery struct {
	Records        int
	TruncatedBytes int64
}

// OpenAuditFile opens or creates a private audit file, verifies its chain and continues it. A torn final line (a crash or a partial write) is cut off and reported; damage anywhere else is tampering and the file is not opened.
func OpenAuditFile(path string) (*Audit, Recovery, error) {
	return openAuditFile(path, genesis, 0)
}

func openAuditFile(path, prev string, seq uint64) (*Audit, Recovery, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, Recovery{}, err
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, Recovery{}, err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		f.Close()
		return nil, Recovery{}, err
	}
	good, n, lastHash, lastSeq := 0, 0, prev, seq
	if end := bytes.IndexByte(data, '\n'); end > 0 && prev == genesis {
		// a rotated segment starts mid-chain; its first line must still be internally sound
		var first auditLine
		if json.Unmarshal(data[:end], &first) == nil && first.Seq > 1 && first.Hash == hashLine(first) {
			lastHash, lastSeq = first.Prev, first.Seq-1
		}
	}
	for good < len(data) {
		end := bytes.IndexByte(data[good:], '\n')
		if end < 0 {
			break
		}
		var l auditLine
		dec := json.NewDecoder(bytes.NewReader(data[good : good+end]))
		dec.DisallowUnknownFields()
		if dec.Decode(&l) != nil || l.Seq != lastSeq+1 || l.Prev != lastHash || l.Hash != hashLine(l) {
			if good+end+1 < len(data) {
				f.Close()
				return nil, Recovery{}, fmt.Errorf("%w: line %d", ErrAuditTampered, n+1)
			}
			break
		}
		lastHash, lastSeq = l.Hash, l.Seq
		good += end + 1
		n++
	}
	rec := Recovery{Records: n, TruncatedBytes: int64(len(data) - good)}
	if rec.TruncatedBytes > 0 {
		if err := f.Truncate(int64(good)); err != nil {
			f.Close()
			return nil, rec, err
		}
		if err := f.Sync(); err != nil {
			f.Close()
			return nil, rec, err
		}
	}
	if _, err := f.Seek(int64(good), io.SeekStart); err != nil {
		f.Close()
		return nil, rec, err
	}
	return &Audit{w: f, file: f, path: path, seq: lastSeq, prev: lastHash}, rec, nil
}

// Rotate seals the current file with a closing record, moves it to archive and continues the same chain in a fresh file at the original path, so the segments verify end to end (VerifyAuditFrom).
func (a *Audit) Rotate(archive string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.file == nil || a.broken {
		return ErrAudit
	}
	if err := a.appendLocked(AuditRecord{At: time.Now(), Event: EventRotated, Reason: ReasonOK}); err != nil {
		return err
	}
	if err := a.file.Close(); err != nil {
		a.broken = true
		return err
	}
	if err := os.Rename(a.path, archive); err != nil {
		a.broken = true
		return err
	}
	next, _, err := openAuditFile(a.path, a.prev, a.seq)
	if err != nil {
		a.broken = true
		return err
	}
	a.w, a.file = next.w, next.file
	return nil
}

// Close flushes and closes a file-backed log.
func (a *Audit) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.file == nil {
		return nil
	}
	a.broken = true
	err := a.file.Sync()
	return errors.Join(err, a.file.Close())
}

// VerifyAudit checks the chain of a log from its start and returns how many records it holds.
func VerifyAudit(r io.Reader) (int, error) {
	n, _, _, err := VerifyAuditFrom(r, genesis, 0)
	return n, err
}

// VerifyAuditFrom checks a log segment that continues a chain, starting after the record with the given hash and sequence number, and returns the count and the last hash and sequence.
func VerifyAuditFrom(r io.Reader, prev string, seq uint64) (int, string, uint64, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	n := 0
	for sc.Scan() {
		var l auditLine
		dec := json.NewDecoder(bytes.NewReader(sc.Bytes()))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&l); err != nil {
			return n, prev, seq, fmt.Errorf("%w: line %d", ErrAuditTampered, n+1)
		}
		if l.Seq != seq+1 || l.Prev != prev || l.Hash != hashLine(l) {
			return n, prev, seq, fmt.Errorf("%w: line %d", ErrAuditTampered, n+1)
		}
		prev, seq = l.Hash, l.Seq
		n++
	}
	return n, prev, seq, sc.Err()
}
