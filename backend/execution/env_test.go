package execution

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/insightlabs38-pixel/vsf/backend/canon"
	"github.com/insightlabs38-pixel/vsf/backend/ir"
	"github.com/insightlabs38-pixel/vsf/backend/ir/rt"
)

const (
	appID        = "vendor-console:app.vendor.test"
	principal    = "prin_b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2"
	variantID    = "var_a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1"
	runtimeID    = "rt_ext01"
	t0Millis     = 1_792_368_400_000
	userCaller   = "caller-ada"
	sessionOne   = "sess-1"
	fixtureAdmin = "ops-admin"
)

var (
	sourceState = "sha256:" + strings.Repeat("3a", 32)
	capability  = "sha256:" + strings.Repeat("c4", 32)
)

func contractDoc(t testing.TB, edit func(body map[string]any)) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "schemas", "ir", "fixtures", "valid", "operation-witnessed-1-1.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	digest, ok := ir.DigestAt("operation", rt.Version{1, 1, 0})
	if !ok {
		t.Fatal("no digest for operation 1.1.0")
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(strings.ReplaceAll(string(raw), "$DIGEST", digest)), &doc); err != nil {
		t.Fatal(err)
	}
	if edit != nil {
		edit(doc["body"].(map[string]any))
	}
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	v, perr := canon.Parse(out)
	if perr != nil {
		t.Fatal(perr)
	}
	return canon.ToCanonical(v)
}

func contractPin(t testing.TB, doc []byte) Pin {
	t.Helper()
	un, err := ir.DecodeDocument(doc)
	if err != nil {
		t.Fatal(err)
	}
	return Pin{Version: string(un.Unsafe().Operation.ContractVersion), Digest: un.Digest()}
}

type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	sleeps []time.Duration
}

func newClock() *fakeClock { return &fakeClock{now: time.UnixMilli(t0Millis)} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	c.sleeps = append(c.sleeps, d)
	c.now = c.now.Add(d)
	c.mu.Unlock()
	return nil
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func (c *fakeClock) Sleeps() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]time.Duration(nil), c.sleeps...)
}

type directory struct {
	mu       sync.Mutex
	callers  map[string]Caller
	sessions map[string]Session
}

func (d *directory) Lookup(id string) (Caller, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	c, ok := d.callers[id]
	return c, ok
}

type sessionView struct{ d *directory }

func (s sessionView) Lookup(id string) (Session, bool) {
	s.d.mu.Lock()
	defer s.d.mu.Unlock()
	v, ok := s.d.sessions[id]
	return v, ok
}

func (d *directory) revoke(id string) {
	d.mu.Lock()
	delete(d.callers, id)
	d.mu.Unlock()
}

func (d *directory) setSession(s Session) {
	d.mu.Lock()
	d.sessions[s.ID] = s
	d.mu.Unlock()
}

func newDirectory() *directory {
	d := &directory{callers: map[string]Caller{}, sessions: map[string]Session{}}
	d.callers[userCaller] = Caller{ID: userCaller, Scopes: []string{"records.write", "records.read"}}
	d.callers["caller-bob"] = Caller{ID: "caller-bob", Scopes: []string{"records.write"}}
	d.sessions[sessionOne] = goodSession(sessionOne, userCaller)
	d.sessions["sess-2"] = goodSession("sess-2", userCaller)
	d.sessions["sess-bob"] = goodSession("sess-bob", "caller-bob")
	return d
}

func goodSession(id, owner string) Session {
	return Session{
		ID: id, Owner: owner, AppID: appID, AppVersion: "2.0.0", VariantClass: "sales-rep", VariantID: variantID,
		PrincipalRef: principal, RuntimeID: runtimeID, SourceState: sourceState, CapabilityDigest: capability,
		SourceScopes: []string{"records.read", "records.write"}, Authenticated: true,
	}
}

// fakeAuth mints Authorized from a decoded document without the Rust worker; only tests may do this.
type fakeAuth struct {
	mu     sync.Mutex
	deny   string
	calls  int
	seenEv []Env
}

func (f *fakeAuth) Authorize(doc []byte, env Env) (Authorized, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.seenEv = append(f.seenEv, env)
	if f.deny != "" {
		return Authorized{}, refuse(f.deny)
	}
	un, err := ir.DecodeDocument(doc)
	if err != nil || un.Unsafe().Operation == nil {
		return Authorized{}, refuse("undecodable")
	}
	return Authorized{digest: un.Digest(), contract: *un.Unsafe().Operation, env: env, minted: true}, nil
}

func (f *fakeAuth) setDeny(r string) {
	f.mu.Lock()
	f.deny = r
	f.mu.Unlock()
}

