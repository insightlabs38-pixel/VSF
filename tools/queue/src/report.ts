import { existsSync } from "node:fs";
import { join } from "node:path";
import type { Plan, State } from "./model.ts";
import { batchComplete, batchTasks, gateOf } from "./queue.ts";

export const MAX_REPORT_LINES = 24;

const REQUIRED_KEYS = [
  "Branch",
  "PR",
  "Closed tasks",
  "Contracts/primitive IDs",
  "Changed modules",
  "Tests",
  "Negative/adversarial",
  "Performance",
  "Security/session scope",
  "Source compatibility",
  "Version/pin/retention",
  "Known limitations",
  "Evidence",
  "Next eligible",
] as const;

export interface ReportProblem {
  id: string;
  message: string;
}

export function checkReport(text: string, plan: Plan, state: State, repoRoot: string | null): ReportProblem[] {
  const out: ReportProblem[] = [];
  const lines = text.replace(/\n+$/, "").split("\n");
  if (lines.length > MAX_REPORT_LINES)
    out.push({ id: "report.too-long", message: `${lines.length} lines; maximum ${MAX_REPORT_LINES}` });
  const head = /^Batch: (B\d\d) \| Gate: (G\d{1,2}) \| Decision: (PROCEED|HOLD|REVISE|REVERT)$/.exec(lines[0] ?? "");
  if (!head) {
    out.push({
      id: "report.header",
      message: "first line must be 'Batch: Bnn | Gate: Gnn | Decision: PROCEED|HOLD|REVISE|REVERT'",
    });
    return out;
  }
  const [, batch = "", gate = "", decision = ""] = head;
  const planned = plan.batches.find((b) => b.id === batch);
  if (!planned) {
    out.push({ id: "report.unknown-batch", message: batch });
    return out;
  }
  if (gateOf(plan, batch) !== gate)
    out.push({ id: "report.gate-mismatch", message: `${batch} uses ${planned.gate}, report says ${gate}` });
  const keys = new Map<string, string>();
  for (const line of lines.slice(1)) {
    for (const part of line.split(" | ")) {
      const m = /^([A-Za-z/ ]+): (.*)$/.exec(part);
      if (m) keys.set(m[1] ?? "", m[2] ?? "");
    }
  }
  for (const k of REQUIRED_KEYS) if (!keys.has(k)) out.push({ id: "report.missing-field", message: k });
  const closed: string[] = (keys.get("Closed tasks") ?? "").match(/VSF-B\d\d-T\d\d/g) ?? [];
  const planIds = batchTasks(plan, batch).map((t) => t.id);
  for (const id of closed) {
    if (!planIds.includes(id)) out.push({ id: "report.unknown-task", message: `${id} is not a task of ${batch}` });
  }
  const gateState = state.gate_states[gate];
  if (decision === "PROCEED") {
    for (const id of planIds) {
      if (!closed.includes(id)) out.push({ id: "report.missing-task", message: `${id} not listed as closed` });
      if (state.task_states[id]?.status !== "DONE")
        out.push({ id: "report.task-not-done", message: `${id} is ${state.task_states[id]?.status}` });
    }
    if (!batchComplete(plan, state, batch))
      out.push({ id: "report.batch-incomplete", message: `${batch} has unfinished tasks` });
    if (gateState?.status !== "ACCEPTED")
      out.push({ id: "report.gate-not-accepted", message: `${gate} is ${gateState?.status ?? "missing"}` });
  } else if (gateState?.status === "ACCEPTED") {
    out.push({ id: "report.decision-mismatch", message: `${gate} is ACCEPTED but the report decision is ${decision}` });
  }
  const evidence = keys.get("Evidence") ?? "";
  if (repoRoot && decision === "PROCEED") {
    const dir = evidence.split(/\s+/)[0] ?? "";
    if (!dir.startsWith(`docs/deep/evidence/VSF-${batch}`) || !existsSync(join(repoRoot, dir))) {
      out.push({ id: "report.evidence-missing", message: `evidence path ${dir || "(none)"} does not exist` });
    }
  }
  return out;
}
