import assert from "node:assert/strict";
import { test } from "node:test";
import {
  checkLicenses,
  licenseAllowed,
  matchAdvisories,
  parseCargoLock,
  parseGoMod,
  parsePnpmLock,
  versionBelow,
} from "../src/depreview.ts";

const GOOD_CARGO = `version = 4

[[package]]
name = "vsf-canon"
version = "0.0.0"

[[package]]
name = "sha2"
version = "0.11.1"
source = "registry+https://github.com/rust-lang/crates.io-index"
checksum = "abc"
`;

test("clean cargo lock passes; workspace members are ignored", () => {
  const r = parseCargoLock(GOOD_CARGO);
  assert.deepEqual(r.findings, []);
  assert.deepEqual(
    r.locked.map((l) => l.name),
    ["sha2"],
  );
});

test("floating git sources and missing checksums are flagged", () => {
  const bad = `${GOOD_CARGO}
[[package]]
name = "evil"
version = "1.0.0"
source = "git+https://example.test/evil?branch=main#deadbeef"

[[package]]
name = "nosum"
version = "1.0.0"
source = "registry+https://github.com/rust-lang/crates.io-index"
`;
  const rules = parseCargoLock(bad).findings.map((f) => `${f.rule}:${f.package}`);
  assert.deepEqual(rules.sort(), [
    "cargo.missing-checksum:evil@1.0.0",
    "cargo.missing-checksum:nosum@1.0.0",
    "cargo.non-registry-source:evil@1.0.0",
  ]);
});

const PNPM = `lockfileVersion: '9.0'

importers:

  .:
    dependencies:
      lodash:
        specifier: 4.17.20
        version: 4.17.20

packages:

  'lodash@4.17.20':
    resolution: {integrity: sha512-aaa}

  '@scope/pkg@1.0.0':
    resolution: {tarball: https://example.test/pkg.tgz}

  'plain@1.0.0':
    resolution: {integrity: sha512-bbb}

snapshots:

  'lodash@4.17.20': {}
`;

test("pnpm lock parsing flags tarball sources and missing integrity", () => {
  const r = parsePnpmLock(PNPM);
  assert.deepEqual(
    r.locked.map((l) => `${l.name}@${l.version}`),
    ["lodash@4.17.20", "@scope/pkg@1.0.0", "plain@1.0.0"],
  );
  assert.deepEqual(r.findings.map((f) => f.rule).sort(), ["npm.missing-integrity", "npm.non-registry-source"]);
});

test("known-vulnerable test dependencies are flagged by the local advisories", () => {
  const advisories = [
    { id: "CVE-2021-23337", ecosystem: "npm" as const, name: "lodash", below: "4.17.21", reason: "command injection" },
    { id: "RUSTSEC-2021-0003", ecosystem: "cargo" as const, name: "smallvec", below: "1.6.1", reason: "overflow" },
  ];
  const hits = matchAdvisories(
    [
      { ecosystem: "npm", name: "lodash", version: "4.17.20" },
      { ecosystem: "npm", name: "lodash", version: "4.17.21" },
      { ecosystem: "cargo", name: "smallvec", version: "1.6.0" },
      { ecosystem: "cargo", name: "smallvec", version: "1.13.2" },
    ],
    advisories,
  );
  assert.deepEqual(
    hits.map((h) => h.package),
    ["lodash@4.17.20", "smallvec@1.6.0"],
  );
});

test("version comparison boundaries", () => {
  assert.equal(versionBelow("4.17.20", "4.17.21"), true);
  assert.equal(versionBelow("4.17.21", "4.17.21"), false);
  assert.equal(versionBelow("5.0.0", "4.17.21"), false);
  assert.equal(versionBelow("0.3.6", "0.3.7"), true);
});

test("go replace directives to remote modules and missing sums are flagged", () => {
  const mod =
    "module m\n\ngo 1.24.7\n\nrequire golang.org/x/text v0.3.6\n\nreplace example.test/a => example.test/b v1.0.0\nreplace example.test/c => ../local\n";
  const r = parseGoMod(mod, "");
  assert.deepEqual(r.findings.map((f) => f.rule).sort(), ["go.missing-sum", "go.non-local-replace"]);
  assert.equal(parseGoMod("module m\n\ngo 1.24.7\n", "").locked.length, 0);
});

test("license policy: permissive passes, copyleft and missing fail", () => {
  assert.equal(licenseAllowed("MIT OR Apache-2.0"), true);
  assert.equal(licenseAllowed("(MIT AND Zlib)"), true);
  assert.equal(licenseAllowed("Apache-2.0 WITH LLVM-exception"), true);
  assert.equal(licenseAllowed("GPL-3.0-only"), false);
  assert.equal(licenseAllowed("MIT AND GPL-3.0-only"), false);
  assert.equal(licenseAllowed("AGPL-3.0 OR MIT"), true);
  const f = checkLicenses([
    { name: "a", version: "1", license: "MIT" },
    { name: "b", version: "1", license: "GPL-3.0-only" },
    { name: "c", version: "1", license: undefined },
  ]);
  assert.deepEqual(
    f.map((x) => x.rule),
    ["license.disallowed", "license.missing"],
  );
});

test("license overrides fill in uninstalled platform packages and do not mask real problems", async () => {
  const { applyOverrides } = await import("../src/depscan.ts");
  const out = applyOverrides(
    [
      { name: "a", version: "1", license: undefined },
      { name: "b", version: "1", license: "GPL-3.0-only" },
    ],
    [
      { name: "a", version: "1", license: "MIT" },
      { name: "b", version: "1", license: "MIT" },
      { name: "c", version: "9", license: "MIT" },
    ],
  );
  assert.equal(out.entries[0]?.license, "MIT");
  assert.equal(out.entries[1]?.license, "GPL-3.0-only");
  assert.deepEqual(
    out.stale.map((s) => s.package),
    ["b@1", "c@9"],
  );
});

test("prefix overrides cover a whole platform-package family", async () => {
  const { applyOverrides } = await import("../src/depscan.ts");
  const out = applyOverrides(
    [
      { name: "@esbuild/linux-arm64", version: "0.28.2", license: undefined },
      { name: "@esbuild/linux-x64", version: "0.28.2", license: "MIT" },
      { name: "@esbuild/win32-x64", version: "9.9.9", license: undefined },
    ],
    [{ prefix: "@esbuild/", version: "0.28.2", license: "MIT" }],
  );
  assert.deepEqual(
    out.entries.map((e) => e.license),
    ["MIT", "MIT", undefined],
  );
  assert.equal(out.stale.length, 0);
});
