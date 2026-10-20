import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import {
  type AssemblyParams,
  assemble,
  assemblyFrame,
  loadFabric,
  parseAssembly,
  WasmCallError,
} from "../src/index.ts";

const root = new URL("../../../../", import.meta.url);
const wasm = readFileSync(new URL("artifacts/wasm/vsf_wasm.wasm", root));
const snapshot = (name: string): Uint8Array =>
  readFileSync(new URL(`fixtures/recognition/snapshots/${name}.json`, root));
const params: AssemblyParams = {
  origin: "http://127.0.0.1:8080",
  variantId: "var_00112233445566778899aabbccddeeff",
  observedAt: 1_792_368_200_000,
  epoch: 7,
};

test("the orders table assembles through WASM into typed rows and unverified actions", async () => {
  const fabric = await loadFabric(wasm);
  const a = assemble(fabric, params, snapshot("table-orders"));
  assert.equal(a.views[0]?.layout, "grid");
  const rows = Object.values(a.rows)[0] ?? [];
  assert.deepEqual(
    rows.map((r) => r.key),
    ["k_1001", "k_1002", "k_1003"],
  );
  assert.ok(Object.values(a.actions).every((x) => x.state === "unknown" && x.reason === "effect_unverified"));
  assert.match(a.semantic_digest, /^sha256:[0-9a-f]{64}$/);
});

test("a second assembly of the same observation is byte-identical", async () => {
  const fabric = await loadFabric(wasm);
  const frame = assemblyFrame(params, snapshot("plain-form"));
  assert.deepEqual(fabric.assemble(frame), fabric.assemble(frame));
});

test("bad frames and snapshots surface stable codes and the module keeps working", async () => {
  const fabric = await loadFabric(wasm);
  assert.throws(() => fabric.assemble(new Uint8Array(2)), { code: "assemble.bad_frame" });
  assert.throws(() => fabric.assemble(assemblyFrame({ ...params, origin: "ftp://x" }, snapshot("plain-form"))), {
    code: "assemble.bad_origin",
  });
  assert.throws(
    () => fabric.assemble(assemblyFrame(params, new TextEncoder().encode("{}"))),
    (e: unknown) => e instanceof WasmCallError && e.code === "obs.bad_schema",
  );
  assert.equal(assemble(fabric, params, snapshot("plain-form")).views.length, 1);
});

test("a document that is not an assembly is refused by the host", () => {
  const enc = (v: unknown): Uint8Array => new TextEncoder().encode(JSON.stringify(v));
  assert.throws(() => parseAssembly(enc({ schema: "other" })), { code: "assembly.malformed" });
  assert.throws(() => parseAssembly(enc(null)), { code: "assembly.malformed" });
});
