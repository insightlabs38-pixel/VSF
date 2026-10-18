import type { Plan, State } from "./model.ts";
import { batchStatus, batchTasks, gateOf, nextTask } from "./queue.ts";

/** Root TASKS.md is derived from state.json; it is never hand-edited. */
export function renderTasks(plan: Plan, state: State): string {
  const next = nextTask(plan, state);
  const nextText = next
    ? next.task === "GATE"
      ? `close ${gateOf(plan, next.batch)} (${next.batch})`
      : `\`${next.task}\``
    : "none";
  const lines: string[] = [
    "# VSF task queue",
    "",
    "Derived from `docs/deep/queue/state.json` by `node tools/queue/src/cli.ts render`; do not edit by hand.",
    "",
    `**Next eligible:** ${nextText}`,
    "",
    "| Batch | Gate | Tasks | Status | Decision |",
    "|---|---|---:|---|---|",
  ];
  for (const b of plan.batches) {
    const tasks = batchTasks(plan, b.id);
    const done = tasks.filter((t) => state.task_states[t.id]?.status === "DONE").length;
    const gate = state.gate_states[b.gate];
    lines.push(
      `| ${b.id} | ${b.gate} | ${done}/${tasks.length} | ${batchStatus(plan, state, b.id)} | ${gate?.decision ?? "-"} |`,
    );
  }
  for (const b of plan.batches) {
    lines.push("", `### ${b.id} · ${b.name}`, "");
    for (const t of batchTasks(plan, b.id)) {
      const s = state.task_states[t.id]?.status ?? "TODO";
      const note = s === "DONE" || s === "TODO" ? "" : ` _(${s})_`;
      lines.push(`- [${s === "DONE" ? "x" : " "}] \`${t.id}\` ${t.title}${note}`);
    }
  }
  return `${lines.join("\n")}\n`;
}
