import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import { decodeDocument } from "@vsf/ir";
import { validBytes } from "@vsf/ir/testing";
import { parse, toCanonical, type Value } from "@vsf/schema";
import { type Fabric, loadFabric, requireValidated, Validated, ValidationRefused, validate } from "../src/index.ts";

const root = new URL("../../../../", import.meta.url);
const wasm = readFileSync(new URL("artifacts/wasm/vsf_wasm.wasm", root));

const edit = (name: string, change: (body: Map<string, Value>) => void): Uint8Array => {
  const doc = parse(validBytes(name)) as Map<string, Value>;
  change(doc.get("body") as Map<string, Value>);
  return toCanonical(doc);
};

test("a document the Rust validator accepts becomes Validated with the digest Rust computed", async () => {
  const fabric = await loadFabric(wasm);
  const bytes = validBytes("operation-witnessed-1-1");
  const v = await validate(fabric, bytes);
  assert.ok(Validated.is(v));
  assert.equal(v.body().family, "operation");
  assert.match(v.digest, /^sha256:[0-9a-f]{64}$/);
  assert.equal(requireValidated(v), v);
});

test("documents TypeScript decodes happily but Rust refuses never become Validated", async () => {
  const fabric = await loadFabric(wasm);
  const cases: [string, (b: Map<string, Value>) => void, string][] = [
    [
      "effect understated",
      (b) => {
        const e = b.get("effect") as Map<string, Value>;
        e.set("class", "idempotent_write");
        e.set("idempotency", "idempotent");
      },
      "contract.effect_understated",
    ],
    [
      "no witness covers the source",
      (b) => {
        for (const w of b.get("witnesses") as Map<string, Value>[])
          (w.get("applies_to") as Map<string, Value>).set("app_id", "other-console:app.other.test");
      },
      "contract.witness_not_applicable",
    ],
    [
      "write without verification",
      (b) => {
        b.set(
          "verification",
          new Map<string, Value>([
            ["required", false],
            ["strategy", "none"],
            ["suite", "update_stage_v2"],
          ]),
        );
      },
      "contract.write_without_verification",
    ],
  ];
  for (const [name, change, code] of cases) {
    const bytes = edit("operation-witnessed-1-1", change);
    assert.ok(decodeDocument(bytes).unsafe(), `${name}: decodes`);
    await assert.rejects(
      () => validate(fabric, bytes),
      (e: unknown) =>
        e instanceof ValidationRefused && e.code === "validate.invalid" && e.violations.some((x) => x.code === code),
      name,
    );
  }
});

test("a document that does not even decode is refused with the decoder's code", async () => {
  const fabric = await loadFabric(wasm);
  const bytes = new TextEncoder().encode('{"schema":"vsf.operation/9.0.0","schema_digest":"x","body":{}}');
  await assert.rejects(
    () => validate(fabric, bytes),
    (e: unknown) => e instanceof ValidationRefused && e.code === "schema.incompatible_major",
  );
});

test("nothing but validate() can create a Validated value", async () => {
  const fabric = await loadFabric(wasm);
  const real = await validate(fabric, validBytes("operation-witnessed-1-1"));
  const body = real.body();
  assert.throws(() => new Validated(Symbol("guess"), body, real.digest, real.version), TypeError);
  const lookalike = { digest: real.digest, version: real.version, body: () => body };
  for (const forged of [lookalike, Object.create(Validated.prototype), null, undefined, "validated"]) {
    assert.throws(
      () => requireValidated(forged),
      (e: unknown) => e instanceof ValidationRefused && e.code === "validate.not_validated",
    );
  }
});

test("a validator that disagrees with TypeScript about the document is not trusted", async () => {
  const real = await loadFabric(wasm);
  const lying: Fabric = {
    ...real,
    validateIr: (doc) => {
      const report = JSON.parse(new TextDecoder().decode(real.validateIr(doc)));
      report.decode.digest = `sha256:${"0".repeat(64)}`;
      return new TextEncoder().encode(JSON.stringify(report));
    },
  };
  await assert.rejects(
    () => validate(lying, validBytes("operation-witnessed-1-1")),
    (e: unknown) => e instanceof ValidationRefused && e.code === "validate.digest_disagreement",
  );
  const mute: Fabric = { ...real, validateIr: () => new TextEncoder().encode("not json") };
  await assert.rejects(
    () => validate(mute, validBytes("operation-witnessed-1-1")),
    (e: unknown) => e instanceof ValidationRefused && e.code === "validate.unavailable",
  );
  const optimistic: Fabric = {
    ...real,
    validateIr: () =>
      new TextEncoder().encode(JSON.stringify({ decode: { ok: true, digest: "x" }, valid: false, violations: [] })),
  };
  await assert.rejects(() => validate(optimistic, validBytes("operation-witnessed-1-1")), ValidationRefused);
});
