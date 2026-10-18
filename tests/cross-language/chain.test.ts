import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import { GOLDEN_SEEDS, sha256Hex } from "@vsf/schema";
import { loadFabric } from "@vsf/wasm-host";

const root = new URL("../../", import.meta.url);
const path = (rel: string): string => fileURLToPath(new URL(rel, root));
const native = process.env.VSF_NATIVE_BIN ?? path("artifacts/bin/vsf-fabric");
const goBin = process.env.VSF_GO_BIN ?? path("artifacts/bin/vsfctl");
const tsCli = path("frontend/packages/schema/src/cli.ts");

interface Run {
  status: number | null;
  stdout: Buffer;
  stderr: string;
}

function exec(cmd: string, args: string[], input?: Uint8Array): Run {
  const r = spawnSync(cmd, args, { input, maxBuffer: 64 * 1024 * 1024 });
  return { status: r.status, stdout: r.stdout, stderr: r.stderr.toString() };
}

const rustVerify = (b: Uint8Array) => exec(native, ["verify", "-"], b);
const tsVerify = (b: Uint8Array) => exec(process.execPath, [tsCli, "verify", "-"], b);
const goVerify = (b: Uint8Array) => exec(goBin, ["verify", "-"], b);

test("Rust -> TypeScript -> Go carries identical schema id and bytes", async () => {
  for (const seed of GOLDEN_SEEDS) {
    const emitted = exec(native, ["emit-fixture", "--seed", String(seed)]);
    assert.equal(emitted.status, 0, emitted.stderr);
    const viaTs = tsVerify(emitted.stdout);
    assert.equal(viaTs.status, 0, viaTs.stderr);
    const viaGo = goVerify(viaTs.stdout);
    assert.equal(viaGo.status, 0, viaGo.stderr);
    assert.deepEqual(viaGo.stdout, emitted.stdout);
    assert.equal(
      exec(goBin, ["digest", "-"], viaTs.stdout).stdout.toString().trim(),
      await sha256Hex(new Uint8Array(emitted.stdout)),
    );
    const schema = JSON.parse(emitted.stdout.toString()).schema;
    assert.match(schema, /^vsf\.smoke\.envelope\/1\.0\.0$/);
  }
});

test("native compile and WASM compile agree byte for byte", async () => {
  const fabric = await loadFabric(readFileSync(path("artifacts/wasm/vsf_wasm.wasm")));
  for (const seed of GOLDEN_SEEDS) {
    const input = exec(native, ["emit-fixture", "--seed", String(seed)]).stdout;
    const nativeOut = exec(native, ["compile", "-"], input);
    assert.equal(nativeOut.status, 0, nativeOut.stderr);
    assert.deepEqual(Buffer.from(fabric.compile(new Uint8Array(input))), nativeOut.stdout);
    assert.deepEqual(goVerify(nativeOut.stdout).stdout, nativeOut.stdout);
  }
});

const mutate = (input: Buffer, from: string, to: string): Uint8Array => {
  const text = input.toString();
  assert.ok(text.includes(from));
  return Buffer.from(text.replace(from, to));
};

test("incompatible schema is rejected by every binding with the same code", () => {
  const base = exec(native, ["emit-fixture", "--seed", "42"]).stdout;
  const cases: [string, string, string][] = [
    ["vsf.smoke.envelope/1.0.0", "vsf.smoke.envelope/2.0.0", "schema.incompatible_major"],
    ["vsf.smoke.envelope/1.0.0", "vsf.smoke.envelope/1.1.0", "schema.too_new"],
    ["vsf.smoke.envelope/1.0.0", "vsf.smoke.other/1.0.0", "schema.unknown"],
  ];
  for (const [from, to, code] of cases) {
    const bad = mutate(base, from, to);
    for (const [name, verify] of [
      ["rust", rustVerify],
      ["ts", tsVerify],
      ["go", goVerify],
    ] as const) {
      const r = verify(bad);
      assert.equal(r.status, 2, `${name} ${to}`);
      assert.equal(r.stderr.trim(), `error: ${code}`, `${name} ${to}`);
      assert.equal(r.stdout.length, 0);
    }
  }
});

test("tampered digest and corrupted bytes are rejected by every binding", () => {
  const base = exec(native, ["emit-fixture", "--seed", "1"]).stdout;
  const digest = /"schema_digest":"sha256:([0-9a-f]{64})"/.exec(base.toString())?.[1] ?? "";
  const flipped = (digest[0] === "0" ? "1" : "0") + digest.slice(1);
  const cases: [Uint8Array, string][] = [
    [mutate(base, digest, flipped), "schema.digest_mismatch"],
    [base.subarray(0, base.length - 3), "canon.syntax"],
    [Buffer.concat([base, Buffer.from(" x")]), "canon.trailing_data"],
  ];
  for (const [bad, code] of cases) {
    for (const verify of [rustVerify, tsVerify, goVerify]) {
      const r = verify(bad);
      assert.equal(r.status, 2);
      assert.equal(r.stderr.trim(), `error: ${code}`);
    }
  }
});
