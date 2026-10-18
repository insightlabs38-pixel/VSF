import { type Field, type Model, pascal, type TypeDef, type TypeRef } from "./idl.ts";

const KEYWORDS = new Set([
  "type",
  "match",
  "struct",
  "enum",
  "fn",
  "mod",
  "use",
  "impl",
  "trait",
  "ref",
  "move",
  "loop",
  "in",
  "as",
  "where",
  "self",
  "super",
  "crate",
  "dyn",
  "static",
  "const",
  "let",
  "mut",
  "pub",
  "return",
  "if",
  "else",
  "for",
  "while",
  "break",
  "continue",
  "true",
  "false",
  "unsafe",
  "async",
  "await",
  "box",
  "do",
  "final",
  "macro",
  "override",
  "priv",
  "try",
  "typeof",
  "unsized",
  "virtual",
  "yield",
]);

export const rustField = (n: string): string => (KEYWORDS.has(n) ? `r#${n}` : n);
const variant = (v: string): string => pascal(v);
const lit = (s: string): string => JSON.stringify(s);
const ver = (v: [number, number, number]): string => `(${v[0]}, ${v[1]}, ${v[2]})`;

function rustType(t: TypeRef, f?: Field): string {
  switch (t.k) {
    case "bool":
      return "bool";
    case "named":
      return t.name;
    case "list":
      return `Vec<${rustType(t.of)}>`;
    case "map":
      return `BTreeMap<${f?.keys ?? "String"}, ${rustType(t.of)}>`;
  }
}

function decodeExpr(f: Field, m: Model, src: string, pathVar: string): string {
  const t = f.type;
  const elem = (r: TypeRef, e: string, p: string): string => {
    if (r.k === "bool") return `rt::boolean(${e}, ${p})?`;
    if (r.k === "named") return `${r.name}::decode(${e}, ${p}, ctx)?`;
    throw new Error("nested containers are not supported");
  };
  switch (t.k) {
    case "bool":
      return `rt::boolean(${src}, ${pathVar})?`;
    case "named":
      return `${t.name}::decode(${src}, ${pathVar}, ctx)?`;
    case "list": {
      const inner = t.of;
      const call =
        inner.k === "bool"
          ? "|e, p| rt::boolean(e, p)"
          : `|e, p| ${(inner as { name: string }).name}::decode(e, p, ctx)`;
      return `rt::list(${src}, ${pathVar}, ${f.minItems}, ${f.maxItems}, ${call})?`;
    }
    case "map": {
      const key = f.keys as string;
      void elem;
      void m;
      return `rt::map(${src}, ${pathVar}, ${f.maxItems}, |k| ${key}::new(k), |e, p| ${(t.of as { name: string }).name}::decode(e, p, ctx))?`;
    }
  }
}

function encodeExpr(t: TypeRef, expr: string): string {
  switch (t.k) {
    case "bool":
      return `Value::Bool(${expr}.to_owned())`;
    case "named":
      return `${expr}.encode()`;
    case "list":
      return `Value::Array(${expr}.iter().map(|e| ${encodeExpr(t.of, "e")}).collect())`;
    case "map":
      return `Value::Object(${expr}.iter().map(|(k, e)| (k.as_str().to_string(), ${encodeExpr(t.of, "e")})).collect())`;
  }
}

