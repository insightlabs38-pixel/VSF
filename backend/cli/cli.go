// Package cli is the command-line surface: it types arguments from the contract, calls the service and prints one structured result.
package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/insightlabs38-pixel/vsf/backend/api"
)

// Env is everything the CLI touches outside its arguments, so it can be driven without a process.
type Env struct {
	Remote api.Remote
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Rand   io.Reader
}

const Usage = `usage: vsfctl [--output json|text] [--quiet] <command>
  discover | namespaces
  ops list [--namespace NS] [--limit N] [--cursor C]
  ops describe NAME [--version V]      ops versions NAME
  run NAME --session ID [--version V] [--digest D] [--request-id R] [--record REC]
      [--param name=value]... [--expect field=value]... [--seen-version V --seen-at T]
      [--confirmation FILE] [--strategy S] [--wait-ms N] [--dry-run]
  status KEY | cancel KEY | reconcile KEY --session ID --expect field=value...
credentials: VSF_API_URL and VSF_TOKEN (or VSF_TOKEN_FILE); never a flag`

func (e Env) readStdin(max int64) ([]byte, error) {
	if e.Stdin == nil {
		return nil, fmt.Errorf("no stdin")
	}
	b, err := io.ReadAll(io.LimitReader(e.Stdin, max+1))
	if err != nil || int64(len(b)) > max {
		return nil, fmt.Errorf("unreadable")
	}
	return b, nil
}

func (e Env) newRequestID() string {
	var b [16]byte
	r := e.Rand
	if r == nil {
		r = rand.Reader
	}
	_, _ = io.ReadFull(r, b[:])
	return "cli_" + hex.EncodeToString(b[:])
}

type printer struct {
	env    Env
	text   bool
	quiet  bool
	failed bool
}

func (p *printer) doc(v any) {
	if p.text {
		p.textDoc(v)
		return
	}
	b, err := json.Marshal(v)
	if err != nil {
		b = []byte(`{"error":{"code":"internal"}}`)
	}
	_, _ = p.env.Stdout.Write(append(b, '\n'))
}

func (p *printer) textDoc(v any) {
	b, _ := json.Marshal(v)
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		fmt.Fprintln(p.env.Stdout, string(b))
		return
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		switch x := m[k].(type) {
		case map[string]any, []any:
			j, _ := json.Marshal(x)
			fmt.Fprintf(p.env.Stdout, "%s: %s\n", k, j)
		default:
			fmt.Fprintf(p.env.Stdout, "%s: %v\n", k, x)
		}
	}
}

func (p *printer) say(format string, a ...any) {
	if !p.quiet {
		fmt.Fprintf(p.env.Stderr, "vsfctl: "+format+"\n", a...)
	}
}

func (p *printer) fail(err *api.Error) int {
	p.doc(map[string]any{"error": err})
	p.say("%s: %s", err.Code, err.Message)
	return api.InfoOf(err.Code).Exit
}

func (p *printer) usageFail(err error) int {
	e := &api.Error{Code: api.CodeInvalidRequest, Message: err.Error(), Reason: "usage", Effect: api.EffectNone, Retry: api.RetryFixRequest}
	p.doc(map[string]any{"error": e})
	p.say("usage: %s", err.Error())
	return api.ExitUsage
}

func (p *printer) result(r api.Result) int {
	p.doc(r)
	code := api.ExitCodeOf(r)
	switch {
	case r.Error != nil:
		p.say("%s %s: %s", r.Category, r.Error.Code, r.Error.Message)
	case r.Category == api.CategoryInProgress:
		p.say("in progress; follow with: status %s", r.Invocation)
	}
	return code
}

var globalFlags = flagSet{"output": kindString, "quiet": kindBool}

// Run executes one command and returns the process exit status; stdout carries exactly one JSON document.
func Run(args []string, env Env) int {
	if env.Stdout == nil {
		env.Stdout = io.Discard
	}
	if env.Stderr == nil {
		env.Stderr = io.Discard
	}
	pr := &printer{env: env}
	args, err := pr.global(args)
	if err != nil {
		return pr.usageFail(err)
	}
	if len(args) == 0 {
		fmt.Fprintln(env.Stderr, Usage)
		return api.ExitUsage
	}
	if env.Remote == nil {
		return pr.fail(&api.Error{Code: api.CodeUnauthenticated, Message: api.InfoOf(api.CodeUnauthenticated).Summary, Reason: "no_credentials", Effect: api.EffectNone, Retry: api.RetryFixRequest})
	}
	ctx := context.Background()
	switch args[0] {
	case "discover":
		return pr.simple(env.Remote.Discover(ctx))
	case "namespaces":
		v, e := env.Remote.Namespaces(ctx)
		return pr.simple(map[string]any{"namespaces": v}, e)
	case "ops":
		return env.ops(ctx, pr, args[1:])
	case "run":
		return env.run(ctx, pr, args[1:])
	case "status", "cancel":
		return env.handle(ctx, pr, args)
	case "reconcile":
		return env.reconcile(ctx, pr, args[1:])
	}
	return pr.usageFail(usage("unknown command %q", clip(args[0])))
}

