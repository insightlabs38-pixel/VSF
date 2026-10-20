// Package jsonschema validates instances against the JSON Schema 2020-12 subset the generated contracts use.
//
// A schema that uses a keyword outside the subset, another dialect or a non-local reference is refused at
// compile time, so nothing is ever validated by a schema this package only half understands.
package jsonschema

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	Dialect       = "https://json-schema.org/draft/2020-12/schema"
	maxDepth      = 32
	maxSubschemas = 2000
	maxNodes      = 200_000
)

var (
	ErrDialect     = errors.New("jsonschema: unsupported dialect")
	ErrKeyword     = errors.New("jsonschema: unsupported keyword")
	ErrReference   = errors.New("jsonschema: unresolvable or non-local $ref")
	ErrTooComplex  = errors.New("jsonschema: schema exceeds the complexity bounds")
	ErrInvalid     = errors.New("jsonschema: invalid schema")
	annotationKeys = map[string]bool{
		"$comment": true, "title": true, "description": true, "default": true, "examples": true,
		"deprecated": true, "readOnly": true, "writeOnly": true, "contentEncoding": true,
		"contentMediaType": true, "$schema": true, "$id": true,
	}
)

// Violation is one place where an instance does not satisfy the schema.
type Violation struct {
	Path    string `json:"path"`
	Keyword string `json:"keyword"`
	Message string `json:"message"`
}

func (v Violation) String() string { return v.Path + ": " + v.Message }

type node struct {
	allowAll, allowNone bool
	types               []string
	enum                []any
	hasConst            bool
	constVal            any
	minLen, maxLen      *int
	pattern             *regexp.Regexp
	format              string
	min, max            *big.Float
	exMin, exMax        *big.Float
	props               map[string]*node
	propOrder           []string
	required            []string
	addl                *node
	addlSet             bool
	minProps, maxProps  *int
	items               *node
	minItems, maxItems  *int
	allOf, anyOf, oneOf []*node
	not                 *node
	ifs, then, els      *node
	ref                 string
}

// Schema is a compiled, immutable schema.
type Schema struct {
	root *node
	defs map[string]*node
}

type compiler struct {
	defs  map[string]*node
	count int
}

