import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";

export type Version = [number, number, number];

export type TypeRef =
  | { k: "bool" }
  | { k: "named"; name: string }
  | { k: "list"; of: TypeRef }
  | { k: "map"; of: TypeRef };

export interface Field {
  name: string;
  type: TypeRef;
  optional: boolean;
  since: Version | null;
  minItems: number;
  maxItems: number;
  keys: string | null;
  doc: string | null;
}

export type TypeDef =
  | {
      kind: "string";
      name: string;
      family: string;
      pattern: string | null;
      minLen: number;
      maxLen: number;
      text: "none" | "label" | "multiline";
      doc: string | null;
    }
  | { kind: "int"; name: string; family: string; min: number; max: number; doc: string | null }
  | { kind: "instant"; name: string; family: string; doc: string | null }
  | { kind: "date"; name: string; family: string; doc: string | null }
  | { kind: "decimal"; name: string; family: string; scale: number | null; doc: string | null }
  | { kind: "enum"; name: string; family: string; values: string[]; doc: string | null }
  | { kind: "struct"; name: string; family: string; fields: Field[]; doc: string | null }
  | { kind: "union"; name: string; family: string; tag: string; variants: [string, string][]; doc: string | null };

export interface Family {
  family: string;
  root: string | null;
  raw: Record<string, unknown>;
}

export interface Model {
  irVersion: Version;
  irVersionText: string;
  families: Family[];
  types: Map<string, TypeDef>;
  raw: Map<string, unknown>;
  order: string[];
}

const FAMILY_ORDER = ["common", "environment", "observation", "semantic", "operation", "presentation", "version"];
const NAME = /^[A-Z][A-Za-z0-9]*$/;
const FIELD = /^[a-z][a-z0-9_]*$/;

export class IdlError extends Error {}

export function parseVersion(s: string): Version {
  const m = /^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$/.exec(s);
  if (!m) throw new IdlError(`bad version ${s}`);
  return [Number(m[1]), Number(m[2]), Number(m[3])];
}

function parseTypeRef(text: string, where: string): TypeRef {
  const m = /^(list|map)<(.+)>$/.exec(text);
  if (m) return { k: m[1] as "list" | "map", of: parseTypeRef(m[2] as string, where) };
  if (text === "bool") return { k: "bool" };
  if (!NAME.test(text)) throw new IdlError(`${where}: bad type expression ${text}`);
  return { k: "named", name: text };
}

type Raw = Record<string, unknown>;
const str = (v: unknown): string | null => (typeof v === "string" ? v : null);
const num = (v: unknown, d: number): number => (typeof v === "number" ? v : d);

export function loadModel(dir: string): Model {
  const files = readdirSync(dir).filter((f) => f.endsWith(".idl.json"));
  const byFamily = new Map<string, Raw>();
  for (const f of files) {
    const raw = JSON.parse(readFileSync(join(dir, f), "utf8")) as Raw;
    byFamily.set(String(raw.family), raw);
  }
  for (const fam of byFamily.keys()) if (!FAMILY_ORDER.includes(fam)) throw new IdlError(`unknown family ${fam}`);
  const ordered = FAMILY_ORDER.filter((f) => byFamily.has(f));
  return buildModel(ordered.map((f) => byFamily.get(f) as Raw));
}

export function buildModel(docs: Raw[]): Model {
  let irVersionText = "";
  const types = new Map<string, TypeDef>();
  const raw = new Map<string, unknown>();
  const order: string[] = [];
  const families: Family[] = [];
  for (const doc of docs) {
    if (doc.idl !== "vsf-idl/1") throw new IdlError(`unsupported idl ${String(doc.idl)}`);
    const v = String(doc.ir_version);
    if (irVersionText && irVersionText !== v) throw new IdlError("families disagree on ir_version");
    irVersionText = v;
    const family = String(doc.family);
    families.push({ family, root: str(doc.root), raw: doc });
    for (const [name, rawDef] of Object.entries(doc.types as Record<string, Raw>)) {
      if (!NAME.test(name)) throw new IdlError(`bad type name ${name}`);
      if (types.has(name)) throw new IdlError(`duplicate type ${name}`);
      types.set(name, parseDef(name, family, rawDef));
      raw.set(name, rawDef);
      order.push(name);
    }
  }
  const model: Model = { irVersion: parseVersion(irVersionText), irVersionText, families, types, raw, order };
  validate(model);
  return model;
}

