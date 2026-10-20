package mcp_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/insightlabs38-pixel/vsf/backend/api"
	"github.com/insightlabs38-pixel/vsf/backend/api/jsonschema"
	st "github.com/insightlabs38-pixel/vsf/backend/internal/stacktest"
	"github.com/insightlabs38-pixel/vsf/backend/mcp"
)

type acceptAll struct{}

func (acceptAll) Valid([]byte) (bool, error) { return true, nil }

// catalogBackend serves a real catalog and a canned invocation outcome, so protocol rules can be tested without a worker.
type catalogBackend struct {
	cat    *api.Catalog
	result api.Result
	calls  []string
}

func (b *catalogBackend) Discovery() api.Discovery { return api.Discovery{Service: "vsf"} }
func (b *catalogBackend) Namespaces(api.Principal) []api.Namespace {
	return b.cat.Namespaces()
}
func (b *catalogBackend) List(_ api.Principal, q api.ListQuery) (api.Page, *api.Error) {
	q.Scopes = []string{"records.write", "records.read"}
	p, err := b.cat.List(q)
	if err != nil {
		return p, api.AsError(err)
	}
	return p, nil
}
func (b *catalogBackend) Describe(_ api.Principal, name, version string) (*api.Spec, *api.Error) {
	s, err := b.cat.Resolve(name, version)
	if err != nil {
		return nil, api.AsError(err)
	}
	return s, nil
}
func (b *catalogBackend) Versions(_ api.Principal, name string) ([]api.VersionInfo, *api.Error) {
	return b.cat.Versions(name), nil
}
func (b *catalogBackend) Invoke(_ context.Context, _ api.Principal, raw []byte, r api.Route) api.Result {
	b.calls = append(b.calls, r.Operation+" "+string(raw))
	if _, err := b.cat.Resolve(r.Operation, ""); err != nil {
		return api.RefusedFor(r, nil, api.AsError(err))
	}
	return b.result
}
func (b *catalogBackend) Status(api.Principal, string, bool) api.Result { return b.result }
func (b *catalogBackend) Cancel(api.Principal, string) api.Result       { return b.result }
func (b *catalogBackend) Reconcile(context.Context, api.Principal, string, []byte) api.Result {
	return b.result
}

var tokenRe = regexp.MustCompile(`^[A-Za-z0-9_.\-]{1,128}$`)

type rig struct {
	t   *testing.T
	be  *catalogBackend
	srv *httptest.Server
	tok string
}

func manyDocs(t testing.TB, n int) [][]byte {
	var docs [][]byte
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("app.records.op_%03d", i)
		docs = append(docs, st.ContractDoc(t, func(b map[string]any) { b["operation_id"] = id }))
	}
	return docs
}

func newRig(t *testing.T, docs [][]byte) *rig {
	t.Helper()
	src, err := api.NewMemorySource(docs...)
	if err != nil {
		t.Fatal(err)
	}
	cat, err := api.NewCatalog(api.CatalogConfig{Source: src, Validator: acceptAll{}})
	if err != nil {
		t.Fatal(err)
	}
	p := st.PrincipalOf(st.Ada, api.SurfaceMCP)
	be := &catalogBackend{cat: cat, result: api.Result{Operation: "x", Category: api.CategorySucceeded, Effect: api.EffectApplied, Retry: api.RetryNever}}
	srv := httptest.NewServer(mcp.New(mcp.Config{Backend: be, Auth: st.Tokens{p.Credential: p, "rest-token": st.PrincipalOf(st.Ada, api.SurfaceREST)}}))
	t.Cleanup(srv.Close)
	return &rig{t: t, be: be, srv: srv, tok: p.Credential}
}

type reply struct {
	Status int
	Header http.Header
	Raw    []byte
	Body   struct {
		ID     json.RawMessage `json:"id"`
		Result map[string]any  `json:"result"`
		Error  *struct {
			Code    int            `json:"code"`
			Message string         `json:"message"`
			Data    map[string]any `json:"data"`
		} `json:"error"`
	}
}