// Compile accepts a decoded JSON value (numbers may be json.Number or float64).
func Compile(v any) (*Schema, error) {
	c := &compiler{defs: map[string]*node{}}
	m, ok := v.(map[string]any)
	if !ok {
		if b, isBool := v.(bool); isBool {
			return &Schema{root: &node{allowAll: b, allowNone: !b}}, nil
		}
		return nil, fmt.Errorf("%w: a schema is an object or a boolean", ErrInvalid)
	}
	if s, ok := m["$schema"]; ok && s != Dialect {
		return nil, fmt.Errorf("%w: %v", ErrDialect, s)
	}
	if defs, ok := m["$defs"].(map[string]any); ok {
		names := make([]string, 0, len(defs))
		for k := range defs {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, k := range names {
			c.defs[k] = &node{}
		}
		for _, k := range names {
			n, err := c.compile(defs[k], 1)
			if err != nil {
				return nil, err
			}
			*c.defs[k] = *n
		}
	}
	root, err := c.compile(v, 0)
	if err != nil {
		return nil, err
	}
	if err := c.checkRefs(root, map[*node]bool{}, 0); err != nil {
		return nil, err
	}
	return &Schema{root: root, defs: c.defs}, nil
}

// MustCompileJSON compiles schema text; it is for fixed schemas written in code.
func MustCompileJSON(text string) *Schema {
	var v any
	d := json.NewDecoder(strings.NewReader(text))
	d.UseNumber()
	if err := d.Decode(&v); err != nil {
		panic(err)
	}
	s, err := Compile(v)
	if err != nil {
		panic(err)
	}
	return s
}

func (c *compiler) checkRefs(n *node, seen map[*node]bool, depth int) error {
	if n == nil || seen[n] {
		return nil
	}
	if depth > maxDepth*4 {
		return ErrTooComplex
	}
	seen[n] = true
	if n.ref != "" {
		t, ok := c.defs[strings.TrimPrefix(n.ref, "#/$defs/")]
		if n.ref == "#" {
			return nil
		}
		if !ok {
			return fmt.Errorf("%w: %s", ErrReference, n.ref)
		}
		if err := c.checkRefs(t, seen, depth+1); err != nil {
			return err
		}
	}
	kids := []*node{n.addl, n.items, n.not, n.ifs, n.then, n.els}
	for _, p := range n.props {
		kids = append(kids, p)
	}
	kids = append(kids, n.allOf...)
	kids = append(kids, n.anyOf...)
	kids = append(kids, n.oneOf...)
	for _, k := range kids {
		if err := c.checkRefs(k, seen, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func asInt(v any) (int, bool) {
	f, ok := toFloat(v)
	if !ok || f < 0 || f != math.Trunc(f) || f > 1<<31 {
		return 0, false
	}
	return int(f), true
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}

func toBig(v any) (*big.Float, bool) {
	switch n := v.(type) {
	case json.Number:
		f, _, err := big.ParseFloat(n.String(), 10, 256, big.ToNearestEven)
		return f, err == nil
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return nil, false
		}
		return big.NewFloat(n), true
	case int:
		return big.NewFloat(float64(n)), true
	case int64:
		return new(big.Float).SetInt64(n), true
	}
	return nil, false
}

func (c *compiler) compile(v any, depth int) (*node, error) {
	c.count++
	if c.count > maxSubschemas || depth > maxDepth {
		return nil, ErrTooComplex
	}
	if b, ok := v.(bool); ok {
		return &node{allowAll: b, allowNone: !b}, nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%w: subschema must be an object or boolean", ErrInvalid)
	}
	n := &node{}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		val := m[k]
		switch {
		case strings.HasPrefix(k, "x-"), annotationKeys[k], k == "$defs":
			if k == "$schema" && val != Dialect {
				return nil, fmt.Errorf("%w: %v", ErrDialect, val)
			}
		case k == "$ref":
			s, ok := val.(string)
			if !ok || (s != "#" && !strings.HasPrefix(s, "#/$defs/")) {
				return nil, fmt.Errorf("%w: %v", ErrReference, val)
			}
			n.ref = s
		case k == "type":
			switch t := val.(type) {
			case string:
				n.types = []string{t}
			case []any:
				for _, e := range t {
					s, ok := e.(string)
					if !ok {
						return nil, fmt.Errorf("%w: type", ErrInvalid)
					}
					n.types = append(n.types, s)
				}
			default:
				return nil, fmt.Errorf("%w: type", ErrInvalid)
			}
			for _, t := range n.types {
				switch t {
				case "string", "integer", "number", "boolean", "object", "array", "null":
				default:
					return nil, fmt.Errorf("%w: type %q", ErrInvalid, t)
				}
			}
		case k == "enum":
			a, ok := val.([]any)
			if !ok || len(a) == 0 {
				return nil, fmt.Errorf("%w: enum", ErrInvalid)
			}
			n.enum = a
		case k == "const":
			n.hasConst, n.constVal = true, val
		case k == "minLength", k == "maxLength", k == "minItems", k == "maxItems", k == "minProperties", k == "maxProperties":
			i, ok := asInt(val)
			if !ok {
				return nil, fmt.Errorf("%w: %s", ErrInvalid, k)
			}
			p := map[string]**int{"minLength": &n.minLen, "maxLength": &n.maxLen, "minItems": &n.minItems, "maxItems": &n.maxItems, "minProperties": &n.minProps, "maxProperties": &n.maxProps}[k]
			*p = &i
		case k == "pattern":
			s, ok := val.(string)
			if !ok || len(s) > 512 {
				return nil, fmt.Errorf("%w: pattern", ErrInvalid)
			}
			re, err := regexp.Compile(s)
			if err != nil {
				return nil, fmt.Errorf("%w: pattern: %v", ErrInvalid, err)
			}
			n.pattern = re
		case k == "format":
			s, _ := val.(string)
			n.format = s
		case k == "minimum", k == "maximum", k == "exclusiveMinimum", k == "exclusiveMaximum":
			f, ok := toBig(val)
			if !ok {
				return nil, fmt.Errorf("%w: %s", ErrInvalid, k)
			}
			switch k {
			case "minimum":
				n.min = f
			case "maximum":
				n.max = f
			case "exclusiveMinimum":
				n.exMin = f
			default:
				n.exMax = f
			}
		case k == "properties":
			pm, ok := val.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("%w: properties", ErrInvalid)
			}
			n.props = map[string]*node{}
			for pk := range pm {
				n.propOrder = append(n.propOrder, pk)
			}
			sort.Strings(n.propOrder)
			for _, pk := range n.propOrder {
				child, err := c.compile(pm[pk], depth+1)
				if err != nil {
					return nil, err
				}
				n.props[pk] = child
			}
		case k == "required":
			a, ok := val.([]any)
			if !ok {
				return nil, fmt.Errorf("%w: required", ErrInvalid)
			}
			for _, e := range a {
				s, ok := e.(string)
				if !ok {
					return nil, fmt.Errorf("%w: required", ErrInvalid)
				}
				n.required = append(n.required, s)
			}
		case k == "additionalProperties":
			child, err := c.compile(val, depth+1)
			if err != nil {
				return nil, err
			}
			n.addl, n.addlSet = child, true
		case k == "items":
			child, err := c.compile(val, depth+1)
			if err != nil {
				return nil, err
			}
			n.items = child
		case k == "allOf", k == "anyOf", k == "oneOf":
			a, ok := val.([]any)
			if !ok || len(a) == 0 {
				return nil, fmt.Errorf("%w: %s", ErrInvalid, k)
			}
			var out []*node
			for _, e := range a {
				child, err := c.compile(e, depth+1)
				if err != nil {
					return nil, err
				}
				out = append(out, child)
			}
			switch k {
			case "allOf":
				n.allOf = out
			case "anyOf":
				n.anyOf = out
			default:
				n.oneOf = out
			}
		case k == "not", k == "if", k == "then", k == "else":
			child, err := c.compile(val, depth+1)
			if err != nil {
				return nil, err
			}
			switch k {
			case "not":
				n.not = child
			case "if":
				n.ifs = child
			case "then":
				n.then = child
			default:
				n.els = child
			}
		default:
			return nil, fmt.Errorf("%w: %q", ErrKeyword, k)
		}
	}
	return n, nil
}

type run struct {
	s     *Schema
	nodes int
	out   []Violation
	over  bool
}

// Validate decodes nothing: pass values from encoding/json decoded with UseNumber (or float64).
func (s *Schema) Validate(v any) []Violation {
	r := &run{s: s}
	r.check(s.root, v, "", 0)
	if r.over {
		return append(r.out, Violation{Path: "", Keyword: "complexity", Message: "validation budget exceeded"})
	}
	sort.SliceStable(r.out, func(i, j int) bool { return r.out[i].Path < r.out[j].Path })
	return r.out
}

func (r *run) fail(path, kw, format string, a ...any) {
	if len(r.out) < 64 {
		r.out = append(r.out, Violation{Path: path, Keyword: kw, Message: fmt.Sprintf(format, a...)})
	}
}

func isInteger(v any) bool {
	switch n := v.(type) {
	case json.Number:
		f, _, err := big.ParseFloat(n.String(), 10, 256, big.ToNearestEven)
		if err != nil {
			return false
		}
		return f.IsInt()
	case float64:
		return n == math.Trunc(n) && !math.IsInf(n, 0)
	case int, int64, int32:
		return true
	}
	return false
}

func typeOf(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case string:
		return "string"
	case json.Number, float64, int, int64, int32:
		return "number"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	return "unknown"
}

func typeMatches(t string, v any) bool {
	got := typeOf(v)
	switch t {
	case "integer":
		return got == "number" && isInteger(v)
	case "number":
		return got == "number"
	}
	return t == got
}

func equal(a, b any) bool {
	switch x := a.(type) {
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for k, v := range x {
			w, ok := y[k]
			if !ok || !equal(v, w) {
				return false
			}
		}
		return true
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !equal(x[i], y[i]) {
				return false
			}
		}
		return true
	}
	if typeOf(a) == "number" && typeOf(b) == "number" {
		fa, ok1 := toBig(a)
		fb, ok2 := toBig(b)
		return ok1 && ok2 && fa.Cmp(fb) == 0
	}
	return a == b
}

