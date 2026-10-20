//go:build integration

package interfaces

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/insightlabs38-pixel/vsf/backend/api"
	"github.com/insightlabs38-pixel/vsf/backend/execution"
	"github.com/insightlabs38-pixel/vsf/backend/internal/fixtureapp"
	st "github.com/insightlabs38-pixel/vsf/backend/internal/stacktest"
)

func eachSurface(t *testing.T, s *st.Stack, x *surfaces) []*ops { return x.all(st.Ada) }

// pinned runs the operation at an exact contract version on every surface.
func pinnedInvoke(x *surfaces, o *ops, version, digest string) api.Result {
	return o.invoke(bodyMap(func(m map[string]any) {
		m["request_id"] = o.requestID("pin")
		if version != "" {
			m["contract_version"] = version
		}
		if digest != "" {
			m["contract_digest"] = digest
		}
	}))
}

// invokeAt differs per surface in how the version is carried; the REST and UI routes carry it in the path.
func (x *surfaces) invokeAt(o *ops, version, digest string) api.Result {
	switch o.name {
	case "rest", "ui":
		body := bodyMap(func(m map[string]any) {
			m["request_id"] = o.requestID("pin")
			if digest != "" {
				m["contract_digest"] = digest
			}
		})
		if o.name == "rest" {
			path := "/v1/operations/" + x.s.Spec.Name + "/versions/" + version + "/invocations"
			r := do(x.t, "POST", x.rest.URL+path, x.s.Token(st.Ada, api.SurfaceREST), "application/json", marshal(body))
			return r.result(x.t)
		}
		c, _ := api.NewClient(x.rest.URL, x.s.Token(st.Ada, api.SurfaceUI), api.SurfaceUI, nil)
		return c.Invoke(x.t.Context(), api.Route{Operation: x.s.Spec.Name, Version: version}, marshal(body))
	case "cli":
		c, _ := api.NewClient(x.rest.URL, x.s.Token(st.Ada, api.SurfaceCLI), api.SurfaceCLI, nil)
		args := []string{"run", x.s.Spec.Name, "--session", st.SessAda, "--request-id", o.requestID("pin"), "--record", "o1",
			"--param", "record_id=o1", "--param", "stage=won", "--expect", "stage=won", "--seen-version", "v1", "--seen-at", "1", "--version", version}
		if digest != "" {
			args = append(args, "--digest", digest)
		}
		out := runCLI(x.t, c, args...)
		var res api.Result
		_ = jsonUnmarshal(out.Stdout, &res)
		if res.Category == "" {
			var env struct {
				Error *api.Error `json:"error"`
			}
			_ = jsonUnmarshal(out.Stdout, &env)
			res = api.Result{Category: api.CategoryRefused, Error: env.Error}
		}
		return res
	}
	return pinnedInvoke(x, o, version, digest)
}

