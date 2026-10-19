import type { Accepted } from "./digest.ts";
import { type Field, type Model, pascal, type TypeDef, type TypeRef } from "./idl.ts";

const lit = (s: string): string => JSON.stringify(s);
const ver = (v: [number, number, number]): string => `rt.Version{${v[0]}, ${v[1]}, ${v[2]}}`;
const goName = (n: string): string => pascal(n);

function goType(t: TypeRef, f?: Field): string {
  switch (t.k) {
    case "bool":
      return "bool";
    case "named":
      return t.name;
    case "list":
      return `[]${goType(t.of)}`;
    case "map":
      return `map[${f?.keys ?? "string"}]${goType(t.of)}`;
  }
}

function one(r: TypeRef): string {
  return r.k === "bool"
    ? "func(e any, p string) (bool, error) { return rt.Bool(e, p) }"
    : `func(e any, p string) (${(r as { name: string }).name}, error) { return Decode${(r as { name: string }).name}(e, p, ctx) }`;
}

function decodeCall(f: Field, src: string, p: string): string {
  const t = f.type;
  switch (t.k) {
    case "bool":
      return `rt.Bool(${src}, ${p})`;
    case "named":
      return `Decode${t.name}(${src}, ${p}, ctx)`;
    case "list":
      return `rt.List(${src}, ${p}, ${f.minItems}, ${f.maxItems}, ${one(t.of)})`;
    case "map":
      return `rt.Map(${src}, ${p}, ${f.maxItems}, func(k string) (${f.keys}, error) { return Decode${f.keys}(k, "", ctx) }, ${one(t.of)})`;
  }
}

function encodeExpr(t: TypeRef, expr: string): string {
  switch (t.k) {
    case "bool":
      return expr;
    case "named":
      return `${expr}.Encode()`;
    case "list":
      return `func() any { out := make([]any, 0, len(${expr})); for _, e := range ${expr} { out = append(out, ${encodeExpr(t.of, "e")}) }; return out }()`;
    case "map":
      return `func() any { out := canon.NewObject(); for k, e := range ${expr} { out.Set(string(k), ${encodeExpr(t.of, "e")}) }; return out }()`;
  }
}

function emitType(t: TypeDef): string {
  const doc = t.doc ? `// ${t.doc.replace(/\n/g, " ")}\n` : "";
  switch (t.kind) {
    case "string":
      return `var rule${t.name} = rt.StrRule{${t.pattern === null ? "" : `Pattern: regexp.MustCompile(${lit(t.pattern)}), `}MaxLen: ${t.maxLen}, Text: rt.Text${t.text === "none" ? "None" : t.text === "label" ? "Label" : "Multiline"}}

${doc}type ${t.name} string

func Decode${t.name}(v any, path string, _ rt.Ctx) (${t.name}, error) {
	s, err := rt.String(v, path, rule${t.name})
	return ${t.name}(s), err
}

func New${t.name}(s string) (${t.name}, error) {
	if err := rt.CheckString(s, "", rule${t.name}); err != nil {
		return "", err
	}
	return ${t.name}(s), nil
}

func (x ${t.name}) Encode() any { return string(x) }
`;
    case "int":
      return `${doc}type ${t.name} int64

func Decode${t.name}(v any, path string, _ rt.Ctx) (${t.name}, error) {
	n, err := rt.Int(v, path, ${t.min}, ${t.max})
	return ${t.name}(n), err
}

func (x ${t.name}) Encode() any { return int64(x) }
`;
    case "instant":
      return `${doc}type ${t.name} int64

func Decode${t.name}(v any, path string, _ rt.Ctx) (${t.name}, error) {
	n, err := rt.Instant(v, path)
	return ${t.name}(n), err
}

func (x ${t.name}) Encode() any { return int64(x) }
`;
    case "date":
      return `${doc}type ${t.name} string

func Decode${t.name}(v any, path string, _ rt.Ctx) (${t.name}, error) {
	s, err := rt.Date(v, path)
	return ${t.name}(s), err
}

func (x ${t.name}) Encode() any { return string(x) }
`;
    case "decimal":
      return `${doc}type ${t.name} string

func Decode${t.name}(v any, path string, _ rt.Ctx) (${t.name}, error) {
	s, err := rt.Decimal(v, path, ${t.scale === null ? -1 : t.scale})
	return ${t.name}(s), err
}

func (x ${t.name}) Encode() any { return string(x) }
`;
    case "enum":
      return `${doc}type ${t.name} string

const (
${t.values.map((v) => `	${t.name}${pascal(v)} ${t.name} = ${lit(v)}`).join("\n")}
)

var all${t.name} = []${t.name}{${t.values.map((v) => `${t.name}${pascal(v)}`).join(", ")}}

func Decode${t.name}(v any, path string, _ rt.Ctx) (${t.name}, error) {
	s, err := rt.EnumStr(v, path)
	if err != nil {
		return "", err
	}
	for _, c := range all${t.name} {
		if string(c) == s {
			return c, nil
		}
	}
	return "", rt.Fail(path, "ir.bad_enum")
}

func (x ${t.name}) Encode() any { return string(x) }
`;
    case "struct": {
      const decl = t.fields
        .map((f) => `	${goName(f.name)} ${f.optional || f.since ? "*" : ""}${goType(f.type, f)}`)
        .join("\n");
      const lines: string[] = [];
      for (const f of t.fields) {
        const id = `f_${f.name}`;
        const p = `rt.Child(path, ${lit(f.name)})`;
        if (f.since || f.optional) {
          const getter = f.since
            ? `raw, found, err := o.Since(${lit(f.name)}, ${ver(f.since)}, ${!f.optional}, ctx)\n		if err != nil {\n			return out, err\n		}`
            : `raw, found := o.Opt(${lit(f.name)})`;
          lines.push(`	var ${id} *${goType(f.type, f)}
	{
		${getter}
		if found {
			x, err := ${decodeCall(f, "raw", p)}
			if err != nil {
				return out, err
			}
			${id} = &x
		}
	}`);
        } else {
          lines.push(`	var ${id} ${goType(f.type, f)}
	{
		raw, err := o.Req(${lit(f.name)})
		if err != nil {
			return out, err
		}
		${id}, err = ${decodeCall(f, "raw", p)}
		if err != nil {
			return out, err
		}
	}`);
        }
      }
      const encodes = t.fields
        .map((f) => {
          if (f.optional || f.since) {
            return `	if x.${goName(f.name)} != nil {
		e.Set(${lit(f.name)}, ${encodeExpr(f.type, `(*x.${goName(f.name)})`)})
	}`;
          }
          return `	e.Set(${lit(f.name)}, ${encodeExpr(f.type, `x.${goName(f.name)}`)})`;
        })
        .join("\n");
      return `${doc}type ${t.name} struct {
${decl}
}

func Decode${t.name}(v any, path string, ctx rt.Ctx) (${t.name}, error) {
	o, err := rt.NewObj(v, path)
	if err != nil {
		return ${t.name}{}, err
	}
	return decode${t.name}Fields(o, path, ctx)
}

func decode${t.name}Fields(o *rt.Obj, path string, ctx rt.Ctx) (${t.name}, error) {
	var out ${t.name}
${lines.join("\n")}
	if err := o.Finish(); err != nil {
		return out, err
	}
	out = ${t.name}{${t.fields.map((f) => `${goName(f.name)}: f_${f.name}`).join(", ")}}
	return out, nil
}

func (x ${t.name}) encodeFields() *canon.Object {
	e := canon.NewObject()
${encodes}
	return e
}

func (x ${t.name}) Encode() any { return x.encodeFields() }
`;
    }
    case "union":
      return `${doc}type ${t.name} struct {
	Tag string
${t.variants.map(([k, ty]) => `	${pascal(k)} *${ty}`).join("\n")}
}

func Decode${t.name}(v any, path string, ctx rt.Ctx) (${t.name}, error) {
	var out ${t.name}
	o, err := rt.NewObj(v, path)
	if err != nil {
		return out, err
	}
	rawTag, err := o.Req(${lit(t.tag)})
	if err != nil {
		return out, err
	}
	tag, err := rt.EnumStr(rawTag, rt.Child(path, ${lit(t.tag)}))
	if err != nil {
		return out, err
	}
	out.Tag = tag
	switch tag {
${t.variants
  .map(
    ([k, ty]) => `	case ${lit(k)}:
		x, err := decode${ty}Fields(o, path, ctx)
		if err != nil {
			return ${t.name}{}, err
		}
		out.${pascal(k)} = &x`,
  )
  .join("\n")}
	default:
		return ${t.name}{}, rt.Fail(rt.Child(path, ${lit(t.tag)}), "ir.unknown_variant")
	}
	return out, nil
}

func (x ${t.name}) Encode() any {
	var e *canon.Object
	switch x.Tag {
${t.variants
  .map(
    ([k]) => `	case ${lit(k)}:
		e = x.${pascal(k)}.encodeFields()`,
  )
  .join("\n")}
	default:
		return nil
	}
	e.Set(${lit(t.tag)}, x.Tag)
	return e
}
`;
  }
}

