import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { writeFileSync } from "node:fs";
import { join } from "node:path";
import { test } from "node:test";
import { checkEvidence, type EvidenceManifest, evidenceDir, initEvidence } from "../src/evidence.ts";
import { closedSandbox } from "./helpers.ts";

function bundle(logText: string, tweak?: (m: EvidenceManifest) => void) {
  const ctx = closedSandbox();
  const dir = initEvidence(ctx.paths, ctx.plan, "B01");
  writeFileSync(join(dir, "logs/run.log"), logText);
  const manifest: EvidenceManifest = {
    schema: "vsf-evidence-v1",
    batch: "B01",
    gate: "G1",
    runs: [
      {
        task_id: "VSF-B01-T01",
        name: "unit",
        command: "node --test",
        exit_code: 0,
        result: "PASS",
        log: "logs/run.log",
        sha256: createHash("sha256").update(logText).digest("hex"),
      },
    ],
  };
  tweak?.(manifest);
  writeFileSync(join(evidenceDir(ctx.paths, "B01"), "manifest.json"), JSON.stringify(manifest));
  return ctx;
}

const ids = (ctx: ReturnType<typeof closedSandbox>): string[] =>
  checkEvidence(ctx.paths, ctx.plan, "B01").map((p) => p.id);

test("a clean bundle passes and the template refuses to overwrite", () => {
  const ctx = bundle("all good\n");
  assert.deepEqual(ids(ctx), []);
  assert.throws(() => initEvidence(ctx.paths, ctx.plan, "B01"), /already exists/);
});

test("checksum mismatch, PASS with nonzero exit and unknown tasks are rejected", () => {
  assert.ok(
    ids(
      bundle("x\n", (m) => {
        (m.runs[0] as { sha256: string }).sha256 = "0".repeat(64);
      }),
    ).includes("evidence.checksum"),
  );
  assert.ok(
    ids(
      bundle("x\n", (m) => {
        (m.runs[0] as { exit_code: number }).exit_code = 1;
      }),
    ).includes("evidence.pass-with-failure"),
  );
  assert.ok(
    ids(
      bundle("x\n", (m) => {
        (m.runs[0] as { task_id: string }).task_id = "VSF-B00-T01";
      }),
    ).includes("evidence.unknown-task"),
  );
});

test("a skipped check needs a reason and cannot hide as a pass", () => {
  const skip = bundle("x\n", (m) => {
    (m.runs[0] as { result: string }).result = "SKIPPED";
  });
  assert.ok(ids(skip).includes("evidence.skip-without-reason"));
});

test("secret-shaped content anywhere in the bundle is rejected", () => {
  const secret = ["gh", "p_", "abcdefghijklmnopqrstuvwxyz0123456789"].join("");
  assert.ok(ids(bundle(`token ${secret}\n`)).includes("evidence.secret-shape"));
});

test("a missing bundle is reported", () => {
  const ctx = closedSandbox();
  assert.deepEqual(ids(ctx), ["evidence.missing"]);
});

test("recorded runs are checksummed, failures stay FAIL and duplicates are refused", async () => {
  const { recordRun } = await import("../src/evidence.ts");
  const ctx = closedSandbox();
  initEvidence(ctx.paths, ctx.plan, "B01");
  const ok = recordRun(ctx.paths, ctx.plan, "B01", "VSF-B01-T01", "pass", [
    process.execPath,
    "-e",
    "console.log('fine')",
  ]);
  assert.equal(ok.result, "PASS");
  const bad = recordRun(ctx.paths, ctx.plan, "B01", "VSF-B01-T01", "fail", [process.execPath, "-e", "process.exit(4)"]);
  assert.deepEqual([bad.result, bad.exit_code], ["FAIL", 4]);
  assert.deepEqual(ids(ctx), []);
  assert.throws(() => recordRun(ctx.paths, ctx.plan, "B01", "VSF-B01-T01", "pass", ["true"]), /already recorded/);
  assert.throws(() => recordRun(ctx.paths, ctx.plan, "B01", "VSF-B00-T01", "x", ["true"]), /not a task of B01/);
});
