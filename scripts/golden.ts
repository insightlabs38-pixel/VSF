import { spawnSync } from "node:child_process";
import { mkdirSync, readdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { canonicalize, sha256Hex } from "../frontend/packages/schema/src/canon.ts";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const bin = process.env.VSF_NATIVE_BIN ?? join(root, "artifacts/bin/vsf-fabric");
const dir = process.env.VSF_GOLDEN_DIR ?? join(root, "fixtures/golden");
const seeds = [1, 42, 20261018];
const mode = process.argv[2];
if (mode !== "update" && mode !== "check") {
  console.error("usage: golden.ts <update|check>");
  process.exit(64);
}

function run(args: string[], input?: Uint8Array): Uint8Array {
  const r = spawnSync(bin, args, { input, maxBuffer: 64 * 1024 * 1024 });
  if (r.status !== 0) throw new Error(`${args.join(" ")}: exit ${r.status} ${r.stderr}`);
  return new Uint8Array(r.stdout);
}

async function generate() {
  const entries: Record<string, unknown>[] = [];
  const files = new Map<string, Uint8Array>();
  for (const seed of seeds) {
    const input = run(["emit-fixture", "--seed", String(seed)]);
    const compiled = run(["compile", "-"], input);
    const file = `compiled-seed-${seed}.json`;
    files.set(file, compiled);
    entries.push({
      seed,
      input_sha256: await sha256Hex(input),
      compiled_file: file,
      compiled_sha256: await sha256Hex(compiled),
    });
  }
  const manifest = new TextEncoder().encode(
    `${JSON.stringify({ schema: "vsf-golden-manifest-v1", entries }, null, 1)}\n`,
  );
  files.set("MANIFEST.json", manifest);
  return files;
}

const first = await generate();
const second = await generate();
for (const [name, bytes] of first) {
  if (Buffer.compare(Buffer.from(bytes), Buffer.from(second.get(name) as Uint8Array)) !== 0) {
    console.error(`non-deterministic output: ${name}`);
    process.exit(1);
  }
}
if (mode === "update") {
  mkdirSync(dir, { recursive: true });
  for (const [name, bytes] of first) writeFileSync(join(dir, name), bytes);
  console.log(`golden updated: ${first.size} files`);
} else {
  const onDisk = new Set(readdirSync(dir).filter((n) => n !== "dom" && n !== "packs.json"));
  let bad = 0;
  for (const [name, bytes] of first) {
    onDisk.delete(name);
    let current: Buffer | undefined;
    try {
      current = readFileSync(join(dir, name));
    } catch {
      current = undefined;
    }
    if (!current || Buffer.compare(current, Buffer.from(bytes)) !== 0) {
      console.error(`golden mismatch: ${name}`);
      bad++;
    } else if (name !== "MANIFEST.json") {
      canonicalize(new Uint8Array(current));
    }
  }
  for (const extra of onDisk) {
    console.error(`unexpected golden file: ${extra}`);
    bad++;
  }
  if (bad > 0) process.exit(1);
  console.log(`golden ok: ${first.size} files byte-identical across two runs and on disk`);
}
