import assert from "node:assert/strict";
import { test } from "node:test";
import {
  CURRENT_VERSION,
  errorCode,
  KNOWN_VERSIONS,
  makeEnvelope,
  SCHEMA_NAME,
  type Value,
  verifyEnvelopeBytes,
} from "../src/index.ts";

const enc = new TextEncoder();
const digest = KNOWN_VERSIONS[0]?.[1] ?? "";
const ok = `${SCHEMA_NAME}/${CURRENT_VERSION}`;

function code(input: string): string {
  try {
    verifyEnvelopeBytes(enc.encode(input));
  } catch (e) {
    return errorCode(e);
  }
  return "ok";
}

test("accepts current envelope and output is canonical", () => {
  const bytes = makeEnvelope(
    7,
    new Map<string, Value>([
      ["b", 1],
      ["a", [true]],
    ]),
  );
  const { envelope, canonical } = verifyEnvelopeBytes(bytes);
  assert.equal(envelope.seq, 7);
  assert.deepEqual(canonical, bytes);
});

test("rejects incompatible schemas with specific codes", () => {
  const cases: [string, string, string][] = [
    ["vsf.smoke.envelope/2.0.0", digest, "schema.incompatible_major"],
    ["vsf.smoke.envelope/1.1.0", digest, "schema.too_new"],
    ["vsf.smoke.envelope/1.0.1", digest, "schema.too_new"],
    ["vsf.other/1.0.0", digest, "schema.unknown"],
    ["vsf.smoke.envelope", digest, "schema.malformed"],
    ["vsf.smoke.envelope/1.0", digest, "schema.malformed"],
    ["vsf.smoke.envelope/01.0.0", digest, "schema.malformed"],
    ["vsf.smoke.envelope/1.0.0", "sha256:00", "schema.digest_mismatch"],
  ];
  for (const [schema, d, want] of cases) {
    assert.equal(code(`{"schema":"${schema}","schema_digest":"${d}","seq":1,"payload":null}`), want, schema);
  }
});

test("rejects missing, invalid and unknown fields", () => {
  const cases: [string, string][] = [
    ["[]", "schema.malformed"],
    ['{"seq":1}', "schema.field_missing"],
    ['{"schema":1}', "schema.field_invalid"],
    [`{"schema":"${ok}","seq":1,"payload":1}`, "schema.field_missing"],
    [`{"schema":"${ok}","schema_digest":"${digest}","seq":-1,"payload":1}`, "schema.field_invalid"],
    [`{"schema":"${ok}","schema_digest":"${digest}","seq":1}`, "schema.field_missing"],
    [`{"schema":"${ok}","schema_digest":"${digest}","seq":1,"payload":1,"x":1}`, "schema.field_invalid"],
    [`{"schema":"${ok}","schema_digest":"${digest}","seq":1.5,"payload":1}`, "canon.non_integer"],
  ];
  for (const [input, want] of cases) assert.equal(code(input), want, input);
});
