import { createHash } from "node:crypto";
import { cpSync, mkdirSync, mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import type { Plan, State } from "../src/model.ts";
import { renderTasks } from "../src/render.ts";
import { refresh, setGate, setTask } from "../src/transition.ts";
import { type Paths, pathsFor } from "../src/validate.ts";

export const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "../../..");
export const SHA = "a".repeat(40);
export const SHA2 = "b".repeat(40);

export const miniPlan: Plan = {
  batches: [
    { id: "B00", name: "Foundation", gate: "G0", depends_on: [], milestone: "M0" },
    { id: "B01", name: "Boundaries", gate: "G1", depends_on: ["B00"], milestone: "M0" },
  ],
  tasks: [
    { id: "VSF-B00-T01", batch: "B00", sequence: 1, title: "First", acceptance: "a", dependencies: [] },
    { id: "VSF-B00-T02", batch: "B00", sequence: 2, title: "Second", acceptance: "b", dependencies: ["VSF-B00-T01"] },
    { id: "VSF-B01-T01", batch: "B01", sequence: 1, title: "Third", acceptance: "c", dependencies: [] },
  ],
};

export function freshState(plan: Plan = miniPlan): State {
  const state: State = {
    schema: "vsf-queue-v1",
    initialized: "2026-10-18",
    source_plan: "test",
    status: "ACTIVE",
    cursor: { batch: null, task: null },
    task_states: Object.fromEntries(
      plan.tasks.map((t) => [t.id, { status: "TODO", claimed_by: null, commit: null, evidence: null }]),
    ),
    gate_states: Object.fromEntries(
      plan.batches.map((b) => [b.gate, { status: "PENDING", decision: null, evidence: null }]),
    ),
  };
  return refresh(plan, state);
}

export function receiptFor(id: string, batch: string): Record<string, unknown> {
  return {
    task_id: id,
    status: "DONE",
    branch: "vsf/test",
    base_commit: SHA2,
    commit: SHA,
    paths: ["src/x.ts"],
    acceptance: "criterion verified",
    positive: { fixture: "fx-1", command: "node --test", result: "PASS" },
    negative: { fixture: "fx-neg", command: "node --test", result: "PASS" },
    regression: [{ command: "pnpm test", result: "PASS" }],
    versions: { schema: "unchanged", primitive: "none" },
    security_and_source_permissions: "unchanged",
    performance_memory_storage: "not measured",
    known_limitations: [],
    evidence_paths: [`docs/deep/evidence/VSF-${batch}/logs/${id}.log`],
    review_result: "ACCEPTED",
  };
}

export const goodReport = (
  batch = "B00",
  gate = "G0",
  decision = "PROCEED",
  closed = "VSF-B00-T01 VSF-B00-T02",
): string =>
  [
    `Batch: ${batch} | Gate: ${gate} | Decision: ${decision}`,
    "Branch: vsf/test | Base: bbb | Commit: aaa",
    "PR: none",
    `Closed tasks: ${closed}`,
    "Contracts/primitive IDs: none",
    "Changed modules: src",
    "Tests: 10 passed, 0 failed",
    "Negative/adversarial: 3 passed",
    "Performance: n/a",
    "Security/session scope: unchanged",
    "Source compatibility: n/a",
    "Version/pin/retention: n/a",
    "Known limitations: none",
    `Evidence: docs/deep/evidence/VSF-${batch}`,
    "Next eligible: VSF-B01-T01",
  ].join("\n");

/** A temp repository holding a fully closed B00 and an open B01. */
export function closedSandbox(): { paths: Paths; plan: Plan; state: State } {
  const root = mkdtempSync(join(tmpdir(), "vsf-queue-"));
  const paths = pathsFor(root);
  mkdirSync(join(paths.queueDir, "receipts"), { recursive: true });
  mkdirSync(join(root, "docs/deep/reports"), { recursive: true });
  mkdirSync(join(root, "docs/deep/evidence/VSF-B00/logs"), { recursive: true });
  cpSync(join(repoRoot, "docs/deep/queue/schemas"), join(paths.queueDir, "schemas"), { recursive: true });
  const plan = miniPlan;
  const state = freshState(plan);
  for (const t of plan.tasks.filter((x) => x.batch === "B00")) {
    writeFileSync(join(root, `docs/deep/evidence/VSF-B00/logs/${t.id}.log`), "ok\n");
    const receipt = `docs/deep/queue/receipts/${t.id}.json`;
    writeFileSync(join(root, receipt), JSON.stringify(receiptFor(t.id, "B00")));
    setTask(plan, state, t.id, "ACTIVE");
    setTask(plan, state, t.id, "REVIEW");
    setTask(plan, state, t.id, "DONE", { commit: SHA, receipt });
  }
  const runs = plan.tasks
    .filter((x) => x.batch === "B00")
    .map((t) => ({
      task_id: t.id,
      name: `verify-${t.id}`,
      command: "node --test",
      exit_code: 0,
      result: "PASS",
      log: `logs/${t.id}.log`,
      sha256: createHash("sha256").update("ok\n").digest("hex"),
    }));
  writeFileSync(
    join(root, "docs/deep/evidence/VSF-B00/manifest.json"),
    JSON.stringify({ schema: "vsf-evidence-v1", batch: "B00", gate: "G0", runs }),
  );
  writeFileSync(join(root, "docs/deep/reports/VSF-B00.md"), `${goodReport()}\n`);
  setGate(plan, state, "G0", "ACCEPTED", "PROCEED", {
    report: "docs/deep/reports/VSF-B00.md",
    evidence: "docs/deep/evidence/VSF-B00",
  });
  writeFileSync(join(root, "TASKS.md"), renderTasks(plan, state));
  writeFileSync(join(paths.queueDir, "tasks.json"), JSON.stringify(plan));
  writeFileSync(join(paths.queueDir, "state.json"), JSON.stringify(state));
  return { paths, plan, state };
}
