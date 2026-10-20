package api

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/insightlabs38-pixel/vsf/backend/ir"
)

// Limits the interfaces impose on arguments, independent of what a contract declares.
const (
	MaxTextChars      = 65536
	MaxReferenceChars = 1024
	MaxBinaryChars    = 1 << 21
	MaxRequestBytes   = 4 << 20
	MaxNameChars      = 128
)

// Param is one declared input or output of an operation.
type Param struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Required    bool     `json:"required"`
	Choices     []string `json:"choices,omitempty"`
	Unit        string   `json:"unit,omitempty"`
	Description string   `json:"description,omitempty"`
}

// Disclosure states, without interpretation, what the contract says running the operation does.
type Disclosure struct {
	EffectClass     string   `json:"effect_class"`
	Write           bool     `json:"write"`
	Idempotency     string   `json:"idempotency"`
	Confirmation    string   `json:"confirmation"`
	Concurrency     string   `json:"concurrency"`
	Freshness       string   `json:"freshness"`
	MaxAgeMs        *int64   `json:"max_age_ms,omitempty"`
	VerifyRequired  bool     `json:"verification_required"`
	VerifyStrategy  string   `json:"verification_strategy"`
	OnAmbiguity     string   `json:"on_ambiguity"`
	Strategies      []string `json:"strategies"`
	RequiresCaller  bool     `json:"requires_vsf_caller"`
	RequiresSession bool     `json:"requires_source_session"`
	RequiredScopes  []string `json:"required_scopes"`
	Entitlement     string   `json:"entitlement,omitempty"`
	SourceApp       string   `json:"source_app"`
	VariantClass    string   `json:"variant_class"`
	SourceMin       string   `json:"source_version_min,omitempty"`
	SourceMax       string   `json:"source_version_max,omitempty"`
}

// Annotations are hints for clients; they are never a substitute for the checks the runtime makes.
type Annotations struct {
	ReadOnly    bool `json:"readOnlyHint"`
	Destructive bool `json:"destructiveHint"`
	Idempotent  bool `json:"idempotentHint"`
	OpenWorld   bool `json:"openWorldHint"`
}

// Spec is the surface-neutral description every interface is generated from.
type Spec struct {
	Name            string         `json:"name"`
	Namespace       string         `json:"namespace"`
	OperationID     string         `json:"operation_id"`
	ContractVersion string         `json:"contract_version"`
	ContractDigest  string         `json:"contract_digest"`
	Lifecycle       string         `json:"lifecycle"`
	Title           string         `json:"title"`
	Description     string         `json:"description"`
	Inputs          []Param        `json:"inputs"`
	Outputs         []Param        `json:"outputs"`
	Effect          Disclosure     `json:"effect"`
	Annotations     Annotations    `json:"annotations"`
	ParamsSchema    map[string]any `json:"params_schema"`
	InputSchema     map[string]any `json:"input_schema"`
	OutputSchema    map[string]any `json:"output_schema"`
}

const opPrefix = "op."

// encodeApp makes an application id safe for a tool name, reversibly and without collisions.
func encodeApp(app string) string {
	var b strings.Builder
	for _, r := range []byte(app) {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
			b.WriteByte(r)
		case r == '_':
			b.WriteString("__")
		case r == '.':
			b.WriteString("_d")
		case r == ':':
			b.WriteString("_c")
		default:
			fmt.Fprintf(&b, "_x%02x", r)
		}
	}
	return b.String()
}

// decodeApp reverses encodeApp; it reports false for text encodeApp could not have produced.
func decodeApp(ns string) (string, bool) {
	var b []byte
	for i := 0; i < len(ns); i++ {
		c := ns[i]
		if c != '_' {
			b = append(b, c)
			continue
		}
		if i+1 >= len(ns) {
			return "", false
		}
		switch ns[i+1] {
		case '_':
			b = append(b, '_')
			i++
		case 'd':
			b = append(b, '.')
			i++
		case 'c':
			b = append(b, ':')
			i++
		case 'x':
			if i+3 >= len(ns) {
				return "", false
			}
			var v byte
			if _, err := fmt.Sscanf(ns[i+2:i+4], "%02x", &v); err != nil {
				return "", false
			}
			b = append(b, v)
			i += 3
		default:
			return "", false
		}
	}
	return string(b), true
}

