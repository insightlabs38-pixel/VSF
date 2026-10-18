import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import { analyzePack, generatePackBytes, PackError, summaryJson } from "@vsf/observer";
import { loadFabric } from "@vsf/wasm-host";

const root = new URL("../../", import.meta.url);
const path = (rel: string): string => fileURLToPath(new URL(rel, root));
const native = process.env.VSF_NATIVE_BIN ?? path("artifacts/bin/vsf-fabric");

function exec(args: string[], input?: Uint8Array) {
  const r = spawnSync(native, args, { input, maxBuffer: 256 * 1024 * 1024 });
  return { status: r.status, stdout: new Uint8Array(r.stdout), stderr: r.stderr.toString().trim() };
}

const SIZES = [0, 1, 2, 100, 1000, 10000];
const SEEDS = [1, 42];
const fabricPromise = loadFabric(readFileSync(path("artifacts/wasm/vsf_wasm.wasm")));

test("TypeScript, Rust native and WASM agree on pack bytes, digests and summaries", async () => {
  const fabric = await fabricPromise;
  for (const seed of SEEDS) {
    for (const n of SIZES) {
      const tsPack = generatePackBytes(seed, n);
      const nativePack = exec(["pack-gen", "--seed", String(seed), "--nodes", String(n)]);
      assert.equal(nativePack.status, 0, nativePack.stderr);
      assert.deepEqual(Buffer.from(tsPack), Buffer.from(nativePack.stdout), `pack bytes seed=${seed} n=${n}`);
      const nativeSum = exec(["pack-analyze", "-"], tsPack);
      assert.equal(nativeSum.status, 0, nativeSum.stderr);
      const wasmSum = fabric.analyzePack(tsPack);
      const tsSum = new TextEncoder().encode(summaryJson(await analyzePack(tsPack)));
      assert.deepEqual(Buffer.from(wasmSum), Buffer.from(nativeSum.stdout), `wasm vs native seed=${seed} n=${n}`);
      assert.deepEqual(Buffer.from(tsSum), Buffer.from(nativeSum.stdout), `ts vs native seed=${seed} n=${n}`);
    }
  }
});

test("committed digests pin the pack format across all three implementations", async () => {
  const pins = JSON.parse(readFileSync(path("fixtures/golden/packs.json"), "utf8")) as {
    seed: number;
    nodes: number;
    digest: string;
  }[];
  assert.ok(pins.length >= 4);
  for (const p of pins) {
    const sum = await analyzePack(generatePackBytes(p.seed, p.nodes));
    assert.equal(sum.digest, p.digest, `seed=${p.seed} nodes=${p.nodes}`);
  }
});

test("malformed, empty and truncated inputs fail identically everywhere", async () => {
  const fabric = await fabricPromise;
  const good = generatePackBytes(5, 200);
  const cases: [string, Uint8Array, string][] = [
    ["empty", new Uint8Array(), "pack.empty"],
    ["short header", good.slice(0, 10), "pack.truncated"],
    ["truncated body", good.slice(0, good.length - 7), "pack.truncated"],
    ["trailing byte", Uint8Array.from([...good, 0]), "pack.length_mismatch"],
    ["bad magic", Uint8Array.from([...good.slice(0, 3), 0x58, ...good.slice(4)]), "pack.bad_magic"],
  ];
  for (const [name, bytes, code] of cases) {
    const n = exec(["pack-analyze", "-"], bytes);
    assert.equal(n.status, 2, name);
    assert.equal(n.stderr, `error: ${code}`, name);
    assert.throws(() => fabric.analyzePack(bytes), { code }, name);
    await assert.rejects(
      () => analyzePack(bytes),
      (e: unknown) => e instanceof PackError && e.code === code,
      name,
    );
  }
});
