package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/insightlabs38-pixel/vsf/backend/api"
)

type fakeRemote struct {
	spec    *api.Spec
	descErr *api.Error
	bodies  [][]byte
	routes  []api.Route
	result  api.Result
	calls   []string
}

func (f *fakeRemote) Discover(context.Context) (api.Discovery, *api.Error) {
	return api.Discovery{Service: "vsf"}, nil
}
func (f *fakeRemote) Namespaces(context.Context) ([]api.Namespace, *api.Error) { return nil, nil }
func (f *fakeRemote) List(_ context.Context, q api.ListQuery) (api.Page, *api.Error) {
	f.calls = append(f.calls, "list:"+q.Namespace+":"+q.Cursor)
	return api.Page{}, nil
}
func (f *fakeRemote) Describe(_ context.Context, name, version string) (*api.Spec, *api.Error) {
	f.calls = append(f.calls, "describe:"+name+"@"+version)
	return f.spec, f.descErr
}
func (f *fakeRemote) Versions(context.Context, string) ([]api.VersionInfo, *api.Error) {
	return nil, nil
}
func (f *fakeRemote) Invoke(_ context.Context, r api.Route, b []byte) api.Result {
	f.routes, f.bodies = append(f.routes, r), append(f.bodies, b)
	return f.result
}
func (f *fakeRemote) Confirm(context.Context, api.Route, []byte) (api.ConfirmationIn, *api.Error) {
	return api.ConfirmationIn{}, nil
}
func (f *fakeRemote) Status(context.Context, string) api.Result { return f.result }
func (f *fakeRemote) Cancel(context.Context, string) api.Result { return f.result }
func (f *fakeRemote) Reconcile(_ context.Context, _ string, b []byte) api.Result {
	f.bodies = append(f.bodies, b)
	return f.result
}

func testSpec() *api.Spec {
	return &api.Spec{
		Name: "op.x.update", ContractVersion: "1.3.0", ContractDigest: "sha256:aa",
		Inputs: []api.Param{
			{Name: "count", Type: "integer", Required: true},
			{Name: "note", Type: "text"},
			{Name: "price", Type: "decimal"},
			{Name: "ok", Type: "boolean"},
			{Name: "due", Type: "date"},
			{Name: "at", Type: "instant"},
			{Name: "blob", Type: "binary"},
			{Name: "stage", Type: "choice", Choices: []string{"won", "lost"}, Required: true},
			{Name: "ref", Type: "reference"},
		},
	}
}

type out struct {
	code           int
	stdout, stderr string
}

func (o out) doc(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	dec := json.NewDecoder(strings.NewReader(o.stdout))
	if err := dec.Decode(&m); err != nil {
		t.Fatalf("stdout is not one json document: %q", o.stdout)
	}
	if dec.More() {
		t.Fatalf("stdout has more than one document: %q", o.stdout)
	}
	return m
}

func run(t *testing.T, f *fakeRemote, stdin string, args ...string) out {
	t.Helper()
	var so, se bytes.Buffer
	code := Run(args, Env{Remote: f, Stdin: strings.NewReader(stdin), Stdout: &so, Stderr: &se, Rand: bytes.NewReader(bytes.Repeat([]byte{7}, 64))})
	return out{code, so.String(), se.String()}
}

func okResult() api.Result {
	return api.Result{Invocation: "op_1", Operation: "op.x.update", ContractVersion: "1.3.0", ContractDigest: "sha256:aa",
		Category: api.CategorySucceeded, Effect: api.EffectApplied, Retry: api.RetryNever}
}

func goodArgs(extra ...string) []string {
	return append([]string{"run", "op.x.update", "--session", "s1", "--param", "count=3", "--param", "stage=won"}, extra...)
}

