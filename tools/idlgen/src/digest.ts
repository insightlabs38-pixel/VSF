import { createHash } from "node:crypto";
import { toCanonical, type Value } from "@vsf/schema";
import { closure, type Model } from "./idl.ts";

export function toValue(j: unknown): Value {
  if (j === null || typeof j === "boolean" || typeof j === "string") return j;
  if (typeof j === "number") {
    if (!Number.isSafeInteger(j)) throw new Error(`non-integer ${j} in IDL`);
    return j;
  }
  if (Array.isArray(j)) return j.map(toValue);
  return new Map(Object.entries(j as Record<string, unknown>).map(([k, v]) => [k, toValue(v)] as [string, Value]));
}

/** Prose never contributes to wire identity: fixing a typo in `doc` must not change a schema digest. */
export function withoutDocs(j: unknown): unknown {
  if (Array.isArray(j)) return j.map(withoutDocs);
  if (j && typeof j === "object") {
    return Object.fromEntries(
      Object.entries(j as Record<string, unknown>)
        .filter(([k]) => k !== "doc")
        .map(([k, v]) => [k, withoutDocs(v)]),
    );
  }
  return j;
}

export interface FamilyDigest {
  family: string;
  root: string | null;
  digest: string;
}

/** Digest of a family's type closure; any change to a reachable definition changes it. */
export function familyDigests(m: Model): FamilyDigest[] {
  return m.families.map((f) => {
    if (!f.root) return { family: f.family, root: null, digest: "" };
    const names = [...closure(m, f.root)].sort();
    const types = new Map<string, Value>(names.map((n) => [n, toValue(withoutDocs(m.raw.get(n)))] as [string, Value]));
    const doc = new Map<string, Value>([
      ["family", f.family],
      ["ir_version", m.irVersionText],
      ["root", f.root],
      ["types", types],
    ]);
    const hash = createHash("sha256").update(toCanonical(doc)).digest("hex");
    return { family: f.family, root: f.root, digest: `sha256:${hash}` };
  });
}
