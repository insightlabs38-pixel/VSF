//go:build integration

package interfaces

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/insightlabs38-pixel/vsf/backend/api"
	st "github.com/insightlabs38-pixel/vsf/backend/internal/stacktest"
)

func TestEverySurfaceAcceptsOnlyItsOwnCredential(t *testing.T) {
	s := st.New(t, st.Opts{})
	x := newSurfaces(t, s)
	toks := map[api.Surface]string{}
	for _, sf := range []api.Surface{api.SurfaceREST, api.SurfaceCLI, api.SurfaceMCP} {
		toks[sf] = s.Token(st.Ada, sf)
	}
	probe := map[api.Surface]func(tok string) bool{
		api.SurfaceREST: func(tok string) bool { return do(t, "GET", x.rest.URL+"/v1/namespaces", tok, "", nil).Status == 200 },
		api.SurfaceCLI: func(tok string) bool {
			c, _ := api.NewClient(x.rest.URL, tok, api.SurfaceCLI, nil)
			return runCLI(t, c, "namespaces").Code == 0
		},
		api.SurfaceMCP: func(tok string) bool {
			return (&mcpClient{t: t, url: x.mcp.URL, token: tok}).call("tools/list", nil).Status == 200
		},
	}
	for issued, tok := range toks {
		for used, f := range probe {
			if got := f(tok); got != (issued == used) {
				t.Errorf("a %s credential on the %s surface: accepted=%v", issued, used, got)
			}
		}
	}
	if s.App.Commits() != 0 {
		t.Fatal("authentication probes reached the source")
	}
}

func TestACallerCannotBorrowAnotherCallersSourceSessionOnAnySurface(t *testing.T) {
	s := st.New(t, st.Opts{})
	x := newSurfaces(t, s)
	borrowed := bodyMap(func(m map[string]any) { st.WithSession(st.SessBob)(m); m["request_id"] = "b1" })
	missing := bodyMap(func(m map[string]any) { st.WithSession("sess-nobody")(m); m["request_id"] = "b2" })
	runs := map[string]func(b map[string]any) api.Result{
		"rest": func(b map[string]any) api.Result {
			_, r := x.restResult(s.Token(st.Ada, api.SurfaceREST), marshal(b))
			return r
		},
		"cli": func(b map[string]any) api.Result { _, r := x.cliResult(s.Token(st.Ada, api.SurfaceCLI), b); return r },
		"mcp": func(b map[string]any) api.Result { _, r := x.mcpResult(s.Token(st.Ada, api.SurfaceMCP), b); return r },
	}
	for name, run := range runs {
		a, b := run(borrowed), run(missing)
		if a.Error == nil || a.Error.Code != api.CodeSessionInvalid {
			t.Fatalf("%s: %+v", name, a)
		}
		sameCore(t, name+": another caller's session and a missing one must look the same", a, b)
	}
	if s.App.Commits() != 0 || len(s.App.Requests()) != 0 {
		t.Fatalf("the source was reached: %v", s.App.Requests())
	}
}

func TestACallerCannotUseAnotherCallersRequestIDOrReadTheirInvocation(t *testing.T) {
	s := st.New(t, st.Opts{})
	x := newSurfaces(t, s)
	_, a := x.restResult(s.Token(st.Ada, api.SurfaceREST), st.Envelope(nil))
	if a.Category != api.CategorySucceeded {
		t.Fatalf("%+v", a)
	}
	bobREST := s.Token(st.Bob, api.SurfaceREST)
	_, b := x.restResult(bobREST, st.Envelope(st.WithSession(st.SessBob)))
	if b.Invocation == a.Invocation || b.Invocation == "" {
		t.Fatal("the same request id from another caller must be a different invocation, never Ada's result")
	}
	for _, path := range []string{"/v1/invocations/" + a.Invocation, "/v1/invocations/op_00000000000000000000000000000000"} {
		r := do(t, "GET", x.rest.URL+path, bobREST, "", nil)
		if r.Status != 404 || r.result(t).Error.Code != api.CodeInvocationNotFound {
			t.Fatalf("%s: %d %s", path, r.Status, r.Body)
		}
	}
	if r := do(t, "POST", x.rest.URL+"/v1/invocations/"+a.Invocation+"/cancel", bobREST, "application/json", []byte("{}")); r.Status != 404 {
		t.Fatalf("cancel: %d", r.Status)
	}
	rr := do(t, "POST", x.rest.URL+"/v1/invocations/"+a.Invocation+"/reconcile", bobREST, "application/json", marshal(map[string]any{"session_id": st.SessBob, "expect": map[string]any{"stage": "won"}}))
	if rr.Status != 404 {
		t.Fatalf("reconcile: %d", rr.Status)
	}
}

