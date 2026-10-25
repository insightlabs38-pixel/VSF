import { execFileSync } from "node:child_process";
import { mkdtempSync, readdirSync, readFileSync } from "node:fs";
import { tmpdir } from "node:os";
import type { Oracle, Workflow } from "../spec.ts";

const output = async (page: import("@playwright/test").Page, name: string): Promise<string> => {
  for (const role of ["textbox", "spinbutton"] as const) {
    const l = page.getByRole(role, { name, exact: true });
    if ((await l.count()) > 0) return l.first().inputValue({ timeout: 1500 }).catch(() => "");
  }
  return "";
};

export const gradio: Workflow[] = [
  {
    id: "GR-01",
    app: "gradio",
    variant: "gradio-calculator",
    title: "Compute with a number and radio form",
    objective: "Multiply 12 by 7 with the calculator form and read the output.",
    effect: "read",
    categories: ["research", "form", "custom-components"],
    difficulty: 1,
    start: "/",
    settle: 2000,
    steps: [
      { do: "fill", target: "num1", role: "spinbutton", value: "12" },
      { do: "click", target: "multiply", role: "radio" },
      { do: "fill", target: "num2", role: "spinbutton", value: "7" },
      { do: "click", target: "Submit", role: "button", wait: 1200 },
    ],
    oracle: "gr01",
  },
  {
    id: "GR-02",
    app: "gradio",
    variant: "gradio-calculator",
    title: "Divide by zero shows an error and no result",
    objective: "Divide 5 by 0, see the application's error message and no computed output.",
    effect: "read",
    categories: ["research", "form", "error-path"],
    difficulty: 2,
    start: "/",
    settle: 2000,
    steps: [
      { do: "fill", target: "num1", role: "spinbutton", value: "5" },
      { do: "click", target: "divide", role: "radio" },
      { do: "fill", target: "num2", role: "spinbutton", value: "0" },
      { do: "click", target: "Submit", role: "button", wait: 1500 },
    ],
    oracle: "gr02",
  },
  {
    id: "GR-03",
    app: "gradio",
    variant: "gradio-sentence_builder",
    title: "Fill a mixed-widget form and read the generated sentence",
    objective: "Set the slider, dropdown, checkbox group, radio, multi-select and checkbox, then read the sentence.",
    effect: "read",
    categories: ["research", "form", "custom-select", "slider", "multi-widget"],
    difficulty: 3,
    start: "/",
    settle: 2000,
    steps: [
      { do: "fill", target: "number input for Count", role: "spinbutton", value: "8" },
      { do: "choose", target: "Animal", role: "combobox", value: "dog" },
      { do: "toggle", target: "USA", role: "checkbox", checked: true },
      { do: "toggle", target: "Japan", role: "checkbox", checked: true },
      { do: "click", target: "zoo", role: "radio" },
      { do: "choose", target: "Activity", role: "combobox", value: "ran" },
      { do: "toggle", target: "Morning", role: "checkbox", checked: true },
      { do: "click", target: "Submit", role: "button", wait: 1500 },
    ],
    oracle: "gr03",
  },
  {
    id: "GR-04",
    app: "gradio",
    variant: "gradio-calculator",
    title: "Load an example row into the form and submit it",
    objective: "Click the example 144 multiply 2.5 to fill the form and submit it.",
    effect: "read",
    categories: ["research", "examples", "composite"],
    difficulty: 2,
    start: "/",
    settle: 2000,
    steps: [
      { do: "click", target: "144", role: "gridcell", wait: 1500 },
      { do: "click", target: "Submit", role: "button", wait: 1500 },
    ],
    oracle: "gr04",
  },
  {
    id: "GR-05",
    app: "gradio",
    variant: "gradio-sales_projections",
    title: "Run a projection on an editable table and read the regression table",
    objective: "Submit the prefilled sales table and read the fitted regression coefficients and the plot.",
    effect: "read",
    categories: ["research", "chart", "table", "specialist"],
    difficulty: 3,
    start: "/",
    settle: 2500,
    steps: [{ do: "click", target: "Submit", role: "button", wait: 3000 }],
    oracle: "gr05",
  },
  {
    id: "GR-06",
    app: "gradio",
    variant: "gradio-zip_files",
    title: "Upload two files and download the generated archive",
    objective: "Upload a.txt and b.txt, submit, and download the resulting zip with both files inside.",
    effect: "write",
    categories: ["upload", "download", "file", "research"],
    difficulty: 3,
    download: true,
    start: "/",
    settle: 2500,
    steps: [
      { do: "click", target: "Clear", role: "button", nth: 0, wait: 600 },
      { do: "upload", target: "Click to upload or drop files", sel: "input[type=file]", unnamed: true, value: "a.txt", text: "alpha\n" },
      { do: "upload", target: "Click to upload or drop files", sel: "input[type=file]", unnamed: true, value: "b.txt", text: "beta\n" },
      { do: "click", target: "Submit", role: "button", wait: 2500 },
      { do: "click", target: "205.0 B ⇣", sel: "a[href$='tmp.zip']", wait: 1500 },
    ],
    oracle: "gr06",
  },
];

