import type { Value } from "@vsf/schema";

export type Version = readonly [number, number, number];

export class IrError extends Error {
  readonly code: string;
  readonly path: string;
  constructor(path: string, code: string) {
    super(`${code} at ${path === "" ? "/" : path}`);
    this.name = "IrError";
    this.code = code;
    this.path = path;
  }
}

export interface Ctx {
  version: Version;
}

export const child = (path: string, key: string): string => `${path}/${key}`;

export function cmpVersion(a: Version, b: Version): number {
  for (let i = 0; i < 3; i++) {
    const d = (a[i] as number) - (b[i] as number);
    if (d !== 0) return d < 0 ? -1 : 1;
  }
  return 0;
}

export class Obj {
  private readonly used = new Set<string>();
  private readonly map: ReadonlyMap<string, Value>;
  readonly path: string;
  constructor(map: ReadonlyMap<string, Value>, path: string) {
    this.map = map;
    this.path = path;
  }

  static of(v: Value, path: string): Obj {
    if (!(v instanceof Map)) throw new IrError(path, "ir.not_object");
    return new Obj(v, path);
  }

  private find(key: string): Value | undefined {
    if (!this.map.has(key)) return undefined;
    this.used.add(key);
    return this.map.get(key);
  }

  req(key: string): Value {
    const v = this.find(key);
    if (v === undefined) throw new IrError(child(this.path, key), "ir.missing_field");
    return v;
  }

  opt(key: string): Value | undefined {
    return this.find(key);
  }

  since(key: string, since: Version, required: boolean, ctx: Ctx): Value | undefined {
    const found = this.find(key);
    if (cmpVersion(ctx.version, since) < 0) {
      if (found !== undefined) throw new IrError(child(this.path, key), "ir.field_from_future");
      return undefined;
    }
    if (found === undefined && required) throw new IrError(child(this.path, key), "ir.missing_field");
    return found;
  }

  finish(): void {
    for (const k of this.map.keys()) {
      if (!this.used.has(k)) throw new IrError(child(this.path, k), "ir.unknown_field");
    }
  }
}

export function array(v: Value, path: string, min: number, max: number): readonly Value[] {
  if (!Array.isArray(v)) throw new IrError(path, "ir.not_array");
  if (v.length > max) throw new IrError(path, "ir.too_many_items");
  if (v.length < min) throw new IrError(path, "ir.too_few_items");
  return v as readonly Value[];
}

export function list<T>(v: Value, path: string, min: number, max: number, f: (e: Value, p: string) => T): T[] {
  return array(v, path, min, max).map((e, i) => f(e, `${path}/${i}`));
}

export function map<T>(
  v: Value,
  path: string,
  max: number,
  key: (k: string) => void,
  f: (e: Value, p: string) => T,
): Map<string, T> {
  const out = new Map<string, T>();
  for (const [k, e] of mapEntries(v, path, max)) {
    const p = child(path, k);
    try {
      key(k);
    } catch {
      throw new IrError(p, "ir.bad_key");
    }
    out.set(k, f(e, p));
  }
  return out;
}

export function mapEntries(v: Value, path: string, max: number): ReadonlyMap<string, Value> {
  if (!(v instanceof Map)) throw new IrError(path, "ir.not_object");
  if (v.size > max) throw new IrError(path, "ir.too_many_items");
  return v;
}

export function boolean(v: Value, path: string): boolean {
  if (typeof v !== "boolean") throw new IrError(path, "ir.wrong_type");
  return v;
}

export function integer(v: Value, path: string, min: number, max: number): number {
  if (typeof v !== "number") throw new IrError(path, "ir.wrong_type");
  if (v < min || v > max) throw new IrError(path, "ir.out_of_range");
  return v;
}

export const INSTANT_MIN = -62_135_596_800_000;
export const INSTANT_MAX = 253_402_300_799_999;

