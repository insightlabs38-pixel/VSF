import assert from "node:assert/strict";
import { mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { type JobFile, runJob, verifyManifest } from "../src/ci.ts";
import { digestTree, rollup } from "../src/digest.ts";

const jobs: JobFile = {
  jobs: {
    ok: [{ name: "echo", cmd: ["node", "-e", "console.log('hello')"] }],
    mixed: [
      { name: "fails", cmd: ["node", "-e", "console.error('boom'); process.exit(3)"] },
      { name: "still-runs", cmd: ["node", "-e", "console.log('after failure')"] },
    ],
    missing: [{ name: "nope", cmd: ["definitely-not-a-real-binary-xyz"] }],
  },
};
const tmp = (): string => mkdtempSync(join(tmpdir(), "vsf-ci-"));

test("passing job records checksums that verify", () => {
  const out = tmp();
  const m = runJob(tmp(), jobs, "ok", out, false);
  assert.equal(m.status, "PASS");
  assert.deepEqual(verifyManifest(out), []);
});

test("a failing step fails the job but later steps still run and are logged", () => {
  const out = tmp();
  const m = runJob(tmp(), jobs, "mixed", out, false);
  assert.equal(m.status, "FAIL");
  assert.deepEqual(
    m.steps.map((s) => s.exit),
    [3, 0],
  );
  assert.match(readFileSync(join(out, "logs/still-runs.log"), "utf8"), /after failure/);
  assert.match(readFileSync(join(out, "summary.md"), "utf8"), /FAILED: fails/);
});

test("induced failure canary turns a green job red", () => {
  const out = tmp();
  const m = runJob(tmp(), jobs, "ok", out, true);
  assert.equal(m.status, "FAIL");
  assert.equal(m.steps.at(-1)?.name, "induced-failure-canary");
  assert.equal(m.induced_failure, true);
});

test("a missing binary is a failure, not a skip", () => {
  const m = runJob(tmp(), jobs, "missing", tmp(), false);
  assert.equal(m.status, "FAIL");
  assert.equal(m.steps[0]?.exit, 127);
});

test("tampered or deleted logs and forged PASS status are detected", () => {
  const out = tmp();
  runJob(tmp(), jobs, "mixed", out, false);
  writeFileSync(join(out, "logs/fails.log"), "edited\n");
  assert.deepEqual(verifyManifest(out), ["fails: checksum mismatch"]);
  const manifest = JSON.parse(readFileSync(join(out, "manifest.json"), "utf8"));
  manifest.status = "PASS";
  writeFileSync(join(out, "manifest.json"), JSON.stringify(manifest));
  assert.ok(verifyManifest(out).includes("manifest status is PASS but a step failed"));
});

test("secret-shaped output never reaches the stored log", () => {
  const secret = ["gh", "p_", "abcdefghijklmnopqrstuvwxyz0123456789"].join("");
  const script = `console.log(${JSON.stringify(secret)}); console.error(['api','_key=','0123456789abcdef'].join(''))`;
  const out = tmp();
  runJob(tmp(), { jobs: { leak: [{ name: "leak", cmd: ["node", "-e", script] }] } }, "leak", out, false);
  const log = readFileSync(join(out, "logs/leak.log"), "utf8");
  assert.ok(!log.includes(secret));
  assert.ok(!log.includes(["api_key", "=", "0123456789abcdef"].join("")));
  assert.match(log, /\[REDACTED:keyed_secret\]/);
  assert.match(log, /\[REDACTED:github_token\]/);
});

test("tree digests are order-independent of creation and change with content", () => {
  const dir = tmp();
  writeFileSync(join(dir, "b.bin"), "2");
  writeFileSync(join(dir, "a.bin"), "1");
  const first = rollup(digestTree(dir, ["."]));
  assert.equal(rollup(digestTree(dir, ["."])), first);
  writeFileSync(join(dir, "a.bin"), "3");
  assert.notEqual(rollup(digestTree(dir, ["."])), first);
});
