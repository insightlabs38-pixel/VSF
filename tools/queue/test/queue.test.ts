import assert from "node:assert/strict";
import { readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { test } from "node:test";
import { nextTask } from "../src/queue.ts";
import { renderTasks } from "../src/render.ts";
import { setGate, setTask } from "../src/transition.ts";
import { loadPlan, loadState, pathsFor, validateQueue, validateReceipt } from "../src/validate.ts";
import { closedSandbox, freshState, miniPlan, receiptFor, repoRoot, SHA } from "./helpers.ts";

const ids = (problems: { id: string }[]): string[] => problems.map((p) => p.id);

test("a fully closed batch validates with receipts, report and rendered TASKS.md", () => {
  const { paths, plan, state } = closedSandbox();
  assert.deepEqual(validateQueue(paths, plan, state, { git: false }), []);
  assert.deepEqual(nextTask(plan, state), { batch: "B01", task: "VSF-B01-T01" });
});

test("illegal transitions are rejected", () => {
  const state = freshState();
  assert.throws(() => setTask(miniPlan, state, "VSF-B00-T01", "DONE"), /not allowed/);
  assert.throws(() => setTask(miniPlan, state, "VSF-B00-T02", "READY"), /not allowed|dependencies/);
  setTask(miniPlan, state, "VSF-B00-T01", "ACTIVE");
  setTask(miniPlan, state, "VSF-B00-T01", "REVIEW");
  assert.throws(() => setTask(miniPlan, state, "VSF-B00-T01", "DONE"), /requires a commit/);
  assert.throws(() => setTask(miniPlan, state, "VSF-B00-T01", "DONE", { commit: SHA }), /requires a receipt/);
});

test("a later batch cannot start before the prior gate is accepted", () => {
  const state = freshState();
  assert.equal(state.task_states["VSF-B01-T01"]?.status, "TODO");
  assert.throws(() => setTask(miniPlan, state, "VSF-B01-T01", "READY"), /not satisfied/);
  state.task_states["VSF-B01-T01"] = { status: "ACTIVE", claimed_by: null, commit: null, evidence: null };
  const { paths } = closedSandbox();
  assert.ok(ids(validateQueue(paths, miniPlan, state, { git: false })).includes("state.gate-order"));
});

test("a gate cannot be accepted with unfinished tasks or without PROCEED", () => {
  const state = freshState();
  assert.throws(
    () => setGate(miniPlan, state, "G0", "ACCEPTED", "PROCEED", { report: "r", evidence: "e" }),
    /unfinished tasks/,
  );
  assert.throws(() => setGate(miniPlan, state, "G0", "ACCEPTED", "HOLD"), /requires decision PROCEED/);
  assert.throws(() => setGate(miniPlan, state, "G0", "PENDING", "PROCEED"), /requires ACCEPTED/);
});

test("DONE without a receipt, or with a tampered receipt, fails validation", () => {
  const { paths, plan, state } = closedSandbox();
  const bad = structuredClone(state);
  (bad.task_states["VSF-B00-T01"] as { receipt: string | null }).receipt = null;
  assert.ok(ids(validateQueue(paths, plan, bad, { git: false })).includes("state.done-without-receipt"));
  const receiptPath = join(paths.root, "docs/deep/queue/receipts/VSF-B00-T02.json");
  const receipt = JSON.parse(readFileSync(receiptPath, "utf8"));
  receipt.commit = "c".repeat(40);
  writeFileSync(receiptPath, JSON.stringify(receipt));
  assert.ok(ids(validateQueue(paths, plan, state, { git: false })).includes("receipt.commit-mismatch"));
});

test("receipt schema accepts valid evidence and rejects incomplete or failing receipts", () => {
  const { paths } = closedSandbox();
  assert.deepEqual(validateReceipt(paths, receiptFor("VSF-B00-T01", "B00")), []);
  const missingNegative = receiptFor("VSF-B00-T01", "B00");
  delete missingNegative.negative;
  assert.ok(validateReceipt(paths, missingNegative).length > 0);
  const failing = receiptFor("VSF-B00-T01", "B00");
  failing.positive = { fixture: "f", command: "c", result: "FAIL" };
  assert.ok(validateReceipt(paths, failing).length > 0);
  const badSha = receiptFor("VSF-B00-T01", "B00");
  badSha.commit = "not-a-sha";
  assert.ok(validateReceipt(paths, badSha).length > 0);
  const offBundle = receiptFor("VSF-B00-T01", "B00");
  offBundle.evidence_paths = ["elsewhere/log.txt"];
  assert.ok(validateReceipt(paths, offBundle).length > 0);
});

test("TASKS.md drift is detected", () => {
  const { paths, plan, state } = closedSandbox();
  writeFileSync(join(paths.root, "TASKS.md"), `${renderTasks(plan, state)}extra\n`);
  assert.ok(ids(validateQueue(paths, plan, state, { git: false })).includes("tasks-md.drift"));
});

test("the repository's real queue is internally consistent", () => {
  const p = pathsFor(repoRoot);
  assert.deepEqual(validateQueue(p, loadPlan(p), loadState(p), { git: false }), []);
  assert.equal(loadPlan(p).tasks.length >= 152, true);
});
