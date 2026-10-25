import { execFileSync } from "node:child_process";
import type { Oracle, Workflow } from "../spec.ts";

const sql = (dir: string, q: string): string[][] =>
  execFileSync("sqlite3", ["-separator", "\t", `${dir}/data/db.sqlite`, q], { encoding: "utf8" })
    .trim()
    .split("\n")
    .filter(Boolean)
    .map((l) => l.split("\t"));
const BOARD = "/?controller=BoardViewController&action=show&project_id=1";

export const kanboard: Workflow[] = [
  {
    id: "KB-01",
    app: "kanboard",
    title: "Create a task in a popup form",
    objective: "Add a task titled Audit task to the Backlog column from the board.",
    effect: "write",
    categories: ["crud", "dialog", "form"],
    difficulty: 1,
    start: BOARD,
    steps: [
      { do: "click", target: "Add a new task", role: "link", nth: 0, wait: 800 },
      { do: "fill", target: "Title", role: "textbox", value: "Audit task" },
      { do: "click", target: "Save", role: "button", wait: 800 },
    ],
    oracle: "kb01",
  },
  {
    id: "KB-02",
    app: "kanboard",
    title: "Create a task with assignee, column and priority",
    objective: "Create Audit detailed task assigned to Worker One in Ready with priority 2 and a description.",
    effect: "write",
    categories: ["crud", "dialog", "form", "native-select", "conditional"],
    difficulty: 2,
    start: BOARD,
    steps: [
      { do: "click", target: "Add a new task", role: "link", nth: 0, wait: 800 },
      { do: "fill", target: "Title", role: "textbox", value: "Audit detailed task" },
      { do: "fill", target: "Description", role: "textbox", value: "Written by the audit" },
      { do: "choose", target: "Assignee", role: "combobox", value: "Worker One" },
      { do: "choose", target: "Column", role: "combobox", value: "Ready" },
      { do: "choose", target: "Priority", role: "combobox", value: "2" },
      { do: "click", target: "Save", role: "button", wait: 800 },
    ],
    oracle: "kb02",
  },
  {
    id: "KB-03",
    app: "kanboard",
    title: "Comment on a task",
    objective: "Add the comment Looks good to task 1 from its detail page.",
    effect: "write",
    categories: ["crud", "dialog", "form"],
    difficulty: 2,
    start: "/?controller=TaskViewController&action=show&task_id=1",
    steps: [
      { do: "click", target: "Add a comment", role: "link", wait: 800 },
      { do: "fill", target: "New comment", role: "textbox", value: "Looks good" },
      { do: "click", target: "Save", role: "button", wait: 800 },
    ],
    oracle: "kb03",
  },
  {
    id: "KB-04",
    app: "kanboard",
    title: "Close a task after confirming",
    objective: "Close task 3 from its detail page and confirm in the dialog.",
    effect: "destructive",
    categories: ["state-transition", "dialog", "destructive"],
    difficulty: 2,
    start: "/?controller=TaskViewController&action=show&task_id=3",
    steps: [
      { do: "click", target: "Close this task", role: "link", wait: 800 },
      { do: "click", target: "Yes", role: "button", wait: 800 },
    ],
    oracle: "kb04",
  },
  {
    id: "KB-05",
    app: "kanboard",
    title: "Move a card to another column by dragging",
    objective: "Drag the card Write spec from Backlog to the Ready column.",
    effect: "write",
    categories: ["drag-and-drop", "state-transition", "unconventional"],
    difficulty: 3,
    start: BOARD,
    steps: [{ do: "drag", target: "Write spec", sel: ".task-board[data-task-id='1']", to: ".board-task-list[data-column-id='2'].board-column-expanded", wait: 1200 }],
    oracle: "kb05",
  },
  {
    id: "KB-06",
    app: "kanboard",
    title: "Filter the board with a search expression",
    objective: "Show only the cards whose title contains login.",
    effect: "read",
    categories: ["filter", "board", "read-only"],
    difficulty: 1,
    start: BOARD,
    steps: [
      { do: "fill", target: "Filter", role: "textbox", value: "login" },
      { do: "press", key: "Enter", wait: 1200 },
    ],
    oracle: "kb06",
  },
];

export const kanboardOracles: Record<string, Oracle> = {
  kb01: async (c) => {
    const r = sql(c.run.dir, "select title, column_id from tasks where title='Audit task'");
    return { ok: r.length === 1 && r[0]?.[1] === "1", detail: JSON.stringify(r) };
  },
  kb02: async (c) => {
    const r = sql(c.run.dir, "select owner_id, column_id, priority, description from tasks where title='Audit detailed task'");
    return { ok: r.length === 1 && r[0]?.[0] === "2" && r[0]?.[1] === "2" && r[0]?.[2] === "2" && r[0]?.[3] === "Written by the audit", detail: JSON.stringify(r) };
  },
  kb03: async (c) => {
    const r = sql(c.run.dir, "select task_id, comment from comments");
    return { ok: r.some((x) => x[0] === "1" && x[1] === "Looks good"), detail: JSON.stringify(r) };
  },
  kb04: async (c) => {
    const r = sql(c.run.dir, "select is_active from tasks where id=3");
    return { ok: r[0]?.[0] === "0", detail: JSON.stringify(r) };
  },
  kb05: async (c) => {
    const r = sql(c.run.dir, "select column_id from tasks where id=1");
    return { ok: r[0]?.[0] === "2", detail: JSON.stringify(r) };
  },
  kb06: async (c) => {
    const want = sql(c.run.dir, "select title from tasks where title like '%login%' and is_active=1");
    const shown = (await c.page.locator(".task-board .task-board-title a").allTextContents()).map((t) => t.trim());
    return { ok: want.length === 1 && shown.length === 1 && shown[0] === want[0]?.[0], detail: `want=${JSON.stringify(want)} shown=${JSON.stringify(shown)}` };
  },
};
