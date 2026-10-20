// Package stacktest wires the real native worker, journal, executor and a fixture source application behind the service.
package stacktest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/insightlabs38-pixel/vsf/backend/api"
	"github.com/insightlabs38-pixel/vsf/backend/canon"
	"github.com/insightlabs38-pixel/vsf/backend/execution"
	"github.com/insightlabs38-pixel/vsf/backend/internal/fixtureapp"
	"github.com/insightlabs38-pixel/vsf/backend/ir"
	"github.com/insightlabs38-pixel/vsf/backend/ir/rt"
)

const (
	TestApp       = "vendor-console:app.vendor.test"
	TestOperation = "app.records.update_stage"
	principalRef  = "prin_b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2"
	variantID     = "var_a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1"
	runtimeID     = "rt_ext01"
	t0Millis      = 1_792_368_400_000
	Ada           = "caller-Ada"
	Bob           = "caller-Bob"
	SessAda       = "sess-Ada"
	SessBob       = "sess-Bob"
)

var (
	sourceState = "sha256:" + strings.Repeat("3a", 32)
	capability  = "sha256:" + strings.Repeat("c4", 32)
)

type Clock struct {
	mu  sync.Mutex
	now time.Time
}

// NewClock starts at the instant the fixture witnesses were observed.
func NewClock() *Clock { return &Clock{now: time.UnixMilli(t0Millis)} }

func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *Clock) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
	return nil
}

func (c *Clock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

// Directory is both the caller and the session directory.
type Directory struct {
	mu       sync.Mutex
	callers  map[string]execution.Caller
	sessions map[string]execution.Session
}

func (d *Directory) Lookup(id string) (execution.Caller, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	c, ok := d.callers[id]
	return c, ok
}

type SessionView struct{ d *Directory }

func (s SessionView) Lookup(id string) (execution.Session, bool) {
	s.d.mu.Lock()
	defer s.d.mu.Unlock()
	v, ok := s.d.sessions[id]
	return v, ok
}

// Revoke removes a caller from the directory.
func (d *Directory) Revoke(id string) {
	d.mu.Lock()
	delete(d.callers, id)
	d.mu.Unlock()
}

// SetSession stores or replaces a source session.
func (d *Directory) SetSession(s execution.Session) {
	d.mu.Lock()
	d.sessions[s.ID] = s
	d.mu.Unlock()
}

// SetScopes replaces a caller's scopes.
func (d *Directory) SetScopes(id string, scopes ...string) {
	d.mu.Lock()
	c := d.callers[id]
	c.Scopes = scopes
	d.callers[id] = c
	d.mu.Unlock()
}

// GoodSession is a source session the fixture contract admits.
func GoodSession(id, owner string) execution.Session {
	return execution.Session{
		ID: id, Owner: owner, AppID: TestApp, AppVersion: "2.0.0", VariantClass: "sales-rep", VariantID: variantID,
		PrincipalRef: principalRef, RuntimeID: runtimeID, Origin: "https://app.example.com", SourceState: sourceState, CapabilityDigest: capability,
		SourceScopes: []string{"records.read", "records.write"}, Authenticated: true,
	}
}

// NewDirectory has two callers, Ada and Bob, each owning one session.
func NewDirectory() *Directory {
	d := &Directory{callers: map[string]execution.Caller{}, sessions: map[string]execution.Session{}}
	d.callers[Ada] = execution.Caller{ID: Ada, Scopes: []string{"records.write", "records.read"}}
	d.callers[Bob] = execution.Caller{ID: Bob, Scopes: []string{"records.write", "records.read"}}
	d.sessions[SessAda] = GoodSession(SessAda, Ada)
	d.sessions[SessBob] = GoodSession(SessBob, Bob)
	return d
}

type Revocations struct {
	mu  sync.Mutex
	set map[string]string
}

// NewRevocations starts empty.
func NewRevocations() *Revocations { return &Revocations{set: map[string]string{}} }

func (r *Revocations) RevokedContract(digest, _, _ string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	why, ok := r.set[digest]
	return why, ok
}

// Revoke marks one contract digest revoked.
func (r *Revocations) Revoke(digest, why string) {
	r.mu.Lock()
	r.set[digest] = why
	r.mu.Unlock()
}

func fabricPath(t testing.TB) string {
	t.Helper()
	p := os.Getenv("VSF_FABRIC")
	if p == "" {
		p = filepath.Join(repoRoot(), "artifacts", "bin", "vsf-fabric")
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("native worker not found at %s (run scripts/build-native.sh or set VSF_FABRIC): %v", p, err)
	}
	return p
}

// repoRoot finds the repository from this file, so tests run from any package directory.
func repoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..")
}

