import type { Decision, GateStatus, Plan, State, TaskStatus } from "./model.ts";
import { batchAccepted, batchComplete, batchTasks, isEligible, nextTask } from "./queue.ts";

const ALLOWED: Record<TaskStatus, TaskStatus[]> = {
  TODO: ["READY"],
  READY: ["ACTIVE", "BLOCKED"],
  ACTIVE: ["REVIEW", "CORRECTIONS", "BLOCKED"],
  REVIEW: ["DONE", "CORRECTIONS", "BLOCKED"],
  CORRECTIONS: ["ACTIVE", "BLOCKED"],
  BLOCKED: ["READY", "ACTIVE"],
  DONE: [],
};

export interface TaskPatch {
  commit?: string;
  branch?: string;
  base_commit?: string;
  receipt?: string;
  evidence?: string;
  claimed_by?: string;
}

/** Promotes dependency-satisfied TODO tasks to READY and recomputes the cursor; never skips ahead of gates. */
export function refresh(plan: Plan, state: State): State {
  for (const t of plan.tasks) {
    const s = state.task_states[t.id];
    if (s && s.status === "TODO" && isEligible(plan, state, t)) s.status = "READY";
  }
  const next = nextTask(plan, state);
  state.cursor = { batch: next?.batch ?? null, task: next?.task ?? null };
  state.status = plan.batches.every((b) => batchAccepted(plan, state, b.id)) ? "COMPLETE" : "ACTIVE";
  return state;
}

export function setTask(plan: Plan, state: State, id: string, to: TaskStatus, patch: TaskPatch = {}): State {
  const t = plan.tasks.find((x) => x.id === id);
  const s = state.task_states[id];
  if (!t || !s) throw new Error(`unknown task ${id}`);
  if (!ALLOWED[s.status].includes(to)) throw new Error(`${id}: transition ${s.status} -> ${to} is not allowed`);
  if (to === "READY" && !isEligible(plan, state, t))
    throw new Error(`${id}: dependencies or prerequisite gates are not satisfied`);
  if (to === "ACTIVE") {
    const earlier = batchTasks(plan, t.batch).find(
      (o) => o.sequence < t.sequence && state.task_states[o.id]?.status !== "DONE",
    );
    if (earlier) throw new Error(`${id}: earlier task ${earlier.id} in the batch is not DONE`);
  }
  if (to === "DONE") {
    if (!patch.commit && !s.commit) throw new Error(`${id}: DONE requires a commit`);
    if (!patch.receipt && !s.receipt) throw new Error(`${id}: DONE requires a receipt`);
  }
  s.status = to;
  Object.assign(s, patch);
  return refresh(plan, state);
}

export interface GatePatch {
  evidence?: string;
  report?: string;
  pr?: string;
  merge_commit?: string;
}

export function setGate(
  plan: Plan,
  state: State,
  gate: string,
  status: GateStatus,
  decision: Decision | null,
  patch: GatePatch = {},
): State {
  const batch = plan.batches.find((b) => b.gate === gate);
  const g = state.gate_states[gate];
  if (!batch || !g) throw new Error(`unknown gate ${gate}`);
  if (status === "ACCEPTED") {
    if (decision !== "PROCEED") throw new Error(`${gate}: ACCEPTED requires decision PROCEED`);
    if (!batchComplete(plan, state, batch.id)) throw new Error(`${gate}: ${batch.id} has unfinished tasks`);
    if (!(patch.report ?? g.report) || !(patch.evidence ?? g.evidence))
      throw new Error(`${gate}: ACCEPTED requires report and evidence`);
  }
  if (status !== "ACCEPTED" && decision === "PROCEED") throw new Error(`${gate}: PROCEED requires ACCEPTED`);
  g.status = status;
  g.decision = decision;
  Object.assign(g, patch);
  return refresh(plan, state);
}
