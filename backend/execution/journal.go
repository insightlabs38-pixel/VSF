package execution

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
)

var ErrCorrupt = errors.New("journal is corrupt")

// ErrBlocked means an unresolved write overlaps the one that wanted to start.
var ErrBlocked = errors.New("an unresolved write overlaps this one")

type Event struct {
	Seq    int64             `json:"seq"`
	Key    string            `json:"key"`
	Kind   string            `json:"kind"`
	From   State             `json:"from,omitempty"`
	To     State             `json:"to,omitempty"`
	Reason string            `json:"reason,omitempty"`
	Detail string            `json:"detail,omitempty"`
	At     int64             `json:"at"`
	Meta   map[string]string `json:"meta,omitempty"`
	Prev   string            `json:"prev"`
	Hash   string            `json:"hash"`
}

type Store interface {
	Append(line []byte) error
	ReadAll() ([][]byte, error)
}

type MemStore struct {
	mu    sync.Mutex
	lines [][]byte
}

func (m *MemStore) Append(line []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lines = append(m.lines, append([]byte(nil), line...))
	return nil
}

func (m *MemStore) ReadAll() ([][]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([][]byte(nil), m.lines...), nil
}

type FileStore struct {
	mu   sync.Mutex
	path string
}

func NewFileStore(path string) *FileStore { return &FileStore{path: path} }

func (f *FileStore) Append(line []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	fh, err := os.OpenFile(f.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := fh.Write(append(append([]byte(nil), line...), '\n')); err != nil {
		_ = fh.Close()
		return err
	}
	if err := fh.Sync(); err != nil {
		_ = fh.Close()
		return err
	}
	return fh.Close()
}

func (f *FileStore) ReadAll() ([][]byte, error) {
	fh, err := os.Open(f.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	var out [][]byte
	sc := bufio.NewScanner(fh)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		out = append(out, append([]byte(nil), sc.Bytes()...))
	}
	return out, sc.Err()
}

// Record is the current view of one operation, folded from its events.
type Record struct {
	Key            string
	OperationID    string
	ContractDigest string
	ParamsDigest   string
	RecordKey      string
	Caller         string
	Session        string
	State          State
	Reason         string
	Detail         string
	Seq            int64
	Footprint      Footprint
	Admitted       *Admission
	Events         []Event
}

// Admission is the immutable execution identity an operation was admitted under.
type Admission struct {
	Env          Env    `json:"env"`
	AppID        string `json:"app_id"`
	VariantClass string `json:"variant_class"`
}

type Journal struct {
	mu      sync.Mutex
	store   Store
	records map[string]*Record
	order   []string
	seq     int64
	prev    string
	broken  error
	open    map[string]struct{}
}

func eventHash(e Event) string {
	e.Hash = ""
	b, _ := json.Marshal(e)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// OpenJournal replays the store and refuses a broken chain, a gap or an illegal transition.
func OpenJournal(store Store) (*Journal, error) {
	j := &Journal{store: store, records: map[string]*Record{}, open: map[string]struct{}{}}
	lines, err := store.ReadAll()
	if err != nil {
		return nil, err
	}
	for i, line := range lines {
		var e Event
		dec := json.NewDecoder(bytes.NewReader(line))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&e); err != nil {
			return nil, fmt.Errorf("%w: line %d", ErrCorrupt, i+1)
		}
		if e.Seq != j.seq+1 || e.Prev != j.prev || e.Hash != eventHash(e) {
			return nil, fmt.Errorf("%w: line %d", ErrCorrupt, i+1)
		}
		if err := j.fold(e); err != nil {
			return nil, fmt.Errorf("%w: line %d: %v", ErrCorrupt, i+1, err)
		}
		j.seq, j.prev = e.Seq, e.Hash
	}
	return j, nil
}

func (j *Journal) fold(e Event) error {
	r := j.records[e.Key]
	switch e.Kind {
	case "create":
		if r != nil || e.To != Prepared {
			return errors.New("duplicate or malformed create")
		}
		r = &Record{
			Key: e.Key, State: Prepared, OperationID: e.Meta["operation_id"], ContractDigest: e.Meta["contract_digest"],
			ParamsDigest: e.Meta["params_digest"], RecordKey: e.Meta["record"], Caller: e.Meta["caller"], Session: e.Meta["session"],
			Footprint: footprintFromMeta(e.Meta),
		}
		j.records[e.Key] = r
		j.order = append(j.order, e.Key)
	case "state":
		if r == nil || r.State != e.From || !canMove(e.From, e.To) {
			return fmt.Errorf("illegal transition %s -> %s", e.From, e.To)
		}
		r.State, r.Reason, r.Detail = e.To, e.Reason, e.Detail
		if e.To == Admitted {
			var a Admission
			if json.Unmarshal([]byte(e.Detail), &a) == nil {
				r.Admitted = &a
			}
		}
		if e.To == Unknown {
			j.open[e.Key] = struct{}{}
		} else {
			delete(j.open, e.Key)
		}
	case "note":
		if r == nil {
			return errors.New("note for unknown operation")
		}
	default:
		return errors.New("unknown event kind")
	}
	r.Seq = e.Seq
	r.Events = append(r.Events, e)
	return nil
}

func (j *Journal) append(e Event) error {
	if j.broken != nil {
		return j.broken
	}
	e.Seq, e.Prev = j.seq+1, j.prev
	e.Hash = eventHash(e)
	line, _ := json.Marshal(e)
	if err := j.fold(e); err != nil {
		return err
	}
	if err := j.store.Append(line); err != nil {
		j.broken = fmt.Errorf("journal write failed: %w", err)
		return j.broken
	}
	j.seq, j.prev = e.Seq, e.Hash
	return nil
}

func (j *Journal) Create(key string, now int64, meta map[string]string) (Record, bool, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if r, ok := j.records[key]; ok {
		return j.copy(r), false, nil
	}
	if err := j.append(Event{Key: key, Kind: "create", To: Prepared, At: now, Meta: meta}); err != nil {
		return Record{}, false, err
	}
	return j.copy(j.records[key]), true, nil
}

// Move applies one legal transition; the journal itself refuses every other one.
func (j *Journal) Move(key string, to State, reason, detail string, now int64) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	r := j.records[key]
	if r == nil {
		return errors.New("unknown operation")
	}
	if !canMove(r.State, to) {
		return fmt.Errorf("illegal transition %s -> %s", r.State, to)
	}
	return j.append(Event{Key: key, Kind: "state", From: r.State, To: to, Reason: reason, Detail: detail, At: now})
}