func TestRevocationExpiryAndLostScopeStopAccessAtOnce(t *testing.T) {
	s := st.New(t, st.Opts{})
	x := newSurfaces(t, s)
	rest := func(tok string) int { return do(t, "GET", x.rest.URL+"/v1/namespaces", tok, "", nil).Status }
	mcpTok := s.Token(st.Ada, api.SurfaceMCP)
	restTok := s.Token(st.Ada, api.SurfaceREST)
	tokens := s.Auth.List(st.Ada)
	for _, ti := range tokens {
		if ti.Surface == api.SurfaceREST {
			s.Auth.Revoke(ti.ID)
		}
	}
	if rest(restTok) != 401 {
		t.Fatal("a revoked credential must stop working at once")
	}
	if (&mcpClient{t: t, url: x.mcp.URL, token: mcpTok}).call("tools/list", nil).Status != 200 {
		t.Fatal("revoking one credential must not affect another surface's")
	}
	s.Dir.SetScopes(st.Ada, "records.read")
	m := &mcpClient{t: t, url: x.mcp.URL, token: mcpTok}
	for name := range m.tools() {
		if strings.HasPrefix(name, "op.") {
			t.Fatalf("an operation the caller lacks the scope for must not be discoverable: %s", name)
		}
	}
	r := m.call("tools/call", map[string]any{"name": s.Spec.Name, "arguments": bodyMap(nil)})
	if r.Error == nil || r.Error.Code != -32602 {
		t.Fatalf("it must be indistinguishable from an unknown tool: %s", r.Raw)
	}
	s.Dir.SetScopes(st.Ada, "records.read", "records.write")
	s.Clock.Advance(2 * time.Hour)
	if m.call("tools/list", nil).Status != 401 {
		t.Fatal("an expired credential must stop working")
	}
	fresh := s.Token(st.Ada, api.SurfaceMCP)
	s.Dir.Revoke(st.Ada)
	if (&mcpClient{t: t, url: x.mcp.URL, token: fresh}).call("tools/list", nil).Status != 401 {
		t.Fatal("a credential must not outlive its caller")
	}
	if s.App.Commits() != 0 {
		t.Fatal("a refused call reached the source")
	}
}

func TestNoSurfaceEchoesCredentialsOrSourceIdentity(t *testing.T) {
	s := st.New(t, st.Opts{})
	x := newSurfaces(t, s)
	toks := map[api.Surface]string{api.SurfaceREST: s.Token(st.Ada, api.SurfaceREST), api.SurfaceCLI: s.Token(st.Ada, api.SurfaceCLI), api.SurfaceMCP: s.Token(st.Ada, api.SurfaceMCP)}
	bobTok := s.Token(st.Bob, api.SurfaceREST)
	var outputs []string
	rr := do(t, "POST", x.rest.URL+invokePath(s.Spec, s.Spec.ContractVersion), toks[api.SurfaceREST], "application/json", st.Envelope(nil))
	outputs = append(outputs, string(rr.Body))
	bad := do(t, "POST", x.rest.URL+invokePath(s.Spec, s.Spec.ContractVersion), toks[api.SurfaceREST], "application/json", st.Envelope(func(m map[string]any) { m["request_id"] = "z"; m["params"].(map[string]any)["stage"] = "TOPSECRET" }))
	outputs = append(outputs, string(bad.Body))
	for _, p := range []string{"/v1/namespaces", "/v1/operations", "/v1/operations/" + s.Spec.Name, "/v1/openapi.json"} {
		outputs = append(outputs, string(do(t, "GET", x.rest.URL+p, toks[api.SurfaceREST], "", nil).Body))
	}
	c, _ := api.NewClient(x.rest.URL, toks[api.SurfaceCLI], api.SurfaceCLI, nil)
	outputs = append(outputs, runCLI(t, c, "ops", "describe", s.Spec.Name).Stdout, runCLI(t, c, "status", "op_ffffffffffffffffffffffffffffffff").Stdout)
	m := &mcpClient{t: t, url: x.mcp.URL, token: toks[api.SurfaceMCP]}
	outputs = append(outputs, string(m.call("tools/list", nil).Raw), string(m.call("resources/list", nil).Raw),
		string(m.call("tools/call", map[string]any{"name": s.Spec.Name, "arguments": bodyMap(func(b map[string]any) { b["request_id"] = "q2" })}).Raw))
	secrets := []string{bobTok, "caller-bob", st.SessBob, "prin_b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2", "var_a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1",
		strings.Repeat("3a", 32), strings.Repeat("c4", 32), "rt_ext01", "test-secret", "TOPSECRET"}
	for _, tk := range toks {
		secrets = append(secrets, tk, strings.SplitN(tk, "_", 3)[2])
	}
	for i, out := range outputs {
		for _, sec := range secrets {
			if strings.Contains(out, sec) {
				t.Errorf("output %d contains %q:\n%.300s", i, sec[:min(len(sec), 8)], out)
			}
		}
	}
}

