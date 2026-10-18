import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import { canonicalize, GOLDEN_SEEDS, generateEnvelopeBytes, parse, sha256Hex } from "@vsf/schema";
import { EXPECTED_ABI_VERSION, loadFabric, WasmAbiError, WasmCallError } from "../src/index.ts";

const root = new URL("../../../../", import.meta.url);
const wasm = readFileSync(new URL("artifacts/wasm/vsf_wasm.wasm", root));
const injected = readFileSync(new URL("artifacts/wasm/vsf_wasm.abi-injected.wasm", root));
const manifest = parse(readFileSync(new URL("fixtures/golden/MANIFEST.json", root))) as Map<string, unknown>;
const entries = manifest.get("entries") as Map<string, unknown>[];

test("wasm output is byte-identical to native golden for every seed", async () => {
  const fabric = await loadFabric(wasm);
  assert.equal(fabric.abiVersion, EXPECTED_ABI_VERSION);
  assert.equal(entries.length, GOLDEN_SEEDS.length);
  for (const e of entries) {
    const input = generateEnvelopeBytes(e.get("seed") as number);
    assert.equal(await sha256Hex(input), e.get("input_sha256"), "TS generator matches Rust generator");
    const golden = readFileSync(new URL(`fixtures/golden/${e.get("compiled_file")}`, root));
    assert.deepEqual(Buffer.from(fabric.compile(input)), golden);
    assert.equal(await sha256Hex(golden), e.get("compiled_sha256"));
  }
});

test("injected incompatible ABI is rejected before any call", async () => {
  await assert.rejects(
    () => loadFabric(injected),
    (e: unknown) => e instanceof WasmAbiError && e.actual === 1000 && e.expected === 1,
  );
  await assert.rejects(() => loadFabric(wasm, 2), WasmAbiError);
});

test("module without the ABI export is rejected", async () => {
  const empty = Uint8Array.from([0, 0x61, 0x73, 0x6d, 1, 0, 0, 0]);
  await assert.rejects(
    () => loadFabric(empty),
    (e: unknown) => e instanceof WasmAbiError && e.actual === undefined,
  );
});

test("wasm surfaces stable error codes and recovers after errors", async () => {
  const fabric = await loadFabric(wasm);
  const enc = new TextEncoder();
  assert.throws(
    () => fabric.compile(enc.encode('{"a":1.5}')),
    (e: unknown) => e instanceof WasmCallError && e.code === "canon.non_integer",
  );
  const bad = enc.encode('{"schema":"vsf.smoke.envelope/2.0.0","schema_digest":"x","seq":1,"payload":null}');
  assert.throws(() => fabric.compile(bad), { code: "schema.incompatible_major" });
  assert.deepEqual(canonicalize(enc.encode("[1]")), enc.encode("[1]"));
});