// global peels the flags that may appear before the command; they are not accepted after it.
func (p *printer) global(args []string) ([]string, error) {
	i := 0
	for i < len(args) && strings.HasPrefix(args[i], "--") {
		name, _, hasVal := strings.Cut(args[i][2:], "=")
		kind, ok := globalFlags[name]
		if !ok {
			return nil, usage("unknown flag --%s", clip(name))
		}
		n := 1
		if kind != kindBool && !hasVal {
			n = 2
		}
		if i+n > len(args) {
			return nil, usage("--%s needs a value", name)
		}
		ps, err := parseFlags(args[i:i+n], globalFlags)
		if err != nil {
			return nil, err
		}
		if ps.on["quiet"] {
			p.quiet = true
		}
		if v, ok := ps.one["output"]; ok {
			switch v {
			case "json":
			case "text":
				p.text = true
			default:
				return nil, usage("--output must be json or text")
			}
		}
		i += n
	}
	return args[i:], nil
}

func (p *printer) simple(v any, e *api.Error) int {
	if e != nil {
		return p.fail(e)
	}
	p.doc(v)
	return api.ExitOK
}

func (e Env) ops(ctx context.Context, pr *printer, args []string) int {
	if len(args) == 0 {
		return pr.usageFail(usage("ops needs list, describe or versions"))
	}
	switch args[0] {
	case "list":
		ps, err := parseFlags(args[1:], flagSet{"namespace": kindString, "limit": kindString, "cursor": kindString})
		if err != nil || len(ps.pos) != 0 {
			return pr.usageFail(firstErr(err, usage("ops list takes no positional arguments")))
		}
		q := api.ListQuery{Namespace: ps.one["namespace"], Cursor: ps.one["cursor"]}
		if v, ok := ps.one["limit"]; ok {
			n, perr := strconv.Atoi(v)
			if perr != nil || n < 1 {
				return pr.usageFail(usage("--limit must be a positive integer"))
			}
			q.Limit = n
		}
		return pr.simple2(e.Remote.List(ctx, q))
	case "describe":
		ps, err := parseFlags(args[1:], flagSet{"version": kindString})
		if err != nil || len(ps.pos) != 1 {
			return pr.usageFail(firstErr(err, usage("ops describe takes one operation name")))
		}
		s, ferr := e.Remote.Describe(ctx, ps.pos[0], ps.one["version"])
		if ferr != nil {
			return pr.fail(ferr)
		}
		pr.doc(s)
		return api.ExitOK
	case "versions":
		ps, err := parseFlags(args[1:], flagSet{})
		if err != nil || len(ps.pos) != 1 {
			return pr.usageFail(firstErr(err, usage("ops versions takes one operation name")))
		}
		v, ferr := e.Remote.Versions(ctx, ps.pos[0])
		return pr.simple(map[string]any{"versions": v}, ferr)
	}
	return pr.usageFail(usage("unknown ops command %q", clip(args[0])))
}

func (p *printer) simple2(v api.Page, e *api.Error) int { return p.simple(v, e) }

func firstErr(a, b error) error {
	if a != nil {
		return a
	}
	return b
}

func (e Env) handle(ctx context.Context, pr *printer, args []string) int {
	ps, err := parseFlags(args[1:], flagSet{})
	if err != nil || len(ps.pos) != 1 {
		return pr.usageFail(firstErr(err, usage("%s takes one invocation key", args[0])))
	}
	if args[0] == "cancel" {
		return pr.result(e.Remote.Cancel(ctx, ps.pos[0]))
	}
	return pr.result(e.Remote.Status(ctx, ps.pos[0]))
}

func expectMap(pairs []string) (map[string]any, error) {
	m := map[string]any{}
	for _, pair := range pairs {
		k, v, err := splitPair(pair, "expect")
		if err != nil {
			return nil, err
		}
		if _, dup := m[k]; dup {
			return nil, usage("--expect %s given twice", k)
		}
		m[k] = v
	}
	return m, nil
}

func (e Env) reconcile(ctx context.Context, pr *printer, args []string) int {
	ps, err := parseFlags(args, flagSet{"session": kindString, "expect": kindMulti})
	if err != nil || len(ps.pos) != 1 || ps.one["session"] == "" {
		return pr.usageFail(firstErr(err, usage("reconcile KEY --session ID --expect field=value")))
	}
	exp, err := expectMap(ps.multi["expect"])
	if err != nil || len(exp) == 0 {
		return pr.usageFail(firstErr(err, usage("reconcile needs --expect field=value")))
	}
	body, _ := json.Marshal(map[string]any{"session_id": ps.one["session"], "expect": exp})
	return pr.result(e.Remote.Reconcile(ctx, ps.pos[0], body))
}