export const gradioOracles: Record<string, Oracle> = {
  gr01: async (c) => {
    const v = await output(c.page, "output");
    return { ok: v === "84", detail: `output=${JSON.stringify(v)}` };
  },
  gr02: async (c) => {
    const text = await c.page.locator("body").innerText();
    const v = await output(c.page, "output");
    return { ok: /Cannot divide by zero!/.test(text) && (v === "0" || v === ""), detail: `output=${JSON.stringify(v)} errorShown=${/Cannot divide by zero!/.test(text)}` };
  },
  gr03: async (c) => {
    const v = await output(c.page, "output");
    const want = "The 8 dogs from USA and Japan went to the zoo where they swam and slept and ran until the morning";
    return { ok: v === want, detail: `output=${JSON.stringify(v)}` };
  },
  gr04: async (c) => {
    const v = await output(c.page, "output");
    return { ok: v === "360", detail: `output=${JSON.stringify(v)}` };
  },
  gr05: async (c) => {
    const data = [[12, 14, 18], [14, 17, 2], [8, 9, 12]];
    const col = (m: number): number[] => data.map((r) => r[m] as number);
    const coef = (m: number): number[] => {
      const [y0, y1, y2] = col(m) as [number, number, number];
      return [(y0 - 2 * y1 + y2) / 2, (-3 * y0 + 4 * y1 - y2) / 2, y0];
    };
    const want = [0, 1, 2].map(coef);
    const grid = c.page.locator("[role=grid][aria-label='output 2']");
    const text = await grid.innerText({ timeout: 2000 }).catch(() => "");
    const nums = (text.match(/-?\d+(?:\.\d+)?/g) ?? []).map(Number);
    const flat = want.flat();
    const found = flat.every((w) => nums.some((n) => Math.abs(n - w) < 0.01));
    const plot = (await c.page.locator("img[alt*='plot' i], .plot-container img, [data-testid='plot'] img, .output-plot img").count()) + (await c.page.locator("canvas, svg.main-svg, .plot img").count());
    return { ok: found && plot > 0, detail: `coefficientsFound=${found} plotElements=${plot} nums=${nums.length}` };
  },
  gr06: async (c) => {
    const name = [...c.files.keys()].find((f) => f.endsWith(".zip"));
    if (!name) return { ok: false, detail: "no zip download" };
    const dir = mkdtempSync(`${tmpdir()}/gr06-`);
    const { writeFileSync } = await import("node:fs");
    writeFileSync(`${dir}/x.zip`, c.files.get(name) as Buffer);
    const listing = execFileSync("python3", ["-I", "-c", "import zipfile,sys;z=zipfile.ZipFile(sys.argv[1]);print(';'.join(sorted(f'{i.filename}={z.read(i).decode()!r}' for i in z.infolist())))", `${dir}/x.zip`], { encoding: "utf8" }).trim();
    void readdirSync;
    void readFileSync;
    return { ok: listing === "a.txt='alpha\\n';b.txt='beta\\n'", detail: listing };
  },
};