type call struct {
	method  string
	params  map[string]any
	meta    map[string]any
	headers map[string]string
	token   string
	noAuth  bool
	rawBody []byte
	verb    string
	ctype   string
}

func goodMeta() map[string]any {
	return map[string]any{
		"io.modelcontextprotocol/protocolVersion":    mcp.Version,
		"io.modelcontextprotocol/clientInfo":         map[string]any{"name": "test-client", "version": "1"},
		"io.modelcontextprotocol/clientCapabilities": map[string]any{},
	}
}

func (r *rig) do(c call) reply {
	r.t.Helper()
	body := c.rawBody
	if body == nil {
		params := map[string]any{}
		for k, v := range c.params {
			params[k] = v
		}
		meta := c.meta
		if meta == nil {
			meta = goodMeta()
		}
		params["_meta"] = meta
		body, _ = json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": c.method, "params": params})
	}
	verb := c.verb
	if verb == "" {
		verb = "POST"
	}
	req, _ := http.NewRequest(verb, r.srv.URL, bytes.NewReader(body))
	ctype := c.ctype
	if ctype == "" {
		ctype = "application/json"
	}
	req.Header.Set("Content-Type", ctype)
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", mcp.Version)
	req.Header.Set("Mcp-Method", c.method)
	if c.method == "tools/call" {
		if n, ok := c.params["name"].(string); ok {
			req.Header.Set("Mcp-Name", n)
		}
	}
	if c.method == "resources/read" {
		if n, ok := c.params["uri"].(string); ok {
			req.Header.Set("Mcp-Name", n)
		}
	}
	if !c.noAuth {
		tok := c.token
		if tok == "" {
			tok = r.tok
		}
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	for k, v := range c.headers {
		if v == "" {
			req.Header.Del(k)
		} else {
			req.Header.Set(k, v)
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		r.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out reply
	out.Status, out.Header = resp.StatusCode, resp.Header
	out.Raw, _ = io.ReadAll(resp.Body)
	if len(out.Raw) > 0 {
		_ = json.Unmarshal(out.Raw, &out.Body)
	}
	return out
}

func TestDiscoverIsUnauthenticatedCacheableAndNamesTheSupportedVersions(t *testing.T) {
	r := newRig(t, manyDocs(t, 1))
	d := r.do(call{method: "server/discover", noAuth: true})
	res := d.Body.Result
	if d.Status != 200 || res["resultType"] != "complete" || res["cacheScope"] != "public" || res["ttlMs"].(float64) < 0 {
		t.Fatalf("%d %s", d.Status, d.Raw)
	}
	if v := res["supportedVersions"].([]any); len(v) != 1 || v[0] != mcp.Version {
		t.Fatalf("%v", v)
	}
	caps := res["capabilities"].(map[string]any)
	if caps["tools"] == nil || caps["resources"] == nil || caps["extensions"] != nil {
		t.Fatalf("only what is implemented may be declared: %v", caps)
	}
	if res["_meta"].(map[string]any)["io.modelcontextprotocol/serverInfo"] == nil {
		t.Fatal("serverInfo missing")
	}
	if strings.Contains(string(d.Raw), "vendor-console") {
		t.Fatal("discovery must not reveal the catalog")
	}
}

func TestVersionNegotiationIsPerRequestAndRetryable(t *testing.T) {
	r := newRig(t, manyDocs(t, 1))
	meta := goodMeta()
	meta["io.modelcontextprotocol/protocolVersion"] = "1900-01-01"
	d := r.do(call{method: "server/discover", meta: meta, headers: map[string]string{"MCP-Protocol-Version": "1900-01-01"}, noAuth: true})
	if d.Status != 400 || d.Body.Error == nil || d.Body.Error.Code != -32022 {
		t.Fatalf("%d %s", d.Status, d.Raw)
	}
	data := d.Body.Error.Data
	sup := data["supported"].([]any)
	if data["requested"] != "1900-01-01" || len(sup) != 1 || sup[0] != mcp.Version {
		t.Fatalf("%v", data)
	}
	meta["io.modelcontextprotocol/protocolVersion"] = sup[0].(string)
	again := r.do(call{method: "server/discover", meta: meta, headers: map[string]string{"MCP-Protocol-Version": sup[0].(string)}, noAuth: true})
	if again.Status != 200 {
		t.Fatalf("a retry with a supported version must work: %d", again.Status)
	}
}

func TestLegacyInitializeIsRefusedWithTheSupportedVersions(t *testing.T) {
	r := newRig(t, manyDocs(t, 1))
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"old","version":"1"}}}`
	d := r.do(call{method: "initialize", rawBody: []byte(body), headers: map[string]string{"MCP-Protocol-Version": ""}, noAuth: true})
	if d.Status != 400 || d.Body.Error == nil || d.Body.Error.Code != -32022 || !strings.Contains(string(d.Raw), mcp.Version) {
		t.Fatalf("%d %s", d.Status, d.Raw)
	}
	if d.Header.Get("Mcp-Session-Id") != "" {
		t.Fatal("this server mints no sessions")
	}
}

func TestHeadersMustMatchTheBody(t *testing.T) {
	r := newRig(t, manyDocs(t, 1))
	name := "vsf.invocation.status"
	args := map[string]any{"name": name, "arguments": map[string]any{"invocation": "op_0123456789abcdef0123456789abcdef"}}
	cases := map[string]call{
		"version header missing":  {method: "tools/call", params: args, headers: map[string]string{"MCP-Protocol-Version": ""}},
		"version header differs":  {method: "tools/call", params: args, headers: map[string]string{"MCP-Protocol-Version": "2025-11-25"}},
		"method header missing":   {method: "tools/call", params: args, headers: map[string]string{"Mcp-Method": ""}},
		"method header differs":   {method: "tools/call", params: args, headers: map[string]string{"Mcp-Method": "tools/list"}},
		"name header missing":     {method: "tools/call", params: args, headers: map[string]string{"Mcp-Name": ""}},
		"name header differs":     {method: "tools/call", params: args, headers: map[string]string{"Mcp-Name": "vsf.invocation.cancel"}},
		"name header bad base64":  {method: "tools/call", params: args, headers: map[string]string{"Mcp-Name": "=?base64?***?="}},
		"name header wrong value": {method: "tools/call", params: args, headers: map[string]string{"Mcp-Name": "=?base64?" + base64.StdEncoding.EncodeToString([]byte("other")) + "?="}},
	}
	for n, c := range cases {
		t.Run(n, func(t *testing.T) {
			d := r.do(c)
			if d.Status != 400 || d.Body.Error == nil || d.Body.Error.Code != -32020 {
				t.Fatalf("%d %s", d.Status, d.Raw)
			}
		})
	}
	ok := r.do(call{method: "tools/call", params: args, headers: map[string]string{"Mcp-Name": "=?base64?" + base64.StdEncoding.EncodeToString([]byte(name)) + "?="}})
	if ok.Status != 200 {
		t.Fatalf("an encoded name that matches must be accepted: %d %s", ok.Status, ok.Raw)
	}
}

func TestPerRequestMetadataIsRequired(t *testing.T) {
	r := newRig(t, manyDocs(t, 1))
	for n, drop := range map[string]string{"version": "io.modelcontextprotocol/protocolVersion", "capabilities": "io.modelcontextprotocol/clientCapabilities"} {
		t.Run(n, func(t *testing.T) {
			m := goodMeta()
			delete(m, drop)
			d := r.do(call{method: "tools/list", meta: m})
			if d.Status != 400 || d.Body.Error == nil || d.Body.Error.Code != -32602 {
				t.Fatalf("%d %s", d.Status, d.Raw)
			}
		})
	}
	m := goodMeta()
	delete(m, "io.modelcontextprotocol/clientInfo")
	if d := r.do(call{method: "tools/list", meta: m}); d.Status != 200 {
		t.Fatalf("clientInfo is optional: %d", d.Status)
	}
}

func TestTransportRules(t *testing.T) {
	r := newRig(t, manyDocs(t, 1))
	if d := r.do(call{method: "tools/list", verb: "GET", rawBody: []byte{}}); d.Status != 405 || d.Header.Get("Allow") != "POST" {
		t.Fatalf("GET: %d", d.Status)
	}
	if d := r.do(call{method: "tools/list", verb: "DELETE", rawBody: []byte{}}); d.Status != 405 {
		t.Fatalf("DELETE: %d", d.Status)
	}
	if d := r.do(call{method: "tools/list", ctype: "text/plain"}); d.Status != 415 {
		t.Fatalf("content type: %d", d.Status)
	}
	if d := r.do(call{method: "tools/list", headers: map[string]string{"Origin": "https://evil.example"}}); d.Status != 403 {
		t.Fatalf("origin: %d", d.Status)
	}
	if d := r.do(call{method: "tools/list", headers: map[string]string{"Mcp-Session-Id": "abc"}}); d.Status != 200 || d.Header.Get("Mcp-Session-Id") != "" {
		t.Fatalf("a session id is ignored and never echoed: %d %q", d.Status, d.Header.Get("Mcp-Session-Id"))
	}
	n := r.do(call{method: "notifications/initialized", rawBody: []byte(`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{}}`)})
	if n.Status != 202 || len(n.Raw) != 0 {
		t.Fatalf("notification: %d %q", n.Status, n.Raw)
	}
	bad := map[string]string{
		"batch":        `[{"jsonrpc":"2.0","id":1,"method":"tools/list"}]`,
		"duplicate":    `{"jsonrpc":"2.0","id":1,"id":2,"method":"tools/list"}`,
		"null id":      `{"jsonrpc":"2.0","id":null,"method":"tools/list"}`,
		"float id":     `{"jsonrpc":"2.0","id":1.5,"method":"tools/list"}`,
		"bool id":      `{"jsonrpc":"2.0","id":true,"method":"tools/list"}`,
		"wrong rpc":    `{"jsonrpc":"1.0","id":1,"method":"tools/list"}`,
		"no method":    `{"jsonrpc":"2.0","id":1}`,
		"not json":     `hello`,
		"params array": `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":[]}`,
	}
	for n, body := range bad {
		t.Run(n, func(t *testing.T) {
			d := r.do(call{method: "tools/list", rawBody: []byte(body)})
			if d.Status != 400 || d.Body.Error == nil {
				t.Fatalf("%d %s", d.Status, d.Raw)
			}
		})
	}
	if d := r.do(call{method: "tools/list", rawBody: bytes.Repeat([]byte(" "), api.MaxRequestBytes+(70<<10))}); d.Status != 413 {
		t.Fatalf("oversize: %d", d.Status)
	}
	if d := r.do(call{method: "subscriptions/listen"}); d.Status != 404 || d.Body.Error.Code != -32601 {
		t.Fatalf("unsupported method: %d %s", d.Status, d.Raw)
	}
}

