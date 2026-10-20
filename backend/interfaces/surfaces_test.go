//go:build integration

package interfaces

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/insightlabs38-pixel/vsf/backend/api"
	"github.com/insightlabs38-pixel/vsf/backend/internal/fixtureapp"
	st "github.com/insightlabs38-pixel/vsf/backend/internal/stacktest"
	"github.com/insightlabs38-pixel/vsf/backend/mcp"
)

// surfaces serves one stack through REST (also carrying the CLI), and MCP, authenticated by real credentials.
type surfaces struct {
	t    *testing.T
	s    *st.Stack
	rest *httptest.Server
	mcp  *httptest.Server
}

func newSurfaces(t *testing.T, s *st.Stack) *surfaces {
	t.Helper()
	x := &surfaces{t: t, s: s, rest: s.REST(s.Auth)}
	x.mcp = httptest.NewServer(mcp.New(mcp.Config{Backend: s.Svc, Auth: s.Auth}))
	t.Cleanup(x.mcp.Close)
	return x
}

func (x *surfaces) restResult(token string, body []byte) (int, api.Result) {
	x.t.Helper()
	r := do(x.t, "POST", x.rest.URL+invokePath(x.s.Spec, x.s.Spec.ContractVersion), token, "application/json", body)
	var res api.Result
	_ = json.Unmarshal(r.Body, &res)
	return r.Status, res
}

func (x *surfaces) cliResult(token string, body map[string]any) (int, api.Result) {
	x.t.Helper()
	c, err := api.NewClient(x.rest.URL, token, api.SurfaceCLI, nil)
	if err != nil {
		x.t.Fatal(err)
	}
	args := []string{"run", x.s.Spec.Name, "--session", body["session_id"].(string), "--request-id", body["request_id"].(string)}
	if rec, ok := body["record"].(string); ok {
		args = append(args, "--record", rec)
	}
	for k, v := range body["params"].(map[string]any) {
		args = append(args, "--param", k+"="+v.(string))
	}
	if e, ok := body["expect"].(map[string]any); ok {
		for k, v := range e {
			args = append(args, "--expect", k+"="+v.(string))
		}
	}
	if sn, ok := body["seen"].(map[string]any); ok {
		args = append(args, "--seen-version", sn["version"].(string), "--seen-at", "1")
	}
	o := runCLI(x.t, c, args...)
	var res api.Result
	_ = json.Unmarshal([]byte(o.Stdout), &res)
	if res.Error == nil && res.Category == "" {
		var env struct {
			Error *api.Error `json:"error"`
		}
		_ = json.Unmarshal([]byte(o.Stdout), &env)
		res = api.Result{Category: api.CategoryRefused, Error: env.Error}
	}
	return o.Code, res
}

func (x *surfaces) mcpResult(token string, body map[string]any) (int, api.Result) {
	x.t.Helper()
	c := &mcpClient{t: x.t, url: x.mcp.URL, token: token}
	r := c.call("tools/call", map[string]any{"name": x.s.Spec.Name, "arguments": body})
	if r.Status == 200 && r.Result != nil {
		var res api.Result
		_ = json.Unmarshal(mustJSON(r.Result["structuredContent"]), &res)
		return r.Status, res
	}
	var code api.Code
	if r.Error != nil {
		if c, ok := r.Error.Data["code"].(string); ok {
			code = api.Code(c)
		}
	}
	return r.Status, api.Result{Category: api.CategoryRefused, Error: &api.Error{Code: code}}
}

func bodyMap(mut func(m map[string]any)) map[string]any {
	var m map[string]any
	_ = json.Unmarshal(st.Envelope(mut), &m)
	return m
}

// core is what must be identical on every surface for the same operation: never ids, timing or progress.
type core struct {
	Category api.Category
	Effect   api.Effect
	Retry    api.Retry
	Code     api.Code
	Reason   string
	State    string
	Digest   string
	Fields   map[string]string
	Version  string
}

func coreOf(r api.Result) core {
	c := core{Category: r.Category, Effect: r.Effect, Retry: r.Retry, State: r.State, Digest: r.ContractDigest}
	if r.Error != nil {
		c.Code, c.Reason = r.Error.Code, r.Error.Reason
	}
	if r.Observed != nil {
		c.Fields, c.Version = r.Observed.Fields, r.Observed.Version
	}
	return c
}

func sameCore(t *testing.T, label string, a, b api.Result) {
	t.Helper()
	if !reflect.DeepEqual(coreOf(a), coreOf(b)) {
		t.Fatalf("%s differ:\n%+v\n%+v", label, coreOf(a), coreOf(b))
	}
}

func marshal(v any) []byte { b, _ := json.Marshal(v); return b }

var _ = bytes.NewReader
var _ = http.StatusOK

func orderWithConfirmation(id string) fixtureapp.Order {
	return fixtureapp.Order{ID: id, Stage: "new", Version: 1, ConfirmationRequired: true}
}
