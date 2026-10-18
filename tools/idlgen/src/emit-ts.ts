import { type Field, type Model, pascal, screamingSnake, type TypeDef, type TypeRef } from "./idl.ts";

const lit = (s: string): string => JSON.stringify(s);
const ver = (v: [number, number, number]): string => `[${v[0]}, ${v[1]}, ${v[2]}]`;

function tsType(t: TypeRef, f?: Field): string {
  switch (t.k) {
    case "bool":
      return "boolean";
    case "named":
      return t.name;
    case "list":
      return `${tsType(t.of)}[]`;
    case "map":
      return `Map<${f?.keys ?? "string"}, ${tsType(t.of)}>`;
  }
}

function decodeExpr(f: Field, src: string, p: string): string {
  const t = f.type;
  const one = (r: TypeRef): string =>
    r.k === "bool" ? "(e, p) => rt.boolean(e, p)" : `(e, p) => decode${(r as { name: string }).name}(e, p, ctx)`;
  switch (t.k) {
    case "bool":
      return `rt.boolean(${src}, ${p})`;
    case "named":
      return `decode${t.name}(${src}, ${p}, ctx)`;
    case "list":
      return `rt.list(${src}, ${p}, ${f.minItems}, ${f.maxItems}, ${one(t.of)})`;
    case "map":
      return `rt.map(${src}, ${p}, ${f.maxItems}, (k) => void decode${f.keys}(k, "", ctx), ${one(t.of)}) as Map<${f.keys}, ${tsType(t.of)}>`;
  }
}

function encodeExpr(t: TypeRef, expr: string): string {
  switch (t.k) {
    case "bool":
      return expr;
    case "named":
      return `encode${t.name}(${expr})`;
    case "list":
      return `${expr}.map((e) => ${encodeExpr(t.of, "e")})`;
    case "map":
      return `new Map<string, Value>([...${expr}].map(([k, e]) => [k, ${encodeExpr(t.of, "e")}] as [string, Value]))`;
  }
}