func (r *run) check(n *node, v any, path string, depth int) {
	r.nodes++
	if r.nodes > maxNodes || depth > maxDepth*4 {
		r.over = true
		return
	}
	if r.over || n == nil {
		return
	}
	if n.allowNone {
		r.fail(path, "false", "no value is allowed here")
		return
	}
	if n.ref != "" {
		if n.ref == "#" {
			r.check(r.s.root, v, path, depth+1)
		} else {
			r.check(r.s.defs[strings.TrimPrefix(n.ref, "#/$defs/")], v, path, depth+1)
		}
	}
	if len(n.types) > 0 {
		ok := false
		for _, t := range n.types {
			if typeMatches(t, v) {
				ok = true
				break
			}
		}
		if !ok {
			r.fail(path, "type", "expected %s, got %s", strings.Join(n.types, " or "), typeOf(v))
			return
		}
	}
	if len(n.enum) > 0 {
		found := false
		for _, e := range n.enum {
			if equal(e, v) {
				found = true
				break
			}
		}
		if !found {
			r.fail(path, "enum", "is not one of the allowed values")
		}
	}
	if n.hasConst && !equal(n.constVal, v) {
		r.fail(path, "const", "does not equal the required value")
	}
	switch x := v.(type) {
	case string:
		r.checkString(n, x, path)
	case json.Number, float64, int, int64, int32:
		r.checkNumber(n, x, path)
	case []any:
		if n.minItems != nil && len(x) < *n.minItems {
			r.fail(path, "minItems", "has %d items, needs at least %d", len(x), *n.minItems)
		}
		if n.maxItems != nil && len(x) > *n.maxItems {
			r.fail(path, "maxItems", "has %d items, allows at most %d", len(x), *n.maxItems)
		}
		if n.items != nil {
			for i, e := range x {
				r.check(n.items, e, fmt.Sprintf("%s/%d", path, i), depth+1)
			}
		}
	case map[string]any:
		r.checkObject(n, x, path, depth)
	}
	for _, s := range n.allOf {
		r.check(s, v, path, depth+1)
	}
	if len(n.anyOf) > 0 && !r.any(n.anyOf, v, path, depth) {
		r.fail(path, "anyOf", "matches none of the alternatives")
	}
	if len(n.oneOf) > 0 {
		matched := 0
		for _, s := range n.oneOf {
			if r.sub(s, v, path, depth) {
				matched++
			}
		}
		if matched != 1 {
			r.fail(path, "oneOf", "matches %d alternatives, needs exactly one", matched)
		}
	}
	if n.not != nil && r.sub(n.not, v, path, depth) {
		r.fail(path, "not", "matches a schema it must not match")
	}
	if n.ifs != nil {
		if r.sub(n.ifs, v, path, depth) {
			r.check(n.then, v, path, depth+1)
		} else {
			r.check(n.els, v, path, depth+1)
		}
	}
}

