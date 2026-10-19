import { spawnSync } from "node:child_process";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { checkAgainstReleased, compare } from "./compat.ts";
import { acceptedVersions, familyDigests } from "./digest.ts";
import { emitGo } from "./emit-go.ts";
import { emitRust } from "./emit-rust.ts";
import { emitTs } from "./emit-ts.ts";
import { loadModel } from "./idl.ts";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "../../..");

export function generate(schemaDir: string): Record<string, string> {
  const model = loadModel(schemaDir);
  const digests = familyDigests(model);
  const accepted = acceptedVersions(schemaDir, model, loadModel);
  const gofmt = spawnSync("gofmt", [], { input: emitGo(model, digests, accepted), encoding: "utf8" });
  if (gofmt.status !== 0) throw new Error(`gofmt failed: ${gofmt.stderr}`);
  return {
    "compiler/crates/vsf-ir/src/generated.rs": emitRust(model, digests, accepted),
    "frontend/packages/ir/src/generated.ts": emitTs(model, digests, accepted),
    "backend/ir/generated.go": gofmt.stdout,
  };
}

function report(v: {
  findings: { level: string; code: string; where: string; detail: string }[];
  errors: { code: string; where: string; detail: string }[];
}): number {
  for (const f of v.findings) console.log(`${f.level.padEnd(8)} ${f.code} ${f.where} ${f.detail}`.trimEnd());
  for (const e of v.errors) console.error(`rejected ${e.code} ${e.where} ${e.detail}`.trimEnd());
  return v.errors.length > 0 ? 1 : 0;
}

if (process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1] && process.argv[2] === "compat") {
  const [, , , oldDir, newDir] = process.argv;
  if (oldDir && newDir) process.exit(report(compare(loadModel(oldDir), loadModel(newDir))));
  const v = checkAgainstReleased(join(root, "schemas/ir"));
  console.log(`baseline: ${v.baseline ?? "none"}`);
  process.exit(report(v));
}

if (process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1]) {
  const check = process.argv.includes("--check");
  const outputs = generate(join(root, "schemas/ir"));
  let drift = 0;
  for (const [rel, body] of Object.entries(outputs)) {
    const path = join(root, rel);
    let current = "";
    try {
      current = readFileSync(path, "utf8");
    } catch {
      current = "";
    }
    if (current === body) continue;
    if (check) {
      console.error(`drift: ${rel}`);
      drift++;
    } else {
      mkdirSync(dirname(path), { recursive: true });
      writeFileSync(path, body);
      console.log(`wrote ${rel}`);
    }
  }
  process.exit(drift > 0 ? 1 : 0);
}
