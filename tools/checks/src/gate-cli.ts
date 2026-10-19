import { readFileSync } from "node:fs";
import { evaluateGate, GATE, parseWorkflow } from "./gate.ts";

const wf = parseWorkflow(readFileSync(process.argv[2] ?? ".github/workflows/ci.yml", "utf8"));
const { ok, lines } = evaluateGate({
  results: JSON.parse(process.env.RESULTS ?? "{}") as Record<string, { result: string }>,
  event: process.env.EVENT_NAME ?? "",
  required: wf.jobs.filter((j) => j.id !== GATE).map((j) => ({ id: j.id, condition: j.condition })),
});
for (const l of lines) console.log(l);
if (!ok) console.error("gate: a required job did not succeed");
process.exit(ok ? 0 : 1);
