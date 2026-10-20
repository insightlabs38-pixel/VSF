import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import { GOLDEN_SEEDS, sha256Hex } from "@vsf/schema";
import { assemblyFrame, loadFabric } from "@vsf/wasm-host";

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

test("native assemble and WASM assemble agree byte for byte on every recognition fixture", async () => {
  const fabric = await loadFabric(readFileSync(path("artifacts/wasm/vsf_wasm.wasm")));
  const names = [
    "table-orders",
    "plain-form",
    "adversarial",
    "aria-grid",
    "heldout-datagrid-edit",
    "heldout-shadow-ds",
  ];
  const params = {
    origin: "http://127.0.0.1:8080",
    variantId: "var_00112233445566778899aabbccddeeff",
    observedAt: 1_792_368_200_000,
    epoch: 7,
  };
  for (const name of names) {
    const snapshot = readFileSync(path(`fixtures/recognition/snapshots/${name}.json`));
    const viaWasm = fabric.assemble(assemblyFrame(params, snapshot));
    const cli = exec(
      native,
      [
        "assemble",
        "-",
        "--origin",
        params.origin,
        "--variant",
        params.variantId,
        "--epoch",
        String(params.epoch),
        "--observed-at",
        String(params.observedAt),
      ],
      snapshot,
    );
    assert.equal(cli.status, 0, cli.stderr);
    assert.deepEqual(new Uint8Array(cli.stdout), viaWasm, name);
  }
  const refused = exec(
    native,
    ["assemble", "-", "--origin", "ftp://x", "--variant", "var_x", "--epoch", "1", "--observed-at", "1"],
    Buffer.from("{}"),
  );
  assert.equal(refused.status, 2);
  assert.match(refused.stderr, /assemble\.bad_origin/);
});

test("the native worker answers assemble frames like the command line and reports errors with stable codes", () => {
  const params = {
    origin: "http://127.0.0.1:8080",
    variantId: "var_00112233445566778899aabbccddeeff",
    observedAt: 1_792_368_200_000,
    epoch: 7,
  };
  const frame = (op: string, payload: Uint8Array): Buffer => {
    const head = Buffer.alloc(5);
    head.writeUInt32LE(payload.length + 1, 0);
    head.write(op, 4, "latin1");
    return Buffer.concat([head, payload]);
  };
  const serve = spawnSync(native, ["serve"], {
    input: Buffer.concat([
      frame("S", assemblyFrame(params, readFileSync(path("fixtures/recognition/snapshots/plain-form.json")))),
      frame("S", new Uint8Array(3)),
    ]),
    maxBuffer: 64 * 1024 * 1024,
  });
  const out = serve.stdout;
  assert.equal(out[0], 0);
  const size = out.readUInt32LE(1);
  const doc = JSON.parse(out.subarray(5, 5 + size).toString());
  assert.equal(doc.schema, "vsf-assembly-v1");
  const next = 5 + size;
  assert.equal(out[next], 2);
  assert.equal(out.subarray(next + 5, next + 5 + out.readUInt32LE(next + 1)).toString(), "assemble.bad_frame");
});
