//go:build integration

package interfaces

import (
	"strings"
	"testing"

	"github.com/insightlabs38-pixel/vsf/backend/api"
	st "github.com/insightlabs38-pixel/vsf/backend/internal/stacktest"
)

// A contract the Rust validator rejects, one that is not active, and one that was never witnessed must not become an interface.
func TestNoSurfaceEmitsAnOperationThatIsNotVerifiedAndActive(t *testing.T) {
	good := st.ContractDoc(t, nil)
	docs := [][]byte{
		good,
		st.ContractDoc(t, func(b map[string]any) {
			b["operation_id"] = "app.records.understated"
			e := b["effect"].(map[string]any)
			e["class"], e["idempotency"] = "idempotent_write", "idempotent"
		}),
		st.ContractDoc(t, func(b map[string]any) { b["operation_id"] = "app.records.deprecated"; b["lifecycle"] = "deprecated" }),
		st.ContractDoc(t, func(b map[string]any) { b["operation_id"] = "app.records.proposed"; b["lifecycle"] = "proposed" }),
		st.ContractDoc(t, func(b map[string]any) {
			b["operation_id"] = "app.records.unwitnessed"
			for _, w := range b["witnesses"].([]any) {
				w.(map[string]any)["applies_to"].(map[string]any)["app_id"] = "other-console:app.other.test"
			}
		}),
	}
	s := st.New(t, st.Opts{Docs: docs})
	x := newSurfaces(t, s)
	hidden := []string{"understated", "deprecated", "proposed", "unwitnessed"}

	rest := s.Token(st.Ada, api.SurfaceREST)
	for _, path := range []string{"/v1/operations", "/v1/openapi.json"} {
		body := string(do(t, "GET", x.rest.URL+path, rest, "", nil).Body)
		if !strings.Contains(body, "update_stage") {
			t.Fatalf("%s: the verified operation is missing", path)
		}
		for _, h := range hidden {
			if strings.Contains(body, "app.records."+h) {
				t.Fatalf("%s lists the unverified %s", path, h)
			}
		}
	}
	mc := &mcpClient{t: t, url: x.mcp.URL, token: s.Token(st.Ada, api.SurfaceMCP)}
	n := 0
	for name := range mc.tools() {
		if strings.HasPrefix(name, "op.") {
			n++
			for _, h := range hidden {
				if strings.Contains(name, h) {
					t.Fatalf("MCP lists %s", name)
				}
			}
		}
	}
	if n != 1 {
		t.Fatalf("%d operation tools", n)
	}
	cli, _ := api.NewClient(x.rest.URL, s.Token(st.Ada, api.SurfaceCLI), api.SurfaceCLI, nil)
	if o := runCLI(t, cli, "ops", "list"); strings.Contains(o.Stdout, "understated") || !strings.Contains(o.Stdout, "update_stage") {
		t.Fatalf("%s", o.Stdout)
	}
	for _, h := range hidden {
		name := api.OperationName(st.TestApp, "app.records."+h)
		path := "/v1/operations/" + name + "/versions/1.3.0/invocations"
		r := do(t, "POST", x.rest.URL+path, rest, "application/json", st.Envelope(nil))
		if r.Status < 400 || r.result(t).Error == nil {
			t.Fatalf("%s ran", h)
		}
		if mr := mc.call("tools/call", map[string]any{"name": name, "arguments": bodyMap(nil)}); mr.Error == nil {
			t.Fatalf("MCP ran %s", h)
		}
		if o := runCLI(t, cli, "run", name, "--session", st.SessAda, "--param", "record_id=o1", "--param", "stage=won"); o.Code == 0 {
			t.Fatalf("the CLI ran %s", h)
		}
		vs := do(t, "GET", x.rest.URL+"/v1/operations/"+name+"/versions", rest, "", nil)
		if !strings.Contains(string(vs.Body), "withheld") {
			t.Fatalf("a withheld version must say why: %s", vs.Body)
		}
	}
	if s.App.Commits() != 0 || len(s.App.Requests()) != 0 {
		t.Fatalf("a withheld operation reached the source: %v", s.App.Requests())
	}
}
