import assert from "node:assert/strict";
import { cpSync, mkdirSync, mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import { GAPS_PATH, REGISTRY_PATH, SOURCE_CSV, SOURCE_GAPS, SOURCE_JSON, validateRegistry } from "../src/catalog.ts";

const repo = resolve(dirname(fileURLToPath(import.meta.url)), "../../..");

function sandbox(): string {
  const dir = mkdtempSync(join(tmpdir(), "vsf-catalog-"));
  for (const rel of [SOURCE_JSON, SOURCE_CSV, SOURCE_GAPS, REGISTRY_PATH, GAPS_PATH]) {
    mkdirSync(dirname(join(dir, rel)), { recursive: true });
    cpSync(join(repo, rel), join(dir, rel));
  }
  return dir;
}

test("imported registry is complete and entirely unimplemented", () => {
  assert.deepEqual(validateRegistry(repo), []);
  const reg = JSON.parse(readFileSync(join(repo, REGISTRY_PATH), "utf8"));
  assert.equal(reg.entries.length, 924);
  assert.equal(reg.families.length, 29);
  assert.ok(reg.entries.every((e: { maturity: string }) => e.maturity === "candidate"));
});

test("gap register imports all 41 gaps as open", () => {
  const gaps = JSON.parse(readFileSync(join(repo, GAPS_PATH), "utf8"));
  assert.equal(gaps.gaps.length, 41);
  assert.equal(gaps.gaps.filter((g: { priority: string }) => g.priority === "P0").length, 16);
});

test("promoting an entry without conformance evidence is rejected", () => {
  const dir = sandbox();
  const path = join(dir, REGISTRY_PATH);
  const reg = JSON.parse(readFileSync(path, "utf8"));
  reg.entries[0].maturity = "implemented";
  writeFileSync(path, JSON.stringify(reg));
  assert.ok(validateRegistry(dir).some((p) => p.includes("without conformance evidence")));
});

test("dropped, duplicated and cross-family entries are rejected", () => {
  const dir = sandbox();
  const path = join(dir, REGISTRY_PATH);
  const reg = JSON.parse(readFileSync(path, "utf8"));
  reg.entries[1] = { ...reg.entries[0] };
  reg.entries.pop();
  writeFileSync(path, JSON.stringify(reg));
  const problems = validateRegistry(dir).join("\n");
  assert.match(problems, /duplicate id/);
  assert.match(problems, /expected 924 entries/);
});

test("a closed gap without evidence is rejected", () => {
  const dir = sandbox();
  const path = join(dir, GAPS_PATH);
  const gaps = JSON.parse(readFileSync(path, "utf8"));
  gaps.gaps[0].status = "closed";
  writeFileSync(path, JSON.stringify(gaps));
  assert.ok(validateRegistry(dir).some((p) => p.includes("closed gap")));
});
