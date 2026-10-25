import { existsSync, readFileSync, statSync } from "node:fs";
import type { Oracle, Workflow } from "../spec.ts";

const work = (c: { run: { dir: string } }, f: string): string => `${c.run.dir}/work/${f}`;
const readNb = (path: string): any => JSON.parse(readFileSync(path, "utf8"));
const text = (o: any): string => (Array.isArray(o.text) ? o.text.join("") : (o.text ?? (o.data?.["text/plain"] ? [].concat(o.data["text/plain"]).join("") : "")));
const TILE = "Python 3 (ipykernel) Python 3 (ipykernel)";

export const jupyterlab: Workflow[] = [
  {
    id: "JUP-01",
    app: "jupyterlab",
    title: "Create a notebook, run a cell and save",
    objective: "Create a new notebook, run a cell that prints 6*7 and save so the output is stored in the file.",
    effect: "write",
    categories: ["specialist", "notebook", "editor", "multi-step"],
    difficulty: 3,
    start: "/lab",
    steps: [
      { do: "click", target: TILE, role: "button", nth: 0, wait: 3500 },
      { do: "fill", target: "Code cell input", sel: ".jp-Notebook .jp-Cell .cm-content >> nth=0", unnamed: true, value: "print(6*7)" },
      { do: "press", key: "Shift+Enter", wait: 3500 },
      { do: "press", key: "Control+s", wait: 800 },
      { do: "click", target: "Rename and Save", role: "button", wait: 1200 },
    ],
    oracle: "jup01",
  },
  {
    id: "JUP-02",
    app: "jupyterlab",
    title: "Create and save a text file in the editor",
    objective: "Create a new text file, type a line of text and save it.",
    effect: "write",
    categories: ["specialist", "editor", "file"],
    difficulty: 2,
    start: "/lab",
    steps: [
      { do: "click", target: "Text File", role: "button", wait: 1500 },
      { do: "fill", target: "Text editor", sel: ".jp-FileEditor .cm-content", unnamed: true, value: "hello from the audit" },
      { do: "press", key: "Control+s", wait: 800 },
      { do: "click", target: "Rename and Save", role: "button", wait: 1200 },
    ],
    oracle: "jup02",
  },
  {
    id: "JUP-03",
    app: "jupyterlab",
    title: "Run a command in the terminal",
    objective: "Open a terminal and run a command that writes proof.txt in the working directory.",
    effect: "write",
    categories: ["specialist", "terminal", "multi-step"],
    difficulty: 3,
    start: "/lab",
    steps: [
      { do: "click", target: "Terminal", role: "button", wait: 4000 },
      { do: "click", target: "Terminal screen", sel: ".jp-Terminal", unnamed: true },
      { do: "type", text: "echo audit-terminal > proof.txt" },
      { do: "press", key: "Enter", wait: 1500 },
    ],
    oracle: "jup03",
  },
  {
    id: "JUP-04",
    app: "jupyterlab",
    title: "Upload a file with the file browser",
    objective: "Upload upload.csv into the working directory.",
    effect: "write",
    categories: ["upload", "file", "toolbar"],
    difficulty: 1,
    start: "/lab",
    settle: 2500,
    steps: [{ do: "upload", target: "Upload Files", role: "button", value: "upload.csv", text: "a,b\n1,2\n" }],
    oracle: "jup04",
  },
  {
    id: "JUP-05",
    app: "jupyterlab",
    title: "Create and name a folder",
    objective: "Create a folder named audit-dir with the New Folder button.",
    effect: "write",
    categories: ["file", "toolbar", "inline-rename"],
    difficulty: 2,
    start: "/lab",
    settle: 2500,
    steps: [
      { do: "click", target: "New Folder", role: "button", wait: 800 },
      { do: "type", text: "audit-dir" },
      { do: "press", key: "Enter", wait: 1200 },
    ],
    oracle: "jup05",
  },
  {
    id: "JUP-06",
    app: "jupyterlab",
    title: "Create a Markdown file from the nested File menu",
    objective: "Create a new Markdown file with File, New, Markdown File.",
    effect: "write",
    categories: ["menu", "nested-menu", "file"],
    difficulty: 2,
    start: "/lab",
    settle: 2500,
    steps: [
      { do: "click", target: "File", role: "menuitem", wait: 500 },
      { do: "click", target: "New", sel: ".lm-Menu-item[data-type='submenu']:has-text('New') >> nth=0", wait: 500 },
      { do: "click", target: "Markdown File", role: "menuitem", wait: 1500 },
    ],
    oracle: "jup06",
  },
  {
    id: "JUP-07",
    app: "jupyterlab",
    title: "Rename a file from the keyboard",
    objective: "Rename notes.txt to renamed.txt in the file browser.",
    effect: "write",
    categories: ["file", "keyboard", "inline-rename"],
    difficulty: 2,
    start: "/lab",
    settle: 2500,
    steps: [
      { do: "click", target: "notes.txt", role: "button", sel: ".jp-DirListing-item[title*='notes.txt']", wait: 600 },
      { do: "press", key: "F2", wait: 500 },
      { do: "press", key: "Control+a" },
      { do: "type", text: "renamed.txt" },
      { do: "press", key: "Enter", wait: 1200 },
    ],
    oracle: "jup07",
  },
  {
    id: "JUP-08",
    app: "jupyterlab",
    title: "Run all cells of an existing notebook",
    objective: "Open seed.ipynb, run all cells from the Run menu and save the stored outputs.",
    effect: "write",
    categories: ["specialist", "notebook", "menu", "multi-step"],
    difficulty: 3,
    start: "/lab/tree/seed.ipynb",
    settle: 4000,
    steps: [
      { do: "click", target: "Run", role: "menuitem", wait: 600 },
      { do: "click", target: "Run All Cells", role: "menuitem", wait: 4000 },
      { do: "press", key: "Control+s", wait: 1000 },
    ],
    oracle: "jup08",
  },
];

