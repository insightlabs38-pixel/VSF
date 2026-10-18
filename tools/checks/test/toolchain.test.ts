import assert from "node:assert/strict";
import { cpSync, mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import { checkInstalled, checkPins } from "../src/toolchain.ts";

const repo = resolve(fileURLToPath(new URL("../../..", import.meta.url)));

function sandbox(): string {
  const dir = mkdtempSync(join(tmpdir(), "vsf-pins-"));
  for (const f of [
    "toolchain/versions.json",
    "rust-toolchain.toml",
    "backend/go.mod",
    ".node-version",
    "package.json",
  ]) {
    cpSync(join(repo, f), join(dir, f), { recursive: true });
  }
  return dir;
}

test("repository pins are consistent with the manifest", () => {
  assert.deepEqual(checkPins(repo), []);
});

test("installed toolchains match the pins", () => {
  assert.deepEqual(checkInstalled(repo), []);
});

test("each drifting pin is reported by id", () => {
  const dir = sandbox();
  writeFileSync(join(dir, ".node-version"), "20.0.0\n");
  const toolchain = join(dir, "rust-toolchain.toml");
  writeFileSync(toolchain, readFileSync(toolchain, "utf8").replace("1.97.0", "stable"));
  const gomod = join(dir, "backend/go.mod");
  writeFileSync(gomod, readFileSync(gomod, "utf8").replace("1.24.7", "1.23.0"));
  const pkg = join(dir, "package.json");
  writeFileSync(pkg, readFileSync(pkg, "utf8").replace('"typescript": "5.9.3"', '"typescript": "^5.9.3"'));
  const ids = checkPins(dir)
    .map((f) => f.id)
    .sort();
  assert.deepEqual(ids, [".node-version", "go.mod.go", "package.devDependencies", "rust-toolchain.channel"]);
});

test("a missing toolchain is reported, never skipped", () => {
  const findings = checkInstalled(repo, { PATH: "/nonexistent" });
  const missing = findings.filter((f) => f.id.startsWith("missing-toolchain.")).map((f) => f.id);
  assert.deepEqual(missing.sort(), [
    "missing-toolchain.cargo",
    "missing-toolchain.go",
    "missing-toolchain.node",
    "missing-toolchain.pnpm",
    "missing-toolchain.rustc",
  ]);
});

test("a wrong installed version is reported", () => {
  const dir = sandbox();
  const manifest = join(dir, "toolchain/versions.json");
  writeFileSync(manifest, readFileSync(manifest, "utf8").replace('"go": "1.24.7"', '"go": "1.0.0"'));
  const ids = checkInstalled(dir).map((f) => f.id);
  assert.ok(ids.includes("toolchain.go"));
});
