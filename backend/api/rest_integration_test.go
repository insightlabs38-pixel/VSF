//go:build integration

package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type httpResp struct {
	Status int
	Header http.Header
	Body   []byte
}

func (r httpResp) result(t testing.TB) Result {
	t.Helper()
	var res Result
	if err := json.Unmarshal(r.Body, &res); err != nil {
		t.Fatalf("not a result: %s", r.Body)
	}
	return res
}

func (r httpResp) errorCode(t testing.TB) Code {
	t.Helper()
	var env struct {
		Error *Error `json:"error"`
	}
	if err := json.Unmarshal(r.Body, &env); err != nil || env.Error == nil {
		t.Fatalf("not an error envelope: %s", r.Body)
	}
	return env.Error.Code
}

func do(t testing.TB, method, url, token, ctype string, body []byte) httpResp {
	t.Helper()
	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return httpResp{resp.StatusCode, resp.Header, b}
}

func invokePath(s *Spec, version string) string {
	return "/v1/operations/" + s.Name + "/versions/" + version + "/invocations"
}

func TestRESTRunsTheVerifiedOperationOnItsExplicitContractRoute(t *testing.T) {
	s := newStack(t, stackOpts{})
	p := principalOf(ada, SurfaceREST)
	srv := s.rest(tokens{p.Credential: p})
	r := do(t, "POST", srv.URL+invokePath(s.spec, s.spec.ContractVersion), p.Credential, "application/json", envelope(withSession(sessAda)))
	if r.Status != 200 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	res := r.result(t)
	if res.Category != CategorySucceeded || res.Effect != EffectApplied || res.Error != nil || res.ContractDigest != s.spec.ContractDigest {
		t.Fatalf("%+v", res)
	}
	if res.Observed == nil || res.Observed.Fields["stage"] != "won" {
		t.Fatalf("the verified read-back must be reported: %+v", res.Observed)
	}
	if s.orderStage("o1") != "won" || s.app.Commits() != 1 {
		t.Fatalf("source not changed once: %s %d", s.orderStage("o1"), s.app.Commits())
	}
	if loc := r.Header.Get("Location"); loc != "/v1/invocations/"+res.Invocation {
		t.Fatalf("location %q", loc)
	}
	if r.Header.Get("Cache-Control") != "no-store" || r.Header.Get("X-VSF-API-Version") != "v1" {
		t.Fatalf("headers %v", r.Header)
	}
	got := do(t, "GET", srv.URL+"/v1/invocations/"+res.Invocation, p.Credential, "", nil)
	if got.Status != 200 || got.result(t).Category != CategorySucceeded {
		t.Fatalf("%d %s", got.Status, got.Body)
	}
}

func TestRESTRefusesWhatTheContractRoutingCannotHonour(t *testing.T) {
	s := newStack(t, stackOpts{})
	p := principalOf(ada, SurfaceREST)
	srv := s.rest(tokens{p.Credential: p})
	body := envelope(withSession(sessAda))
	cases := []struct {
		name   string
		path   string
		body   []byte
		ctype  string
		status int
		code   Code
	}{
		{"an unserved contract version", invokePath(s.spec, "9.9.9"), body, "application/json", 404, CodeContractVersionUnsupported},
		{"an older served-looking version", invokePath(s.spec, "1.0.0"), body, "application/json", 404, CodeContractVersionUnsupported},
		{"an unknown operation", "/v1/operations/op.nope.x/versions/1.3.0/invocations", body, "application/json", 404, CodeOperationNotFound},
		{"a contract digest that is not the served one", invokePath(s.spec, "1.3.0"), envelope(func(m map[string]any) { withSession(sessAda)(m); m["contract_digest"] = "sha256:00" }), "application/json", 409, CodeContractPinMismatch},
		{"a body version that contradicts the route", invokePath(s.spec, "1.3.0"), envelope(func(m map[string]any) { withSession(sessAda)(m); m["contract_version"] = "1.2.0" }), "application/json", 400, CodeInvalidRequest},
		{"a missing argument", invokePath(s.spec, "1.3.0"), envelope(func(m map[string]any) { withSession(sessAda)(m); delete(m["params"].(map[string]any), "stage") }), "application/json", 400, CodeInvalidArgument},
		{"a form body", invokePath(s.spec, "1.3.0"), []byte("a=b"), "application/x-www-form-urlencoded", 400, CodeInvalidRequest},
		{"duplicate keys", invokePath(s.spec, "1.3.0"), []byte(`{"session_id":"sess-ada","session_id":"sess-bob","params":{}}`), "application/json", 400, CodeInvalidRequest},
		{"a body over the limit", invokePath(s.spec, "1.3.0"), bytes.Repeat([]byte(" "), MaxRequestBytes+10), "application/json", 413, CodeTooLarge},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := do(t, "POST", srv.URL+c.path, p.Credential, c.ctype, c.body)
			if r.Status != c.status {
				t.Fatalf("status %d, want %d: %s", r.Status, c.status, r.Body)
			}
			code := Code("")
			if strings.Contains(string(r.Body), `"category"`) {
				code = r.result(t).Error.Code
			} else {
				code = r.errorCode(t)
			}
			if code != c.code {
				t.Fatalf("code %s, want %s", code, c.code)
			}
		})
	}
	if s.app.Commits() != 0 || len(s.app.Requests()) != 0 {
		t.Fatalf("a refused request reached the source: %v", s.app.Requests())
	}
}

