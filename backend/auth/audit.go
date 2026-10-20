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
)

var events = map[Event]bool{EventIssued: true, EventRefused: true, EventRevoked: true, EventPaused: true, EventResumed: true, EventSignedIn: true,
	EventSignedOut: true, EventLease: true, EventRevealed: true, EventCancelled: true, EventProfile: true, EventPlanStale: true, EventDelegated: true, EventUndelegated: true}

var reasons = map[Reason]bool{}

func init() {
	for _, r := range []Reason{ReasonUnknownGrant, ReasonOwner, ReasonSource, ReasonAudience, ReasonScope, ReasonExpired, ReasonRevoked, ReasonPaused, ReasonOK} {
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

// Audit is an append-only, hash-chained log: removing, reordering or editing any line breaks the chain from there on.
type Audit struct {
	mu   sync.Mutex
	w    io.Writer
	seq  uint64
	prev string
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
	a.seq++
	l := auditLine{Seq: a.seq, At: r.At.UnixMilli(), Event: r.Event, Grant: r.Grant, Account: r.Account, Actor: r.Actor, Origin: r.Origin, Reason: r.Reason, Ref: r.Ref, Prev: a.prev}
	l.Hash = hashLine(l)
	b, _ := json.Marshal(l)
	if _, err := a.w.Write(append(b, '\n')); err != nil {
		a.seq--
		return err
	}
	a.prev = l.Hash
	return nil
}

// VerifyAudit checks the chain of a log from its start and returns how many records it holds.
func VerifyAudit(r io.Reader) (int, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	prev, n := genesis, 0
	for sc.Scan() {
		var l auditLine
		dec := json.NewDecoder(bytes.NewReader(sc.Bytes()))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&l); err != nil {
			return n, fmt.Errorf("%w: line %d", ErrAuditTampered, n+1)
		}
		if l.Seq != uint64(n+1) || l.Prev != prev || l.Hash != hashLine(l) {
			return n, fmt.Errorf("%w: line %d", ErrAuditTampered, n+1)
		}
		prev = l.Hash
		n++
	}
	return n, sc.Err()
}