func TestCredentialsAreRequiredAndBoundToTheMCPSurface(t *testing.T) {
	r := newRig(t, manyDocs(t, 1))
	d := r.do(call{method: "tools/list", noAuth: true})
	if d.Status != 401 || d.Header.Get("WWW-Authenticate") == "" {
		t.Fatalf("%d", d.Status)
	}
	if d := r.do(call{method: "tools/list", token: "nonsense"}); d.Status != 401 {
		t.Fatalf("%d", d.Status)
	}
	if d := r.do(call{method: "tools/list", token: "rest-token"}); d.Status != 403 {
		t.Fatalf("a REST credential must not open the MCP surface: %d", d.Status)
	}
}

func TestToolsListConformsAndIsDeterministicAndCacheable(t *testing.T) {
	r := newRig(t, manyDocs(t, 7))
	first := r.do(call{method: "tools/list"})
	second := r.do(call{method: "tools/list"})
	if first.Status != 200 || !bytes.Equal(first.Raw, second.Raw) {
		t.Fatal("tools/list must be byte-identical across calls")
	}
	res := first.Body.Result
	if res["resultType"] != "complete" || res["cacheScope"] != "private" || res["ttlMs"].(float64) <= 0 {
		t.Fatalf("a list that varies by caller must be private and cacheable: %v", res)
	}
	tools := res["tools"].([]any)
	if len(tools) != 4+7 {
		t.Fatalf("%d tools", len(tools))
	}
	seen := map[string]bool{}
	prev := ""
	for i, raw := range tools {
		tool := raw.(map[string]any)
		name := tool["name"].(string)
		if !tokenRe.MatchString(name) || seen[strings.ToLower(name)] {
			t.Fatalf("bad or colliding tool name %q", name)
		}
		seen[strings.ToLower(name)] = true
		if i >= 4 {
			if prev != "" && name <= prev {
				t.Fatalf("operation tools out of order: %s after %s", name, prev)
			}
			prev = name
		}
		for _, k := range []string{"inputSchema", "outputSchema"} {
			sc, err := jsonschema.Compile(tool[k])
			if err != nil {
				t.Fatalf("%s %s: %v", name, k, err)
			}
			if tool[k].(map[string]any)["type"] != "object" {
				t.Fatalf("%s %s must be an object schema", name, k)
			}
			_ = sc
		}
		for _, h := range []string{"readOnlyHint", "destructiveHint", "idempotentHint", "openWorldHint"} {
			if _, ok := tool["annotations"].(map[string]any)[h].(bool); !ok {
				t.Fatalf("%s lacks annotation %s", name, h)
			}
		}
		if tool["description"] == "" {
			t.Fatalf("%s has no description", name)
		}
	}
	for _, fixed := range []string{"vsf.invocation.status", "vsf.invocation.cancel", "vsf.invocation.reconcile", "vsf.operation.versions"} {
		if !seen[fixed] {
			t.Fatalf("missing %s", fixed)
		}
	}
}