function emitType(t: TypeDef, m: Model): string {
  const doc = t.doc ? `/// ${t.doc.replace(/\n/g, " ")}\n` : "";
  switch (t.kind) {
    case "string": {
      const rule = `static ${t.name.toUpperCase()}_RULE: StrRule = StrRule { pattern: ${t.pattern === null ? "None" : `Some(${lit(t.pattern)})`}, max_len: ${t.maxLen}, text: TextMode::${t.text === "none" ? "None" : t.text === "label" ? "Label" : "Multiline"} };`;
      return `${rule}
${doc}#[derive(Debug, Clone, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub struct ${t.name}(String);
impl ${t.name} {
    pub fn new(s: &str) -> Res<Self> {
        rt::check_string(s, "", &${t.name.toUpperCase()}_RULE)?;
        Ok(Self(s.to_string()))
    }
    pub fn as_str(&self) -> &str {
        &self.0
    }
    pub fn decode(v: &Value, path: &str, _ctx: &Ctx) -> Res<Self> {
        Ok(Self(rt::string(v, path, &${t.name.toUpperCase()}_RULE)?))
    }
    pub fn encode(&self) -> Value {
        Value::Str(self.0.clone())
    }
}
`;
    }
    case "int":
      return `${doc}#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub struct ${t.name}(pub i64);
impl ${t.name} {
    pub const MIN: i64 = ${t.min};
    pub const MAX: i64 = ${t.max};
    pub fn decode(v: &Value, path: &str, _ctx: &Ctx) -> Res<Self> {
        Ok(Self(rt::integer(v, path, Self::MIN, Self::MAX)?))
    }
    pub fn encode(&self) -> Value {
        Value::Int(self.0)
    }
}
`;
    case "instant":
      return `${doc}#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub struct ${t.name}(pub i64);
impl ${t.name} {
    pub fn decode(v: &Value, path: &str, _ctx: &Ctx) -> Res<Self> {
        Ok(Self(rt::instant(v, path)?))
    }
    pub fn encode(&self) -> Value {
        Value::Int(self.0)
    }
}
`;
    case "date":
      return `${doc}#[derive(Debug, Clone, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub struct ${t.name}(String);
impl ${t.name} {
    pub fn as_str(&self) -> &str {
        &self.0
    }
    pub fn decode(v: &Value, path: &str, _ctx: &Ctx) -> Res<Self> {
        Ok(Self(rt::date(v, path)?))
    }
    pub fn encode(&self) -> Value {
        Value::Str(self.0.clone())
    }
}
`;
    case "decimal":
      return `${doc}#[derive(Debug, Clone, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub struct ${t.name}(String);
impl ${t.name} {
    pub fn as_str(&self) -> &str {
        &self.0
    }
    pub fn decode(v: &Value, path: &str, _ctx: &Ctx) -> Res<Self> {
        Ok(Self(rt::decimal(v, path, ${t.scale === null ? "None" : `Some(${t.scale})`})?))
    }
    pub fn encode(&self) -> Value {
        Value::Str(self.0.clone())
    }
}
`;
    case "enum": {
      const names = t.values.map(variant);
      return `${doc}#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub enum ${t.name} {
${names.map((n) => `    ${n},`).join("\n")}
}
impl ${t.name} {
    pub const ALL: &'static [Self] = &[${names.map((n) => `Self::${n}`).join(", ")}];
    pub fn as_str(self) -> &'static str {
        match self {
${t.values.map((v, i) => `            Self::${names[i]} => ${lit(v)},`).join("\n")}
        }
    }
    pub fn parse(s: &str) -> Option<Self> {
        match s {
${t.values.map((v, i) => `            ${lit(v)} => Some(Self::${names[i]}),`).join("\n")}
            _ => None,
        }
    }
    pub fn decode(v: &Value, path: &str, _ctx: &Ctx) -> Res<Self> {
        Self::parse(rt::enum_str(v, path)?).ok_or_else(|| DecodeError::new(path, "ir.bad_enum"))
    }
    pub fn encode(self) -> Value {
        Value::Str(self.as_str().to_string())
    }
}
`;
    }
    case "struct": {
      const fieldsDecl = t.fields
        .map((f) => {
          const base = rustType(f.type, f);
          const ty = f.optional || f.since ? `Option<${base}>` : base;
          return `    pub ${rustField(f.name)}: ${ty},`;
        })
        .join("\n");
      const decodes = t.fields
        .map((f) => {
          const id = rustField(f.name);
          const p = `&rt::child(&path, ${lit(f.name)})`;
          const expr = (src: string): string => decodeExpr(f, m, src, p);
          if (f.since) {
            return `        let ${id} = match o.since(${lit(f.name)}, ${ver(f.since)}, ${!f.optional}, ctx)? {
            Some(v) => Some(${expr("v")}),
            None => None,
        };`;
          }
          if (f.optional) {
            return `        let ${id} = match o.opt(${lit(f.name)}) {
            Some(v) => Some(${expr("v")}),
            None => None,
        };`;
          }
          return `        let ${id} = ${expr(`o.req(${lit(f.name)})?`)};`;
        })
        .join("\n");
      const encodes = t.fields
        .map((f) => {
          const id = rustField(f.name);
          if (f.optional || f.since) {
            return `        if let Some(x) = &self.${id} {
            f.push((${lit(f.name)}.to_string(), ${encodeExpr(f.type, "x")}));
        }`;
          }
          return `        f.push((${lit(f.name)}.to_string(), ${encodeExpr(f.type, `self.${id}`)}));`;
        })
        .join("\n");
      return `${doc}#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ${t.name} {
${fieldsDecl}
}
impl ${t.name} {
    pub fn decode(v: &Value, path: &str, ctx: &Ctx) -> Res<Self> {
        Self::decode_fields(rt::Obj::new(v, path)?, ctx)
    }
    pub(crate) fn decode_fields(mut o: rt::Obj<'_>, ctx: &Ctx) -> Res<Self> {
        let path = o.path().to_string();
${decodes}
        o.finish()?;
        Ok(Self { ${t.fields.map((f) => rustField(f.name)).join(", ")} })
    }
    pub(crate) fn encode_fields(&self) -> Vec<(String, Value)> {
        let mut f: Vec<(String, Value)> = Vec::new();
${encodes}
        f
    }
    pub fn encode(&self) -> Value {
        Value::Object(self.encode_fields())
    }
}
`;
    }
    case "union": {
      const names = t.variants.map(([k]) => variant(k));
      return `${doc}#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ${t.name} {
${t.variants.map(([, ty], i) => `    ${names[i]}(${ty}),`).join("\n")}
}
impl ${t.name} {
    pub fn tag(&self) -> &'static str {
        match self {
${t.variants.map(([k], i) => `            Self::${names[i]}(_) => ${lit(k)},`).join("\n")}
        }
    }
    pub fn decode(v: &Value, path: &str, ctx: &Ctx) -> Res<Self> {
        let mut o = rt::Obj::new(v, path)?;
        let tag = rt::enum_str(o.req(${lit(t.tag)})?, &rt::child(path, ${lit(t.tag)}))?;
        match tag {
${t.variants.map(([k, ty], i) => `            ${lit(k)} => Ok(Self::${names[i]}(${ty}::decode_fields(o, ctx)?)),`).join("\n")}
            _ => Err(DecodeError::new(&rt::child(path, ${lit(t.tag)}), "ir.unknown_variant")),
        }
    }
    pub fn encode(&self) -> Value {
        let mut f = match self {
${t.variants.map(([,], i) => `            Self::${names[i]}(x) => x.encode_fields(),`).join("\n")}
        };
        f.push((${lit(t.tag)}.to_string(), Value::Str(self.tag().to_string())));
        Value::Object(f)
    }
}
`;
    }
  }
}

