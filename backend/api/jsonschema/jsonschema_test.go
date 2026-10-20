package jsonschema

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func decode(t testing.TB, s string) any {
	t.Helper()
	var v any
	d := json.NewDecoder(strings.NewReader(s))
	d.UseNumber()
	if err := d.Decode(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func compile(t testing.TB, s string) *Schema {
	t.Helper()
	sc, err := Compile(decode(t, s))
	if err != nil {
		t.Fatal(err)
	}
	return sc
}

func TestValidatesTheKeywordsTheContractsUse(t *testing.T) {
	sc := compile(t, `{
	  "$schema": "https://json-schema.org/draft/2020-12/schema",
	  "type": "object", "additionalProperties": false, "required": ["id", "stage"],
	  "properties": {
	    "id": {"type": "string", "minLength": 1, "maxLength": 8, "pattern": "^[a-z0-9]+$"},
	    "stage": {"enum": ["qualified", "won"]},
	    "total": {"type": "number", "minimum": 0, "exclusiveMaximum": 100},
	    "n": {"type": "integer"},
	    "day": {"type": "string", "format": "date"},
	    "tags": {"type": "array", "items": {"type": "string"}, "maxItems": 2}
	  }}`)
	good := []string{
		`{"id":"a1","stage":"won"}`,
		`{"id":"a1","stage":"won","total":99.5,"n":3,"day":"2026-02-28","tags":["x"]}`,
		`{"id":"a1","stage":"won","n":1.0}`,
	}
	for _, g := range good {
		if v := sc.Validate(decode(t, g)); len(v) != 0 {
			t.Errorf("%s: %v", g, v)
		}
	}
	bad := map[string]string{
		`{"stage":"won"}`:                                "required",
		`{"id":"a1","stage":"lost"}`:                     "enum",
		`{"id":"","stage":"won"}`:                        "minLength",
		`{"id":"A","stage":"won"}`:                       "pattern",
		`{"id":"a1","stage":"won","extra":1}`:            "additionalProperties",
		`{"id":"a1","stage":"won","total":100}`:          "exclusiveMaximum",
		`{"id":"a1","stage":"won","total":-0.1}`:         "minimum",
		`{"id":"a1","stage":"won","n":1.5}`:              "type",
		`{"id":"a1","stage":"won","n":"1"}`:              "type",
		`{"id":"a1","stage":"won","day":"2026-02-30"}`:   "format",
		`{"id":"a1","stage":"won","tags":["a","b","c"]}`: "maxItems",
		`{"id":"a1","stage":"won","tags":[1]}`:           "type",
		`[]`:                                             "type",
		`{"id":"toolongvalue","stage":"won"}`:            "maxLength",
	}
	for in, kw := range bad {
		v := sc.Validate(decode(t, in))
		found := false
		for _, x := range v {
			if x.Keyword == kw {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: expected %s, got %v", in, kw, v)
		}
	}
}

func TestLargeIntegersKeepTheirPrecision(t *testing.T) {
	sc := compile(t, `{"type":"integer","maximum":9007199254740993}`)
	if v := sc.Validate(decode(t, `9007199254740993`)); len(v) != 0 {
		t.Fatalf("%v", v)
	}
	if v := sc.Validate(decode(t, `9007199254740994`)); len(v) == 0 {
		t.Fatal("the next integer must exceed the maximum")
	}
}

func TestCompositionAndReferences(t *testing.T) {
	sc := compile(t, `{
	  "$defs": {"node": {"type":"object","required":["v"],"properties":{"v":{"type":"integer"},"next":{"$ref":"#/$defs/node"}},"additionalProperties":false}},
	  "oneOf": [{"$ref":"#/$defs/node"}, {"type":"string"}]}`)
	if v := sc.Validate(decode(t, `{"v":1,"next":{"v":2,"next":{"v":3}}}`)); len(v) != 0 {
		t.Fatalf("%v", v)
	}
	if v := sc.Validate(decode(t, `{"v":1,"next":{"v":"x"}}`)); len(v) == 0 {
		t.Fatal("a nested type error must be found")
	}
	if v := sc.Validate(decode(t, `7`)); len(v) == 0 {
		t.Fatal("neither alternative matches")
	}
	not := compile(t, `{"not":{"type":"string"},"if":{"type":"integer"},"then":{"minimum":10},"else":{"type":"null"}}`)
	for in, ok := range map[string]bool{`12`: true, `3`: false, `null`: true, `true`: false} {
		if got := len(not.Validate(decode(t, in))) == 0; got != ok {
			t.Errorf("%s: valid=%v want %v", in, got, ok)
		}
	}
}

func TestSchemasOutsideTheSubsetAreRefusedNotHalfApplied(t *testing.T) {
	cases := map[string]error{
		`{"$schema":"http://json-schema.org/draft-07/schema#","type":"object"}`: ErrDialect,
		`{"patternProperties":{"^a":{"type":"string"}}}`:                        ErrKeyword,
		`{"unevaluatedProperties":false}`:                                       ErrKeyword,
		`{"$ref":"https://example.com/other.json"}`:                             ErrReference,
		`{"$ref":"file:///etc/passwd"}`:                                         ErrReference,
		`{"$ref":"#/$defs/missing"}`:                                            ErrReference,
		`{"type":"integer","enum":[]}`:                                          ErrInvalid,
		`{"type":"strng"}`:                                                      ErrInvalid,
		`{"pattern":"("}`:                                                       ErrInvalid,
		`{"properties":{"a":{"minLength":-1}}}`:                                 ErrInvalid,
		`7`:                                                                     ErrInvalid,
	}
	for in, want := range cases {
		if _, err := Compile(decode(t, in)); !errors.Is(err, want) {
			t.Errorf("%s: got %v want %v", in, err, want)
		}
	}
}

func TestComplexityIsBounded(t *testing.T) {
	deep := strings.Repeat(`{"items":`, 40) + `{"type":"string"}` + strings.Repeat(`}`, 40)
	if _, err := Compile(decode(t, deep)); !errors.Is(err, ErrTooComplex) {
		t.Fatalf("depth: %v", err)
	}
	var wide strings.Builder
	wide.WriteString(`{"allOf":[`)
	for i := 0; i < 2100; i++ {
		if i > 0 {
			wide.WriteString(",")
		}
		wide.WriteString(`{"type":"string"}`)
	}
	wide.WriteString(`]}`)
	if _, err := Compile(decode(t, wide.String())); !errors.Is(err, ErrTooComplex) {
		t.Fatalf("width: %v", err)
	}
	// A schema that is cheap to write but expensive to run is cut off by the validation budget.
	rec := compile(t, `{"$defs":{"a":{"allOf":[{"$ref":"#/$defs/a2"},{"$ref":"#/$defs/a2"}]},"a2":{"allOf":[{"$ref":"#/$defs/a3"},{"$ref":"#/$defs/a3"}]},"a3":{"type":"integer"}},"$ref":"#/$defs/a"}`)
	if v := rec.Validate(decode(t, `1`)); len(v) != 0 {
		t.Fatalf("%v", v)
	}
	nested := decode(t, strings.Repeat(`[`, 200)+strings.Repeat(`]`, 200))
	items := compile(t, `{"$defs":{"t":{"type":"array","items":{"$ref":"#/$defs/t"}}},"$ref":"#/$defs/t"}`)
	if v := items.Validate(nested); len(v) == 0 {
		t.Fatal("an instance nested deeper than the bound must not be accepted silently")
	}
}

func TestBooleanSchemasAndPathEscaping(t *testing.T) {
	if len(compile(t, `true`).Validate(decode(t, `1`))) != 0 || len(compile(t, `false`).Validate(decode(t, `1`))) == 0 {
		t.Fatal("boolean schemas")
	}
	sc := compile(t, `{"type":"object","required":["a/b"],"additionalProperties":false}`)
	v := sc.Validate(decode(t, `{"c~d":1}`))
	paths := []string{}
	for _, x := range v {
		paths = append(paths, x.Path)
	}
	joined := strings.Join(paths, ",")
	if !strings.Contains(joined, "/a~1b") || !strings.Contains(joined, "/c~0d") {
		t.Fatalf("%v", paths)
	}
}
