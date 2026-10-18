import type { Plan, PlanBatch, PlanTask, State, TaskStatus } from "./model.ts";

export function batchTasks(plan: Plan, batch: string): PlanTask[] {
  return plan.tasks.filter((t) => t.batch === batch).sort((a, b) => a.sequence - b.sequence);
}

export function gateOf(plan: Plan, batch: string): string {
  const b = plan.batches.find((x) => x.id === batch);
  if (!b) throw new Error(`unknown batch ${batch}`);
  return b.gate;
}

export function batchAccepted(plan: Plan, state: State, batch: string): boolean {
  return state.gate_states[gateOf(plan, batch)]?.status === "ACCEPTED";
}

export function prerequisiteBatchesAccepted(plan: Plan, state: State, batch: PlanBatch): boolean {
  return batch.depends_on.every((b) => batchAccepted(plan, state, b));
}

export function dependenciesDone(plan: Plan, state: State, task: PlanTask): boolean {
  void plan;
  return task.dependencies.every((d) => state.task_states[d]?.status === "DONE");
}

export function isEligible(plan: Plan, state: State, task: PlanTask): boolean {
  const batch = plan.batches.find((b) => b.id === task.batch);
  if (!batch) return false;
  return prerequisiteBatchesAccepted(plan, state, batch) && dependenciesDone(plan, state, task);
}

export function batchComplete(plan: Plan, state: State, batch: string): boolean {
  return batchTasks(plan, batch).every((t) => state.task_states[t.id]?.status === "DONE");
}

/** The earliest unfinished batch retains priority; within it, BLOCKED/CORRECTIONS/REVIEW/ACTIVE go first. */
export function nextTask(plan: Plan, state: State): { batch: string; task: string } | null {
  for (const batch of plan.batches) {
    if (batchAccepted(plan, state, batch.id)) continue;
    const tasks = batchTasks(plan, batch.id);
    const priority: TaskStatus[] = ["BLOCKED", "CORRECTIONS", "REVIEW", "ACTIVE"];
    for (const status of priority) {
      const hit = tasks.find((t) => state.task_states[t.id]?.status === status);
      if (hit) return { batch: batch.id, task: hit.id };
    }
    const open = tasks.find((t) => state.task_states[t.id]?.status !== "DONE" && isEligible(plan, state, t));
    if (open) return { batch: batch.id, task: open.id };
    if (!batchComplete(plan, state, batch.id)) return null;
    return { batch: batch.id, task: "GATE" };
  }
  return null;
}

export function batchStatus(plan: Plan, state: State, batch: string): string {
  if (batchAccepted(plan, state, batch)) return "DONE";
  const tasks = batchTasks(plan, batch);
  if (tasks.every((t) => state.task_states[t.id]?.status === "TODO")) return "TODO";
  if (tasks.every((t) => ["TODO", "READY"].includes(state.task_states[t.id]?.status ?? ""))) return "READY";
  if (tasks.some((t) => state.task_states[t.id]?.status === "BLOCKED")) return "BLOCKED";
  const gate = state.gate_states[gateOf(plan, batch)];
  if (gate && (gate.status === "REVISE" || gate.status === "REVERT" || gate.status === "BLOCKED")) return gate.status;
  return batchComplete(plan, state, batch) ? "GATE_PENDING" : "IN_PROGRESS";
}
