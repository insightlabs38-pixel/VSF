import { readdirSync, readFileSync } from "node:fs";
import { parse, toCanonical, type Value } from "@vsf/schema";
import { familyDigest } from "./doc.ts";

const dir = new URL("../../../../schemas/ir/fixtures/", import.meta.url);
const enc = new TextEncoder();

export const readFixture = (rel: string): string => readFileSync(new URL(rel, dir), "utf8");

export const validNames = (): string[] =>
  readdirSync(new URL("valid/", dir))
    .map((f) => f.replace(/\.json$/, ""))
    .sort();

/** Canonical bytes of a valid fixture with its schema digest placeholder filled in. */
export function validBytes(name: string): Uint8Array {
  const text = readFixture(`valid/${name}.json`);
  const family = /"schema": "vsf\.([a-z]+)\//.exec(text)?.[1] as string;
  return toCanonical(parse(enc.encode(text.replace("$DIGEST", familyDigest(family) as string))));
}

export function pointerSet(root: Value, path: string, value: Value | undefined): void {
  const segs = path.split("/").slice(1);
  const last = segs.pop() as string;
  let cur: Value = root;
  for (const seg of segs) {
    if (cur instanceof Map) cur = cur.get(seg) as Value;
    else if (Array.isArray(cur)) cur = cur[Number(seg)] as Value;
    else throw new Error(`bad path ${path}`);
  }
  if (cur instanceof Map) {
    const m = cur as Map<string, Value>;
    if (value === undefined) m.delete(last);
    else m.set(last, value);
  } else if (Array.isArray(cur)) {
    const a = cur as Value[];
    if (value === undefined) a.splice(Number(last), 1);
    else a[Number(last)] = value;
  } else throw new Error(`bad path ${path}`);
}

export interface FixtureCase {
  name: string;
  bytes: Uint8Array;
  expect: unknown;
}

/** Cases from `<file>` (invalid/cases.json or semantic/cases.json): raw bytes or edits over a valid base. */
export function loadCases(file: string): FixtureCase[] {
  const doc = parse(enc.encode(readFixture(file))) as Map<string, unknown>;
  return (doc.get("cases") as Map<string, unknown>[]).map((c) => {
    let bytes: Uint8Array;
    if (typeof c.get("raw") === "string") {
      bytes = enc.encode(c.get("raw") as string);
    } else {
      const v = parse(validBytes(c.get("base") as string));
      for (const e of c.get("edits") as Map<string, unknown>[]) {
        pointerSet(v, e.get("path") as string, e.get("op") === "remove" ? undefined : (e.get("value") as Value));
      }
      bytes = toCanonical(v);
    }
    return { name: c.get("name") as string, bytes, expect: c.get("expect") };
  });
}
