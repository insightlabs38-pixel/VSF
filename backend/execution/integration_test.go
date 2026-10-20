//go:build integration

package execution

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/insightlabs38-pixel/vsf/backend/internal/fixtureapp"
)

var (
	sharedBrowser *Browser
	browserOnce   sync.Once
	browserErr    error
)

func TestMain(m *testing.M) {
	code := m.Run()
	if sharedBrowser != nil {
		_ = sharedBrowser.Close()
	}
	os.Exit(code)
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

func newWorker(t testing.TB) *Worker {
	t.Helper()
	w, err := StartWorker(fabricPath(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return w
}

func theBrowser(t testing.TB) *Browser {
	t.Helper()
	browserOnce.Do(func() {
		p := os.Getenv("VSF_CHROMIUM")
		if p == "" {
			browserErr = errors.New("set VSF_CHROMIUM to a Chromium executable")
			return
		}
		sharedBrowser, browserErr = LaunchBrowser(context.Background(), BrowserOptions{Path: p, NoSandbox: true})
	})
	if browserErr != nil {
		t.Fatalf("browser unavailable: %v", browserErr)
	}
	return sharedBrowser
}

func plan(app *fixtureapp.App) BrowserPlan {
	return BrowserPlan{
		BaseURL: app.URL(), PagePath: "/ui/orders/%s", VersionSel: "#stage", Fields: map[string]string{"stage": "#stage"},
		Inputs: map[string]string{"stage": "#next"}, Submit: "#save", Result: "#result", Timeout: 5 * time.Second,
	}
}

func apiFor(app *fixtureapp.App, dedup bool) *APIExecutor {
	return NewAPIExecutor(APIConfig{
		BaseURL: app.URL(), ReadPath: "/api/orders/%s", WritePath: "/api/orders/%s/stage", Dedup: dedup,
		Client: &http.Client{Timeout: 2 * time.Second},
	})
}

// liveRig wires the real Rust worker and both real executors to one fixture application.
func liveRig(t *testing.T, dedup bool, edit func(map[string]any)) (*rig, *fixtureapp.App) {
	t.Helper()
	app := fixtureapp.New(dedup)
	t.Cleanup(app.Close)
	r := newRig(t, edit)
	r.cfg.Authorizer = newWorker(t)
	r.cfg.Executors = map[string]Executor{
		"provider_api_authorized": apiFor(app, dedup),
		"verified_browser_plan":   NewBrowserExecutor(theBrowser(t), plan(app)),
	}
	r.cfg.ApplyTimeout = 3 * time.Second
	var err error
	if r.rt, err = NewRuntime(r.cfg); err != nil {
		t.Fatal(err)
	}
	return r, app
}

func TestRustAuthorizesTheWitnessedContractAndBothStrategiesCompleteIt(t *testing.T) {
	for _, strategy := range []string{"provider_api_authorized", "verified_browser_plan"} {
		t.Run(strategy, func(t *testing.T) {
			r, app := liveRig(t, true, nil)
			q := r.request("req-1")
			q.Strategy = strategy
			rec, err := r.run(q)
			if err != nil {
				t.Fatal(err)
			}
			mustState(t, rec, Verified, "verified")
			if got := app.Order("o1"); got.Stage != "won" || app.Commits() != 1 {
				t.Fatalf("order %+v commits %d", got, app.Commits())
			}
		})
	}
}

func TestRustRefusesContractsTheGoDecoderHappilyDecodes(t *testing.T) {
	cases := []struct {
		name   string
		reason string
		edit   func(map[string]any)
	}{
		{"effect understated", "invalid_contract", func(b map[string]any) {
			e := b["effect"].(map[string]any)
			e["class"], e["idempotency"] = "idempotent_write", "idempotent"
		}},
		{"no witness covers the source", "invalid_contract", func(b map[string]any) {
			for _, w := range b["witnesses"].([]any) {
				w.(map[string]any)["applies_to"].(map[string]any)["app_id"] = "other-console:app.other.test"
			}
		}},
		{"write without verification", "invalid_contract", func(b map[string]any) {
			b["verification"] = map[string]any{"required": false, "strategy": "none", "suite": "update_stage_v2"}
		}},
		{"synthetic witness on an active contract", "invalid_contract", func(b map[string]any) {
			b["witnesses"].([]any)[0].(map[string]any)["synthetic"] = true
		}},
		{"not active", "not_executable", nil},
		{"user assertion only", "invalid_contract", func(b map[string]any) {
			b["witnesses"].([]any)[0].(map[string]any)["kind"] = "user_assertion"
		}},
	}
	cases[4].edit = func(b map[string]any) { b["lifecycle"] = "deprecated" }
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, app := liveRig(t, false, c.edit)
			h, err := r.rt.Submit(context.Background(), r.request("req-1"))
			var ref *Refusal
			if !errors.As(err, &ref) || ref.Reason != c.reason {
				t.Fatalf("err %v", err)
			}
			if h.State() != Failed {
				t.Fatalf("state %s", h.State())
			}
			if len(app.Requests()) != 0 || r.ex.applyCount() != 0 {
				t.Fatalf("the source was contacted: %v", app.Requests())
			}
		})
	}
}