export function emitRust(m: Model, digests: { family: string; root: string | null; digest: string }[]): string {
  const parts: string[] = [];
  parts.push(`// generated by tools/idlgen from schemas/ir/*.idl.json; do not edit
#![allow(clippy::all, clippy::pedantic, clippy::unwrap_used, clippy::expect_used, unused_variables)]
use crate::rt::{self, Ctx, DecodeError, Res, StrRule, TextMode};
use std::collections::BTreeMap;
use vsf_canon::Value;

pub const IR_VERSION: (u32, u32, u32) = ${ver(m.irVersion)};
pub const IR_VERSION_TEXT: &str = ${lit(m.irVersionText)};
pub const FAMILIES: &[(&str, &str, &str)] = &[
${digests
  .filter((d) => d.root)
  .map((d) => `    (${lit(d.family)}, ${lit(d.root as string)}, ${lit(d.digest)}),`)
  .join("\n")}
];
`);
  for (const name of m.order) parts.push(emitType(m.types.get(name) as TypeDef, m));
  const roots = digests.filter((d) => d.root);
  parts.push(`#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Body {
${roots.map((d) => `    ${pascal(d.family)}(${d.root}),`).join("\n")}
}
impl Body {
    pub fn family(&self) -> &'static str {
        match self {
${roots.map((d) => `            Self::${pascal(d.family)}(_) => ${lit(d.family)},`).join("\n")}
        }
    }
    pub fn encode(&self) -> Value {
        match self {
${roots.map((d) => `            Self::${pascal(d.family)}(x) => x.encode(),`).join("\n")}
        }
    }
    pub fn decode(family: &str, v: &Value, path: &str, ctx: &Ctx) -> Res<Self> {
        match family {
${roots.map((d) => `            ${lit(d.family)} => Ok(Self::${pascal(d.family)}(${d.root}::decode(v, path, ctx)?)),`).join("\n")}
            _ => Err(DecodeError::new(path, "schema.unknown")),
        }
    }
}
`);
  return parts.join("\n");
}
