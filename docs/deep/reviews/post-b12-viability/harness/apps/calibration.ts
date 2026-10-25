import type { Oracle, Workflow } from "../spec.ts";

const items = (page: import("@playwright/test").Page): Promise<string[]> => page.locator(".todo-list li label").allTextContents();
const done = (page: import("@playwright/test").Page): Promise<string[]> => page.locator(".todo-list li.completed label").allTextContents();

export const calibration: Workflow[] = [
  {
    id: "CAL-01",
    app: "todomvc",
    title: "Add a todo",
    objective: "Add the todo Buy milk.",
    effect: "write",
    categories: ["calibration"],
    difficulty: 1,
    start: "/react/dist/index.html",
    steps: [
      { do: "fill", target: "New Todo Input", role: "textbox", value: "Buy milk" },
      { do: "press", key: "Enter" },
    ],
    oracle: "cal01",
  },
  {
    id: "CAL-02",
    app: "todomvc",
    title: "Add two todos and complete one",
    objective: "Add Buy milk and Walk the dog and mark the second complete.",
    effect: "write",
    categories: ["calibration"],
    difficulty: 2,
    start: "/react/dist/index.html",
    steps: [
      { do: "fill", target: "New Todo Input", role: "textbox", value: "Buy milk" },
      { do: "press", key: "Enter" },
      { do: "fill", target: "New Todo Input", role: "textbox", value: "Walk the dog" },
      { do: "press", key: "Enter" },
      { do: "toggle", target: "Walk the dog", sel: ".todo-list li:has-text('Walk the dog') .toggle", checked: true },
    ],
    oracle: "cal02",
  },
  {
    id: "CAL-03",
    app: "todomvc",
    title: "Filter to active todos",
    objective: "Add two todos, complete one and show only the active ones.",
    effect: "read",
    categories: ["calibration"],
    difficulty: 2,
    start: "/react/dist/index.html",
    steps: [
      { do: "fill", target: "New Todo Input", role: "textbox", value: "Buy milk" },
      { do: "press", key: "Enter" },
      { do: "fill", target: "New Todo Input", role: "textbox", value: "Walk the dog" },
      { do: "press", key: "Enter" },
      { do: "toggle", target: "Walk the dog", sel: ".todo-list li:has-text('Walk the dog') .toggle", checked: true },
      { do: "click", target: "Active", role: "link" },
    ],
    oracle: "cal03",
  },
];

export const calibrationOracles: Record<string, Oracle> = {
  cal01: async (c) => ({ ok: (await items(c.page)).join("|") === "Buy milk", detail: (await items(c.page)).join("|") }),
  cal02: async (c) => ({ ok: (await items(c.page)).join("|") === "Buy milk|Walk the dog" && (await done(c.page)).join("|") === "Walk the dog", detail: `${(await items(c.page)).join("|")} done=${(await done(c.page)).join("|")}` }),
  cal03: async (c) => ({ ok: (await items(c.page)).join("|") === "Buy milk" && c.page.url().endsWith("#/active"), detail: `${(await items(c.page)).join("|")} ${c.page.url()}` }),
};