func TestToolsListPaginatesWithoutDuplicatesOrGaps(t *testing.T) {
	const n = 130
	r := newRig(t, manyDocs(t, n))
	var names []string
	cursor, pages := "", 0
	for {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		d := r.do(call{method: "tools/list", params: params})
		if d.Status != 200 {
			t.Fatalf("%d %s", d.Status, d.Raw)
		}
		pages++
		for _, raw := range d.Body.Result["tools"].([]any) {
			names = append(names, raw.(map[string]any)["name"].(string))
		}
		next, _ := d.Body.Result["nextCursor"].(string)
		if next == "" {
			break
		}
		cursor = next
		if pages > 20 {
			t.Fatal("pagination does not terminate")
		}
	}
	if pages < 2 || len(names) != n+4 {
		t.Fatalf("%d pages, %d tools", pages, len(names))
	}
	uniq := map[string]bool{}
	for _, nm := range names {
		if uniq[nm] {
			t.Fatalf("duplicate %s", nm)
		}
		uniq[nm] = true
	}
	for _, bad := range []string{"garbage", strings.Repeat("x", 500)} {
		d := r.do(call{method: "tools/list", params: map[string]any{"cursor": bad}})
		if d.Body.Error == nil || d.Body.Error.Code != -32602 {
			t.Fatalf("cursor %q: %s", bad[:5], d.Raw)
		}
	}
}