func TestRustRefusesAnotherEnvironmentAnExpiredWitnessAndARevokedOne(t *testing.T) {
	cases := []struct {
		name   string
		reason string
		setup  func(r *rig)
		edit   func(map[string]any)
	}{
		{"another runtime", "witness_other_environment", func(r *rig) {
			s := goodSession(sessionOne, userCaller)
			s.RuntimeID = "rt_other"
			r.dir.setSession(s)
		}, nil},
		{"source state changed", "witness_source_state_changed", func(r *rig) {
			s := goodSession(sessionOne, userCaller)
			s.SourceState = "sha256:" + string(make([]byte, 0)) + "9d9d9d9d9d9d9d9d9d9d9d9d9d9d9d9d9d9d9d9d9d9d9d9d9d9d9d9d9d9d9d9d"
			r.dir.setSession(s)
		}, nil},
		{"capabilities changed", "witness_capability_changed", func(r *rig) {
			s := goodSession(sessionOne, userCaller)
			s.CapabilityDigest = "sha256:" + "5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e"
			r.dir.setSession(s)
		}, nil},
		{"another principal", "witness_other_principal", func(r *rig) {
			s := goodSession(sessionOne, userCaller)
			s.PrincipalRef = "prin_c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3"
			r.dir.setSession(s)
		}, nil},
		{"witness expired", "witness_expired", func(r *rig) { r.clock.Advance(48 * time.Hour) }, nil},
		{"the only witness is revoked", "invalid_contract", nil, func(b map[string]any) {
			b["witnesses"].([]any)[0].(map[string]any)["revoked_at"] = 1792368350000
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, app := liveRig(t, false, c.edit)
			if c.setup != nil {
				c.setup(r)
			}
			_, err := r.rt.Submit(context.Background(), r.request("req-1"))
			var ref *Refusal
			if !errors.As(err, &ref) || ref.Reason != c.reason {
				t.Fatalf("err %v, want %s", err, c.reason)
			}
			if len(app.Requests()) != 0 {
				t.Fatal("the source was contacted")
			}
		})
	}
}

func TestOnlyTheWorkerCanMintAuthority(t *testing.T) {
	w := newWorker(t)
	doc := contractDoc(t, nil)
	env := Env{Now: t0Millis, RuntimeID: runtimeID, SourceState: sourceState, CapabilityDigest: capability, PrincipalRef: principal, VariantID: variantID}
	a, err := w.Authorize(doc, env)
	if err != nil || !a.valid() {
		t.Fatalf("%v %+v", err, a)
	}
	for _, bad := range [][]byte{nil, []byte("{}"), contractDoc(t, func(b map[string]any) { b["contract_version"] = "not-semver" })} {
		if _, err := w.Authorize(bad, env); err == nil {
			t.Fatal("an undecodable document was authorized")
		}
	}
}

type outcomeCase struct {
	name  string
	setup func(app *fixtureapp.App)
	call  Call
	want  string
}

func normalize(res Result, err error) string {
	if err != nil {
		if errors.Is(err, ErrNotSent) {
			return "unavailable"
		}
		return "unknown"
	}
	return string(res.Outcome)
}