function parseDef(name: string, family: string, d: Raw): TypeDef {
  const doc = str(d.doc);
  switch (d.kind) {
    case "string": {
      const text = (str(d.text) ?? "none") as "none" | "label" | "multiline";
      if (!["none", "label", "multiline"].includes(text)) throw new IdlError(`${name}: bad text mode`);
      return {
        kind: "string",
        name,
        family,
        pattern: str(d.pattern),
        minLen: num(d.minLen, 0),
        maxLen: num(d.maxLen, 256),
        text,
        doc,
      };
    }
    case "int":
      return {
        kind: "int",
        name,
        family,
        min: num(d.min, Number.MIN_SAFE_INTEGER),
        max: num(d.max, Number.MAX_SAFE_INTEGER),
        doc,
      };
    case "instant":
      return { kind: "instant", name, family, doc };
    case "date":
      return { kind: "date", name, family, doc };
    case "decimal":
      return { kind: "decimal", name, family, scale: typeof d.scale === "number" ? d.scale : null, doc };
    case "enum": {
      const values = d.values as string[];
      if (!Array.isArray(values) || values.length === 0 || new Set(values).size !== values.length)
        throw new IdlError(`${name}: bad enum values`);
      for (const v of values) if (!/^[a-z][a-z0-9_]*$/.test(v)) throw new IdlError(`${name}: bad enum value ${v}`);
      return { kind: "enum", name, family, values, doc };
    }
    case "struct": {
      const fields: Field[] = Object.entries(d.fields as Record<string, Raw>).map(([fname, f]) => {
        if (!FIELD.test(fname)) throw new IdlError(`${name}.${fname}: bad field name`);
        const type = parseTypeRef(String(f.type), `${name}.${fname}`);
        return {
          name: fname,
          type,
          optional: f.optional === true,
          since: typeof f.since === "string" ? parseVersion(f.since) : null,
          minItems: num(f.minItems, 0),
          maxItems: num(f.maxItems, 1024),
          keys: str(f.keys),
          doc: str(f.doc),
        };
      });
      return { kind: "struct", name, family, fields, doc };
    }
    case "union": {
      const variants = Object.entries(d.variants as Record<string, string>);
      if (variants.length === 0) throw new IdlError(`${name}: union without variants`);
      return { kind: "union", name, family, tag: String(d.tag), variants, doc };
    }
    default:
      throw new IdlError(`${name}: unknown kind ${String(d.kind)}`);
  }
}

function refNames(t: TypeRef): string[] {
  return t.k === "named" ? [t.name] : t.k === "bool" ? [] : refNames(t.of);
}

function validate(m: Model): void {
  const need = (name: string, where: string): TypeDef => {
    const t = m.types.get(name);
    if (!t) throw new IdlError(`${where}: unknown type ${name}`);
    return t;
  };
  for (const t of m.types.values()) {
    if (t.kind === "string" && t.pattern !== null) new RegExp(t.pattern);
    if (t.kind === "decimal" && t.scale !== null && (t.scale < 0 || t.scale > 18))
      throw new IdlError(`${t.name}: bad scale`);
    if (t.kind === "struct") {
      const seen = new Set<string>();
      for (const f of t.fields) {
        if (seen.has(f.name)) throw new IdlError(`${t.name}: duplicate field ${f.name}`);
        seen.add(f.name);
        for (const r of refNames(f.type)) need(r, `${t.name}.${f.name}`);
        if (f.type.k === "map") {
          if (!f.keys) throw new IdlError(`${t.name}.${f.name}: map needs keys`);
          if (need(f.keys, `${t.name}.${f.name}`).kind !== "string")
            throw new IdlError(`${t.name}.${f.name}: map keys must be a string type`);
        }
        if (f.since && f.since[0] !== m.irVersion[0])
          throw new IdlError(`${t.name}.${f.name}: since must share the major version`);
        if (
          f.since &&
          (f.since[1] > m.irVersion[1] || (f.since[1] === m.irVersion[1] && f.since[2] > m.irVersion[2]))
        ) {
          throw new IdlError(`${t.name}.${f.name}: since is newer than ir_version`);
        }
      }
    }
    if (t.kind === "union") {
      for (const [, v] of t.variants) {
        const target = need(v, `${t.name} variant`);
        if (target.kind !== "struct") throw new IdlError(`${t.name}: variant ${v} must be a struct`);
        if (target.fields.some((f) => f.name === t.tag))
          throw new IdlError(`${t.name}: variant ${v} declares the tag field`);
      }
    }
  }
  const state = new Map<string, number>();
  const visit = (name: string, trail: string[]): void => {
    if (state.get(name) === 2) return;
    if (state.get(name) === 1) throw new IdlError(`recursive type: ${[...trail, name].join(" -> ")}`);
    state.set(name, 1);
    const t = m.types.get(name) as TypeDef;
    const deps: string[] = [];
    if (t.kind === "struct") for (const f of t.fields) deps.push(...refNames(f.type));
    if (t.kind === "union") for (const [, v] of t.variants) deps.push(v);
    for (const d of deps) visit(d, [...trail, name]);
    state.set(name, 2);
  };
  for (const name of m.types.keys()) visit(name, []);
  for (const f of m.families)
    if (f.root && !m.types.has(f.root)) throw new IdlError(`family ${f.family}: unknown root ${f.root}`);
}

/** Types reachable from a root, in model order. */
export function closure(m: Model, root: string): string[] {
  const seen = new Set<string>();
  const walk = (name: string): void => {
    if (seen.has(name)) return;
    seen.add(name);
    const t = m.types.get(name) as TypeDef;
    if (t.kind === "struct") for (const f of t.fields) refNames(f.type).forEach(walk);
    if (t.kind === "union") for (const [, v] of t.variants) walk(v);
    if (t.kind === "struct") for (const f of t.fields) if (f.keys) walk(f.keys);
  };
  walk(root);
  return m.order.filter((n) => seen.has(n));
}

export function pascal(snake: string): string {
  return snake
    .split(/[_-]/)
    .map((p) => (p === "id" ? "ID" : p.charAt(0).toUpperCase() + p.slice(1)))
    .join("");
}

export function camel(snake: string): string {
  const p = pascal(snake);
  return p.charAt(0).toLowerCase() + p.slice(1);
}

export function screamingSnake(name: string): string {
  return name.replace(/([a-z0-9])([A-Z])/g, "$1_$2").toUpperCase();
}
