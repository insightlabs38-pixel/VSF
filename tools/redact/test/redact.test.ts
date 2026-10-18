import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import { findSecretShapes, patternCount, redact } from "../src/index.ts";

const cases = JSON.parse(
  readFileSync(new URL("../../../schemas/redaction/cases.v1.json", import.meta.url), "utf8"),
) as {
  positive: { id: string; kind: string; parts: string[] }[];
  negative: { id: string; text: string }[];
};
const nativeBin =
  process.env.VSF_NATIVE_BIN ?? fileURLToPath(new URL("../../../artifacts/bin/vsf-fabric", import.meta.url));

test("loads every shared pattern", () => {
  assert.equal(patternCount(), 11);
});

test("redacts every positive case with its kind and leaks nothing", () => {
  for (const c of cases.positive) {
    const planted = c.parts.join("");
    const out = redact(`before ${planted} after`);
    assert.ok(out.includes(`[REDACTED:${c.kind}]`), `${c.id}: ${out}`);
    assert.ok(!out.includes(planted), `${c.id} leaked`);
    assert.deepEqual(findSecretShapes(out), [], `${c.id}: output still secret-shaped`);
  }
});

test("leaves non-secret text untouched", () => {
  for (const c of cases.negative) assert.equal(redact(c.text), c.text, c.id);
});

test("TypeScript and Rust redactors agree on every case", () => {
  const inputs = [...cases.positive.map((c) => `x ${c.parts.join("")} y`), ...cases.negative.map((c) => c.text)];
  for (const input of inputs) {
    const r = spawnSync(nativeBin, ["redact"], { input });
    assert.equal(r.status, 0, r.stderr.toString());
    assert.equal(r.stdout.toString(), redact(input), input.slice(0, 20));
  }
});
