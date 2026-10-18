import { CanonError, parse, sha256Hex, toCanonical, type Value } from "@vsf/schema";
import { type Body, decodeBody, encodeBody, FAMILIES, IR_VERSION, IR_VERSION_TEXT } from "./generated.ts";
import { enumStr, IrError, Obj, parseVersion } from "./rt.ts";

export function familyDigest(family: string): string | undefined {
  return FAMILIES.find(([f]) => f === family)?.[2];
}

const encoder = new TextEncoder();

/** Domain-separated digest so a digest of one kind of artifact can never validate as another. */
export async function typedDigest(kind: string, canonical: Uint8Array): Promise<string> {
  const head = encoder.encode(`vsf.digest.v1\0${kind}\0`);
  const input = new Uint8Array(head.length + canonical.length);
  input.set(head, 0);
  input.set(canonical, head.length);
  return `sha256:${await sha256Hex(input)}`;
}

export function decodeDocumentValue(v: Value): Body {
  const o = Obj.of(v, "");
  const schema = enumStr(o.req("schema"), "/schema");
  const digest = enumStr(o.req("schema_digest"), "/schema_digest");
  const body = o.req("body");
  o.finish();
  const slash = schema.lastIndexOf("/");
  if (slash < 0) throw new IrError("/schema", "schema.malformed");
  const name = schema.slice(0, slash);
  if (!name.startsWith("vsf.")) throw new IrError("/schema", "schema.unknown");
  const family = name.slice(4);
  const expected = familyDigest(family);
  if (expected === undefined) throw new IrError("/schema", "schema.unknown");
  const got = parseVersion(schema.slice(slash + 1));
  if (!got) throw new IrError("/schema", "schema.malformed");
  if (got[0] !== IR_VERSION[0]) throw new IrError("/schema", "schema.incompatible_major");
  if (got[1] > IR_VERSION[1] || (got[1] === IR_VERSION[1] && got[2] > IR_VERSION[2]))
    throw new IrError("/schema", "schema.too_new");
  if (got[0] !== IR_VERSION[0] || got[1] !== IR_VERSION[1] || got[2] !== IR_VERSION[2])
    throw new IrError("/schema", "schema.unknown_version");
  if (digest !== expected) throw new IrError("/schema_digest", "schema.digest_mismatch");
  return decodeBody(family, body, "/body", { version: got });
}

export function decodeDocument(bytes: Uint8Array): Body {
  let v: Value;
  try {
    v = parse(bytes);
  } catch (e) {
    if (e instanceof CanonError) throw new IrError("", e.code);
    throw e;
  }
  return decodeDocumentValue(v);
}

export function encodeDocument(b: Body): Uint8Array {
  const doc = new Map<string, Value>([
    ["schema", `vsf.${b.family}/${IR_VERSION_TEXT}`],
    ["schema_digest", familyDigest(b.family) ?? ""],
    ["body", encodeBody(b)],
  ]);
  return toCanonical(doc);
}

export async function documentDigest(b: Body): Promise<string> {
  return typedDigest(`document:${b.family}`, encodeDocument(b));
}