func TestRunTypesArgumentsFromTheContractAndPinsTheDescribedDigest(t *testing.T) {
	f := &fakeRemote{spec: testSpec(), result: okResult()}
	o := run(t, f, "", goodArgs("--param", "price=1.50", "--param", "ok=true", "--param", "due=2026-10-20",
		"--param", "at=2026-10-20T00:00:00Z", "--param", "ref=o1", "--param", "blob=aGk=", "--expect", "stage=won",
		"--seen-version", "v1", "--seen-at", "1792368400000", "--wait-ms", "0", "--record", "o1")...)
	if o.code != 0 {
		t.Fatalf("%d %s %s", o.code, o.stdout, o.stderr)
	}
	if o.doc(t)["category"] != "succeeded" {
		t.Fatal(o.stdout)
	}
	if len(f.bodies) != 1 || f.routes[0].Operation != "op.x.update" || f.routes[0].Version != "1.3.0" {
		t.Fatalf("%+v", f.routes)
	}
	var body map[string]any
	dec := json.NewDecoder(bytes.NewReader(f.bodies[0]))
	dec.UseNumber()
	_ = dec.Decode(&body)
	p := body["params"].(map[string]any)
	if p["count"] != json.Number("3") || p["price"] != json.Number("1.50") || p["ok"] != true || p["at"] != json.Number("1792454400000") || p["stage"] != "won" {
		t.Fatalf("arguments were not typed from the contract: %v", p)
	}
	if body["contract_digest"] != "sha256:aa" || !strings.HasPrefix(body["request_id"].(string), "cli_") || body["wait_ms"] != json.Number("0") {
		t.Fatalf("%v", body)
	}
	if o.stderr != "" {
		t.Fatalf("a success must be quiet on stderr: %q", o.stderr)
	}
}

func TestVersionPinFlagSelectsTheContractAndTheDigestIsEnforced(t *testing.T) {
	f := &fakeRemote{spec: testSpec(), result: okResult()}
	run(t, f, "", goodArgs("--version", "1.3.0", "--digest", "sha256:bb")...)
	if f.calls[0] != "describe:op.x.update@1.3.0" {
		t.Fatalf("%v", f.calls)
	}
	if !strings.Contains(string(f.bodies[0]), `"contract_digest":"sha256:bb"`) {
		t.Fatalf("the explicit pin must be sent as given for the service to enforce: %s", f.bodies[0])
	}
}

func TestMalformedArgumentsAreRefusedLocallyBeforeAnythingIsSent(t *testing.T) {
	cases := map[string][]string{
		"integer with fraction":  {"--param", "count=1.5"},
		"integer exponent":       {"--param", "count=1e3"},
		"integer leading zero":   {"--param", "count=01"},
		"integer plus sign":      {"--param", "count=+1"},
		"integer huge":           {"--param", "count=" + strings.Repeat("9", 400)},
		"decimal garbage":        {"--param", "price=1,5"},
		"boolean yes":            {"--param", "ok=yes"},
		"boolean upper":          {"--param", "ok=TRUE"},
		"date wrong":             {"--param", "due=20-10-2026"},
		"date impossible":        {"--param", "due=2026-02-30"},
		"instant negative":       {"--param", "at=-5"},
		"instant garbage":        {"--param", "at=tomorrow"},
		"binary not base64":      {"--param", "blob=***"},
		"choice outside set":     {"--param", "stage=maybe"},
		"unknown param":          {"--param", "extra=1"},
		"duplicate param":        {"--param", "count=4"},
		"param without equals":   {"--param", "count"},
		"unknown flag":           {"--bogus", "x"},
		"flag without value":     {"--record"},
		"bad wait":               {"--wait-ms", "soon"},
		"negative wait":          {"--wait-ms", "-1"},
		"seen needs both":        {"--seen-version", "v1"},
		"unreadable @file":       {"--param", "note=@/nonexistent/file"},
		"expect without equals":  {"--expect", "stage"},
		"duplicate expect":       {"--expect", "a=1", "--expect", "a=2"},
		"unknown dry-run value":  {"--dry-run=yes"},
		"repeated single flag":   {"--record", "a", "--record", "b"},
		"not valid utf8 as text": {"--param", "note=\xff\xfe"},
	}
	for name, extra := range cases {
		t.Run(name, func(t *testing.T) {
			f := &fakeRemote{spec: testSpec(), result: okResult()}
			o := run(t, f, "", goodArgs(extra...)...)
			if o.code != api.ExitUsage {
				t.Fatalf("exit %d: %s", o.code, o.stdout)
			}
			if len(f.bodies) != 0 {
				t.Fatal("a malformed command reached the service")
			}
			e := o.doc(t)["error"].(map[string]any)
			if e["code"] != "invalid_request" || e["effect"] != "none" || o.stderr == "" {
				t.Fatalf("%v / %q", e, o.stderr)
			}
		})
	}
}

