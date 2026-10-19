export interface WorkflowJob {
  id: string;
  condition: string | null;
  needs: string[];
  runs: string[];
  ciRuns: string[];
  continueOnError: boolean;
}

export interface Workflow {
  jobs: WorkflowJob[];
  triggers: string[];
}

const PR_ONLY = "github.event_name == 'pull_request'";

/** Reads only the shapes this repository's workflow uses; anything it cannot place is reported by `checkWorkflow`. */
export function parseWorkflow(text: string): Workflow {
  const lines = text.split("\n");
  const jobs: WorkflowJob[] = [];
  const triggers: string[] = [];
  let section = "";
  let cur: WorkflowJob | null = null;
  let listing = false;
  for (const raw of lines) {
    const line = raw.replace(/\s+#.*$/, "");
    const top = /^([a-z_]+):/.exec(line);
    if (top) {
      section = top[1] as string;
      cur = null;
      listing = false;
      continue;
    }
    if (section === "on") {
      const t = /^ {2}([a-z_]+):/.exec(line);
      if (t) triggers.push(t[1] as string);
      continue;
    }
    if (section !== "jobs") continue;
    const id = /^ {2}([A-Za-z0-9_-]+):\s*$/.exec(line);
    if (id) {
      cur = { id: id[1] as string, condition: null, needs: [], runs: [], ciRuns: [], continueOnError: false };
      jobs.push(cur);
      listing = false;
      continue;
    }
    if (!cur) continue;
    const cond = /^ {4}if:\s*(.+)$/.exec(line);
    if (cond) {
      cur.condition = (cond[1] as string).trim();
      continue;
    }
    const needs = /^ {4}needs:\s*(.*)$/.exec(line);
    if (needs) {
      const flow = /^\[(.*)\]$/.exec((needs[1] as string).trim());
      if (flow)
        cur.needs = (flow[1] as string)
          .split(",")
          .map((s) => s.trim())
          .filter(Boolean);
      else listing = true;
      continue;
    }
    if (listing) {
      const item = /^ {6}-\s*([A-Za-z0-9_-]+)\s*$/.exec(line);
      if (item) {
        cur.needs.push(item[1] as string);
        continue;
      }
      listing = false;
    }
    if (/^\s+continue-on-error:\s*(true|\$\{\{)/.test(line)) cur.continueOnError = true;
    const run = /\brun:\s*(.+)$/.exec(line);
    if (run) {
      cur.runs.push((run[1] as string).trim());
      const ci = /\bci-run\s+([a-z0-9-]+)/.exec(line);
      if (ci) cur.ciRuns.push(ci[1] as string);
    }
    const inline = /(?:^|\s)(?:if VSF_INDUCE_FAILURE=1 )?node tools\/checks\/src\/cli\.ts ci-run ([a-z0-9-]+)/.exec(
      line,
    );
    if (inline && !cur.ciRuns.includes(inline[1] as string)) cur.ciRuns.push(inline[1] as string);
  }
  return { jobs, triggers };
}

export const GATE = "gate";

/** Structural problems that would let a failing, skipped or forgotten job coexist with a green gate. */
export function checkWorkflow(wf: Workflow, jobsFile: Record<string, unknown[]>): string[] {
  const problems: string[] = [];
  const gate = wf.jobs.find((j) => j.id === GATE);
  if (!gate) return ["workflow has no gate job"];
  const others = wf.jobs.filter((j) => j.id !== GATE);
  if (gate.condition !== "always()") problems.push("gate must run `if: always()` so a failed need cannot skip it");
  for (const j of others) {
    if (!gate.needs.includes(j.id)) problems.push(`gate does not wait for job ${j.id}`);
    if (j.continueOnError) problems.push(`job ${j.id} sets continue-on-error`);
    if (j.condition !== null && j.condition !== PR_ONLY)
      problems.push(`job ${j.id} has an unrecognised condition: ${j.condition}`);
  }
  for (const n of gate.needs) if (!others.some((j) => j.id === n)) problems.push(`gate waits for unknown job ${n}`);
  if (wf.triggers.includes("pull_request_target")) problems.push("pull_request_target is not allowed");
  const invoked = new Set(wf.jobs.flatMap((j) => j.ciRuns));
  for (const [name, steps] of Object.entries(jobsFile)) {
    if (!invoked.has(name)) problems.push(`ci/jobs.json job ${name} is never run by the workflow`);
    if (steps.length === 0) problems.push(`ci/jobs.json job ${name} has no steps`);
  }
  for (const name of invoked) if (!(name in jobsFile)) problems.push(`workflow runs unknown ci job ${name}`);
  const gateRun = gate.runs.join(" ");
  if (!/gate-cli\.ts/.test(gateRun)) problems.push("gate must evaluate results with tools/checks/src/gate-cli.ts");
  return problems;
}

export interface GateInput {
  results: Record<string, { result: string }>;
  event: string;
  /** Jobs the workflow defines, other than the gate. */
  required: { id: string; condition: string | null }[];
}

/** Every required job must be present and `success`; a skipped PR-only job is acceptable only off pull requests. */
export function evaluateGate(input: GateInput): { ok: boolean; lines: string[] } {
  const lines: string[] = [];
  let ok = true;
  for (const j of input.required) {
    const r = input.results[j.id]?.result;
    let verdict: string;
    if (r === undefined) {
      verdict = "ABSENT";
      ok = false;
    } else if (r === "success") {
      verdict = "ok";
    } else if (r === "skipped" && j.condition === PR_ONLY && input.event !== "pull_request") {
      verdict = "skipped (pull-request-only job, not a pull request)";
    } else {
      verdict = `NOT OK (${r})`;
      ok = false;
    }
    lines.push(`${j.id}: ${verdict}`);
  }
  for (const id of Object.keys(input.results)) {
    if (!input.required.some((j) => j.id === id)) lines.push(`${id}: not a workflow job (ignored)`);
  }
  return { ok, lines };
}
