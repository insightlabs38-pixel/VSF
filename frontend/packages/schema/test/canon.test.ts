import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import { CanonError, canonicalize, MAX_INPUT_BYTES, parse, sha256Hex } from "../src/index.ts";

const enc = new TextEncoder();
const dec = new TextDecoder();
const vectors = parse(readFileSync(new URL("../../../../schemas/smoke/canon-vectors.json", import.meta.url))) as Map<
  string,
  unknown
>;

test("shared canon vectors match Rust and Go expectations", () => {
  const cases = vectors.get("cases") as Map<string, string>[];
  assert.ok(cases.length >= 20);
  for (const c of cases) {
    const id = c.get("id");
    const input = enc.encode(c.get("input"));
    if (c.has("canonical")) {
      assert.equal(dec.decode(canonicalize(input)), c.get("canonical"), id);
    } else {
      assert.throws(
        () => parse(input),
        (e: unknown) => e instanceof CanonError && e.code === c.get("error"),
        id,
      );
    }
  }
});

test("rejects invalid UTF-8 and oversized input", () => {
  assert.throws(() => parse(Uint8Array.from([0x22, 0xff, 0x22])), { code: "canon.invalid_utf8" });
  assert.throws(() => parse(new Uint8Array(MAX_INPUT_BYTES + 1)), { code: "canon.too_large" });
});

test("canonicalization is idempotent", () => {
  const once = canonicalize(enc.encode('{"k":"a\\u00e9\\ud83d\\ude00","a":[3,{"z":1,"y":2}]}'));
  assert.deepEqual(canonicalize(once), once);
});

test("sha256 known vector", async () => {
  assert.equal(await sha256Hex(enc.encode("abc")), "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad");
});

test("__proto__ keys stay data and never touch prototypes", () => {
  const v = parse(enc.encode('{"__proto__":{"polluted":1}}')) as Map<string, unknown>;
  assert.ok(v.has("__proto__"));
  assert.equal(({} as Record<string, unknown>).polluted, undefined);
});