func TestMissingRequiredParameterAndSessionAreUsageErrors(t *testing.T) {
	f := &fakeRemote{spec: testSpec(), result: okResult()}
	if o := run(t, f, "", "run", "op.x.update", "--session", "s1", "--param", "stage=won"); o.code != api.ExitUsage {
		t.Fatalf("%d", o.code)
	}
	if o := run(t, f, "", "run", "op.x.update", "--param", "count=1", "--param", "stage=won"); o.code != api.ExitUsage {
		t.Fatalf("%d", o.code)
	}
	if len(f.bodies) != 0 {
		t.Fatal("reached the service")
	}
}

func TestTextAndFileValuesAreReadSafely(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "note.txt")
	_ = os.WriteFile(file, []byte("from a file"), 0o600)
	f := &fakeRemote{spec: testSpec(), result: okResult()}
	run(t, f, "stdin text", goodArgs("--param", "note=@"+file)...)
	run(t, f, "stdin text", goodArgs("--param", "note=@-")...)
	run(t, f, "", goodArgs("--param", "note=@@literal")...)
	for i, want := range []string{"from a file", "stdin text", "@literal"} {
		var b map[string]any
		_ = json.Unmarshal(f.bodies[i], &b)
		if b["params"].(map[string]any)["note"] != want {
			t.Fatalf("%d: %v", i, b["params"])
		}
	}
}

func TestExitStatusFollowsTheTaxonomyAndStdoutStaysParseable(t *testing.T) {
	cases := []struct {
		name string
		res  api.Result
		exit int
	}{
		{"succeeded", okResult(), 0},
		{"in progress", api.Result{Invocation: "op_1", Category: api.CategoryInProgress, Effect: api.EffectUnknown, Retry: api.RetrySameRequest}, api.ExitInProgress},
		{"unknown commit", api.Result{Invocation: "op_1", Category: api.CategoryUnknownCommit, Effect: api.EffectUnknown, Retry: api.RetryReconcile,
			Error: &api.Error{Code: api.CodeUnknownCommit, Message: "m", Effect: api.EffectUnknown, Retry: api.RetryReconcile}}, api.ExitUnknown},
		{"unverified", api.Result{Category: api.CategoryUnverifiedEffect, Error: &api.Error{Code: api.CodeUnverifiedEffect, Message: "m"}}, api.ExitUnverified},
		{"refused", api.Result{Category: api.CategoryRefused, Error: &api.Error{Code: api.CodeOperationNotFound, Message: "m"}}, api.ExitRefused},
		{"failed", api.Result{Category: api.CategoryFailed, Error: &api.Error{Code: api.CodeSourceRejected, Message: "m"}}, api.ExitFailed},
		{"permission", api.Result{Category: api.CategoryRefused, Error: &api.Error{Code: api.CodePermissionDenied, Message: "m"}}, api.ExitAuth},
		{"unreachable", api.Result{Category: api.CategoryFailed, Effect: api.EffectUnknown, Error: &api.Error{Code: api.CodeServiceUnreachable, Message: "m"}}, api.ExitUnreachable},
	}
	seen := map[int]string{}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			o := run(t, &fakeRemote{spec: testSpec(), result: c.res}, "", goodArgs()...)
			if o.code != c.exit {
				t.Fatalf("exit %d, want %d", o.code, c.exit)
			}
			o.doc(t)
			if c.exit != 0 && o.stderr == "" {
				t.Fatal("a non-success needs a human line on stderr")
			}
			if strings.Contains(o.stderr, "{") {
				t.Fatalf("stderr must not carry the structured document: %q", o.stderr)
			}
		})
		if prev, dup := seen[c.exit]; dup && c.exit != 0 {
			t.Fatalf("%s and %s share exit %d", prev, c.name, c.exit)
		}
		seen[c.exit] = c.name
	}
}