function emitType(t: TypeDef): string {
  const doc = t.doc ? `/** ${t.doc.replace(/\*\//g, "* /").replace(/\n/g, " ")} */\n` : "";
  switch (t.kind) {
    case "string": {
      const mode = t.text;
      return `${doc}export type ${t.name} = string & { readonly __brand: ${lit(t.name)} };
const ${screamingSnake(t.name)}_RULE: rt.StrRule = { pattern: ${t.pattern === null ? "null" : `new RegExp(${lit(t.pattern)})`}, maxLen: ${t.maxLen}, text: ${lit(mode)} };
export function decode${t.name}(v: Value, path: string, _ctx: Ctx): ${t.name} {
  return rt.string(v, path, ${screamingSnake(t.name)}_RULE) as ${t.name};
}
export function make${t.name}(s: string): ${t.name} {
  rt.checkString(s, "", ${screamingSnake(t.name)}_RULE);
  return s as ${t.name};
}
export function encode${t.name}(x: ${t.name}): Value {
  return x;
}
`;
    }
    case "int":
      return `${doc}export type ${t.name} = number & { readonly __brand: ${lit(t.name)} };
export function decode${t.name}(v: Value, path: string, _ctx: Ctx): ${t.name} {
  return rt.integer(v, path, ${t.min}, ${t.max}) as ${t.name};
}
export function encode${t.name}(x: ${t.name}): Value {
  return x;
}
`;
    case "instant":
      return `${doc}export type ${t.name} = number & { readonly __brand: ${lit(t.name)} };
export function decode${t.name}(v: Value, path: string, _ctx: Ctx): ${t.name} {
  return rt.instant(v, path) as ${t.name};
}
export function encode${t.name}(x: ${t.name}): Value {
  return x;
}
`;
    case "date":
      return `${doc}export type ${t.name} = string & { readonly __brand: ${lit(t.name)} };
export function decode${t.name}(v: Value, path: string, _ctx: Ctx): ${t.name} {
  return rt.date(v, path) as ${t.name};
}
export function encode${t.name}(x: ${t.name}): Value {
  return x;
}
`;
    case "decimal":
      return `${doc}export type ${t.name} = string & { readonly __brand: ${lit(t.name)} };
export function decode${t.name}(v: Value, path: string, _ctx: Ctx): ${t.name} {
  return rt.decimal(v, path, ${t.scale === null ? "null" : t.scale}) as ${t.name};
}
export function encode${t.name}(x: ${t.name}): Value {
  return x;
}
`;
    case "enum":
      return `${doc}export const ${screamingSnake(t.name)} = [${t.values.map(lit).join(", ")}] as const;
export type ${t.name} = (typeof ${screamingSnake(t.name)})[number];
export function decode${t.name}(v: Value, path: string, _ctx: Ctx): ${t.name} {
  const s = rt.enumStr(v, path);
  if (!(${screamingSnake(t.name)} as readonly string[]).includes(s)) throw new rt.IrError(path, "ir.bad_enum");
  return s as ${t.name};
}
export function encode${t.name}(x: ${t.name}): Value {
  return x;
}
`;
    case "struct": {
      const props = t.fields
        .map((f) => `  ${f.name}${f.optional || f.since ? "?" : ""}: ${tsType(f.type, f)};`)
        .join("\n");
      const decodes = t.fields
        .map((f) => {
          const p = `rt.child(path, ${lit(f.name)})`;
          const expr = (src: string): string => decodeExpr(f, src, p);
          if (f.since) {
            return `  const ${f.name}_raw = o.since(${lit(f.name)}, ${ver(f.since)}, ${!f.optional}, ctx);\n  const ${f.name}_ = ${f.name}_raw === undefined ? undefined : ${expr(`${f.name}_raw`)};`;
          }
          if (f.optional) {
            return `  const ${f.name}_raw = o.opt(${lit(f.name)});\n  const ${f.name}_ = ${f.name}_raw === undefined ? undefined : ${expr(`${f.name}_raw`)};`;
          }
          return `  const ${f.name}_ = ${expr(`o.req(${lit(f.name)})`)};`;
        })
        .join("\n");
      const assigns = t.fields
        .map((f) =>
          f.since || f.optional
            ? `  if (${f.name}_ !== undefined) out.${f.name} = ${f.name}_;`
            : `  out.${f.name} = ${f.name}_;`,
        )
        .join("\n");
      const required = t.fields.filter((f) => !f.optional && !f.since);
      const encodes = t.fields
        .map((f) => {
          if (f.optional || f.since) {
            return `  if (x.${f.name} !== undefined) e.set(${lit(f.name)}, ${encodeExpr(f.type, `x.${f.name}`)});`;
          }
          return `  e.set(${lit(f.name)}, ${encodeExpr(f.type, `x.${f.name}`)});`;
        })
        .join("\n");
      return `${doc}export interface ${t.name} {
${props}
}
export function decode${t.name}Fields(o: rt.Obj, ctx: Ctx): ${t.name} {
  const path = o.path;
${decodes}
  o.finish();
  const out = {} as ${t.name};
${required.length ? "" : ""}${assigns}
  return out;
}
export function decode${t.name}(v: Value, path: string, ctx: Ctx): ${t.name} {
  return decode${t.name}Fields(rt.Obj.of(v, path), ctx);
}
export function encode${t.name}Fields(x: ${t.name}): Map<string, Value> {
  const e = new Map<string, Value>();
${encodes}
  return e;
}
export function encode${t.name}(x: ${t.name}): Value {
  return encode${t.name}Fields(x);
}
`;
    }
    case "union": {
      const variants = t.variants.map(([k, ty]) => `({ ${t.tag}: ${lit(k)} } & ${ty})`).join("\n  | ");
      return `${doc}export type ${t.name} =
  | ${variants};
export function decode${t.name}(v: Value, path: string, ctx: Ctx): ${t.name} {
  const o = rt.Obj.of(v, path);
  const tag = rt.enumStr(o.req(${lit(t.tag)}), rt.child(path, ${lit(t.tag)}));
  switch (tag) {
${t.variants.map(([k, ty]) => `    case ${lit(k)}:\n      return { ${t.tag}: ${lit(k)}, ...decode${ty}Fields(o, ctx) };`).join("\n")}
    default:
      throw new rt.IrError(rt.child(path, ${lit(t.tag)}), "ir.unknown_variant");
  }
}
export function encode${t.name}(x: ${t.name}): Value {
  switch (x.${t.tag}) {
${t.variants.map(([k, ty]) => `    case ${lit(k)}: {\n      const { ${t.tag}: _tag, ...rest } = x;\n      const e = encode${ty}Fields(rest as ${ty});\n      e.set(${lit(t.tag)}, ${lit(k)});\n      return e;\n    }`).join("\n")}
  }
}
`;
    }
  }
}

export function emitTs(m: Model, digests: { family: string; root: string | null; digest: string }[]): string {
  const roots = digests.filter((d) => d.root);
  const parts: string[] = [];
  parts.push(`// generated by tools/idlgen from schemas/ir/*.idl.json; do not edit
import type { Value } from "@vsf/schema";
import * as rt from "./rt.ts";
import type { Ctx } from "./rt.ts";

export const IR_VERSION: rt.Version = ${ver(m.irVersion)};
export const IR_VERSION_TEXT = ${lit(m.irVersionText)};
export const FAMILIES: readonly (readonly [string, string, string])[] = [
${roots.map((d) => `  [${lit(d.family)}, ${lit(d.root as string)}, ${lit(d.digest)}],`).join("\n")}
];
`);
  for (const name of m.order) parts.push(emitType(m.types.get(name) as TypeDef));
  parts.push(`export type Body =
${roots.map((d) => `  | { family: ${lit(d.family)}; body: ${d.root} }`).join("\n")};

export function decodeBody(family: string, v: Value, path: string, ctx: Ctx): Body {
  switch (family) {
${roots.map((d) => `    case ${lit(d.family)}:\n      return { family: ${lit(d.family)}, body: decode${d.root}(v, path, ctx) };`).join("\n")}
    default:
      throw new rt.IrError(path, "schema.unknown");
  }
}

export function encodeBody(b: Body): Value {
  switch (b.family) {
${roots.map((d) => `    case ${lit(d.family)}:\n      return encode${d.root}(b.body);`).join("\n")}
  }
}
`);
  void pascal;
  return parts.join("\n");
}