func TestBrowserAndAPIExecutorsShareOneResultTaxonomy(t *testing.T) {
	call := func(stage, ifMatch string) Call {
		return Call{Key: "k", IfMatch: ifMatch, Target: Target{OperationID: "app.records.update_stage", Record: "o1", Params: map[string]any{"record_id": "o1", "stage": stage}}}
	}
	cases := []outcomeCase{
		{"committed", nil, call("won", "v1"), "committed"},
		{"stale version", nil, call("won", "v0"), "conflict"},
		{"source rejects the value", nil, call("bogus", "v1"), "rejected"},
		{"source throttles", func(a *fixtureapp.App) { a.SetFaults(fixtureapp.Faults{ThrottleNext: 1}) }, call("won", "v1"), "throttled"},
		{"disconnect after commit", func(a *fixtureapp.App) { a.SetFaults(fixtureapp.Faults{DropAfterCommit: 1}) }, call("won", "v1"), "unknown"},
		{"disconnect before commit", func(a *fixtureapp.App) { a.SetFaults(fixtureapp.Faults{DropBeforeCommit: 1}) }, call("won", "v1"), "unknown"},
		{"source unreachable", func(a *fixtureapp.App) { a.Close() }, call("won", "v1"), "unavailable"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			type outcome struct {
				got     string
				commits int
				stage   string
				retry   time.Duration
			}
			run := func(kind string) outcome {
				app := fixtureapp.New(false)
				defer app.Close()
				if c.setup != nil {
					c.setup(app)
				}
				var ex Executor = apiFor(app, false)
				if kind == "browser" {
					ex = NewBrowserExecutor(theBrowser(t), plan(app))
				}
				ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
				defer cancel()
				res, err := ex.Apply(ctx, c.call)
				o := outcome{got: normalize(res, err), commits: app.Commits(), stage: app.Order("o1").Stage, retry: res.RetryAfter}
				if res.Outcome == OutcomeCommitted && (res.Echo["stage"] != "won" || res.Echo["version"] != "v2") {
					t.Errorf("%s echo %v", kind, res.Echo)
				}
				return o
			}
			api, browser := run("api"), run("browser")
			if api.got != c.want || browser.got != c.want {
				t.Fatalf("api %q browser %q, want %q", api.got, browser.got, c.want)
			}
			if api.commits != browser.commits || api.stage != browser.stage || api.retry != browser.retry {
				t.Fatalf("source state differs: api %+v browser %+v", api, browser)
			}
		})
	}
}

