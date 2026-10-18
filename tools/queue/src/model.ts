export type TaskStatus = "TODO" | "READY" | "ACTIVE" | "REVIEW" | "CORRECTIONS" | "BLOCKED" | "DONE";
export type GateStatus = "PENDING" | "ACCEPTED" | "BLOCKED" | "REVISE" | "REVERT";
export type Decision = "PROCEED" | "HOLD" | "REVISE" | "REVERT";

export interface PlanBatch {
  id: string;
  name: string;
  gate: string;
  depends_on: string[];
  milestone: string;
}

export interface PlanTask {
  id: string;
  batch: string;
  sequence: number;
  title: string;
  acceptance: string;
  dependencies: string[];
  origin?: "plan" | "adaptive";
  adr?: string;
}

export interface Plan {
  batches: PlanBatch[];
  tasks: PlanTask[];
}

export interface TaskState {
  status: TaskStatus;
  claimed_by: string | null;
  branch?: string | null;
  base_commit?: string | null;
  commit: string | null;
  evidence: string | null;
  receipt?: string | null;
}

export interface GateState {
  status: GateStatus;
  decision: Decision | null;
  evidence: string | null;
  report?: string | null;
  pr?: string | null;
  merge_commit?: string | null;
}

export interface State {
  schema: "vsf-queue-v1";
  initialized: string;
  source_plan: string;
  status: "ACTIVE" | "COMPLETE";
  cursor: { batch: string | null; task: string | null };
  task_states: Record<string, TaskState>;
  gate_states: Record<string, GateState>;
}

export const TASK_ID = /^VSF-B(\d\d)-T(\d\d)$/;