// ContractDoc loads the witnessed update_stage contract, optionally edited, as a canonical document.
func ContractDoc(t testing.TB, edit func(body map[string]any)) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot(), "schemas", "ir", "fixtures", "valid", "operation-witnessed-1-1.json"))
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

// Envelope builds an update_stage invocation body for Ada's session, optionally edited.
func Envelope(mut func(m map[string]any)) []byte {
	m := map[string]any{
		"request_id": "req-1", "session_id": SessAda, "record": "o1",
		"params": map[string]any{"record_id": "o1", "stage": "won"},
		"expect": map[string]any{"stage": "won"}, "seen": map[string]any{"version": "v1", "observed_at": 1},
	}
	if mut != nil {
		mut(m)
	}
	b, _ := json.Marshal(m)
	return b
}

// WithSession selects the source session of an envelope.
func WithSession(sess string) func(m map[string]any) {
	return func(m map[string]any) { m["session_id"] = sess }
}

// Stack is the real service over the real Rust worker, journal, executor and a fixture source application.
type Stack struct {
	T      *testing.T
	Svc    *api.Service
	Cat    *api.Catalog
	RT     *execution.Runtime
	App    *fixtureapp.App
	Dir    *Directory
	Clock  *Clock
	Rev    *Revocations
	Worker *execution.Worker
	Auth   *api.TokenAuth
	Doc    []byte
	Spec   *api.Spec
	Cfg    execution.Config
}

// Opts tunes the stack; the zero value is the ordinary one-contract, dedup-capable source.
type Opts struct {
	Docs     [][]byte
	Limiter  *api.CallLimiter
	Apply    time.Duration
	NoDedup  bool
	Receipts bool
	// WrapExecutor lets a test hold or observe the source calls of the one executor.
	WrapExecutor func(execution.Executor) execution.Executor
	// Delegations lets a person confirm for another caller.
	Delegations api.Delegations
	// WrapSessions lets a test put a layer, such as the sign-in overlay, in front of the directory's sessions.
	WrapSessions func(execution.Sessions) execution.Sessions
}