var runFlags = flagSet{
	"session": kindString, "version": kindString, "digest": kindString, "request-id": kindString, "record": kindString,
	"param": kindMulti, "expect": kindMulti, "seen-version": kindString, "seen-at": kindString,
	"confirmation": kindString, "strategy": kindString, "wait-ms": kindString, "dry-run": kindBool,
}

func (e Env) run(ctx context.Context, pr *printer, args []string) int {
	ps, err := parseFlags(args, runFlags)
	if err != nil || len(ps.pos) != 1 {
		return pr.usageFail(firstErr(err, usage("run takes one operation name")))
	}
	if ps.one["session"] == "" {
		return pr.usageFail(usage("run needs --session"))
	}
	spec, ferr := e.Remote.Describe(ctx, ps.pos[0], ps.one["version"])
	if ferr != nil {
		return pr.fail(ferr)
	}
	body, err := e.invocationBody(spec, ps)
	if err != nil {
		return pr.usageFail(err)
	}
	digest := spec.ContractDigest
	if d, ok := ps.one["digest"]; ok {
		digest = d
	}
	body["contract_digest"] = digest
	raw, _ := json.Marshal(body)
	res := e.Remote.Invoke(ctx, api.Route{Operation: spec.Name, Version: spec.ContractVersion}, raw)
	if res.RequestID == "" {
		if id, ok := body["request_id"].(string); ok {
			res.RequestID = id
		}
	}
	return pr.result(res)
}

func (e Env) invocationBody(spec *api.Spec, ps parsed) (map[string]any, error) {
	params, err := e.buildParams(spec, ps.multi["param"])
	if err != nil {
		return nil, err
	}
	body := map[string]any{"session_id": ps.one["session"], "params": params}
	switch id := ps.one["request-id"]; {
	case id != "":
		body["request_id"] = id
	default:
		body["request_id"] = e.newRequestID()
	}
	if v := ps.one["record"]; v != "" {
		body["record"] = v
	}
	if exp, err := expectMap(ps.multi["expect"]); err != nil {
		return nil, err
	} else if len(exp) > 0 {
		body["expect"] = exp
	}
	if v, at := ps.one["seen-version"], ps.one["seen-at"]; v != "" || at != "" {
		if v == "" || at == "" {
			return nil, usage("--seen-version and --seen-at go together")
		}
		ms, err := e.instantMs(at)
		if err != nil {
			return nil, err
		}
		body["seen"] = map[string]any{"version": v, "observed_at": ms}
	}
	if f := ps.one["confirmation"]; f != "" {
		raw, err := e.readFile(f, 16<<10)
		if err != nil {
			return nil, usage("cannot read the confirmation file")
		}
		v, derr := api.DecodeStrict(raw)
		if derr != nil {
			return nil, usage("the confirmation file is not valid json")
		}
		body["confirmation"] = v
	}
	if v := ps.one["strategy"]; v != "" {
		body["strategy"] = v
	}
	if v, ok := ps.one["wait-ms"]; ok {
		n, perr := strconv.ParseInt(v, 10, 64)
		if perr != nil || n < 0 {
			return nil, usage("--wait-ms must be a non-negative integer")
		}
		body["wait_ms"] = n
	}
	if ps.on["dry-run"] {
		body["dry_run"] = true
	}
	return body, nil
}

// RemoteFromEnv builds the transport from the environment; the credential never comes from a flag or the process list.
func RemoteFromEnv(getenv func(string) string) (api.Remote, error) {
	base := getenv("VSF_API_URL")
	tok := getenv("VSF_TOKEN")
	if f := getenv("VSF_TOKEN_FILE"); f != "" && tok == "" {
		info, err := os.Stat(f)
		if err != nil {
			return nil, fmt.Errorf("cannot read VSF_TOKEN_FILE")
		}
		if info.Mode().Perm()&0o077 != 0 {
			return nil, fmt.Errorf("VSF_TOKEN_FILE must not be readable by group or others")
		}
		b, err := os.ReadFile(f)
		if err != nil {
			return nil, fmt.Errorf("cannot read VSF_TOKEN_FILE")
		}
		tok = strings.TrimSpace(string(b))
	}
	if base == "" || tok == "" {
		return nil, fmt.Errorf("set VSF_API_URL and VSF_TOKEN (or VSF_TOKEN_FILE)")
	}
	return api.NewClient(base, tok, api.SurfaceCLI, nil)
}