func TestAnOlderClientPinIsHonouredRefusedOrRedirectedTheSameWayOnEverySurface(t *testing.T) {
	old := st.ContractDoc(t, func(b map[string]any) { b["contract_version"] = "1.2.0" })
	dep := st.ContractDoc(t, func(b map[string]any) { b["contract_version"] = "1.1.0"; b["lifecycle"] = "deprecated" })
	cur := st.ContractDoc(t, nil)
	revoked := specDigest(t, old)
	cases := []struct {
		name     string
		version  string
		digest   string
		setup    func(s *st.Stack)
		code     api.Code
		version2 string
	}{
		{"an older version still served runs that version", "1.2.0", "", nil, "", "1.2.0"},
		{"the newest version runs", "1.3.0", "", nil, "", "1.3.0"},
		{"a revoked version is never revived by a pin", "1.2.0", "", func(s *st.Stack) { s.Rev.Revoke(revoked, "security fix") }, api.CodeContractRevoked, ""},
		{"a deprecated version names what is served", "1.1.0", "", nil, api.CodeContractVersionUnsupported, ""},
		{"a version from the future", "7.0.0", "", nil, api.CodeContractVersionUnsupported, ""},
		{"a digest of another build", "1.3.0", "sha256:" + strings.Repeat("9", 64), nil, api.CodeContractPinMismatch, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			results := map[string]api.Result{}
			for _, name := range []string{"rest", "ui", "cli", "mcp"} {
				s := st.New(t, st.Opts{Docs: [][]byte{old, dep, cur}})
				x := newSurfaces(t, s)
				if c.setup != nil {
					c.setup(s)
				}
				var o *ops
				for _, k := range x.all(st.Ada) {
					if k.name == name {
						o = k
					}
				}
				r := x.invokeAt(o, c.version, c.digest)
				if c.code == "" {
					if r.Category != api.CategorySucceeded || r.ContractVersion != c.version2 {
						t.Fatalf("%s: %+v", name, r)
					}
				} else {
					if r.Error == nil || r.Error.Code != c.code || r.Effect != api.EffectNone {
						t.Fatalf("%s: %+v", name, r)
					}
					if c.code == api.CodeContractVersionUnsupported {
						sup, _ := r.Error.Details["supported"].([]any)
						if len(sup) == 0 {
							t.Fatalf("%s: an unsupported pin must list what is served: %+v", name, r.Error)
						}
					}
					if s.App.Commits() != 0 {
						t.Fatalf("%s: a refused pin changed the source", name)
					}
				}
				results[name] = r
			}
			base := results["rest"]
			for name, r := range results {
				if r.Category != base.Category || r.Effect != base.Effect || r.Retry != base.Retry || r.ContractVersion != base.ContractVersion ||
					(r.Error == nil) != (base.Error == nil) || (r.Error != nil && (r.Error.Code != base.Error.Code || r.Error.Reason != base.Error.Reason)) {
					t.Errorf("%s differs from rest: %+v vs %+v", name, r, base)
				}
			}
		})
	}
}

func TestMalformedArgumentsAreTheSameTypedErrorOnEverySurface(t *testing.T) {
	bad := map[string]func(m map[string]any){
		"unknown param":    func(m map[string]any) { m["params"].(map[string]any)["extra"] = "x" },
		"wrong choice":     func(m map[string]any) { m["params"].(map[string]any)["stage"] = "lost" },
		"missing param":    func(m map[string]any) { delete(m["params"].(map[string]any), "stage") },
		"unknown envelope": func(m map[string]any) { m["admin"] = "yes" },
		"bad request id":   func(m map[string]any) { m["request_id"] = "has space" },
	}
	for n, mut := range bad {
		t.Run(n, func(t *testing.T) {
			s := st.New(t, st.Opts{})
			x := newSurfaces(t, s)
			for _, o := range x.all(st.Ada) {
				if o.name == "cli" {
					continue
				}
				r := o.invoke(bodyMap(func(m map[string]any) { m["request_id"] = o.requestID("bad"); mut(m) }))
				if r.Error == nil || r.Error.Code != api.CodeInvalidArgument || r.Category != api.CategoryRefused || r.Effect != api.EffectNone || r.Retry != api.RetryFixRequest {
					t.Fatalf("%s: %+v", o.name, r)
				}
				if _, ok := r.Error.Details["violations"]; !ok {
					t.Fatalf("%s: the violations must be reported", o.name)
				}
			}
			if s.App.Commits() != 0 || len(s.App.Requests()) != 0 {
				t.Fatal("a malformed call reached the source")
			}
		})
	}
}

// gate holds the first source read so a test can change the world between admission and the effect.
type gate struct {
	execution.Executor
	once    sync.Once
	reached chan struct{}
	release chan struct{}
}

func newGate(next execution.Executor) *gate {
	return &gate{Executor: next, reached: make(chan struct{}), release: make(chan struct{})}
}

func (g *gate) Read(ctx context.Context, t execution.Target) (execution.Snapshot, error) {
	g.once.Do(func() { close(g.reached); <-g.release })
	return g.Executor.Read(ctx, t)
}

func (g *gate) Reconcile(ctx context.Context, p execution.Probe) (execution.Evidence, error) {
	if r, ok := g.Executor.(execution.Reconciler); ok {
		return r.Reconcile(ctx, p)
	}
	return execution.Evidence{Level: execution.Inconclusive, Basis: "none"}, nil
}

func TestARoleRevokedWhileAWriteWaitsStopsItBeforeTheSourceIsTouched(t *testing.T) {
	cases := []struct {
		name   string
		revoke func(s *st.Stack)
		code   api.Code
	}{
		{"source session revoked", func(s *st.Stack) {
			sess := st.GoodSession(st.SessAda, st.Ada)
			sess.Revoked = true
			s.Dir.SetSession(sess)
		}, api.CodeSessionInvalid},
		{"source role lost", func(s *st.Stack) {
			sess := st.GoodSession(st.SessAda, st.Ada)
			sess.SourceScopes = []string{"records.read"}
			s.Dir.SetSession(sess)
		}, api.CodePermissionDenied},
		{"caller role lost", func(s *st.Stack) { s.Dir.SetScopes(st.Ada, "records.read") }, api.CodePermissionDenied},
		{"caller removed", func(s *st.Stack) { s.Dir.Revoke(st.Ada) }, api.CodePermissionDenied},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var g *gate
			s := st.New(t, st.Opts{WrapExecutor: func(e execution.Executor) execution.Executor { g = newGate(e); return g }})
			x := newSurfaces(t, s)
			restTok := s.Token(st.Ada, api.SurfaceREST)
			mcpTok := s.Token(st.Ada, api.SurfaceMCP)
			_, r := x.restResult(restTok, st.Envelope(func(m map[string]any) { m["wait_ms"] = 0 }))
			if r.Category != api.CategoryInProgress {
				t.Fatalf("%+v", r)
			}
			<-g.reached
			c.revoke(s)
			close(g.release)
			var final api.Result
			for i := 0; i < 200; i++ {
				mc := &mcpClient{t: t, url: x.mcp.URL, token: mcpTok}
				if c.name == "caller removed" {
					break
				}
				out := mc.call("tools/call", map[string]any{"name": "vsf.invocation.status", "arguments": map[string]any{"invocation": r.Invocation}})
				final = decodeStructured(out)
				if final.Category != api.CategoryInProgress {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if c.name == "caller removed" {
				deadline := time.Now().Add(3 * time.Second)
				for time.Now().Before(deadline) {
					if h, ok := s.RT.Handle(r.Invocation); ok && h.State().Terminal() {
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
				rec, _ := s.RT.Get(r.Invocation)
				if rec.State != execution.Failed {
					t.Fatalf("state %s/%s", rec.State, rec.Reason)
				}
			} else if final.Error == nil || final.Error.Code != c.code || final.Effect != api.EffectNone {
				t.Fatalf("%+v", final)
			}
			if s.App.Commits() != 0 || s.App.WriteAttempts() != 0 {
				t.Fatalf("a write whose authority was revoked reached the source: %d", s.App.WriteAttempts())
			}
		})
	}
}

func TestALongRunWithoutAWaitReturnsAHandleEveryFollowUpSurfaceUnderstands(t *testing.T) {
	s := st.New(t, st.Opts{})
	x := newSurfaces(t, s)
	s.App.SetFaults(fixtureapp.Faults{DelayBeforeCommit: 500 * time.Millisecond})
	by := map[string]*ops{}
	for _, o := range x.all(st.Ada) {
		by[o.name] = o
	}
	r := by["rest"].write("long", func(m map[string]any) { m["wait_ms"] = 0 })
	if r.Category != api.CategoryInProgress || r.Effect != api.EffectUnknown || r.Retry != api.RetrySameRequest || r.Invocation == "" {
		t.Fatalf("%+v", r)
	}
	st1 := map[string]api.Result{}
	for name, o := range by {
		st1[name] = o.status(r.Invocation)
		if st1[name].Category != api.CategoryInProgress {
			t.Fatalf("%s: %+v", name, st1[name])
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	final := map[string]api.Result{}
	for name, o := range by {
		for time.Now().Before(deadline) {
			final[name] = o.status(r.Invocation)
			if final[name].Category != api.CategoryInProgress {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	allSame(t, "the finished long run", final)
	if final["mcp"].Category != api.CategorySucceeded || s.App.Commits() != 1 {
		t.Fatalf("%+v commits %d", final["mcp"], s.App.Commits())
	}
}

func TestCancellingARunInFlightNeverClaimsTheSourceWasLeftAlone(t *testing.T) {
	s := st.New(t, st.Opts{Receipts: true})
	x := newSurfaces(t, s)
	s.App.SetFaults(fixtureapp.Faults{DelayBeforeCommit: 600 * time.Millisecond})
	var o *ops
	for _, k := range x.all(st.Ada) {
		if k.name == "rest" {
			o = k
		}
	}
	r := o.write("cancel", func(m map[string]any) { m["wait_ms"] = 0 })
	time.Sleep(150 * time.Millisecond)
	c := o.cancel(r.Invocation)
	if c.Cancel != "requested" && c.Cancel != "effect_unknown" && c.Cancel != "cancelled_before_effect" {
		t.Fatalf("%+v", c)
	}
	var final api.Result
	for i := 0; i < 300; i++ {
		final = o.status(r.Invocation)
		if final.Category != api.CategoryInProgress {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	switch final.Category {
	case api.CategoryUnknownCommit:
		if final.Effect != api.EffectUnknown || final.Retry != api.RetryReconcile {
			t.Fatalf("%+v", final)
		}
	case api.CategoryFailed:
		if final.Error.Code != api.CodeCancelled || final.Effect != api.EffectNone || s.App.Commits() != 0 {
			t.Fatalf("%+v commits %d", final, s.App.Commits())
		}
	default:
		t.Fatalf("a cancelled write must end unknown or provably unapplied, not %+v", final)
	}
}

func TestEveryErrorAnySurfaceReturnsIsInTheSharedTaxonomy(t *testing.T) {
	s := st.New(t, st.Opts{})
	x := newSurfaces(t, s)
	known := map[api.Code]bool{}
	for _, c := range api.Codes() {
		known[c] = true
	}
	probes := []func(o *ops) api.Result{
		func(o *ops) api.Result { return o.write("p1", func(m map[string]any) { delete(m, "seen") }) },
		func(o *ops) api.Result { return o.write("p2", func(m map[string]any) { m["session_id"] = "nobody" }) },
		func(o *ops) api.Result {
			return o.write("p3", func(m map[string]any) { m["params"] = map[string]any{} })
		},
		func(o *ops) api.Result { return o.status("op_00000000000000000000000000000000") },
		func(o *ops) api.Result { return o.cancel("op_00000000000000000000000000000000") },
		func(o *ops) api.Result {
			return o.reconcile("op_00000000000000000000000000000000", st.SessAda, map[string]string{"stage": "won"})
		},
	}
	for _, o := range x.all(st.Ada) {
		for i, p := range probes {
			if o.name == "cli" && i == 2 {
				continue
			}
			r := p(o)
			if r.Error == nil || !known[r.Error.Code] || r.Error.Message != api.InfoOf(r.Error.Code).Summary {
				t.Fatalf("%s probe %d: %+v", o.name, i, r.Error)
			}
			if r.Error.Effect != api.InfoOf(r.Error.Code).Effect || r.Error.Retry != api.InfoOf(r.Error.Code).Retry {
				t.Fatalf("%s probe %d: the typed error disagrees with the taxonomy: %+v", o.name, i, r.Error)
			}
		}
	}
}