// New starts the worker and the source, and serves the fixture contract through the service.
func New(t *testing.T, o Opts) *Stack {
	t.Helper()
	w, err := execution.StartWorker(fabricPath(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	dedup := !o.NoDedup
	app := fixtureapp.New(dedup)
	t.Cleanup(app.Close)
	if o.Receipts {
		app.SetReceipts(true, true)
	}
	s := &Stack{T: t, App: app, Dir: NewDirectory(), Clock: NewClock(), Rev: NewRevocations(), Worker: w}
	s.Doc = ContractDoc(t, nil)
	docs := o.Docs
	if len(docs) == 0 {
		docs = [][]byte{s.Doc}
	}
	src, err := api.NewMemorySource(docs...)
	if err != nil {
		t.Fatal(err)
	}
	s.Cat, err = api.NewCatalog(api.CatalogConfig{Source: src, Validator: api.WorkerValidator{W: w}, Revocations: s.Rev})
	if err != nil {
		t.Fatal(err)
	}
	spec, serr := api.BuildSpec(s.Doc)
	if serr != nil {
		t.Fatal(serr)
	}
	s.Spec = &spec
	j, err := execution.OpenJournal(&execution.MemStore{})
	if err != nil {
		t.Fatal(err)
	}
	apply := o.Apply
	if apply == 0 {
		apply = 3 * time.Second
	}
	var sessions execution.Sessions = SessionView{s.Dir}
	if o.WrapSessions != nil {
		sessions = o.WrapSessions(sessions)
	}
	cfg := execution.APIConfig{
		BaseURL: app.URL(), ReadPath: "/api/orders/%s", WritePath: "/api/orders/%s/stage", Dedup: dedup,
		Client: &http.Client{Timeout: 2 * time.Second},
	}
	if o.Receipts {
		cfg.ReceiptPath, cfg.Fenced = "/api/receipts/%s", true
	}
	var ex execution.Executor = execution.NewAPIExecutor(cfg)
	if o.WrapExecutor != nil {
		ex = o.WrapExecutor(ex)
	}
	s.Cfg = execution.Config{
		Journal: j, Authorizer: w, Callers: s.Dir, Sessions: sessions, Revocations: s.Rev,
		Executors: map[string]execution.Executor{"provider_api_authorized": ex},
		Confirmer: execution.NewConfirmer([]byte("test-secret")), Clock: s.Clock, ApplyTimeout: apply,
		Retry: execution.RetryPolicy{MaxAttempts: 3, Base: 10 * time.Millisecond, Max: 50 * time.Millisecond},
	}
	s.RT, err = execution.NewRuntime(s.Cfg)
	if err != nil {
		t.Fatal(err)
	}
	if s.Auth, err = api.NewTokenAuth(s.Dir, s.Clock.Now, nil); err != nil {
		t.Fatal(err)
	}
	s.Svc, err = api.NewService(api.ServiceConfig{Catalog: s.Cat, Runtime: s.RT, Callers: s.Dir, Sessions: sessions, Limiter: o.Limiter,
		Confirmer: s.Cfg.Confirmer, Now: s.Clock.Now, Delegations: o.Delegations})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// Tokens is a stand-in authenticator for tests that do not exercise authentication.
type Tokens map[string]api.Principal

func (k Tokens) Authenticate(surface api.Surface, cred string) (api.Principal, *api.Error) {
	p, ok := k[cred]
	if !ok {
		return api.Principal{}, &api.Error{Code: api.CodeUnauthenticated, Message: api.InfoOf(api.CodeUnauthenticated).Summary, Retry: api.RetryFixRequest, Effect: api.EffectNone}
	}
	if p.Surface != surface {
		return api.Principal{}, &api.Error{Code: api.CodeWrongSurface, Message: api.InfoOf(api.CodeWrongSurface).Summary, Retry: api.RetryNever, Effect: api.EffectNone}
	}
	return p, nil
}

// REST serves the service over HTTP with the given authenticator.
func (s *Stack) REST(a api.Authenticator) *httptest.Server {
	h := api.NewREST(api.RESTConfig{Backend: s.Svc, Auth: a, OpenAPI: func(p api.Principal, ns string) (map[string]any, *api.Error) { return api.OpenAPI(s.Svc, p, ns) }})
	srv := httptest.NewServer(h)
	s.T.Cleanup(srv.Close)
	return srv
}

// Invoke runs the fixture operation in-process as p.
func (s *Stack) Invoke(p api.Principal, mut func(m map[string]any)) api.Result {
	s.T.Helper()
	return s.Svc.Invoke(context.Background(), p, Envelope(mut), api.Route{Operation: s.Spec.Name, Version: s.Spec.ContractVersion})
}

// Stage is the current stage of a source order.
func (s *Stack) Stage(id string) string { return s.App.Order(id).Stage }

// PrincipalOf is a principal as an authenticator would return it.
func PrincipalOf(caller string, surface api.Surface) api.Principal {
	return api.Principal{Caller: caller, Surface: surface, Credential: "cred-" + caller + "-" + string(surface)}
}

// Token issues a credential for one caller on one surface.
func (s *Stack) Token(caller string, surface api.Surface) string {
	s.T.Helper()
	tok, _, err := s.Auth.Issue(caller, surface, time.Hour, "test")
	if err != nil {
		s.T.Fatal(err)
	}
	return tok
}

// ResetOrder puts the source order back to its initial state.
func (s *Stack) ResetOrder(id string) { s.App.Put(fixtureapp.Order{ID: id, Stage: "new", Version: 1}) }

// Principal is the in-process principal of a caller on a surface, for measuring without the credential layer.
func (s *Stack) Principal(caller string, surface api.Surface) api.Principal {
	return api.Principal{Caller: caller, Surface: surface, Credential: "in-process"}
}
