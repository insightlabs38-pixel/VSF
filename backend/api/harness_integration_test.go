//go:build integration

package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/insightlabs38-pixel/vsf/backend/execution"
	"github.com/insightlabs38-pixel/vsf/backend/internal/fixtureapp"
)

const (
	principalRef = "prin_b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2"
	variantID    = "var_a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1"
	runtimeID    = "rt_ext01"
	t0Millis     = 1_792_368_400_000
	ada          = "caller-ada"
	bob          = "caller-bob"
	sessAda      = "sess-ada"
	sessBob      = "sess-bob"
)

var (
	sourceState = "sha256:" + strings.Repeat("3a", 32)
	capability  = "sha256:" + strings.Repeat("c4", 32)
)

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

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
	c.now = c.now.Add(d)
	c.mu.Unlock()
	return nil
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

type directory struct {
	mu       sync.Mutex
	callers  map[string]execution.Caller
	sessions map[string]execution.Session
}

func (d *directory) Lookup(id string) (execution.Caller, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	c, ok := d.callers[id]
	return c, ok
}

type sessionView struct{ d *directory }

func (s sessionView) Lookup(id string) (execution.Session, bool) {
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

func (d *directory) setSession(s execution.Session) {
	d.mu.Lock()
	d.sessions[s.ID] = s
	d.mu.Unlock()
}

func (d *directory) setScopes(id string, scopes ...string) {
	d.mu.Lock()
	c := d.callers[id]
	c.Scopes = scopes
	d.callers[id] = c
	d.mu.Unlock()
}

func goodSession(id, owner string) execution.Session {
	return execution.Session{
		ID: id, Owner: owner, AppID: testApp, AppVersion: "2.0.0", VariantClass: "sales-rep", VariantID: variantID,
		PrincipalRef: principalRef, RuntimeID: runtimeID, SourceState: sourceState, CapabilityDigest: capability,
		SourceScopes: []string{"records.read", "records.write"}, Authenticated: true,
	}
}

func newDirectory() *directory {
	d := &directory{callers: map[string]execution.Caller{}, sessions: map[string]execution.Session{}}
	d.callers[ada] = execution.Caller{ID: ada, Scopes: []string{"records.write", "records.read"}}
	d.callers[bob] = execution.Caller{ID: bob, Scopes: []string{"records.write", "records.read"}}
	d.sessions[sessAda] = goodSession(sessAda, ada)
	d.sessions[sessBob] = goodSession(sessBob, bob)
	return d
}

type revocations struct {
	mu  sync.Mutex
	set map[string]string
}

func (r *revocations) RevokedContract(digest, _, _ string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	why, ok := r.set[digest]
	return why, ok
}

func (r *revocations) revoke(digest, why string) {
	r.mu.Lock()
	r.set[digest] = why
	r.mu.Unlock()
}

func fabricPath(t testing.TB) string {
	t.Helper()
	p := os.Getenv("VSF_FABRIC")
	if p == "" {
		p = filepath.Join("..", "..", "artifacts", "bin", "vsf-fabric")
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("native worker not found at %s (run scripts/build-native.sh or set VSF_FABRIC): %v", p, err)
	}
	return p
}

// stack is the real service over the real Rust worker, journal, executor and a fixture source application.
type stack struct {
	t      *testing.T
	svc    *Service
	cat    *Catalog
	rt     *execution.Runtime
	app    *fixtureapp.App
	dir    *directory
	clock  *fakeClock
	rev    *revocations
	worker *execution.Worker
	doc    []byte
	spec   *Spec
	cfg    execution.Config
}

type stackOpts struct {
	docs    [][]byte
	limiter *CallLimiter
	apply   time.Duration
	dedup   *bool
	receipt bool
}

func newStack(t *testing.T, o stackOpts) *stack {
	t.Helper()
	w, err := execution.StartWorker(fabricPath(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	dedup := true
	if o.dedup != nil {
		dedup = *o.dedup
	}
	app := fixtureapp.New(dedup)
	t.Cleanup(app.Close)
	if o.receipt {
		app.SetReceipts(true, true)
	}
	s := &stack{t: t, app: app, dir: newDirectory(), clock: &fakeClock{now: time.UnixMilli(t0Millis)}, rev: &revocations{set: map[string]string{}}, worker: w}
	s.doc = contractDoc(t, nil)
	docs := o.docs
	if len(docs) == 0 {
		docs = [][]byte{s.doc}
	}
	src, err := NewMemorySource(docs...)
	if err != nil {
		t.Fatal(err)
	}
	s.cat, err = NewCatalog(CatalogConfig{Source: src, Validator: WorkerValidator{W: w}, Revocations: s.rev})
	if err != nil {
		t.Fatal(err)
	}
	s.spec = specOfDoc(t, s.doc)
	j, err := execution.OpenJournal(&execution.MemStore{})
	if err != nil {
		t.Fatal(err)
	}
	apply := o.apply
	if apply == 0 {
		apply = 3 * time.Second
	}
	ex := execution.NewAPIExecutor(execution.APIConfig{
		BaseURL: app.URL(), ReadPath: "/api/orders/%s", WritePath: "/api/orders/%s/stage", Dedup: dedup,
		ReceiptPath: map[bool]string{true: "/api/receipts/%s"}[o.receipt], Fenced: o.receipt,
		Client: &http.Client{Timeout: 2 * time.Second},
	})
	s.cfg = execution.Config{
		Journal: j, Authorizer: w, Callers: s.dir, Sessions: sessionView{s.dir}, Revocations: s.rev,
		Executors: map[string]execution.Executor{"provider_api_authorized": ex},
		Confirmer: execution.NewConfirmer([]byte("test-secret")), Clock: s.clock, ApplyTimeout: apply,
		Retry: execution.RetryPolicy{MaxAttempts: 3, Base: 10 * time.Millisecond, Max: 50 * time.Millisecond},
	}
	s.rt, err = execution.NewRuntime(s.cfg)
	if err != nil {
		t.Fatal(err)
	}
	s.svc, err = NewService(ServiceConfig{Catalog: s.cat, Runtime: s.rt, Callers: s.dir, Sessions: sessionView{s.dir}, Limiter: o.limiter})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// tokens is a stand-in authenticator for tests that do not exercise authentication.
type tokens map[string]Principal

func (k tokens) Authenticate(surface Surface, cred string) (Principal, *Error) {
	p, ok := k[cred]
	if !ok {
		return Principal{}, newError(CodeUnauthenticated, "")
	}
	if p.Surface != surface {
		return Principal{}, newError(CodeWrongSurface, "")
	}
	return p, nil
}

func (s *stack) rest(a Authenticator) *httptest.Server {
	h := NewREST(RESTConfig{Backend: s.svc, Auth: a, OpenAPI: func(p Principal, ns string) (map[string]any, *Error) { return OpenAPI(s.svc, p, ns) }})
	srv := httptest.NewServer(h)
	s.t.Cleanup(srv.Close)
	return srv
}

func (s *stack) invoke(p Principal, mut func(m map[string]any)) Result {
	s.t.Helper()
	return s.svc.Invoke(context.Background(), p, envelope(mut), Route{Operation: s.spec.Name, Version: s.spec.ContractVersion})
}

func (s *stack) orderStage(id string) string { return s.app.Order(id).Stage }

func principalOf(caller string, surface Surface) Principal {
	return Principal{Caller: caller, Surface: surface, Credential: "cred-" + caller + "-" + string(surface)}
}

func withSession(sess string) func(m map[string]any) {
	return func(m map[string]any) { m["session_id"] = sess }
}