// OperationName is the one name an operation has on every surface.
func OperationName(app, operationID string) string {
	return opPrefix + encodeApp(app) + "." + operationID
}

// SplitName recovers the application and operation id from a name.
func SplitName(name string) (app, operationID string, ok bool) {
	rest, found := strings.CutPrefix(name, opPrefix)
	if !found {
		return "", "", false
	}
	ns, op, found := strings.Cut(rest, ".")
	if !found || op == "" {
		return "", "", false
	}
	app, ok = decodeApp(ns)
	return app, op, ok
}

func cleanText(s string, max int) string {
	var b strings.Builder
	n := 0
	for _, r := range s {
		if n >= max {
			break
		}
		if unicode.IsControl(r) || r == ' ' || r == ' ' {
			r = ' '
		}
		b.WriteRune(r)
		n++
	}
	return strings.TrimSpace(b.String())
}

func paramList(in map[ir.ParamName]ir.ParamDef) ([]Param, error) {
	out := make([]Param, 0, len(in))
	for name, d := range in {
		if d.Type == ir.ValueTypeUnknown {
			return nil, fmt.Errorf("param %s has no known type", name)
		}
		p := Param{Name: string(name), Type: string(d.Type), Required: d.Required}
		if d.Choices != nil {
			for _, c := range *d.Choices {
				p.Choices = append(p.Choices, string(c))
			}
		}
		if d.Unit != nil {
			p.Unit = cleanText(string(d.Unit.Symbol), 40)
		}
		if d.Description != nil {
			p.Description = cleanText(string(*d.Description), 200)
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func valueSchema(p Param) map[string]any {
	var s map[string]any
	switch ir.ValueType(p.Type) {
	case ir.ValueTypeText:
		s = map[string]any{"type": "string", "maxLength": MaxTextChars}
	case ir.ValueTypeReference:
		s = map[string]any{"type": "string", "minLength": 1, "maxLength": MaxReferenceChars}
	case ir.ValueTypeDate:
		s = map[string]any{"type": "string", "format": "date"}
	case ir.ValueTypeInstant:
		s = map[string]any{"type": "integer", "minimum": 0, "description": "Milliseconds since the Unix epoch."}
	case ir.ValueTypeInteger:
		s = map[string]any{"type": "integer"}
	case ir.ValueTypeDecimal:
		s = map[string]any{"type": "number"}
	case ir.ValueTypeBoolean:
		s = map[string]any{"type": "boolean"}
	case ir.ValueTypeBinary:
		s = map[string]any{"type": "string", "contentEncoding": "base64", "maxLength": MaxBinaryChars}
	case ir.ValueTypeChoice:
		vals := make([]any, len(p.Choices))
		for i, c := range p.Choices {
			vals[i] = c
		}
		s = map[string]any{"type": "string", "enum": vals}
	}
	if p.Description != "" {
		if _, has := s["description"]; !has {
			s["description"] = p.Description
		}
	}
	if p.Unit != "" {
		s["x-vsf-unit"] = p.Unit
	}
	s["x-vsf-type"] = p.Type
	return s
}

func objectSchema(ps []Param) map[string]any {
	props := map[string]any{}
	req := []any{}
	for _, p := range ps {
		props[p.Name] = valueSchema(p)
		if p.Required {
			req = append(req, p.Name)
		}
	}
	s := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(req) > 0 {
		s["required"] = req
	}
	return s
}

var effectSentence = map[ir.EffectClass]string{
	ir.EffectClassReadOnly:           "Reads only; the source is not changed.",
	ir.EffectClassIdempotentWrite:    "Changes the source; repeating the same request does not change it again.",
	ir.EffectClassConditionalWrite:   "Changes the source only if the record is unchanged since it was read.",
	ir.EffectClassNonIdempotentWrite: "Changes the source; repeating it would change it again.",
	ir.EffectClassDestructive:        "Destroys or irreversibly changes data in the source.",
	ir.EffectClassIndeterminate:      "Its effect on the source is not established.",
}

func disclosure(c ir.OperationContract) Disclosure {
	d := Disclosure{
		EffectClass: string(c.Effect.Class), Write: c.Effect.Class != ir.EffectClassReadOnly,
		Idempotency: string(c.Effect.Idempotency), Confirmation: string(c.Effect.Confirmation),
		Concurrency: string(c.Effect.Concurrency), Freshness: string(c.Freshness.BeforeWrite),
		VerifyRequired: c.Verification.Required, VerifyStrategy: string(c.Verification.Strategy),
		OnAmbiguity: string(c.OnAmbiguity), RequiresCaller: c.Authorization.RequiresVsfCaller,
		RequiresSession: c.Authorization.RequiresSourceSession,
		SourceApp:       string(c.Source.AppID), VariantClass: string(c.Source.VariantClass),
	}
	if c.Freshness.MaxAgeMs != nil {
		v := int64(*c.Freshness.MaxAgeMs)
		d.MaxAgeMs = &v
	}
	for _, s := range c.Strategies {
		d.Strategies = append(d.Strategies, string(s))
	}
	sort.Strings(d.Strategies)
	for _, s := range c.Authorization.RequiredScopes {
		d.RequiredScopes = append(d.RequiredScopes, string(s))
	}
	sort.Strings(d.RequiredScopes)
	if c.Authorization.Entitlement != nil {
		d.Entitlement = string(*c.Authorization.Entitlement)
	}
	if r := c.Source.VersionRange; r != nil {
		if r.Min != nil {
			d.SourceMin = string(*r.Min)
		}
		if r.Max != nil {
			d.SourceMax = string(*r.Max)
		}
	}
	return d
}

// invocationSchema is the single argument envelope all surfaces accept.
func invocationSchema(d Disclosure, params map[string]any) map[string]any {
	strategies := make([]any, len(d.Strategies))
	for i, s := range d.Strategies {
		strategies[i] = s
	}
	props := map[string]any{
		"request_id": map[string]any{"type": "string", "minLength": 1, "maxLength": 256, "pattern": `^[\x21-\x7e]+$`,
			"description": "Your idempotency key. Reuse it only to repeat the very same request."},
		"session_id": map[string]any{"type": "string", "minLength": 1, "maxLength": 256,
			"description": "The source session this call runs in."},
		"record": map[string]any{"type": "string", "minLength": 1, "maxLength": MaxReferenceChars,
			"description": "The record the operation applies to, when it applies to one."},
		"contract_version": map[string]any{"type": "string", "minLength": 1, "maxLength": 64,
			"description": "The contract version to run; defaults to the newest served."},
		"contract_digest": map[string]any{"type": "string", "minLength": 1, "maxLength": 128,
			"description": "Pin the exact contract document; the call is refused if it differs."},
		"params": params,
		"expect": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string", "maxLength": MaxTextChars},
			"description": "Field values the source must show afterwards for the result to count as verified."},
		"seen": map[string]any{"type": "object", "additionalProperties": false, "required": []any{"version", "observed_at"},
			"properties": map[string]any{
				"version":     map[string]any{"type": "string", "minLength": 1, "maxLength": 256},
				"observed_at": map[string]any{"type": "integer", "minimum": 0, "description": "Milliseconds since the Unix epoch."},
			}, "description": "The record version you last read, for operations that require a fresh read."},
		"confirmation": map[string]any{"type": "object", "additionalProperties": false, "required": []any{"token", "by", "digest", "expires_at"},
			"properties": map[string]any{
				"token":      map[string]any{"type": "string", "minLength": 1, "maxLength": 256},
				"by":         map[string]any{"type": "string", "minLength": 1, "maxLength": 256},
				"digest":     map[string]any{"type": "string", "minLength": 1, "maxLength": 128},
				"expires_at": map[string]any{"type": "integer", "minimum": 0},
			}, "description": "A person's confirmation of this exact request."},
		"wait_ms": map[string]any{"type": "integer", "minimum": 0, "maximum": 120000,
			"description": "How long to wait for a result before returning a handle; 0 returns at once and omitting it waits up to 10000."},
		"dry_run": map[string]any{"type": "boolean",
			"description": "Validate the arguments against the contract and stop; nothing is run and nothing is authorised."},
	}
	if len(strategies) > 0 {
		props["strategy"] = map[string]any{"type": "string", "enum": strategies}
	}
	req := []any{"session_id", "params"}
	if d.Write {
		req = append([]any{"request_id"}, req...)
	}
	return map[string]any{"type": "object", "properties": props, "required": req, "additionalProperties": false}
}

func codeEnum() []any {
	var out []any
	for _, c := range Codes() {
		out = append(out, string(c))
	}
	return out
}

func enumOf(vals ...string) []any {
	out := make([]any, len(vals))
	for i, v := range vals {
		out[i] = v
	}
	return out
}

func str(extra ...string) map[string]any {
	s := map[string]any{"type": "string"}
	if len(extra) > 0 {
		s["description"] = extra[0]
	}
	return s
}

func constOrString(v string) map[string]any {
	if v == "" {
		return map[string]any{"type": "string"}
	}
	return map[string]any{"const": v}
}

// ResultSchemaFor is the result schema of one operation; empty identity fields describe any operation's result.
func ResultSchemaFor(name, version, digest string, outputs []Param) map[string]any {
	return resultSchema(name, version, digest, outputs)
}

// resultSchema describes Result for one operation; the declared outputs are listed, never invented as fields.
func resultSchema(name, version, digest string, outputs []Param) map[string]any {
	return map[string]any{
		"type": "object", "additionalProperties": false,
		"required": []any{"operation", "category", "effect", "retry"},
		"properties": map[string]any{
			"invocation": str("The handle for status, cancellation and reconciliation."),
			"request_id": str("The request id this result answers; repeat it only to repeat the same request."),
			"reconciliation": map[string]any{"type": "object", "required": []any{"attempt", "settled"}, "properties": map[string]any{
				"attempt": map[string]any{"type": "integer"}, "evidence_level": str(), "basis": str(), "version": str(),
				"settled": map[string]any{"type": "boolean"},
			}},
			"operation":        constOrString(name),
			"contract_version": str("The contract version that ran, or that was asked for."),
			"contract_digest":  str("The digest of the contract that ran; empty when none was resolved."),
			"category": map[string]any{"enum": enumOf(string(CategorySucceeded), string(CategoryRefused), string(CategoryFailed),
				string(CategoryUnknownCommit), string(CategoryUnverifiedEffect), string(CategoryInProgress), string(CategoryValidated))},
			"state":  str(),
			"effect": map[string]any{"enum": enumOf(string(EffectNone), string(EffectApplied), string(EffectUnknown))},
			"retry": map[string]any{"enum": enumOf(string(RetryNever), string(RetryFixRequest), string(RetrySameRequest),
				string(RetryReconcile), string(RetryNewRequestID))},
			"reason": str(),
			"cancel": str(),
			"observed": map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{
				"version":   str("The record version the source showed."),
				"fields":    map[string]any{"type": "object", "additionalProperties": str()},
				"truncated": map[string]any{"type": "boolean"},
			}},
			"error": map[string]any{"type": "object", "required": []any{"code", "message", "effect", "retry"}, "properties": map[string]any{
				"code": map[string]any{"enum": codeEnum()}, "message": str(), "reason": str(),
				"effect":         map[string]any{"enum": enumOf(string(EffectNone), string(EffectApplied), string(EffectUnknown))},
				"retry":          map[string]any{"type": "string"},
				"retry_after_ms": map[string]any{"type": "integer", "minimum": 0},
				"details":        map[string]any{"type": "object"},
			}},
			"disclosure": map[string]any{"type": "object"},
			"progress": map[string]any{"type": "array", "items": map[string]any{"type": "object", "properties": map[string]any{
				"seq": map[string]any{"type": "integer"}, "stage": str(), "fraction": map[string]any{"type": "number"},
			}}},
		},
		"x-vsf-declared-outputs": declaredOutputs(outputs),
	}
}

func declaredOutputs(ps []Param) []any {
	out := make([]any, len(ps))
	for i, p := range ps {
		out[i] = map[string]any{"name": p.Name, "type": p.Type, "required": p.Required}
	}
	return out
}

// ToolNameSafe is true for names every client accepts as a tool name and cannot fold into another name.
func ToolNameSafe(n string) bool {
	if n == "" || len(n) > MaxNameChars {
		return false
	}
	for _, r := range n {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.') {
			return false
		}
	}
	return true
}

