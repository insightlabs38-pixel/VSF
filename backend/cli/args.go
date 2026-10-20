package cli

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/insightlabs38-pixel/vsf/backend/api"
)

type flagKind int

const (
	kindString flagKind = iota
	kindBool
	kindMulti
)

type flagSet map[string]flagKind

type parsed struct {
	pos   []string
	one   map[string]string
	multi map[string][]string
	on    map[string]bool
}

type usageError struct{ reason string }

func (u usageError) Error() string { return u.reason }

func usage(format string, a ...any) error { return usageError{fmt.Sprintf(format, a...)} }

// parseFlags accepts --name value, --name=value and -- to end flags; anything unknown or repeated is refused.
func parseFlags(args []string, set flagSet) (parsed, error) {
	out := parsed{one: map[string]string{}, multi: map[string][]string{}, on: map[string]bool{}}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			out.pos = append(out.pos, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "--") {
			out.pos = append(out.pos, a)
			continue
		}
		name, val, hasVal := strings.Cut(a[2:], "=")
		kind, known := set[name]
		if !known {
			return out, usage("unknown flag --%s", clip(name))
		}
		if kind == kindBool {
			if hasVal {
				return out, usage("--%s takes no value", name)
			}
			out.on[name] = true
			continue
		}
		if !hasVal {
			if i+1 >= len(args) {
				return out, usage("--%s needs a value", name)
			}
			i++
			val = args[i]
		}
		if kind == kindMulti {
			out.multi[name] = append(out.multi[name], val)
			continue
		}
		if _, dup := out.one[name]; dup {
			return out, usage("--%s given twice", name)
		}
		out.one[name] = val
	}
	return out, nil
}

func clip(s string) string {
	if len(s) > 40 {
		return s[:40]
	}
	return s
}

var (
	intRe     = regexp.MustCompile(`^-?(0|[1-9][0-9]*)$`)
	decimalRe = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)
)

// readValue resolves @file and @@ escapes; a bare value is used as given.
func (e Env) readValue(raw string, max int) (string, error) {
	if !strings.HasPrefix(raw, "@") {
		return raw, nil
	}
	if strings.HasPrefix(raw, "@@") {
		return raw[1:], nil
	}
	b, err := e.readFile(raw[1:], int64(max))
	if err != nil {
		return "", usage("cannot read %s", clip(raw[1:]))
	}
	return string(b), nil
}

// typedValue reads one argument according to the type the contract declares; the service remains the authority.
func (e Env) typedValue(p api.Param, raw string) (any, error) {
	bad := func() (any, error) { return nil, usage("--param %s: not a valid %s", p.Name, p.Type) }
	switch p.Type {
	case "text":
		v, err := e.readValue(raw, api.MaxTextChars*4)
		if err != nil {
			return nil, err
		}
		if !utf8.ValidString(v) || utf8.RuneCountInString(v) > api.MaxTextChars {
			return bad()
		}
		return v, nil
	case "reference":
		if raw == "" || len(raw) > api.MaxReferenceChars || !utf8.ValidString(raw) {
			return bad()
		}
		return raw, nil
	case "integer":
		if !intRe.MatchString(raw) {
			return bad()
		}
		if _, ok := new(big.Int).SetString(raw, 10); !ok {
			return bad()
		}
		return json.Number(raw), nil
	case "decimal":
		if !decimalRe.MatchString(raw) {
			return bad()
		}
		return json.Number(raw), nil
	case "boolean":
		switch raw {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
		return bad()
	case "date":
		if _, err := time.Parse("2006-01-02", raw); err != nil {
			return bad()
		}
		return raw, nil
	case "instant":
		if intRe.MatchString(raw) && !strings.HasPrefix(raw, "-") {
			return json.Number(raw), nil
		}
		t, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil || t.UnixMilli() < 0 {
			return bad()
		}
		return json.Number(fmt.Sprint(t.UnixMilli())), nil
	case "binary":
		if strings.HasPrefix(raw, "@") && !strings.HasPrefix(raw, "@@") {
			b, err := e.readFile(raw[1:], api.MaxBinaryChars)
			if err != nil {
				return nil, usage("cannot read %s", clip(raw[1:]))
			}
			return base64.StdEncoding.EncodeToString(b), nil
		}
		if _, err := base64.StdEncoding.DecodeString(raw); err != nil || len(raw) > api.MaxBinaryChars {
			return bad()
		}
		return raw, nil
	case "choice":
		for _, c := range p.Choices {
			if c == raw {
				return raw, nil
			}
		}
		return nil, usage("--param %s: must be one of %s", p.Name, strings.Join(p.Choices, ", "))
	}
	return nil, usage("--param %s: type %q is not supported by this client", p.Name, p.Type)
}

func (e Env) readFile(path string, max int64) ([]byte, error) {
	if path == "-" {
		return e.readStdin(max)
	}
	b, err := os.ReadFile(path)
	if err != nil || int64(len(b)) > max {
		return nil, fmt.Errorf("unreadable")
	}
	return b, nil
}

func splitPair(s, flag string) (string, string, error) {
	k, v, ok := strings.Cut(s, "=")
	if !ok || k == "" {
		return "", "", usage("--%s expects name=value", flag)
	}
	return k, v, nil
}

// buildParams types every --param against the declared inputs and refuses unknown, repeated and missing ones.
func (e Env) buildParams(spec *api.Spec, pairs []string) (map[string]any, error) {
	decl := map[string]api.Param{}
	names := make([]string, 0, len(spec.Inputs))
	for _, in := range spec.Inputs {
		decl[in.Name] = in
		names = append(names, in.Name)
	}
	out := map[string]any{}
	for _, pair := range pairs {
		k, v, err := splitPair(pair, "param")
		if err != nil {
			return nil, err
		}
		p, ok := decl[k]
		if !ok {
			return nil, usage("unknown param %q; declared: %s", clip(k), strings.Join(names, ", "))
		}
		if _, dup := out[k]; dup {
			return nil, usage("--param %s given twice", k)
		}
		if out[k], err = e.typedValue(p, v); err != nil {
			return nil, err
		}
	}
	for _, in := range spec.Inputs {
		if _, ok := out[in.Name]; in.Required && !ok {
			return nil, usage("missing required --param %s (%s)", in.Name, in.Type)
		}
	}
	return out, nil
}

func (e Env) instantMs(raw string) (int64, error) {
	v, err := e.typedValue(api.Param{Name: "seen-at", Type: "instant"}, raw)
	if err != nil {
		return 0, usage("--seen-at: not an instant")
	}
	n, _ := v.(json.Number).Int64()
	return n, nil
}