func TestListingDoesNotDecodeTheWholeCatalog(t *testing.T) {
	r := newRig(t, manyDocs(t, 400))
	d := r.do(call{method: "tools/list"})
	if d.Status != 200 {
		t.Fatal(d.Status)
	}
	if got := len(d.Body.Result["tools"].([]any)); got > 4+api.DefaultPageSize {
		t.Fatalf("a page must be bounded: %d", got)
	}
	if ev := r.be.cat.Evaluations(); ev > api.DefaultPageSize*4 {
		t.Fatalf("one page evaluated %d of 400 contracts", ev)
	}
}

func TestToolCallRefusesWhatIsNotAToolOrNotWellFormed(t *testing.T) {
	r := newRig(t, manyDocs(t, 2))
	cases := map[string]map[string]any{
		"unknown tool":        {"name": "nope", "arguments": map[string]any{}},
		"unknown op tool":     {"name": "op.nobody.app.records.x", "arguments": map[string]any{}},
		"extra call param":    {"name": "vsf.invocation.status", "arguments": map[string]any{"invocation": "x"}, "inputResponses": map[string]any{}},
		"extra tool arg":      {"name": "vsf.invocation.status", "arguments": map[string]any{"invocation": "x", "admin": true}},
		"arguments a list":    {"name": "vsf.invocation.status", "arguments": []any{}},
		"empty name":          {"name": "", "arguments": map[string]any{}},
		"numeric name":        {"name": 5, "arguments": map[string]any{}},
		"collision lookalike": {"name": "VSF.invocation.status", "arguments": map[string]any{"invocation": "x"}},
	}
	for n, p := range cases {
		t.Run(n, func(t *testing.T) {
			headers := map[string]string{}
			if s, ok := p["name"].(string); ok {
				headers["Mcp-Name"] = s
			} else {
				headers["Mcp-Name"] = "x"
			}
			d := r.do(call{method: "tools/call", params: p, headers: headers})
			if n == "empty name" || n == "numeric name" {
				if d.Body.Error == nil {
					t.Fatalf("%d %s", d.Status, d.Raw)
				}
				return
			}
			if d.Body.Error == nil || d.Body.Error.Code != -32602 {
				t.Fatalf("%d %s", d.Status, d.Raw)
			}
		})
	}
	if len(r.be.calls) != 1 || !strings.HasPrefix(r.be.calls[0], "op.nobody.") {
		t.Fatalf("only the unknown operation name may reach the backend, to be refused there: %v", r.be.calls)
	}
}