func TestRESTSupportedVersionsAreListedInTheRefusal(t *testing.T) {
	s := newStack(t, stackOpts{})
	p := principalOf(ada, SurfaceREST)
	srv := s.rest(tokens{p.Credential: p})
	r := do(t, "POST", srv.URL+invokePath(s.spec, "0.1.0"), p.Credential, "application/json", envelope(withSession(sessAda)))
	e := r.result(t).Error
	sup, _ := e.Details["supported"].([]any)
	if len(sup) != 1 || sup[0] != s.spec.ContractVersion || e.Retry != RetryFixRequest || e.Effect != EffectNone {
		t.Fatalf("%+v", e)
	}
}

func TestRESTNeedsACredentialAndRejectsForeignOrigins(t *testing.T) {
	s := newStack(t, stackOpts{})
	p := principalOf(ada, SurfaceREST)
	srv := s.rest(tokens{p.Credential: p})
	path := srv.URL + invokePath(s.spec, s.spec.ContractVersion)
	for name, tok := range map[string]string{"none": "", "unknown": "nope"} {
		r := do(t, "POST", path, tok, "application/json", envelope(withSession(sessAda)))
		if r.Status != 401 || r.Header.Get("WWW-Authenticate") == "" || r.errorCode(t) != CodeUnauthenticated {
			t.Fatalf("%s: %d %s", name, r.Status, r.Body)
		}
	}
	req, _ := http.NewRequest("GET", srv.URL+"/v1/operations", nil)
	req.Header.Set("Authorization", "Bearer "+p.Credential)
	req.Header.Set("Origin", "https://evil.example")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatalf("a cross-origin browser call must be refused: %d", resp.StatusCode)
	}
	if s.app.Commits() != 0 {
		t.Fatal("unauthenticated request reached the source")
	}
}

func TestOpenAPISpecifiesStableArgumentErrorAndEffectContracts(t *testing.T) {
	s := newStack(t, stackOpts{})
	p := principalOf(ada, SurfaceREST)
	doc, err := OpenAPI(s.svc, p, "")
	if err != nil {
		t.Fatal(err)
	}
	again, _ := OpenAPI(s.svc, p, "")
	a, _ := json.Marshal(doc)
	b, _ := json.Marshal(again)
	if !bytes.Equal(a, b) {
		t.Fatal("the document must be deterministic")
	}
	path := "/v1/operations/" + s.spec.Name + "/versions/" + s.spec.ContractVersion + "/invocations"
	op := doc["paths"].(map[string]any)[path].(map[string]any)["post"].(map[string]any)
	if op["x-vsf-effect"].(Disclosure).EffectClass != "conditional_write" || !strings.Contains(op["description"].(string), "unknown_commit") {
		t.Fatalf("effect disclosure missing: %v", op["x-vsf-effect"])
	}
	comps := doc["components"].(map[string]any)["schemas"].(map[string]any)
	in, _ := json.Marshal(comps[componentKey(s.spec, "Invocation")])
	want, _ := json.Marshal(s.spec.InputSchema)
	if !bytes.Equal(in, want) {
		t.Fatal("the documented argument schema must be the one enforced")
	}
	resp := op["responses"].(map[string]any)
	for _, c := range Codes() {
		if _, ok := resp[itoa(InfoOf(c).HTTP)]; !ok {
			t.Fatalf("status %d for %s is not documented", InfoOf(c).HTTP, c)
		}
	}
	table := doc["x-vsf-errors"].([]any)
	if len(table) != len(Codes()) {
		t.Fatalf("the error table lists %d of %d codes", len(table), len(Codes()))
	}
	if doc["openapi"] != "3.1.0" {
		t.Fatal(doc["openapi"])
	}
	for path := range doc["paths"].(map[string]any) {
		if strings.Contains(path, "/invocations") && strings.Contains(path, "versions") && !strings.Contains(path, s.spec.ContractVersion) && !strings.Contains(path, "{") {
			t.Fatalf("an invocation route without an explicit contract version: %s", path)
		}
	}
}

func TestOpenAPIServedOverRESTHidesWhatTheCallerMayNotRun(t *testing.T) {
	s := newStack(t, stackOpts{})
	s.dir.setScopes(bob, "records.read")
	pa, pb := principalOf(ada, SurfaceREST), principalOf(bob, SurfaceREST)
	srv := s.rest(tokens{pa.Credential: pa, pb.Credential: pb})
	has := func(tok string) bool {
		r := do(t, "GET", srv.URL+"/v1/openapi.json", tok, "", nil)
		if r.Status != 200 {
			t.Fatalf("%d %s", r.Status, r.Body)
		}
		return strings.Contains(string(r.Body), s.spec.Name+"/versions/")
	}
	if !has(pa.Credential) || has(pb.Credential) {
		t.Fatal("a caller without the required scope must not see the operation")
	}
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }
