import assert from "node:assert/strict";
import { test } from "node:test";
import { checkReport } from "../src/report.ts";
import { closedSandbox, goodReport } from "./helpers.ts";

const ids = (text: string, ctx: ReturnType<typeof closedSandbox>): string[] =>
  checkReport(text, ctx.plan, ctx.state, ctx.paths.root).map((p) => p.id);

test("valid report passes", () => {
  const ctx = closedSandbox();
  assert.deepEqual(ids(goodReport(), ctx), []);
});

test("reports over 24 lines are rejected; exactly 24 are accepted", () => {
  const ctx = closedSandbox();
  const base = goodReport().split("\n");
  const at24 = [...base, ...Array.from({ length: 24 - base.length }, (_, i) => `Note ${i}: x`)].join("\n");
  assert.equal(at24.split("\n").length, 24);
  assert.deepEqual(ids(at24, ctx), []);
  assert.ok(ids(`${at24}\nNote: extra`, ctx).includes("report.too-long"));
});

test("a report that omits a closed task ID is rejected", () => {
  const ctx = closedSandbox();
  assert.ok(ids(goodReport("B00", "G0", "PROCEED", "VSF-B00-T01"), ctx).includes("report.missing-task"));
});

test("a report naming a foreign or nonexistent task is rejected", () => {
  const ctx = closedSandbox();
  assert.ok(
    ids(goodReport("B00", "G0", "PROCEED", "VSF-B00-T01 VSF-B00-T02 VSF-B01-T01"), ctx).includes("report.unknown-task"),
  );
});

test("PROCEED is rejected while the gate is not accepted or tasks are unfinished", () => {
  const ctx = closedSandbox();
  const open = {
    ...ctx.state,
    gate_states: { ...ctx.state.gate_states, G1: { status: "PENDING" as const, decision: null, evidence: null } },
  };
  const report = goodReport("B01", "G1", "PROCEED", "VSF-B01-T01");
  const found = checkReport(report, ctx.plan, open, null).map((p) => p.id);
  assert.ok(found.includes("report.task-not-done"));
  assert.ok(found.includes("report.gate-not-accepted"));
});

test("a failing-gate report cannot claim an accepted gate", () => {
  const ctx = closedSandbox();
  assert.ok(ids(goodReport("B00", "G0", "HOLD"), ctx).includes("report.decision-mismatch"));
  assert.ok(ids(goodReport("B00", "G1", "PROCEED"), ctx).includes("report.gate-mismatch"));
});

test("malformed header and missing fields are rejected", () => {
  const ctx = closedSandbox();
  assert.deepEqual(ids("Batch B00", ctx), ["report.header"]);
  const missing = goodReport()
    .split("\n")
    .filter((l) => !l.startsWith("Tests:"))
    .join("\n");
  assert.ok(ids(missing, ctx).includes("report.missing-field"));
  assert.ok(ids(goodReport("B99", "G99"), ctx).includes("report.unknown-batch"));
});

test("PROCEED requires an existing evidence directory", () => {
  const ctx = closedSandbox();
  const moved = goodReport().replace("docs/deep/evidence/VSF-B00", "docs/deep/evidence/VSF-B00-nope");
  assert.ok(ids(moved, ctx).includes("report.evidence-missing"));
});