export const jupyterOracles: Record<string, Oracle> = {
  jup01: async (c) => {
    const f = work(c, "Untitled.ipynb");
    if (!existsSync(f)) return { ok: false, detail: "no notebook" };
    const nb = readNb(f);
    const src = [].concat(nb.cells?.[0]?.source ?? []).join("");
    const out = (nb.cells?.[0]?.outputs ?? []).map(text).join("");
    return { ok: src.includes("print(6*7)") && out.trim() === "42", detail: `source=${JSON.stringify(src)} output=${JSON.stringify(out)}` };
  },
  jup02: async (c) => {
    const f = work(c, "untitled.txt");
    const t = existsSync(f) ? readFileSync(f, "utf8") : null;
    return { ok: t === "hello from the audit", detail: `content=${JSON.stringify(t)}` };
  },
  jup03: async (c) => {
    const f = work(c, "proof.txt");
    const t = existsSync(f) ? readFileSync(f, "utf8") : null;
    return { ok: t === "audit-terminal\n", detail: `content=${JSON.stringify(t)}` };
  },
  jup04: async (c) => {
    const f = work(c, "upload.csv");
    const t = existsSync(f) ? readFileSync(f, "utf8") : null;
    return { ok: t === "a,b\n1,2\n", detail: `content=${JSON.stringify(t)}` };
  },
  jup05: async (c) => {
    const f = work(c, "audit-dir");
    return { ok: existsSync(f) && statSync(f).isDirectory(), detail: existsSync(f) ? "dir exists" : "absent" };
  },
  jup06: async (c) => {
    const f = work(c, "untitled.md");
    return { ok: existsSync(f), detail: existsSync(f) ? "untitled.md exists" : "absent" };
  },
  jup07: async (c) => {
    const a = existsSync(work(c, "renamed.txt"));
    const b = existsSync(work(c, "notes.txt"));
    return { ok: a && !b && readFileSync(work(c, "renamed.txt"), "utf8") === "first line\n", detail: `renamed=${a} original=${b}` };
  },
  jup08: async (c) => {
    const nb = readNb(work(c, "seed.ipynb"));
    const o0 = (nb.cells[0].outputs ?? []).map(text).join("").trim();
    const o1 = (nb.cells[1].outputs ?? []).map(text).join("").trim();
    return { ok: o0 === "21" && o1 === "40", detail: `cell1=${JSON.stringify(o0)} cell2=${JSON.stringify(o1)}` };
  },
};