// BuildSpec derives the description of one operation; it refuses what cannot be described honestly.
func BuildSpec(doc []byte) (Spec, error) {
	un, err := ir.DecodeDocument(doc)
	if err != nil {
		return Spec{}, fmt.Errorf("undecodable")
	}
	body := un.Unsafe()
	if body.Operation == nil {
		return Spec{}, fmt.Errorf("not_operation_contract")
	}
	c := *body.Operation
	return specOf(c, un.Digest())
}

func specOf(c ir.OperationContract, digest string) (Spec, error) {
	ins, err := paramList(c.Inputs)
	if err != nil {
		return Spec{}, err
	}
	outs, err := paramList(c.Outputs)
	if err != nil {
		return Spec{}, err
	}
	app := string(c.Source.AppID)
	name := OperationName(app, string(c.OperationID))
	if len(name) > MaxNameChars {
		return Spec{}, fmt.Errorf("name_too_long")
	}
	if !ToolNameSafe(name) {
		return Spec{}, fmt.Errorf("name_unsafe")
	}
	d := disclosure(c)
	ps := objectSchema(ins)
	s := Spec{
		Name: name, Namespace: encodeApp(app), OperationID: string(c.OperationID),
		ContractVersion: string(c.ContractVersion), ContractDigest: digest, Lifecycle: string(c.Lifecycle),
		Title:  string(c.OperationID),
		Inputs: ins, Outputs: outs, Effect: d,
		Annotations: Annotations{
			ReadOnly: !d.Write, Destructive: c.Effect.Class == ir.EffectClassDestructive,
			Idempotent: c.Effect.Idempotency == ir.IdempotencyIdempotent, OpenWorld: true,
		},
		ParamsSchema: ps,
		InputSchema:  invocationSchema(d, ps),
		OutputSchema: resultSchema(name, string(c.ContractVersion), digest, outs),
	}
	s.Description = describe(s, c)
	return s, nil
}

func describe(s Spec, c ir.OperationContract) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Runs %s on source %s (contract %s). ", s.OperationID, s.Effect.SourceApp, s.ContractVersion)
	b.WriteString(effectSentence[c.Effect.Class])
	if s.Effect.Write {
		b.WriteString(" A result is reported as succeeded only after the source's own read-back confirms it.")
		b.WriteString(" If the outcome cannot be established it is reported as unknown_commit and must be reconciled, never retried blindly.")
		b.WriteString(" Supply a unique request_id and reuse it only to repeat the same request.")
	}
	return b.String()
}