export function emitGo(
  m: Model,
  digests: { family: string; root: string | null; digest: string }[],
  accepted: Accepted[],
): string {
  const roots = digests.filter((d) => d.root);
  const parts: string[] = [];
  parts.push(`// generated by tools/idlgen from schemas/ir/*.idl.json; do not edit
package ir

import (
	"regexp"

	"github.com/insightlabs38-pixel/vsf/backend/canon"
	"github.com/insightlabs38-pixel/vsf/backend/ir/rt"
)

var _ = regexp.MustCompile

const IRVersionText = ${lit(m.irVersionText)}

var IRVersion = ${ver(m.irVersion)}

// Families lists each document family, its root type and the digest of its type closure.
var Families = [][3]string{
${roots.map((d) => `	{${lit(d.family)}, ${lit(d.root as string)}, ${lit(d.digest)}},`).join("\n")}
}

// Accepted lists every version and family digest a reader decodes: all released snapshots and the current schema.
var Accepted = []struct {
	Version [3]uint32
	Family  string
	Digest  string
}{
${accepted.map((a) => `	{${ver(a.version)}, ${lit(a.family)}, ${lit(a.digest)}},`).join("\n")}
}
`);
  for (const name of m.order) parts.push(emitType(m.types.get(name) as TypeDef));
  parts.push(`// Body holds exactly one decoded document root.
type Body struct {
	Family string
${roots.map((d) => `	${pascal(d.family)} *${d.root}`).join("\n")}
}

func decodeBody(family string, v any, path string, ctx rt.Ctx) (Body, error) {
	out := Body{Family: family}
	switch family {
${roots
  .map(
    (d) => `	case ${lit(d.family)}:
		x, err := Decode${d.root}(v, path, ctx)
		if err != nil {
			return Body{}, err
		}
		out.${pascal(d.family)} = &x`,
  )
  .join("\n")}
	default:
		return Body{}, rt.Fail(path, "schema.unknown")
	}
	return out, nil
}

func (b Body) Encode() any {
	switch b.Family {
${roots
  .map(
    (d) => `	case ${lit(d.family)}:
		return b.${pascal(d.family)}.Encode()`,
  )
  .join("\n")}
	}
	return nil
}
`);
  return parts.join("\n");
}
