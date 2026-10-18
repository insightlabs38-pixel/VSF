import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { cpSync, mkdirSync, mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

const repo = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const tmp = (): string => mkdtempSync(join(tmpdir(), "vsf-adv-"));

function run(cmd: string, args: string[], cwd: string, env: NodeJS.ProcessEnv = process.env) {
  const r = spawnSync(cmd, args, { cwd, env, encoding: "utf8", maxBuffer: 64 * 1024 * 1024 });
  return { status: r.status, out: `${r.stdout}${r.stderr}` };
}

test("broken-lockfile: cargo --locked refuses a manifest the lockfile does not cover", () => {
  const dir = tmp();
  cpSync(join(repo, "compiler"), join(dir, "compiler"), {
    recursive: true,
    filter: (src) => !src.includes("/target"),
  });
  cpSync(join(repo, "rust-toolchain.toml"), join(dir, "rust-toolchain.toml"));
  cpSync(join(repo, "schemas"), join(dir, "schemas"), { recursive: true });
  const manifest = join(dir, "compiler/crates/vsf-canon/Cargo.toml");
  writeFileSync(manifest, readFileSync(manifest, "utf8").replace("[dependencies]", '[dependencies]\nitoa = "1"'));
  const r = run("cargo", ["build", "--locked", "--manifest-path", join(dir, "compiler/Cargo.toml")], dir);
  assert.notEqual(r.status, 0);
  assert.match(r.out, /lock file .* needs to be updated|cannot update the lock file/);
});

test("broken-lockfile: pnpm --frozen-lockfile refuses package.json drift", () => {
  const dir = tmp();
  for (const f of ["package.json", "pnpm-lock.yaml", "pnpm-workspace.yaml"]) cpSync(join(repo, f), join(dir, f));
  const pkg = join(dir, "package.json");
  writeFileSync(pkg, readFileSync(pkg, "utf8").replace('"typescript": "5.9.3"', '"typescript": "5.9.2"'));
  const r = run("pnpm", ["install", "--frozen-lockfile", "--offline", "--ignore-scripts"], dir);
  assert.notEqual(r.status, 0);
  assert.match(r.out, /ERR_PNPM_OUTDATED_LOCKFILE|frozen-lockfile/);
});

test("broken-lockfile: floating git sources and missing checksums are flagged by dep-review", () => {
  const dir = tmp();
  mkdirSync(join(dir, "compiler"), { recursive: true });
  const lock = `[[package]]\nname = "floating"\nversion = "1.0.0"\nsource = "git+https://example.test/x?branch=main#abc"\n`;
  writeFileSync(join(dir, "compiler/Cargo.lock"), lock);
  const script = `import {parseCargoLock} from ${JSON.stringify(join(repo, "tools/checks/src/depreview.ts"))};
    const r = parseCargoLock(process.argv[1]); console.log(JSON.stringify(r.findings.map(f=>f.rule)));`;
  const r = run(process.execPath, ["--input-type=module", "-e", script, lock], dir);
  assert.deepEqual(JSON.parse(r.out.trim()).sort(), ["cargo.missing-checksum", "cargo.non-registry-source"]);
});

test("fixture intentional-failure: a tampered golden fixture fails the check", () => {
  const dir = tmp();
  cpSync(join(repo, "fixtures/golden"), dir, { recursive: true });
  const file = join(dir, "compiled-seed-42.json");
  writeFileSync(file, readFileSync(file, "utf8").replace("synthetic-orders", "synthetic-orderz"));
  const r = run(process.execPath, [join(repo, "scripts/golden.ts"), "check"], repo, {
    ...process.env,
    VSF_GOLDEN_DIR: dir,
  });
  assert.equal(r.status, 1);
  assert.match(r.out, /golden mismatch: compiled-seed-42\.json/);
});

test("fixture intentional-failure: the CI canary job reports FAIL and verified logs", () => {
  const r = run(process.execPath, [join(repo, "tools/checks/src/cli.ts"), "ci-run", "canary"], repo, {
    ...process.env,
    VSF_INDUCE_FAILURE: "1",
  });
  assert.equal(r.status, 1);
  const manifest = JSON.parse(readFileSync(join(repo, "artifacts/ci/canary/manifest.json"), "utf8"));
  assert.equal(manifest.status, "FAIL");
  assert.equal(
    run(process.execPath, [join(repo, "tools/checks/src/cli.ts"), "ci-verify", join(repo, "artifacts/ci/canary")], repo)
      .status,
    0,
  );
});

test("redaction-leak: the repository scan flags a planted secret shape", () => {
  const planted = ["gh", "p_", "abcdefghijklmnopqrstuvwxyz0123456789"].join("");
  const file = join(repo, "tests/adversarial/.planted-leak.txt");
  writeFileSync(file, `token ${planted}\n`);
  try {
    const r = run(process.execPath, [join(repo, "tools/checks/src/cli.ts"), "secrets"], repo);
    assert.equal(r.status, 1);
    assert.match(r.out, /\.planted-leak\.txt: github_token/);
  } finally {
    writeFileSync(file, "");
    spawnSync("rm", ["-f", file]);
  }
  assert.equal(run(process.execPath, [join(repo, "tools/checks/src/cli.ts"), "secrets"], repo).status, 0);
});

test("missing-toolchain: the toolchain check fails loudly without cargo/go on PATH", () => {
  const r = run(process.execPath, [join(repo, "tools/checks/src/cli.ts"), "toolchain"], repo, {
    ...process.env,
    PATH: dirname(process.execPath),
  });
  assert.equal(r.status, 1);
  assert.match(r.out, /missing-toolchain\.cargo/);
  assert.match(r.out, /missing-toolchain\.go/);
});