export function instant(v: Value, path: string): number {
  if (typeof v !== "number") throw new IrError(path, "ir.wrong_type");
  if (v < INSTANT_MIN || v > INSTANT_MAX) throw new IrError(path, "ir.bad_instant");
  return v;
}

export type TextMode = "none" | "label" | "multiline";

export interface StrRule {
  pattern: RegExp | null;
  maxLen: number;
  text: TextMode;
}

function forbiddenChar(u: number, multiline: boolean): boolean {
  if (multiline && (u === 0x0a || u === 0x09)) return false;
  return (
    u < 0x20 ||
    (u >= 0x7f && u <= 0x9f) ||
    u === 0x2028 ||
    u === 0x2029 ||
    (u >= 0x202a && u <= 0x202e) ||
    (u >= 0x2066 && u <= 0x2069) ||
    u === 0xfffe ||
    u === 0xffff
  );
}

export function checkString(s: string, path: string, rule: StrRule): void {
  let count = 0;
  for (const _ of s) count++;
  if (count > rule.maxLen) throw new IrError(path, "ir.too_long");
  if (rule.text !== "none") {
    for (const ch of s) {
      if (forbiddenChar(ch.codePointAt(0) as number, rule.text === "multiline")) throw new IrError(path, "ir.bad_text");
    }
  }
  if (rule.pattern !== null && !rule.pattern.test(s)) throw new IrError(path, "ir.bad_pattern");
}

export function string(v: Value, path: string, rule: StrRule): string {
  if (typeof v !== "string") throw new IrError(path, "ir.wrong_type");
  checkString(v, path, rule);
  return v;
}

export function validDecimal(s: string, scale: number | null): boolean {
  if (s.length === 0 || s.length > 64) return false;
  const neg = s.startsWith("-");
  const body = neg ? s.slice(1) : s;
  const dot = body.indexOf(".");
  const int = dot < 0 ? body : body.slice(0, dot);
  const frac = dot < 0 ? null : body.slice(dot + 1);
  if (!/^[0-9]+$/.test(int) || (int.length > 1 && int.startsWith("0"))) return false;
  let fracOk: boolean;
  if (frac === null) fracOk = scale === null || scale === 0;
  else if (scale !== null) fracOk = scale > 0 && frac.length === scale && /^[0-9]+$/.test(frac);
  else fracOk = frac.length > 0 && /^[0-9]+$/.test(frac) && !frac.endsWith("0");
  if (!fracOk) return false;
  const zero = /^[0.]+$/.test(body);
  return !(neg && zero);
}

export function decimal(v: Value, path: string, scale: number | null): string {
  if (typeof v !== "string") throw new IrError(path, "ir.wrong_type");
  if (!validDecimal(v, scale)) throw new IrError(path, "ir.bad_decimal");
  return v;
}

export function validDate(s: string): boolean {
  if (!/^[0-9]{4}-[0-9]{2}-[0-9]{2}$/.test(s)) return false;
  const y = Number(s.slice(0, 4));
  const m = Number(s.slice(5, 7));
  const d = Number(s.slice(8, 10));
  const leap = (y % 4 === 0 && y % 100 !== 0) || y % 400 === 0;
  const days = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31][m - 1];
  return y >= 1 && days !== undefined && d >= 1 && d <= days;
}

export function date(v: Value, path: string): string {
  if (typeof v !== "string") throw new IrError(path, "ir.wrong_type");
  if (!validDate(v)) throw new IrError(path, "ir.bad_date");
  return v;
}

export function enumStr(v: Value, path: string): string {
  if (typeof v !== "string") throw new IrError(path, "ir.wrong_type");
  return v;
}

export function parseVersion(s: string): Version | null {
  const m = /^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$/.exec(s);
  if (!m) return null;
  const v: [number, number, number] = [Number(m[1]), Number(m[2]), Number(m[3])];
  return v.every((n) => n <= 0xffffffff) ? v : null;
}

export function obj(entries: [string, Value][]): Value {
  return new Map<string, Value>(entries);
}
