import { spawnSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { Ajv } from "ajv";
import { checkEvidence } from "./evidence.ts";
import type { Plan, State } from "./model.ts";
import { TASK_ID } from "./model.ts";
import { batchComplete, dependenciesDone, gateOf, nextTask } from "./queue.ts";
import { renderTasks } from "./render.ts";
import { checkReport } from "./report.ts";

export interface Problem {
  id: string;
  message: string;
}

export interface Paths {
  root: string;
  queueDir: string;
}

export const pathsFor = (root: string): Paths => ({ root, queueDir: join(root, "docs/deep/queue") });

const readJson = <T>(path: string): T => JSON.parse(readFileSync(path, "utf8")) as T;

export function loadPlan(p: Paths): Plan {
  return readJson<Plan>(join(p.queueDir, "tasks.json"));
}

export function loadState(p: Paths): State {
  return readJson<State>(join(p.queueDir, "state.json"));
}

function commitExists(root: string, sha: string): boolean {
  return spawnSync("git", ["cat-file", "-e", `${sha}^{commit}`], { cwd: root }).status === 0;
}

export interface ValidateOptions {
  git: boolean;
}

export function validateReceipt(p: Paths, receipt: unknown): Problem[] {
  const ajv = new Ajv({ allErrors: true, strict: false });
  const validate = ajv.compile(readJson(join(p.queueDir, "schemas/receipt.schema.json")));
  if (validate(receipt)) return [];
  return (validate.errors ?? []).map((e) => ({
    id: "receipt.schema",
    message: `${e.instancePath || "/"} ${e.message}`,
  }));
}

export function validateQueue(p: Paths, plan: Plan, state: State, opts: ValidateOptions): Problem[] {
  const out: Problem[] = [];
  const add = (id: string, message: string): void => void out.push({ id, message });

  const ajv = new Ajv({ allErrors: true, strict: false });
  const validateState = ajv.compile(readJson(join(p.queueDir, "schemas/state.schema.json")));
  if (!validateState(state)) {
    for (const e of validateState.errors ?? []) add("state.schema", `${e.instancePath || "/"} ${e.message}`);
    return out;
  }

  const planIds = new Set(plan.tasks.map((t) => t.id));
  for (const t of plan.tasks) {
    if (!TASK_ID.test(t.id)) add("plan.task-id", t.id);
    for (const d of t.dependencies) if (!planIds.has(d)) add("plan.unknown-dependency", `${t.id} -> ${d}`);
    if (t.origin === "adaptive" && (!t.adr || !t.acceptance))
      add("plan.adaptive-task", `${t.id} needs adr and acceptance`);
  }
  for (const id of planIds) if (!state.task_states[id]) add("state.missing-task", id);
  for (const id of Object.keys(state.task_states)) if (!planIds.has(id)) add("state.unknown-task", id);
  for (const b of plan.batches) if (!state.gate_states[b.gate]) add("state.missing-gate", b.gate);

  for (const t of plan.tasks) {
    const s = state.task_states[t.id];
    if (!s) continue;
    const deps = dependenciesDone(plan, state, t);
    if (["READY", "ACTIVE", "REVIEW", "DONE"].includes(s.status) && !deps)
      add("state.dependency-order", `${t.id} is ${s.status} but a dependency is not DONE`);
    const batch = plan.batches.find((b) => b.id === t.batch);
    if (
      batch &&
      s.status !== "TODO" &&
      !batch.depends_on.every((b) => state.gate_states[gateOf(plan, b)]?.status === "ACCEPTED")
    ) {
      add("state.gate-order", `${t.id} is ${s.status} before prerequisite gates were accepted`);
    }
    if (s.status === "DONE") {
      if (!s.commit) add("state.done-without-commit", t.id);
      if (!s.receipt) add("state.done-without-receipt", t.id);
      else if (!existsSync(join(p.root, s.receipt))) add("state.receipt-missing", `${t.id}: ${s.receipt}`);
      else {
        const receipt = readJson<{ task_id?: string; commit?: string; evidence_paths?: string[] }>(
          join(p.root, s.receipt),
        );
        for (const prob of validateReceipt(p, receipt)) add(prob.id, `${t.id}: ${prob.message}`);
        if (receipt.task_id !== t.id) add("receipt.task-mismatch", `${t.id}: receipt names ${receipt.task_id}`);
        if (receipt.commit !== s.commit) add("receipt.commit-mismatch", t.id);
        for (const ev of receipt.evidence_paths ?? [])
          if (!existsSync(join(p.root, ev))) add("receipt.evidence-missing", `${t.id}: ${ev}`);
      }
      if (opts.git && s.commit && !commitExists(p.root, s.commit)) add("state.commit-unknown", `${t.id}: ${s.commit}`);
    }
  }

  for (const b of plan.batches) {
    const gate = state.gate_states[b.gate];
    if (!gate) continue;
    if (gate.status === "ACCEPTED") {
      if (!batchComplete(plan, state, b.id))
        add("gate.accepted-incomplete", `${b.gate} accepted but ${b.id} has unfinished tasks`);
      if (gate.decision !== "PROCEED") add("gate.accepted-without-proceed", b.gate);
      if (!gate.report) add("gate.report-missing", b.gate);
      else if (!existsSync(join(p.root, gate.report))) add("gate.report-file-missing", `${b.gate}: ${gate.report}`);
      else {
        for (const prob of checkReport(readFileSync(join(p.root, gate.report), "utf8"), plan, state, p.root))
          add(prob.id, `${b.gate}: ${prob.message}`);
      }
      if (!gate.evidence) add("gate.evidence-missing", b.gate);
      else for (const prob of checkEvidence(p, plan, b.id)) add(prob.id, `${b.gate}: ${prob.message}`);
    } else if (gate.decision === "PROCEED") {
      add("gate.proceed-without-accept", b.gate);
    }
  }

  const next = nextTask(plan, state);
  if (state.status === "ACTIVE" && (state.cursor.task ?? null) !== (next?.task ?? null)) {
    add("state.cursor", `cursor is ${state.cursor.task} but next eligible is ${next?.task ?? "none"}`);
  }
  const tasksPath = join(p.root, "TASKS.md");
  if (!existsSync(tasksPath)) add("tasks-md.missing", "TASKS.md");
  else if (readFileSync(tasksPath, "utf8") !== renderTasks(plan, state))
    add("tasks-md.drift", "TASKS.md differs from state.json rendering");
  return out;
}
