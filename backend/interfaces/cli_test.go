//go:build integration

package interfaces

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/insightlabs38-pixel/vsf/backend/api"
	"github.com/insightlabs38-pixel/vsf/backend/cli"
	"github.com/insightlabs38-pixel/vsf/backend/internal/fixtureapp"
	st "github.com/insightlabs38-pixel/vsf/backend/internal/stacktest"
)

type cliOut struct {
	Code   int
	Stdout string
	Stderr string
}

func (o cliOut) result(t testing.TB) api.Result {
	t.Helper()
	var r api.Result
	if err := json.Unmarshal([]byte(o.Stdout), &r); err != nil {
		t.Fatalf("stdout is not a result: %q", o.Stdout)
	}
	return r
}

func runCLI(t testing.TB, remote api.Remote, args ...string) cliOut {
	t.Helper()
	var so, se bytes.Buffer
	code := cli.Run(args, cli.Env{Remote: remote, Stdout: &so, Stderr: &se})
	return cliOut{code, so.String(), se.String()}
}

func cliClient(t testing.TB, url, token string, hc *http.Client) api.Remote {
	t.Helper()
	c, err := api.NewClient(url, token, api.SurfaceCLI, hc)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func updateArgs(s *st.Stack, extra ...string) []string {
	return append([]string{"run", s.Spec.Name, "--session", st.SessAda, "--record", "o1", "--param", "record_id=o1", "--param", "stage=won",
		"--expect", "stage=won", "--seen-version", "v1", "--seen-at", "1"}, extra...)
}

func TestCLIRunsTheVerifiedOperationAndExitsZero(t *testing.T) {
	s := st.New(t, st.Opts{})
	p := st.PrincipalOf(st.Ada, api.SurfaceCLI)
	srv := s.REST(st.Tokens{p.Credential: p})
	o := runCLI(t, cliClient(t, srv.URL, p.Credential, nil), updateArgs(s, "--request-id", "cli-req-1")...)
	if o.Code != 0 {
		t.Fatalf("%d %s %s", o.Code, o.Stdout, o.Stderr)
	}
	r := o.result(t)
	if r.Category != api.CategorySucceeded || r.Effect != api.EffectApplied || r.RequestID != "cli-req-1" || r.ContractDigest != s.Spec.ContractDigest {
		t.Fatalf("%+v", r)
	}
	if s.Stage("o1") != "won" || s.App.Commits() != 1 {
		t.Fatalf("%s %d", s.Stage("o1"), s.App.Commits())
	}
	d := runCLI(t, cliClient(t, srv.URL, p.Credential, nil), "ops", "describe", s.Spec.Name)
	var spec api.Spec
	if d.Code != 0 || json.Unmarshal([]byte(d.Stdout), &spec) != nil || spec.ContractDigest != s.Spec.ContractDigest {
		t.Fatalf("%d %s", d.Code, d.Stdout)
	}
}

func TestCLIPinsAreEnforcedByTheServiceNotTrusted(t *testing.T) {
	s := st.New(t, st.Opts{})
	p := st.PrincipalOf(st.Ada, api.SurfaceCLI)
	srv := s.REST(st.Tokens{p.Credential: p})
	rem := cliClient(t, srv.URL, p.Credential, nil)
	for name, tc := range map[string]struct {
		extra []string
		code  string
	}{
		"old contract version":     {[]string{"--version", "1.0.0"}, "contract_version_unsupported"},
		"digest of another build":  {[]string{"--digest", "sha256:" + strings.Repeat("0", 64)}, "contract_pin_mismatch"},
		"bad argument vs contract": {[]string{"--param", "stage=lost"}, ""},
	} {
		t.Run(name, func(t *testing.T) {
			args := updateArgs(s, tc.extra...)
			if name == "bad argument vs contract" {
				args = []string{"run", s.Spec.Name, "--session", st.SessAda, "--param", "record_id=o1", "--param", "stage=won", "--param", "bogus=1"}
			}
			o := runCLI(t, rem, args...)
			if o.Code == 0 {
				t.Fatalf("should fail: %s", o.Stdout)
			}
			var doc struct {
				Error *api.Error `json:"error"`
			}
			_ = json.Unmarshal([]byte(o.Stdout), &doc)
			if tc.code != "" && (doc.Error == nil || string(doc.Error.Code) != tc.code) {
				t.Fatalf("%s", o.Stdout)
			}
		})
	}
	if s.App.Commits() != 0 {
		t.Fatal("a refused pin reached the source")
	}
}

type dropResponse struct {
	next   http.RoundTripper
	armed  bool
	called int
}

func (d *dropResponse) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := d.next.RoundTrip(r)
	if d.armed && strings.HasSuffix(r.URL.Path, "/invocations") && r.Method == "POST" {
		d.called++
		if err == nil {
			resp.Body.Close()
		}
		return nil, errors.New("connection reset")
	}
	return resp, err
}

