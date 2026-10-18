import { CanonError, parse, sha256Hex, toCanonical } from "./canon.js";
import { CURRENT_VERSION, KNOWN_VERSIONS, SCHEMA_NAME } from "./generated.js";
export class SchemaError extends Error {
  code;
  constructor(code) {
    super(code);
    this.name = "SchemaError";
    this.code = code;
  }
}
function parseVersion(text) {
  const m = /^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$/.exec(text);
  if (!m) throw new SchemaError("schema.malformed");
  return [Number(m[1]), Number(m[2]), Number(m[3])];
}
export function validateEnvelope(value) {
  if (!(value instanceof Map)) throw new SchemaError("schema.malformed");
  const schema = value.get("schema");
  if (schema === undefined) throw new SchemaError("schema.field_missing");
  if (typeof schema !== "string") throw new SchemaError("schema.field_invalid");
  const slash = schema.lastIndexOf("/");
  if (slash < 0) throw new SchemaError("schema.malformed");
  if (schema.slice(0, slash) !== SCHEMA_NAME) throw new SchemaError("schema.unknown");
  const version = schema.slice(slash + 1);
  const got = parseVersion(version);
  const want = parseVersion(CURRENT_VERSION);
  if (got[0] !== want[0]) throw new SchemaError("schema.incompatible_major");
  if (got[1] > want[1] || (got[1] === want[1] && got[2] > want[2])) throw new SchemaError("schema.too_new");
  const known = KNOWN_VERSIONS.find(([v]) => v === version);
  if (!known) throw new SchemaError("schema.unknown_version");
  const digest = value.get("schema_digest");
  if (digest === undefined) throw new SchemaError("schema.field_missing");
  if (typeof digest !== "string") throw new SchemaError("schema.field_invalid");
  if (digest !== known[1]) throw new SchemaError("schema.digest_mismatch");
  const seq = value.get("seq");
  if (seq === undefined) throw new SchemaError("schema.field_missing");
  if (typeof seq !== "number" || seq < 0) throw new SchemaError("schema.field_invalid");
  if (!value.has("payload")) throw new SchemaError("schema.field_missing");
  for (const key of value.keys()) {
    if (key !== "schema" && key !== "schema_digest" && key !== "seq" && key !== "payload") {
      throw new SchemaError("schema.field_invalid");
    }
  }
  return { schema, schemaDigest: digest, seq, payload: value.get("payload") };
}
export function verifyEnvelopeBytes(input) {
  const envelope = validateEnvelope(parse(input));
  return { envelope, canonical: toCanonical(parse(input)) };
}
export function makeEnvelope(seq, payload) {
  const known = KNOWN_VERSIONS.find(([v]) => v === CURRENT_VERSION);
  if (!known) throw new SchemaError("schema.unknown_version");
  return toCanonical(
    new Map([
      ["schema", `${SCHEMA_NAME}/${CURRENT_VERSION}`],
      ["schema_digest", known[1]],
      ["seq", seq],
      ["payload", payload],
    ]),
  );
}
export function errorCode(err) {
  if (err instanceof SchemaError || err instanceof CanonError) return err.code;
  throw err;
}
export { sha256Hex };