func TestBothExecutorsReadTheSameSnapshot(t *testing.T) {
	app := fixtureapp.New(false)
	defer app.Close()
	ctx := context.Background()
	a, err := apiFor(app, false).Read(ctx, Target{Record: "o1"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewBrowserExecutor(theBrowser(t), plan(app)).Read(ctx, Target{Record: "o1"})
	if err != nil {
		t.Fatal(err)
	}
	if a.Version != b.Version || a.Fields["stage"] != b.Fields["stage"] {
		t.Fatalf("api %+v browser %+v", a, b)
	}
}

func TestAmbiguousWriteNeverRetriesInEitherStrategy(t *testing.T) {
	for _, strategy := range []string{"provider_api_authorized", "verified_browser_plan"} {
		t.Run(strategy, func(t *testing.T) {
			r, app := liveRig(t, true, effectEdit("idempotent_write", "idempotent"))
			app.SetFaults(fixtureapp.Faults{DropAfterCommit: 1})
			q := r.request("req-1")
			q.Strategy = strategy
			rec, _ := r.run(q)
			mustState(t, rec, Unknown, ReasonUnknownCommit)
			if app.Commits() != 1 {
				t.Fatalf("commits %d", app.Commits())
			}
			if n := app.WriteAttempts(); n != 1 {
				t.Fatalf("%d submissions; an ambiguous write was resent", n)
			}
		})
	}
}

func TestADeniedCallerCannotMutateTheSource(t *testing.T) {
	for _, strategy := range []string{"provider_api_authorized", "verified_browser_plan"} {
		t.Run(strategy, func(t *testing.T) {
			r, app := liveRig(t, false, nil)
			for name, edit := range map[string]func(*Request){
				"unknown caller":           func(q *Request) { q.Caller = "intruder" },
				"another caller's session": func(q *Request) { q.SessionID = "sess-bob" },
				"caller without scope": func(q *Request) {
					r.dir.callers[userCaller] = Caller{ID: userCaller, Scopes: []string{"records.read"}}
				},
			} {
				q := r.request("req-" + name)
				q.Strategy = strategy
				edit(&q)
				if _, err := r.rt.Submit(context.Background(), q); err == nil {
					t.Errorf("%s: accepted", name)
				}
			}
			if app.Commits() != 0 || len(app.Requests()) != 0 {
				t.Fatalf("the source was touched: %v", app.Requests())
			}
		})
	}
}

func TestStaleBrowserScreenBlocksTheWriteAtTheSource(t *testing.T) {
	r, app := liveRig(t, false, nil)
	h, _ := r.rt.Submit(context.Background(), func() Request {
		q := r.request("req-1")
		q.Strategy = "verified_browser_plan"
		q.Seen = &Seen{Version: "v1", ObservedAt: r.clock.Now()}
		return q
	}())
	h.Wait(context.Background())
	app.Change("o1", "qualified")
	q := r.request("req-2")
	q.Strategy = "verified_browser_plan"
	q.Params["stage"] = "proposal"
	q.Expect = map[string]string{"stage": "proposal"}
	rec, _ := r.run(q)
	mustState(t, rec, Failed, "stale_read")
	if app.Order("o1").Stage != "qualified" {
		t.Fatal("the stale write landed")
	}
}

func TestABrowsersOwnResendCannotTurnACommittedWriteIntoAFailure(t *testing.T) {
	r, app := liveRig(t, false, nil)
	app.SetKeepAlive(true)
	app.SetFaults(fixtureapp.Faults{DropAfterCommit: 1})
	q := r.request("req-1")
	q.Strategy = "verified_browser_plan"
	rec, _ := r.run(q)
	if app.Commits() != 1 {
		t.Fatalf("commits %d", app.Commits())
	}
	if rec.State == Failed {
		t.Fatalf("a committed write was recorded as failed (%s)", rec.Reason)
	}
	if rec.State != Unknown && rec.State != Verified {
		t.Fatalf("state %s", rec.State)
	}
}

func TestTheBrowserOnlyPressesControlsAPersonCouldUse(t *testing.T) {
	var clicks int
	var mu sync.Mutex
	srv := http.NewServeMux()
	page := func(body string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<!doctype html><meta charset=utf-8><strong id="stage" data-version="v1">new</strong>
<select id="next"><option>won</option></select><p id="result" data-status=""></p>` + body))
		}
	}
	srv.HandleFunc("/hidden/{id}", page(`<button id="save" style="display:none" onclick="fetch('/click')">Save</button>`))
	srv.HandleFunc("/disabled/{id}", page(`<button id="save" disabled onclick="fetch('/click')">Save</button>`))
	srv.HandleFunc("/covered/{id}", page(`<button id="save" onclick="fetch('/click')">Save</button>
<div style="position:fixed;inset:0;background:#fff8" id="consent">Accept cookies</div>`))
	srv.HandleFunc("/open/{id}", page(`<button id="save" onclick="fetch('/click');document.getElementById('result').dataset.status='committed'">Save</button>`))
	srv.HandleFunc("/click", func(http.ResponseWriter, *http.Request) { mu.Lock(); clicks++; mu.Unlock() })
	ts := httptest.NewServer(srv)
	defer ts.Close()
	run := func(path string) (Result, error) {
		e := NewBrowserExecutor(theBrowser(t), BrowserPlan{
			BaseURL: ts.URL, PagePath: path, VersionSel: "#stage", Fields: map[string]string{"stage": "#stage"},
			Inputs: map[string]string{"stage": "#next"}, Submit: "#save", Result: "#result", Timeout: 2 * time.Second,
		})
		return e.Apply(context.Background(), Call{Target: Target{Record: "o1", Params: map[string]any{"stage": "won"}}})
	}
	for _, p := range []string{"/hidden/%s", "/disabled/%s", "/covered/%s"} {
		if _, err := run(p); !errors.Is(err, ErrNotSent) {
			t.Errorf("%s: err %v; an unusable control must be reported as not sent", p, err)
		}
	}
	mu.Lock()
	n := clicks
	mu.Unlock()
	if n != 0 {
		t.Fatalf("%d clicks reached controls a person could not press", n)
	}
	if res, err := run("/open/%s"); err != nil || res.Outcome != OutcomeCommitted {
		t.Fatalf("%+v %v", res, err)
	}
}