func TestCLIReportsALostAnswerAsUnknownAndRepeatingTheRequestIDIsIdempotent(t *testing.T) {
	s := st.New(t, st.Opts{})
	p := st.PrincipalOf(st.Ada, api.SurfaceCLI)
	srv := s.REST(st.Tokens{p.Credential: p})
	lossy := &dropResponse{next: http.DefaultTransport, armed: true}
	o := runCLI(t, cliClient(t, srv.URL, p.Credential, &http.Client{Transport: lossy}), updateArgs(s, "--request-id", "cli-lost-1")...)
	r := o.result(t)
	if o.Code != api.ExitUnreachable || r.Error == nil || r.Error.Code != api.CodeServiceUnreachable ||
		r.Effect != api.EffectUnknown || r.Retry != api.RetrySameRequest || r.RequestID != "cli-lost-1" {
		t.Fatalf("%d %+v", o.Code, r)
	}
	again := runCLI(t, cliClient(t, srv.URL, p.Credential, nil), updateArgs(s, "--request-id", "cli-lost-1")...)
	if again.Code != 0 || again.result(t).Category != api.CategorySucceeded {
		t.Fatalf("%d %s", again.Code, again.Stdout)
	}
	if s.App.Commits() != 1 {
		t.Fatalf("repeating the same request id applied the change %d times", s.App.Commits())
	}
}

func TestCLIExitsTenForAnUnknownCommitAndNeverRetriesBlindly(t *testing.T) {
	s := st.New(t, st.Opts{NoDedup: true})
	p := st.PrincipalOf(st.Ada, api.SurfaceCLI)
	srv := s.REST(st.Tokens{p.Credential: p})
	rem := cliClient(t, srv.URL, p.Credential, nil)
	s.App.SetFaults(fixtureapp.Faults{DropAfterCommit: 1})
	o := runCLI(t, rem, updateArgs(s, "--request-id", "cli-unk-1")...)
	r := o.result(t)
	if o.Code != api.ExitUnknown || r.Category != api.CategoryUnknownCommit || r.Effect != api.EffectUnknown || r.Retry != api.RetryReconcile || r.Error.Code != api.CodeUnknownCommit {
		t.Fatalf("%d %s", o.Code, o.Stdout)
	}
	if s.App.WriteAttempts() != 1 {
		t.Fatalf("an ambiguous write was retried: %d attempts", s.App.WriteAttempts())
	}
	next := runCLI(t, rem, updateArgs(s, "--request-id", "cli-unk-2")...)
	if next.Code != api.ExitRefused || next.result(t).Error.Code != api.CodeBlockedByUnknownCommit {
		t.Fatalf("a later write to the same scope must wait for reconciliation: %d %s", next.Code, next.Stdout)
	}
	if s.App.WriteAttempts() != 1 {
		t.Fatal("the blocked write reached the source")
	}
}

func TestCredentialsAreBoundToTheSurfaceThatIssuedThem(t *testing.T) {
	s := st.New(t, st.Opts{})
	pc, pr := st.PrincipalOf(st.Ada, api.SurfaceCLI), st.PrincipalOf(st.Ada, api.SurfaceREST)
	srv := s.REST(st.Tokens{pc.Credential: pc, pr.Credential: pr})
	if o := runCLI(t, cliClient(t, srv.URL, pr.Credential, nil), "namespaces"); o.Code != api.ExitAuth {
		t.Fatalf("a REST credential must not work as a CLI one: %d %s", o.Code, o.Stdout)
	}
	if r := do(t, "GET", srv.URL+"/v1/discover", pc.Credential, "", nil); r.Status != 200 {
		t.Fatalf("discover is unauthenticated metadata: %d", r.Status)
	}
	if r := do(t, "GET", srv.URL+"/v1/namespaces", pc.Credential, "", nil); r.Status != 403 {
		t.Fatalf("a CLI credential must not work as a REST one: %d %s", r.Status, r.Body)
	}
}
