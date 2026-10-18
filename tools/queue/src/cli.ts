import { existsSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { checkEvidence, initEvidence, recordRun } from "./evidence.ts";
import type { Decision, GateStatus, State, TaskStatus } from "./model.ts";
import { nextTask } from "./queue.ts";
import { renderTasks } from "./render.ts";
import { checkReport } from "./report.ts";
import { refresh, setGate, setTask } from "./transition.ts";
import { loadPlan, loadState, pathsFor, validateQueue, validateReceipt } from "./validate.ts";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "../../..");
const p = pathsFor(root);
const [, , cmd = "", ...args] = process.argv;

const flag = (name: string): string | undefined => {
  const i = args.indexOf(`--${name}`);
  return i >= 0 ? args[i + 1] : undefined;
};

function save(state: State): void {
  writeFileSync(join(p.queueDir, "state.json"), `${JSON.stringify(state, null, 2)}\n`);
  writeFileSync(join(root, "TASKS.md"), renderTasks(loadPlan(p), state));
}

function printProblems(items: { id: string; message: string }[]): number {
  for (const i of items) console.error(`${i.id}: ${i.message}`);
  return items.length === 0 ? 0 : 1;
}

try {
  switch (cmd) {
    case "validate": {
      const problems = validateQueue(p, loadPlan(p), loadState(p), { git: !args.includes("--no-git") });
      const code = printProblems(problems);
      if (code === 0) console.log("queue: ok");
      process.exit(code);
      break;
    }
    case "next": {
      const n = nextTask(loadPlan(p), loadState(p));
      console.log(n ? `${n.batch} ${n.task}` : "none");
      break;
    }
    case "render": {
      const text = renderTasks(loadPlan(p), loadState(p));
      if (args.includes("--check")) process.exit(readFileSync(join(root, "TASKS.md"), "utf8") === text ? 0 : 1);
      writeFileSync(join(root, "TASKS.md"), text);
      break;
    }
    case "init": {
      if (existsSync(join(p.queueDir, "state.json")))
        throw new Error("state.json already exists; refusing to overwrite progress");
      const boot = JSON.parse(readFileSync(join(p.queueDir, "state.bootstrap.json"), "utf8")) as State & {
        status: string;
      };
      const state: State = { ...boot, status: "ACTIVE", cursor: { batch: null, task: null } };
      save(refresh(loadPlan(p), state));
      break;
    }
    case "task": {
      const [id = "", to = ""] = args;
      const patch = {
        ...(flag("commit") && { commit: flag("commit") as string }),
        ...(flag("branch") && { branch: flag("branch") as string }),
        ...(flag("base") && { base_commit: flag("base") as string }),
        ...(flag("receipt") && { receipt: flag("receipt") as string }),
        ...(flag("evidence") && { evidence: flag("evidence") as string }),
        ...(flag("claimed-by") && { claimed_by: flag("claimed-by") as string }),
      };
      save(setTask(loadPlan(p), loadState(p), id, to as TaskStatus, patch));
      break;
    }
    case "gate": {
      const [gate = "", status = "", decision = "-"] = args;
      const patch = {
        ...(flag("evidence") && { evidence: flag("evidence") as string }),
        ...(flag("report") && { report: flag("report") as string }),
        ...(flag("pr") && { pr: flag("pr") as string }),
        ...(flag("merge") && { merge_commit: flag("merge") as string }),
      };
      save(
        setGate(
          loadPlan(p),
          loadState(p),
          gate,
          status as GateStatus,
          decision === "-" ? null : (decision as Decision),
          patch,
        ),
      );
      break;
    }
    case "report-check": {
      const file = resolve(args[0] ?? "");
      process.exit(printProblems(checkReport(readFileSync(file, "utf8"), loadPlan(p), loadState(p), root)));
      break;
    }
    case "receipt-check": {
      const receipt = JSON.parse(readFileSync(resolve(args[0] ?? ""), "utf8"));
      process.exit(printProblems(validateReceipt(p, receipt)));
      break;
    }
    case "evidence-init": {
      console.log(initEvidence(p, loadPlan(p), args[0] ?? ""));
      break;
    }
    case "evidence-run": {
      const sep = args.indexOf("--");
      const [batch = "", taskId = "", name = ""] = args.slice(0, sep);
      const entry = recordRun(p, loadPlan(p), batch, taskId, name, args.slice(sep + 1));
      console.log(`${entry.result} ${taskId}/${name} exit=${entry.exit_code}`);
      process.exit(entry.exit_code === 0 ? 0 : 1);
      break;
    }
    case "evidence-check": {
      const problems = checkEvidence(p, loadPlan(p), args[0] ?? "");
      const code = printProblems(problems);
      if (code === 0) console.log("evidence: ok");
      process.exit(code);
      break;
    }
    default:
      console.error(
        "usage: queue <validate|next|render|init|task|gate|report-check|receipt-check|evidence-init|evidence-run|evidence-check>",
      );
      process.exit(64);
  }
} catch (err) {
  console.error(`queue: ${(err as Error).message}`);
  process.exit(2);
}