func (r *run) sub(n *node, v any, path string, depth int) bool {
	save := r.out
	r.out = nil
	r.check(n, v, path, depth+1)
	ok := len(r.out) == 0
	r.out = save
	return ok
}

func (r *run) any(ns []*node, v any, path string, depth int) bool {
	for _, s := range ns {
		if r.sub(s, v, path, depth) {
			return true
		}
	}
	return false
}

func (r *run) checkString(n *node, s string, path string) {
	l := len([]rune(s))
	if n.minLen != nil && l < *n.minLen {
		r.fail(path, "minLength", "is shorter than %d characters", *n.minLen)
	}
	if n.maxLen != nil && l > *n.maxLen {
		r.fail(path, "maxLength", "is longer than %d characters", *n.maxLen)
	}
	if n.pattern != nil && !n.pattern.MatchString(s) {
		r.fail(path, "pattern", "does not match the required pattern")
	}
	switch n.format {
	case "date":
		if _, err := time.Parse("2006-01-02", s); err != nil {
			r.fail(path, "format", "is not a date (YYYY-MM-DD)")
		}
	case "date-time":
		if _, err := time.Parse(time.RFC3339, s); err != nil {
			r.fail(path, "format", "is not an RFC 3339 date-time")
		}
	}
}

func (r *run) checkNumber(n *node, v any, path string) {
	f, ok := toBig(v)
	if !ok {
		r.fail(path, "type", "is not a finite number")
		return
	}
	if n.min != nil && f.Cmp(n.min) < 0 {
		r.fail(path, "minimum", "is below %s", n.min.Text('g', 20))
	}
	if n.max != nil && f.Cmp(n.max) > 0 {
		r.fail(path, "maximum", "is above %s", n.max.Text('g', 20))
	}
	if n.exMin != nil && f.Cmp(n.exMin) <= 0 {
		r.fail(path, "exclusiveMinimum", "must be above %s", n.exMin.Text('g', 20))
	}
	if n.exMax != nil && f.Cmp(n.exMax) >= 0 {
		r.fail(path, "exclusiveMaximum", "must be below %s", n.exMax.Text('g', 20))
	}
}

func escape(k string) string {
	return strings.NewReplacer("~", "~0", "/", "~1").Replace(k)
}

func (r *run) checkObject(n *node, o map[string]any, path string, depth int) {
	for _, req := range n.required {
		if _, ok := o[req]; !ok {
			r.fail(path+"/"+escape(req), "required", "is required")
		}
	}
	if n.minProps != nil && len(o) < *n.minProps {
		r.fail(path, "minProperties", "has fewer than %d properties", *n.minProps)
	}
	if n.maxProps != nil && len(o) > *n.maxProps {
		r.fail(path, "maxProperties", "has more than %d properties", *n.maxProps)
	}
	keys := make([]string, 0, len(o))
	for k := range o {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if p, ok := n.props[k]; ok {
			r.check(p, o[k], path+"/"+escape(k), depth+1)
			continue
		}
		if n.addlSet {
			if n.addl != nil && n.addl.allowNone {
				r.fail(path+"/"+escape(k), "additionalProperties", "is not allowed")
				continue
			}
			r.check(n.addl, o[k], path+"/"+escape(k), depth+1)
		}
	}
}
