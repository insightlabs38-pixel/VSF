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

const lanePlan = (): typeof miniPlan => ({
  batches: miniPlan.batches,
  tasks: [
    { id: "VSF-B00-T01", batch: "B00", sequence: 1, title: "First", acceptance: "a", dependencies: [] },
    { id: "VSF-B00-T02", batch: "B00", sequence: 2, title: "Second", acceptance: "b", dependencies: ["VSF-B00-T01"] },
    {
      id: "VSF-B00-T03",
      batch: "B00",
      sequence: 3,
      title: "Fix",
      acceptance: "c",
      dependencies: [],
      origin: "adaptive",
      adr: "ADR-032",
      lane: "correction",
      scope: "s",
      contract: "k",
      positive: "p",
      adversarial: "n",
      evidence_required: "e",
    },
    { id: "VSF-B01-T01", batch: "B01", sequence: 1, title: "Third", acceptance: "c", dependencies: [] },
  ],
});

test("a correction task runs ahead of the sequential lane without completing earlier tasks", () => {
  const plan = lanePlan();
  const state = freshState(plan);
  assert.deepEqual(nextTask(plan, state), { batch: "B00", task: "VSF-B00-T03" });
  setTask(plan, state, "VSF-B00-T03", "ACTIVE");
  assert.equal(state.task_states["VSF-B00-T01"]?.status, "READY");
  assert.throws(() => setTask(plan, state, "VSF-B00-T02", "ACTIVE"), /not allowed|not DONE/);
  setTask(plan, state, "VSF-B00-T03", "REVIEW");
  setTask(plan, state, "VSF-B00-T03", "DONE", { commit: SHA, receipt: "r" });
  assert.deepEqual(nextTask(plan, state), { batch: "B00", task: "VSF-B00-T01" });
});

test("a gate needs every correction task, and the validator names the open one", () => {
  const plan = lanePlan();
  const state = freshState(plan);
  for (const id of ["VSF-B00-T01", "VSF-B00-T02"]) {
    state.task_states[id] = { status: "DONE", claimed_by: null, commit: SHA, evidence: null, receipt: "r" };
  }
  assert.throws(
    () => setGate(plan, state, "G0", "ACCEPTED", "PROCEED", { report: "r", evidence: "e" }),
    /correction tasks unfinished: VSF-B00-T03/,
  );
  state.gate_states.G0 = { status: "ACCEPTED", decision: "PROCEED", evidence: "e", report: "r" };
  const { paths } = closedSandbox();
  assert.ok(ids(validateQueue(paths, plan, state, { git: false })).includes("gate.accepted-corrections-open"));
});

test("a correction task must state scope, contract, tests and evidence", () => {
  const plan = lanePlan();
  const t = plan.tasks[2] as { adversarial?: string };
  delete t.adversarial;
  const { paths } = closedSandbox();
  const found = validateQueue(paths, plan, freshState(plan), { git: false });
  assert.ok(found.some((p) => p.id === "plan.correction-task" && p.message.includes("adversarial")));
});

test("a prerequisite appended with a later number runs first when an earlier task depends on it", () => {
  const plan = structuredClone(miniPlan);
  const first = plan.tasks.find((t) => t.id === "VSF-B00-T01");
  if (!first) throw new Error("fixture");
  first.dependencies = ["VSF-B00-T03"];
  plan.tasks.push({
    id: "VSF-B00-T03",
    batch: "B00",
    sequence: 3,
    title: "Appended prerequisite",
    acceptance: "d",
    dependencies: [],
    origin: "adaptive",
    adr: "ADR-040",
    lane: "correction",
    scope: "x",
    contract: "x",
    positive: "x",
    adversarial: "x",
    evidence_required: "x",
  });
  const state = freshState(plan);
  assert.deepEqual(nextTask(plan, state), { batch: "B00", task: "VSF-B00-T03" });
  assert.equal(state.task_states["VSF-B00-T01"]?.status, "TODO");
  assert.throws(() => setTask(plan, state, "VSF-B00-T01", "READY"), /not allowed|dependencies/);
  setTask(plan, state, "VSF-B00-T03", "ACTIVE");
  setTask(plan, state, "VSF-B00-T03", "REVIEW");
  setTask(plan, state, "VSF-B00-T03", "DONE", { commit: SHA, receipt: "r.json" });
  assert.equal(state.task_states["VSF-B00-T01"]?.status, "READY");
  assert.deepEqual(nextTask(plan, state), { batch: "B00", task: "VSF-B00-T01" });
});
