//go:build integration

package interfaces

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/insightlabs38-pixel/vsf/backend/api"
	"github.com/insightlabs38-pixel/vsf/backend/api/jsonschema"
	"github.com/insightlabs38-pixel/vsf/backend/internal/fixtureapp"
	st "github.com/insightlabs38-pixel/vsf/backend/internal/stacktest"
	"github.com/insightlabs38-pixel/vsf/backend/mcp"
)

// mcpClient is a minimal client written against the 2026-07-28 text: per-request metadata, mirrored headers, no handshake.
type mcpClient struct {
	t     *testing.T
	url   string
	token string
	id    int
}

type mcpReply struct {
	Status int
	Result map[string]any
	Error  *struct {
		Code    int            `json:"code"`
		Message string         `json:"message"`
		Data    map[string]any `json:"data"`
	}
	Raw []byte
}

func (s *mcpClient) call(method string, params map[string]any) mcpReply {
	s.t.Helper()
	s.id++
	if params == nil {
		params = map[string]any{}
	}
	params["_meta"] = map[string]any{
		"io.modelcontextprotocol/protocolVersion":    mcp.Version,
		"io.modelcontextprotocol/clientInfo":         map[string]any{"name": "vsf-test-client", "version": "1"},
		"io.modelcontextprotocol/clientCapabilities": map[string]any{},
	}
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": s.id, "method": method, "params": params})
	req, _ := http.NewRequest("POST", s.url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", mcp.Version)
	req.Header.Set("Mcp-Method", method)
	if n, ok := params["name"].(string); ok && method == "tools/call" {
		req.Header.Set("Mcp-Name", n)
	}
	if n, ok := params["uri"].(string); ok && method == "resources/read" {
		req.Header.Set("Mcp-Name", n)
	}
	if s.token != "" {
		req.Header.Set("Authorization", "Bearer "+s.token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var env struct {
		Result map[string]any `json:"result"`
		Error  *struct {
			Code    int            `json:"code"`
			Message string         `json:"message"`
			Data    map[string]any `json:"data"`
		} `json:"error"`
	}
	_ = json.Unmarshal(raw, &env)
	return mcpReply{Status: resp.StatusCode, Result: env.Result, Error: env.Error, Raw: raw}
}

func (s *mcpClient) tools() map[string]map[string]any {
	s.t.Helper()
	out := map[string]map[string]any{}
	cursor := ""
	for {
		p := map[string]any{}
		if cursor != "" {
			p["cursor"] = cursor
		}
		r := s.call("tools/list", p)
		if r.Status != 200 {
			s.t.Fatalf("%d %s", r.Status, r.Raw)
		}
		for _, raw := range r.Result["tools"].([]any) {
			t := raw.(map[string]any)
			out[t["name"].(string)] = t
		}
		next, _ := r.Result["nextCursor"].(string)
		if next == "" {
			return out
		}
		cursor = next
	}
}

func newMCP(t *testing.T, s *st.Stack, caller string) *mcpClient {
	t.Helper()
	p := st.PrincipalOf(caller, api.SurfaceMCP)
	srv := httptest.NewServer(mcp.New(mcp.Config{Backend: s.Svc, Auth: st.Tokens{p.Credential: p}}))
	t.Cleanup(srv.Close)
	return &mcpClient{t: t, url: srv.URL, token: p.Credential}
}

func mustValidate(t *testing.T, schema any, v any) {
	t.Helper()
	sc, err := jsonschema.Compile(schema)
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(bytes.NewReader(mustJSON(v)))
	dec.UseNumber()
	var inst any
	if err := dec.Decode(&inst); err != nil {
		t.Fatal(err)
	}
	if vs := sc.Validate(inst); len(vs) > 0 {
		t.Fatalf("schema violations: %v\n%s", vs, mustJSON(v))
	}
}

func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }

func updateArgsMCP(mut func(m map[string]any)) map[string]any {
	var m map[string]any
	_ = json.Unmarshal(st.Envelope(mut), &m)
	return m
}

func TestMCPRunsTheVerifiedOperationAndTheResultConformsToItsOutputSchema(t *testing.T) {
	s := st.New(t, st.Opts{})
	c := newMCP(t, s, st.Ada)
	tool, ok := c.tools()[s.Spec.Name]
	if !ok {
		t.Fatal("the operation is not listed as a tool")
	}
	args := updateArgsMCP(nil)
	mustValidate(t, tool["inputSchema"], args)
	r := c.call("tools/call", map[string]any{"name": s.Spec.Name, "arguments": args})
	if r.Status != 200 || r.Error != nil {
		t.Fatalf("%d %s", r.Status, r.Raw)
	}
	res := r.Result
	if res["resultType"] != "complete" || res["isError"] != false {
		t.Fatalf("%s", r.Raw)
	}
	mustValidate(t, tool["outputSchema"], res["structuredContent"])
	text := res["content"].([]any)[0].(map[string]any)["text"].(string)
	var fromText any
	if err := json.Unmarshal([]byte(text), &fromText); err != nil || !reflect.DeepEqual(fromText, res["structuredContent"]) {
		t.Fatal("the text block must carry the serialized structured content")
	}
	sc := res["structuredContent"].(map[string]any)
	if sc["category"] != "succeeded" || sc["effect"] != "applied" || s.Stage("o1") != "won" || s.App.Commits() != 1 {
		t.Fatalf("%v %s %d", sc, s.Stage("o1"), s.App.Commits())
	}
}

func TestMCPArgumentsAreValidatedByTheSameSchemaTheClientSaw(t *testing.T) {
	s := st.New(t, st.Opts{})
	c := newMCP(t, s, st.Ada)
	tool := c.tools()[s.Spec.Name]
	bad := updateArgsMCP(func(m map[string]any) { m["params"].(map[string]any)["stage"] = "lost" })
	sc, _ := jsonschema.Compile(tool["inputSchema"])
	var inst any
	_ = json.Unmarshal(mustJSON(bad), &inst)
	if len(sc.Validate(inst)) == 0 {
		t.Fatal("the published schema must reject the bad argument")
	}
	r := c.call("tools/call", map[string]any{"name": s.Spec.Name, "arguments": bad})
	sctx := r.Result["structuredContent"].(map[string]any)
	if r.Result["isError"] != true || sctx["error"].(map[string]any)["code"] != "invalid_argument" || sctx["effect"] != "none" {
		t.Fatalf("%s", r.Raw)
	}
	mustValidate(t, tool["outputSchema"], r.Result["structuredContent"])
	if s.App.Commits() != 0 || len(s.App.Requests()) != 0 {
		t.Fatal("a refused call reached the source")
	}
}

func TestMCPReportsAnUnknownCommitAsAnErrorThatPointsToReconciliation(t *testing.T) {
	s := st.New(t, st.Opts{NoDedup: true})
	c := newMCP(t, s, st.Ada)
	tool := c.tools()[s.Spec.Name]
	s.App.SetFaults(fixtureapp.Faults{DropAfterCommit: 1})
	r := c.call("tools/call", map[string]any{"name": s.Spec.Name, "arguments": updateArgsMCP(nil)})
	sc := r.Result["structuredContent"].(map[string]any)
	if r.Result["isError"] != true || sc["category"] != "unknown_commit" || sc["effect"] != "unknown" || sc["retry"] != "reconcile_first" {
		t.Fatalf("%s", r.Raw)
	}
	mustValidate(t, tool["outputSchema"], sc)
	if s.App.WriteAttempts() != 1 {
		t.Fatalf("an ambiguous write was retried: %d", s.App.WriteAttempts())
	}
	key := sc["invocation"].(string)
	st1 := c.call("tools/call", map[string]any{"name": "vsf.invocation.status", "arguments": map[string]any{"invocation": key}})
	if st1.Result["structuredContent"].(map[string]any)["category"] != "unknown_commit" {
		t.Fatalf("%s", st1.Raw)
	}
	rc := c.call("tools/call", map[string]any{"name": "vsf.invocation.reconcile", "arguments": map[string]any{
		"invocation": key, "session_id": st.SessAda, "expect": map[string]any{"stage": "won"}}})
	rsc := rc.Result["structuredContent"].(map[string]any)
	if rc.Result["isError"] != true || rsc["error"].(map[string]any)["code"] != "still_unknown" {
		t.Fatalf("a matching state alone must not settle the outcome: %s", rc.Raw)
	}
	mustValidate(t, c.tools()["vsf.invocation.reconcile"]["outputSchema"], rsc)
}

func TestMCPAnotherCallerCannotSeeOrUseMyInvocation(t *testing.T) {
	s := st.New(t, st.Opts{})
	ada, bob := newMCP(t, s, st.Ada), newMCP(t, s, st.Bob)
	r := ada.call("tools/call", map[string]any{"name": s.Spec.Name, "arguments": updateArgsMCP(nil)})
	key := r.Result["structuredContent"].(map[string]any)["invocation"].(string)
	for _, tool := range []string{"vsf.invocation.status", "vsf.invocation.cancel"} {
		d := bob.call("tools/call", map[string]any{"name": tool, "arguments": map[string]any{"invocation": key}})
		sc := d.Result["structuredContent"].(map[string]any)
		if sc["error"].(map[string]any)["code"] != "invocation_not_found" {
			t.Fatalf("%s: %s", tool, d.Raw)
		}
	}
	rd := bob.call("resources/read", map[string]any{"uri": "vsf://invocation/" + key})
	if rd.Error == nil || rd.Error.Code != -32602 {
		t.Fatalf("%s", rd.Raw)
	}
	steal := bob.call("tools/call", map[string]any{"name": s.Spec.Name, "arguments": updateArgsMCP(st.WithSession(st.SessAda))})
	if steal.Result["structuredContent"].(map[string]any)["error"].(map[string]any)["code"] != "source_session_invalid" {
		t.Fatalf("a caller must not run in another caller's source session: %s", steal.Raw)
	}
	if s.App.Commits() != 1 {
		t.Fatalf("commits %d", s.App.Commits())
	}
}

func TestMCPResourcesServeTheContractAndTheInvocationOutcome(t *testing.T) {
	s := st.New(t, st.Opts{})
	c := newMCP(t, s, st.Ada)
	l := c.call("resources/list", nil)
	items := l.Result["resources"].([]any)
	if len(items) != 1 {
		t.Fatalf("%s", l.Raw)
	}
	uri := items[0].(map[string]any)["uri"].(string)
	rd := c.call("resources/read", map[string]any{"uri": uri})
	var spec api.Spec
	text := rd.Result["contents"].([]any)[0].(map[string]any)["text"].(string)
	if err := json.Unmarshal([]byte(text), &spec); err != nil || spec.ContractDigest != s.Spec.ContractDigest || spec.Effect.EffectClass != "conditional_write" {
		t.Fatalf("%v %s", err, text)
	}
	r := c.call("tools/call", map[string]any{"name": s.Spec.Name, "arguments": updateArgsMCP(nil)})
	key := r.Result["structuredContent"].(map[string]any)["invocation"].(string)
	inv := c.call("resources/read", map[string]any{"uri": "vsf://invocation/" + key})
	var res api.Result
	itext := inv.Result["contents"].([]any)[0].(map[string]any)["text"].(string)
	if err := json.Unmarshal([]byte(itext), &res); err != nil || res.Category != api.CategorySucceeded || inv.Result["ttlMs"].(float64) != 0 {
		t.Fatalf("%v %s", err, inv.Raw)
	}
}

func TestMCPAnOlderContractVersionIsStillAddressableByPinNotByDefault(t *testing.T) {
	older := st.ContractDoc(t, func(b map[string]any) { b["contract_version"] = "1.2.0" })
	s := st.New(t, st.Opts{Docs: [][]byte{older, st.ContractDoc(t, nil)}})
	c := newMCP(t, s, st.Ada)
	tools := c.tools()
	n := 0
	for name := range tools {
		if strings.HasPrefix(name, "op.") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("two contract versions of one operation must be one tool, got %d", n)
	}
	d := c.call("tools/call", map[string]any{"name": "vsf.operation.versions", "arguments": map[string]any{"name": s.Spec.Name}})
	vs := d.Result["structuredContent"].(map[string]any)["versions"].([]any)
	if len(vs) != 2 || vs[0].(map[string]any)["version"] != "1.3.0" || vs[1].(map[string]any)["version"] != "1.2.0" {
		t.Fatalf("%s", d.Raw)
	}
	r := c.call("tools/call", map[string]any{"name": s.Spec.Name, "arguments": updateArgsMCP(func(m map[string]any) { m["contract_version"] = "1.2.0" })})
	if r.Result["structuredContent"].(map[string]any)["contract_version"] != "1.2.0" || r.Result["structuredContent"].(map[string]any)["category"] != "succeeded" {
		t.Fatalf("a pin to an older served version must run that version: %s", r.Raw)
	}
	r2 := c.call("tools/call", map[string]any{"name": s.Spec.Name, "arguments": updateArgsMCP(func(m map[string]any) { m["contract_version"] = "0.9.0"; m["request_id"] = "req-2" })})
	e := r2.Result["structuredContent"].(map[string]any)["error"].(map[string]any)
	if e["code"] != "contract_version_unsupported" {
		t.Fatalf("%s", r2.Raw)
	}
}

func TestMCPDistinctApplicationsNeverShareAToolName(t *testing.T) {
	other := st.ContractDoc(t, func(b map[string]any) {
		const app = "vendor-console:app.vendor-test"
		b["source"].(map[string]any)["app_id"] = app
		for _, w := range b["witnesses"].([]any) {
			w.(map[string]any)["applies_to"].(map[string]any)["app_id"] = app
		}
	})
	s := st.New(t, st.Opts{Docs: [][]byte{st.ContractDoc(t, nil), other}})
	c := newMCP(t, s, st.Ada)
	names := []string{}
	for name := range c.tools() {
		if strings.HasPrefix(name, "op.") {
			names = append(names, name)
		}
	}
	if len(names) != 2 || strings.EqualFold(names[0], names[1]) {
		t.Fatalf("%v", names)
	}
	for _, n := range names {
		app, op, ok := api.SplitName(n)
		if !ok || op != "app.records.update_stage" || (app != "vendor-console:app.vendor.test" && app != "vendor-console:app.vendor-test") {
			t.Fatalf("%s -> %q %q", n, app, op)
		}
	}
	r := c.call("tools/call", map[string]any{"name": names[0], "arguments": updateArgsMCP(nil)})
	sc := r.Result["structuredContent"].(map[string]any)
	if sc["operation"] != names[0] {
		t.Fatalf("a call must run the contract of the tool it named: %v", sc["operation"])
	}
}

func TestMCPLongRunningCallsFallBackToAHandleThatCanBePolled(t *testing.T) {
	s := st.New(t, st.Opts{})
	c := newMCP(t, s, st.Ada)
	s.App.SetFaults(fixtureapp.Faults{DelayBeforeCommit: 600 * 1e6})
	r := c.call("tools/call", map[string]any{"name": s.Spec.Name, "arguments": updateArgsMCP(func(m map[string]any) { m["wait_ms"] = 0 })})
	sc := r.Result["structuredContent"].(map[string]any)
	if sc["category"] != "in_progress" || r.Result["isError"] != false || sc["effect"] != "unknown" || sc["invocation"] == "" {
		t.Fatalf("%s", r.Raw)
	}
	key := sc["invocation"].(string)
	for i := 0; i < 100; i++ {
		d := c.call("tools/call", map[string]any{"name": "vsf.invocation.status", "arguments": map[string]any{"invocation": key}})
		cat := d.Result["structuredContent"].(map[string]any)["category"]
		if cat == "succeeded" {
			return
		}
		if cat != "in_progress" {
			t.Fatalf("%s", d.Raw)
		}
		waitMs(20)
	}
	t.Fatal("the run did not finish")
}