func TestAConfirmationBelongsToTheCallerAndSessionItWasGivenFor(t *testing.T) {
	s := st.New(t, st.Opts{})
	x := newSurfaces(t, s)
	n := 0
	confirmed := func(rec string) map[string]any {
		return bodyMap(func(m map[string]any) {
			n++
			m["request_id"] = "conf-" + string(rune('a'+n))
			m["record"] = rec
			m["params"].(map[string]any)["record_id"] = rec
			m["seen"] = map[string]any{"version": "v1", "observed_at": 1}
		})
	}
	uiTok := s.Token(st.Ada, api.SurfaceUI)
	ui, _ := api.NewClient(x.rest.URL, uiTok, api.SurfaceUI, nil)
	body := marshal(confirmed("o2"))
	route := api.Route{Operation: s.Spec.Name, Version: s.Spec.ContractVersion}
	restTok, bobTok := s.Token(st.Ada, api.SurfaceREST), s.Token(st.Bob, api.SurfaceREST)

	if _, r := x.restResult(restTok, body); r.Error == nil || r.Error.Code != api.CodeConfirmationRequired {
		t.Fatalf("a write the source wants confirmed must say so: %+v", r)
	}
	agent, _ := api.NewClient(x.rest.URL, restTok, api.SurfaceREST, nil)
	if _, e := agent.Confirm(t.Context(), route, body); e == nil {
		t.Fatal("an agent surface must not be able to confirm for itself")
	}
	conf, e := ui.Confirm(t.Context(), route, body)
	if e != nil {
		t.Fatal(e)
	}
	withConf := func(session string) map[string]any {
		m := confirmed("o2")
		m["session_id"], m["confirmation"] = session, conf
		return m
	}
	bobBody := confirmed("o2")
	bobBody["session_id"], bobBody["confirmation"] = st.SessBob, conf
	if _, r := x.restResult(bobTok, marshal(bobBody)); r.Error == nil || r.Error.Code != api.CodeConfirmationRequired {
		t.Fatalf("another caller must not be able to use Ada's confirmation: %+v", r)
	}
	if s.App.Commits() != 0 {
		t.Fatal("a borrowed confirmation reached the source")
	}
	changed := withConf(st.SessAda)
	changed["params"].(map[string]any)["stage"] = "proposal"
	changed["expect"] = map[string]any{"stage": "proposal"}
	if _, r := x.restResult(restTok, marshal(changed)); r.Error == nil || r.Error.Code != api.CodeConfirmationRequired {
		t.Fatalf("a confirmation must cover exactly the request it was given for: %+v", r)
	}
	ok := withConf(st.SessAda)
	if _, r := x.restResult(restTok, marshal(ok)); r.Category != api.CategorySucceeded {
		t.Fatalf("the caller's own confirmation, on another surface, must work: %+v", r)
	}
	replay := withConf(st.SessAda)
	s.ResetOrder("o2")
	s.App.Put(orderWithConfirmation("o2"))
	if _, r := x.restResult(restTok, marshal(replay)); r.Error == nil || r.Error.Code != api.CodeConfirmationRequired {
		t.Fatalf("a confirmation works once: %+v", r)
	}
	var raw map[string]any
	_ = json.Unmarshal(marshal(conf), &raw)
	if raw["by"] != st.Ada {
		t.Fatalf("%v", raw)
	}
}