func (j *Journal) Note(key, detail string, now int64) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.records[key] == nil {
		return errors.New("unknown operation")
	}
	return j.append(Event{Key: key, Kind: "note", Detail: detail, At: now})
}

func (j *Journal) copy(r *Record) Record {
	c := *r
	c.Events = append([]Event(nil), r.Events...)
	return c
}

func (j *Journal) Get(key string) (Record, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	r, ok := j.records[key]
	if !ok {
		return Record{}, false
	}
	return j.copy(r), true
}

func (j *Journal) Keys() []string {
	j.mu.Lock()
	defer j.mu.Unlock()
	return append([]string(nil), j.order...)
}

func (j *Journal) Err() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.broken
}

// footprintFromMeta reads what a create event recorded; a record without it is assumed to span its whole session.
func footprintFromMeta(m map[string]string) Footprint {
	fp := Footprint{App: m["app_id"], Session: m["session"], Record: m["record"], Level: Level(m["level"]), Exclusive: m["exclusive"] == "true"}
	switch fp.Level {
	case LevelRecord, LevelSession, LevelGlobal:
	default:
		fp.Level = LevelSession
	}
	return fp
}

func footprintMeta(fp Footprint) map[string]string {
	return map[string]string{"app_id": fp.App, "level": string(fp.Level), "exclusive": fmt.Sprint(fp.Exclusive)}
}

// blocking returns an unresolved write, other than except, whose footprint overlaps fp.
func (j *Journal) blocking(fp Footprint, except string) (Record, bool) {
	for key := range j.open {
		if key == except {
			continue
		}
		if r := j.records[key]; r.Footprint.Overlaps(fp) {
			return j.copy(r), true
		}
	}
	return Record{}, false
}

// Blocking is the advisory form of the check Start makes atomically.
func (j *Journal) Blocking(fp Footprint, except string) (Record, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.blocking(fp, except)
}

// Start moves an admitted write to Running only if no overlapping write is unresolved, in one critical section.
// Every transition into Unknown passes through the same mutex, so no source effect can slip between check and start.
func (j *Journal) Start(key string, detail string, now int64) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	r := j.records[key]
	if r == nil {
		return errors.New("unknown operation")
	}
	if j.broken != nil {
		return j.broken
	}
	if !canMove(r.State, Running) {
		return fmt.Errorf("illegal transition %s -> %s", r.State, Running)
	}
	if other, ok := j.blocking(r.Footprint, key); ok {
		return fmt.Errorf("%w: %s", ErrBlocked, other.Key)
	}
	return j.append(Event{Key: key, Kind: "state", From: r.State, To: Running, Detail: detail, At: now})
}

// Unresolved lists operations whose outcome is still unknown, oldest first.
func (j *Journal) Unresolved() []Record {
	j.mu.Lock()
	defer j.mu.Unlock()
	var out []Record
	for _, k := range j.order {
		if _, ok := j.open[k]; ok {
			out = append(out, j.copy(j.records[k]))
		}
	}
	return out
}
