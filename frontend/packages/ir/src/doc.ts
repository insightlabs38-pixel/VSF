import { CanonError, parse, sha256Hex, toCanonical, type Value } from "@vsf/schema";
import { ACCEPTED, type Body, decodeBody, encodeBody, FAMILIES, IR_VERSION } from "./generated.ts";
import type * as rt from "./rt.ts";
import { enumStr, IrError, Obj, parseVersion } from "./rt.ts";

export function familyDigest(family: string): string | undefined {
  return FAMILIES.find(([f]) => f === family)?.[2];
}

/** The digest a document of this family and version must carry; every released version stays readable. */
export function digestAt(family: string, version: readonly [number, number, number]): string | undefined {
  return ACCEPTED.find(
    ([v, f]) => f === family && v[0] === version[0] && v[1] === version[1] && v[2] === version[2],
  )?.[2];
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

/** A decoded document no validation has accepted; it can never be executed. */
export class Unvalidated {
  readonly #body: Body;
  readonly version: rt.Version;

  constructor(body: Body, version: rt.Version) {
    this.#body = body;
    this.version = version;
  }

  /** The decoded body, checked for shape only. */
  unsafe(): Body {
    return this.#body;
  }
}

export function decodeDocumentValue(v: Value): Unvalidated {
  const { body, version } = decodeEnvelope(v);
  return new Unvalidated(body, version);
}

function decodeEnvelope(v: Value): { body: Body; version: rt.Version } {
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
  if (familyDigest(family) === undefined) throw new IrError("/schema", "schema.unknown");
  const got = parseVersion(schema.slice(slash + 1));
  if (!got) throw new IrError("/schema", "schema.malformed");
  if (got[0] !== IR_VERSION[0]) throw new IrError("/schema", "schema.incompatible_major");
  if (got[1] > IR_VERSION[1] || (got[1] === IR_VERSION[1] && got[2] > IR_VERSION[2]))
    throw new IrError("/schema", "schema.too_new");
  const expected = digestAt(family, got);
  if (expected === undefined) throw new IrError("/schema", "schema.unknown_version");
  if (digest !== expected) throw new IrError("/schema_digest", "schema.digest_mismatch");
  return { body: decodeBody(family, body, "/body", { version: got }), version: got };
}

export function decodeDocument(bytes: Uint8Array): Unvalidated {
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
  return encodeDocumentAt(b, IR_VERSION) as Uint8Array;
}

/** Writes the envelope of a released version; undefined when that version is not known. */
export function encodeDocumentAt(b: Body, version: readonly [number, number, number]): Uint8Array | undefined {
  const digest = digestAt(b.family, version);
  if (digest === undefined) return undefined;
  const doc = new Map<string, Value>([
    ["schema", `vsf.${b.family}/${version.join(".")}`],
    ["schema_digest", digest],
    ["body", encodeBody(b)],
  ]);
  return toCanonical(doc);
}

export async function documentDigest(b: Body): Promise<string> {
  return typedDigest(`document:${b.family}`, encodeDocument(b));
}

/** Typed digest of a document as it was written under `version`. */
export async function documentDigestAt(
  b: Body,
  version: readonly [number, number, number],
): Promise<string | undefined> {
  const bytes = encodeDocumentAt(b, version);
  return bytes === undefined ? undefined : typedDigest(`document:${b.family}`, bytes);
}