func TestResourcesExposeOperationContractsAndInvocationsByStableURI(t *testing.T) {
	r := newRig(t, manyDocs(t, 3))
	l := r.do(call{method: "resources/list"})
	items := l.Body.Result["resources"].([]any)
	if l.Status != 200 || len(items) != 3 || l.Body.Result["cacheScope"] != "private" {
		t.Fatalf("%d %s", l.Status, l.Raw)
	}
	uri := items[0].(map[string]any)["uri"].(string)
	if !strings.HasPrefix(uri, "vsf://operation/op.") || !strings.HasSuffix(uri, "/1.3.0") {
		t.Fatal(uri)
	}
	rd := r.do(call{method: "resources/read", params: map[string]any{"uri": uri}})
	contents := rd.Body.Result["contents"].([]any)
	if rd.Status != 200 || len(contents) != 1 {
		t.Fatalf("%d %s", rd.Status, rd.Raw)
	}
	var spec api.Spec
	if err := json.Unmarshal([]byte(contents[0].(map[string]any)["text"].(string)), &spec); err != nil || spec.Effect.EffectClass == "" || spec.ContractDigest == "" {
		t.Fatalf("%v %+v", err, spec)
	}
	for _, bad := range []string{"vsf://operation/op.nobody.app.records.x/1.3.0", "vsf://operation/" + spec.Name + "/9.9.9", "file:///etc/passwd", "vsf://operation/../../x/1.3.0", "https://example.com/"} {
		d := r.do(call{method: "resources/read", params: map[string]any{"uri": bad}})
		if d.Body.Error == nil || d.Body.Error.Code != -32602 || d.Body.Error.Data["uri"] != bad {
			t.Fatalf("%s: %s", bad, d.Raw)
		}
	}
	tl := r.do(call{method: "resources/templates/list"})
	if tl.Status != 200 || len(tl.Body.Result["resourceTemplates"].([]any)) != 2 {
		t.Fatalf("%s", tl.Raw)
	}
}