// scripted is an executor whose answers and blocking points a test controls.
type scripted struct {
	mu       sync.Mutex
	kind     string
	caps     Capabilities
	version  string
	fields   map[string]string
	confirm  bool
	applies  []Call
	reads    int
	results  []Result
	errs     []error
	applyFn  func(ctx context.Context, c Call) (Result, error)
	readErr  error
	inflight int
	maxIn    int
	onCommit func(s *scripted)
}

func newScripted() *scripted {
	return &scripted{kind: "provider_api_authorized", caps: Capabilities{ReadBack: true}, version: "v1", fields: map[string]string{"stage": "new"}}
}

func (s *scripted) Kind() string               { return s.kind }
func (s *scripted) Capabilities() Capabilities { return s.caps }

func (s *scripted) Read(ctx context.Context, t Target) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reads++
	if s.readErr != nil {
		return Snapshot{}, s.readErr
	}
	f := map[string]string{}
	for k, v := range s.fields {
		f[k] = v
	}
	return Snapshot{Version: s.version, Fields: f, ConfirmationRequired: s.confirm, ObservedAt: time.UnixMilli(t0Millis)}, nil
}

func (s *scripted) Apply(ctx context.Context, c Call) (Result, error) {
	s.mu.Lock()
	s.applies = append(s.applies, c)
	n := len(s.applies)
	s.inflight++
	if s.inflight > s.maxIn {
		s.maxIn = s.inflight
	}
	fn := s.applyFn
	var res Result
	var err error
	if n <= len(s.results) {
		res = s.results[n-1]
	} else {
		res = Result{Outcome: OutcomeCommitted, Echo: map[string]string{}}
	}
	if n <= len(s.errs) {
		err = s.errs[n-1]
	}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.inflight--
		s.mu.Unlock()
	}()
	if fn != nil {
		return fn(ctx, c)
	}
	if err == nil && res.Outcome == OutcomeCommitted {
		s.mu.Lock()
		s.fields["stage"] = "won"
		s.version = "v" + string(rune('1'+n))
		cb := s.onCommit
		s.mu.Unlock()
		if cb != nil {
			cb(s)
		}
	}
	return res, err
}

func (s *scripted) applyCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.applies)
}

func (s *scripted) readCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reads
}

type rig struct {
	t     *testing.T
	rt    *Runtime
	ex    *scripted
	dir   *directory
	auth  *fakeAuth
	clock *fakeClock
	store *MemStore
	conf  *Confirmer
	doc   []byte
	cfg   Config
}

func newRig(t *testing.T, edit func(map[string]any)) *rig {
	t.Helper()
	r := &rig{t: t, ex: newScripted(), dir: newDirectory(), auth: &fakeAuth{}, clock: newClock(), store: &MemStore{}, conf: NewConfirmer([]byte("test-secret"))}
	r.doc = contractDoc(t, edit)
	r.build(r.store)
	return r
}

func (r *rig) build(store Store) {
	j, err := OpenJournal(store)
	if err != nil {
		r.t.Fatal(err)
	}
	r.cfg = Config{
		Journal: j, Authorizer: r.auth, Callers: r.dir, Sessions: sessionView{r.dir},
		Executors: map[string]Executor{"provider_api_authorized": r.ex, "verified_browser_plan": r.ex},
		Confirmer: r.conf, Clock: r.clock, ApplyTimeout: 5 * time.Second,
		Retry: RetryPolicy{MaxAttempts: 3, Base: 100 * time.Millisecond, Max: time.Second},
	}
	r.rt, err = NewRuntime(r.cfg)
	if err != nil {
		r.t.Fatal(err)
	}
}

func (r *rig) request(id string) Request {
	return Request{
		RequestID: id, Caller: userCaller, SessionID: sessionOne, Contract: r.doc, Pin: contractPin(r.t, r.doc),
		Params: map[string]any{"record_id": "o1", "stage": "won"}, Record: "o1", Expect: map[string]string{"stage": "won"},
		Seen: &Seen{Version: "v1", ObservedAt: time.UnixMilli(t0Millis)},
	}
}

func (r *rig) run(req Request) (Record, error) {
	r.t.Helper()
	h, err := r.rt.Submit(context.Background(), req)
	if h == nil {
		return Record{}, err
	}
	rec, werr := h.Wait(context.Background())
	if err == nil {
		err = werr
	}
	return rec, err
}

func (r *rig) confirmation(req Request) Confirmation {
	return r.conf.Issue(requestDigest(req.Pin.Digest, req.Record, req.Params), "ada", r.clock.Now().Add(time.Hour))
}

func mustState(t *testing.T, rec Record, s State, reason string) {
	t.Helper()
	if rec.State != s || (reason != "" && rec.Reason != reason) {
		t.Fatalf("state %s/%q (%s), want %s/%q", rec.State, rec.Reason, rec.Detail, s, reason)
	}
}

func errorsIs(err, target error) bool { return errors.Is(err, target) }