func TestDescribeFailureIsReportedWithItsTypedError(t *testing.T) {
	f := &fakeRemote{descErr: &api.Error{Code: api.CodeContractVersionUnsupported, Message: "m", Details: map[string]any{"supported": []any{"1.3.0"}}}}
	o := run(t, f, "", goodArgs("--version", "0.1.0")...)
	if o.code != api.ExitRefused || o.doc(t)["error"].(map[string]any)["code"] != "contract_version_unsupported" || o.doc(t)["category"] != "refused" {
		t.Fatalf("%d %s", o.code, o.stdout)
	}
	if len(f.bodies) != 0 {
		t.Fatal("an unsupported pin must stop before invoking")
	}
}

func TestQuietAndTextOutputAndGlobalFlagPlacement(t *testing.T) {
	f := &fakeRemote{spec: testSpec(), result: api.Result{Category: api.CategoryRefused, Error: &api.Error{Code: api.CodeOperationNotFound, Message: "m"}}}
	o := run(t, f, "", append([]string{"--quiet"}, goodArgs()...)...)
	if o.stderr != "" || o.code != api.ExitRefused {
		t.Fatalf("%q %d", o.stderr, o.code)
	}
	o = run(t, f, "", append([]string{"--output", "text"}, "status", "op_1")...)
	if !strings.Contains(o.stdout, "category: refused") {
		t.Fatalf("%q", o.stdout)
	}
	if o = run(t, f, "", "status", "op_1", "--quiet"); o.code != api.ExitUsage {
		t.Fatalf("a global flag after the command must be refused: %d", o.code)
	}
	if o = run(t, f, "", "--output", "xml", "status", "op_1"); o.code != api.ExitUsage {
		t.Fatalf("%d", o.code)
	}
}

func TestReconcileNeedsASessionAndExpectedValues(t *testing.T) {
	f := &fakeRemote{spec: testSpec(), result: okResult()}
	if o := run(t, f, "", "reconcile", "op_1", "--session", "s1"); o.code != api.ExitUsage {
		t.Fatalf("%d", o.code)
	}
	if o := run(t, f, "", "reconcile", "op_1", "--session", "s1", "--expect", "stage=won"); o.code != 0 {
		t.Fatalf("%d %s", o.code, o.stdout)
	}
	if !strings.Contains(string(f.bodies[0]), `"expect":{"stage":"won"}`) {
		t.Fatalf("%s", f.bodies[0])
	}
}

func TestNoCredentialsIsAnAuthFailureNotAPanic(t *testing.T) {
	var so, se bytes.Buffer
	code := Run([]string{"ops", "list"}, Env{Stdout: &so, Stderr: &se})
	if code != api.ExitAuth || !strings.Contains(so.String(), "unauthenticated") {
		t.Fatalf("%d %s", code, so.String())
	}
}

func TestTokensComeOnlyFromTheEnvironmentOrAPrivateFile(t *testing.T) {
	dir := t.TempDir()
	tok := filepath.Join(dir, "tok")
	_ = os.WriteFile(tok, []byte("secret-token\n"), 0o644)
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	if _, err := RemoteFromEnv(env(map[string]string{"VSF_API_URL": "http://127.0.0.1:1", "VSF_TOKEN_FILE": tok})); err == nil {
		t.Fatal("a group- or world-readable token file must be refused")
	}
	_ = os.Chmod(tok, 0o600)
	if _, err := RemoteFromEnv(env(map[string]string{"VSF_API_URL": "http://127.0.0.1:1", "VSF_TOKEN_FILE": tok})); err != nil {
		t.Fatal(err)
	}
	for name, m := range map[string]map[string]string{
		"no url":        {"VSF_TOKEN": "x"},
		"no token":      {"VSF_API_URL": "https://example.test"},
		"plain remote":  {"VSF_API_URL": "http://example.test", "VSF_TOKEN": "x"},
		"url userinfo":  {"VSF_API_URL": "https://u:p@example.test", "VSF_TOKEN": "x"},
		"ftp":           {"VSF_API_URL": "ftp://example.test", "VSF_TOKEN": "x"},
		"token in text": {"VSF_API_URL": "", "VSF_TOKEN": "x"},
	} {
		if _, err := RemoteFromEnv(env(m)); err == nil {
			t.Fatalf("%s must be refused", name)
		} else if strings.Contains(err.Error(), "secret") {
			t.Fatal("an error leaked the token")
		}
	}
}
