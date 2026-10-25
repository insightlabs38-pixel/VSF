import { readFileSync } from "node:fs";
import { execFileSync } from "node:child_process";
import type { Oracle, Workflow } from "../spec.ts";
import { CACHE } from "../apps.ts";

const q = (sql: string): string[][] =>
  execFileSync("sqlite3", ["-separator", "\t", `${CACHE}seed/datasette/films.db`, sql], { encoding: "utf8" })
    .trim()
    .split("\n")
    .filter(Boolean)
    .map((l) => l.split("\t"));
const shownTitles = (page: import("@playwright/test").Page): Promise<string[]> => page.locator("table.rows-and-columns tbody tr td.col-title").allTextContents().then((a) => a.map((t) => t.trim()));
const heading = (page: import("@playwright/test").Page): Promise<string> => page.locator("h3").first().innerText().then((t) => t.trim());

export const datasette: Workflow[] = [
  {
    id: "DS-01",
    app: "datasette",
    title: "Filter a table with the filter form",
    objective: "Show only the films whose genre equals Drama using the column, operator and value controls.",
    effect: "read",
    categories: ["table", "filter", "form", "read-only"],
    difficulty: 2,
    start: "/films/films",
    steps: [
      { do: "choose", target: "column", sel: "select[name='_filter_column']", unnamed: true, value: "genre" },
      { do: "fill", target: "value", sel: "input[name='_filter_value']", unnamed: true, value: "Drama" },
      { do: "click", target: "Apply", role: "button" },
    ],
    oracle: "ds01",
  },
  {
    id: "DS-02",
    app: "datasette",
    title: "Narrow a table with a facet",
    objective: "Show only the films from Japan by clicking the JP value in the country facet.",
    effect: "read",
    categories: ["table", "facet", "navigation", "read-only"],
    difficulty: 1,
    start: "/films/films",
    steps: [{ do: "click", target: "JP", role: "link" }],
    oracle: "ds02",
  },
  {
    id: "DS-03",
    app: "datasette",
    title: "Sort a table in descending order",
    objective: "Order the films by rating from highest to lowest using the column header.",
    effect: "read",
    categories: ["table", "sort", "read-only"],
    difficulty: 1,
    start: "/films/films",
    steps: [
      { do: "click", target: "rating", role: "link", within: "table.rows-and-columns thead" },
      { do: "click", target: "rating ▼", role: "link", within: "table.rows-and-columns thead" },
    ],
    oracle: "ds03",
  },
  {
    id: "DS-04",
    app: "datasette",
    title: "Go to the next page of results",
    objective: "Show the second page of twenty films with the next link.",
    effect: "read",
    categories: ["table", "pagination", "read-only"],
    difficulty: 1,
    start: "/films/films",
    steps: [{ do: "click", target: "Next page", sel: "a[href*='_next=']" }],
    oracle: "ds04",
  },
  {
    id: "DS-05",
    app: "datasette",
    title: "Run a custom SQL query in the editor",
    objective: "Write a grouping query in the SQL editor and run it to list film counts per genre.",
    effect: "read",
    categories: ["specialist", "editor", "research", "read-only"],
    difficulty: 2,
    start: "/films",
    steps: [
      { do: "click", target: "SQL editor", sel: ".CodeMirror", unnamed: true },
      { do: "press", key: "Control+a" },
      { do: "type", text: "select genre, count(*) as n from films group by genre order by genre" },
      { do: "click", target: "Run SQL", role: "button", wait: 800 },
    ],
    oracle: "ds05",
  },
  {
    id: "DS-06",
    app: "datasette",
    title: "Download a table as CSV",
    objective: "Export the whole films table as a CSV file using the export panel and save it.",
    effect: "read",
    categories: ["download", "file", "export"],
    difficulty: 2,
    download: true,
    start: "/films/films",
    steps: [
      { do: "click", target: "advanced", role: "link", wait: 400 },
      { do: "toggle", target: "download file", role: "checkbox", checked: true },
      { do: "click", target: "Export CSV", role: "button", wait: 1500 },
    ],
    oracle: "ds06",
  },
];

export const datasetteOracles: Record<string, Oracle> = {
  ds01: async (c) => {
    const want = q("select title from films where genre='Drama' order by id limit 20").map((r) => r[0] ?? "");
    const shown = await shownTitles(c.page);
    const h = await heading(c.page);
    return { ok: JSON.stringify(want) === JSON.stringify(shown) && /^30 rows/.test(h), detail: `heading=${h} shown=${shown.length}` };
  },
  ds02: async (c) => {
    const want = q("select title from films where country='JP' order by id limit 20").map((r) => r[0] ?? "");
    const shown = await shownTitles(c.page);
    const h = await heading(c.page);
    return { ok: JSON.stringify(want) === JSON.stringify(shown) && /^25 rows/.test(h), detail: `heading=${h} shown=${shown.length}` };
  },
  ds03: async (c) => {
    const want = q("select title from films order by rating desc, id limit 20").map((r) => r[0] ?? "");
    const shown = await shownTitles(c.page);
    return { ok: JSON.stringify(want) === JSON.stringify(shown), detail: `first=${shown[0]} want=${want[0]}` };
  },
  ds04: async (c) => {
    const want = q("select title from films order by id limit 20 offset 20").map((r) => r[0] ?? "");
    const shown = await shownTitles(c.page);
    return { ok: JSON.stringify(want) === JSON.stringify(shown), detail: `first=${shown[0]} want=${want[0]}` };
  },
  ds05: async (c) => {
    const want = q("select genre, count(*) from films group by genre order by genre");
    const cells = (await c.page.locator("table.rows-and-columns tbody tr").allInnerTexts()).map((t) => t.trim().split(/\s+/).join(" "));
    return { ok: want.length > 0 && JSON.stringify(want.map((r) => r.join(" "))) === JSON.stringify(cells), detail: `rows=${cells.length} want=${want.length}` };
  },
  ds06: async (c) => {
    const name = [...c.files.keys()].find((f) => f.endsWith(".csv"));
    if (!name) return { ok: false, detail: "no csv download" };
    const got = (c.files.get(name) as Buffer).toString("utf8").replace(/\r\n/g, "\n").trim().split("\n");
    const rows = q("select id, title, year, genre, rating, country from films order by id");
    return { ok: got.length === 151 && got[1]?.startsWith(`${rows[0]?.[0]},${rows[0]?.[1]}`) === true, detail: `file=${name} lines=${got.length}` };
  },
};
