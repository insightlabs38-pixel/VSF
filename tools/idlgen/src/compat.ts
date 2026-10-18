import { cpSync, existsSync, mkdtempSync, readdirSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { type Field, loadModel, type Model, type TypeDef, type TypeRef, type Version } from "./idl.ts";

export interface Finding {
  level: "breaking" | "additive";
  code: string;
  where: string;
  detail: string;
}

export interface Verdict {
  findings: Finding[];
  errors: { code: string; where: string; detail: string }[];
}

const tref = (t: TypeRef): string => (t.k === "bool" ? "bool" : t.k === "named" ? t.name : `${t.k}<${tref(t.of)}>`);
const vtext = (v: Version | null): string => (v ? v.join(".") : "-");
const cmp = (a: Version, b: Version): number => a[0] - b[0] || a[1] - b[1] || a[2] - b[2];

function fieldFindings(owner: string, a: Field, b: Field, out: Finding[]): void {
  const where = `${owner}.${a.name}`;
  const breaking = (code: string, detail: string) => out.push({ level: "breaking", code, where, detail });
  const additive = (code: string, detail: string) => out.push({ level: "additive", code, where, detail });
  if (tref(a.type) !== tref(b.type)) breaking("idl.field_type_changed", `${tref(a.type)} -> ${tref(b.type)}`);
  if (a.keys !== b.keys) breaking("idl.keys_changed", `${a.keys} -> ${b.keys}`);
  if (a.optional && !b.optional) breaking("idl.field_became_required", "");
  if (!a.optional && b.optional) additive("idl.field_became_optional", "");
  if (vtext(a.since) !== vtext(b.since)) breaking("idl.since_changed", `${vtext(a.since)} -> ${vtext(b.since)}`);
  if (b.minItems > a.minItems) breaking("idl.items_narrowed", `minItems ${a.minItems} -> ${b.minItems}`);
  if (b.minItems < a.minItems) additive("idl.items_widened", `minItems ${a.minItems} -> ${b.minItems}`);
  if (b.maxItems < a.maxItems) breaking("idl.items_narrowed", `maxItems ${a.maxItems} -> ${b.maxItems}`);
  if (b.maxItems > a.maxItems) additive("idl.items_widened", `maxItems ${a.maxItems} -> ${b.maxItems}`);
}

function typeFindings(a: TypeDef, b: TypeDef, out: Finding[]): void {
  const where = a.name;
  const breaking = (code: string, detail: string) => out.push({ level: "breaking", code, where, detail });
  const additive = (code: string, detail: string) => out.push({ level: "additive", code, where, detail });
  if (a.kind !== b.kind) {
    breaking("idl.kind_changed", `${a.kind} -> ${b.kind}`);
    return;
  }
  switch (a.kind) {
    case "string": {
      const s = b as typeof a;
      if (a.pattern !== s.pattern) breaking("idl.pattern_changed", `${a.pattern} -> ${s.pattern}`);
      if (a.text !== s.text) breaking("idl.text_mode_changed", `${a.text} -> ${s.text}`);
      if (s.minLen > a.minLen) breaking("idl.length_narrowed", `minLen ${a.minLen} -> ${s.minLen}`);
      if (s.minLen < a.minLen) additive("idl.length_widened", `minLen ${a.minLen} -> ${s.minLen}`);
      if (s.maxLen < a.maxLen) breaking("idl.length_narrowed", `maxLen ${a.maxLen} -> ${s.maxLen}`);
      if (s.maxLen > a.maxLen) additive("idl.length_widened", `maxLen ${a.maxLen} -> ${s.maxLen}`);
      break;
    }
    case "int": {
      const s = b as typeof a;
      if (s.min > a.min || s.max < a.max)
        breaking("idl.int_range_narrowed", `[${a.min},${a.max}] -> [${s.min},${s.max}]`);
      if (s.min < a.min || s.max > a.max)
        additive("idl.int_range_widened", `[${a.min},${a.max}] -> [${s.min},${s.max}]`);
      break;
    }
    case "decimal":
      if (a.scale !== (b as typeof a).scale) breaking("idl.scale_changed", `${a.scale} -> ${(b as typeof a).scale}`);
      break;
    case "enum": {
      const s = b as typeof a;
      for (const v of a.values) if (!s.values.includes(v)) breaking("idl.enum_value_removed", v);
      for (const v of s.values) if (!a.values.includes(v)) additive("idl.enum_value_added", v);
      break;
    }
    case "struct": {
      const s = b as typeof a;
      const old = new Map(a.fields.map((f) => [f.name, f]));
      const now = new Map(s.fields.map((f) => [f.name, f]));
      for (const [name, f] of old) {
        const g = now.get(name);
        if (!g) out.push({ level: "breaking", code: "idl.field_removed", where: `${where}.${name}`, detail: "" });
        else fieldFindings(where, f, g, out);
      }
      for (const [name, g] of now) {
        if (old.has(name)) continue;
        const at = `${where}.${name}`;
        if (!g.optional && !g.since)
          out.push({ level: "breaking", code: "idl.required_field_added", where: at, detail: "" });
        else
          out.push({
            level: "additive",
            code: "idl.field_added",
            where: at,
            detail: g.since ? `since ${vtext(g.since)}` : "optional",
          });
      }
      break;
    }
    case "union": {
      const s = b as typeof a;
      if (a.tag !== s.tag) breaking("idl.tag_changed", `${a.tag} -> ${s.tag}`);
      const now = new Map(s.variants);
      for (const [tag, target] of a.variants) {
        if (!now.has(tag)) breaking("idl.variant_removed", tag);
        else if (now.get(tag) !== target) breaking("idl.variant_retargeted", `${tag}: ${target} -> ${now.get(tag)}`);
      }
      for (const [tag] of s.variants) if (!a.variants.some(([t]) => t === tag)) additive("idl.variant_added", tag);
      break;
    }
    default:
      break;
  }
}

/** Classifies every difference and applies the version-bump rules from ADR-027. */
export function compare(oldM: Model, newM: Model): Verdict {
  const findings: Finding[] = [];
  for (const [name, a] of oldM.types) {
    const b = newM.types.get(name);
    if (!b) findings.push({ level: "breaking", code: "idl.type_removed", where: name, detail: "" });
    else typeFindings(a, b, findings);
  }
  for (const name of newM.types.keys()) {
    if (!oldM.types.has(name)) findings.push({ level: "additive", code: "idl.type_added", where: name, detail: "" });
  }
  const roots = (m: Model) => new Map(m.families.map((f) => [f.family, f.root]));
  const oldRoots = roots(oldM);
  for (const [family, root] of roots(newM)) {
    if (oldRoots.has(family) && oldRoots.get(family) !== root)
      findings.push({
        level: "breaking",
        code: "idl.root_changed",
        where: family,
        detail: `${oldRoots.get(family)} -> ${root}`,
      });
  }
  for (const family of oldRoots.keys()) {
    if (!roots(newM).has(family))
      findings.push({ level: "breaking", code: "idl.family_removed", where: family, detail: "" });
  }

  const errors: Verdict["errors"] = [];
  const [o, n] = [oldM.irVersion, newM.irVersion];
  const breaking = findings.filter((f) => f.level === "breaking");
  const additive = findings.filter((f) => f.level === "additive");
  const c = cmp(n, o);
  if (c < 0) errors.push({ code: "idl.version_regressed", where: "ir_version", detail: `${vtext(o)} -> ${vtext(n)}` });
  else if (breaking.length > 0 && n[0] === o[0])
    errors.push({
      code: "idl.breaking_without_major",
      where: "ir_version",
      detail: `${breaking.length} breaking change(s) need a new major, found ${vtext(n)}`,
    });
  else if (additive.length > 0 && c === 0)
    errors.push({
      code: "idl.change_without_version_bump",
      where: "ir_version",
      detail: `${additive.length} additive change(s) need a minor bump`,
    });
  if (n[0] === o[0] && c > 0) {
    for (const [name, t] of newM.types) {
      const prior = oldM.types.get(name);
      if (t.kind !== "struct" || !prior || prior.kind !== "struct") continue;
      for (const f of t.fields) {
        if (prior.fields.some((p) => p.name === f.name)) continue;
        if (!f.since || cmp(f.since, o) <= 0)
          errors.push({
            code: "idl.added_field_without_since",
            where: `${name}.${f.name}`,
            detail: `a field added in ${vtext(n)} must declare since > ${vtext(o)}`,
          });
      }
    }
  }
  return { findings, errors };
}

/** The highest released snapshot under `<schemaDir>/released/<version>/`, if any. */
export function latestReleased(schemaDir: string): { version: Version; dir: string } | null {
  const root = join(schemaDir, "released");
  if (!existsSync(root)) return null;
  const versions = readdirSync(root)
    .filter((d) => /^\d+\.\d+\.\d+$/.test(d))
    .map((d) => d.split(".").map(Number) as Version)
    .sort(cmp);
  const v = versions[versions.length - 1];
  return v ? { version: v, dir: join(root, v.join(".")) } : null;
}

export function checkAgainstReleased(schemaDir: string): Verdict & { baseline: string | null } {
  const rel = latestReleased(schemaDir);
  if (!rel) return { findings: [], errors: [], baseline: null };
  const work = mkdtempSync(join(tmpdir(), "idl-compat-"));
  cpSync(rel.dir, work, { recursive: true });
  return { ...compare(loadModel(work), loadModel(schemaDir)), baseline: vtext(rel.version) };
}
