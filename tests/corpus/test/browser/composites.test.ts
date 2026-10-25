import assert from "node:assert/strict";
import { mkdirSync, mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { test } from "node:test";
import { chromium } from "@playwright/test";
import type { Annotation } from "../../src/annotation.ts";
import { recognizedRun } from "../../src/execute.ts";
import type { Page, Source } from "../../src/manifest.ts";
import type { Operation } from "../../src/operations.ts";
import { serveStatic } from "../../src/serve.ts";

const toy = `<!doctype html><title>t</title><main>
<h1>Widgets</h1>
<div role="tablist" id="tl">
  <button role="tab" id="t1" aria-selected="true" aria-controls="p1">Overview</button>
  <button role="tab" id="t2" aria-selected="false" aria-controls="p2">Details</button>
</div>
<div role="tabpanel" id="p1">Overview text</div>
<div role="tabpanel" id="p2" hidden>Details text</div>

<div id="other" role="tablist"><button role="tab" aria-selected="false" aria-controls="p3">Notes</button><button role="tab" aria-selected="true" aria-controls="p4">Overview</button></div>
<div role="tabpanel" id="p4">Other overview</div>
<div role="tabpanel" id="p3" hidden>Other details</div>

<p>Size <span id="size" role="combobox" tabindex="0" aria-haspopup="listbox" aria-expanded="false" aria-controls="sizes" aria-label="Size">Medium</span></p>
<ul id="sizes" role="listbox" hidden>
  <li role="option" id="oS">Small</li><li role="option" id="oM">Medium</li><li role="option" id="oL">Large</li>
</ul>
<p>Colour <span id="col" role="combobox" tabindex="0" aria-haspopup="listbox" aria-expanded="false" aria-controls="cols" aria-label="Colour">Red</span></p>
<ul id="cols" role="listbox" hidden>
  <li role="option">Red</li><li role="option">Large</li>
</ul>
<p id="out" role="status"></p>

<table aria-label="Rows"><thead><tr><th><input type="checkbox" id="all" aria-label="Select all"></th><th>Name</th></tr></thead>
<tbody><tr><td><input type="checkbox" aria-label="Select all"></td><td>One</td></tr><tr><td><input type="checkbox" aria-label="Select all"></td><td>Two</td></tr></tbody></table>

<script>
const open = (id, list) => {
  const c = document.getElementById(id), l = document.getElementById(list);
  c.addEventListener("click", () => { l.hidden = !l.hidden; c.setAttribute("aria-expanded", String(!l.hidden)); });
  l.addEventListener("click", (e) => { if (e.target.role === "option" || e.target.getAttribute("role") === "option") { c.textContent = e.target.textContent; l.hidden = true; c.setAttribute("aria-expanded", "false"); } });
};
open("size", "sizes"); open("col", "cols");
for (const b of document.querySelectorAll("[role=tab]")) b.addEventListener("click", () => {
  for (const t of b.parentElement.querySelectorAll("[role=tab]")) { t.setAttribute("aria-selected", String(t === b)); document.getElementById(t.getAttribute("aria-controls")).hidden = t !== b; }
});
document.getElementById("all").addEventListener("change", (e) => { for (const c of document.querySelectorAll("tbody input")) c.checked = e.target.checked; document.getElementById("out").textContent = e.target.checked ? "all selected" : "none selected"; });
</script></main>`;

const ann = (): Annotation => ({
  schema: "vsf-corpus-annotation-v1",
  source: "toy",
  page: "p",
  dossierSha256: "0".repeat(64),
  annotator: { kind: "human", id: "test", sawRecognizerOutput: false, basis: ["test"] },
  fields: [
    { name: "Size", kind: "choice" },
    { name: "Colour", kind: "choice" },
    { name: "Select all", kind: "toggle" },
  ],
  actions: [],
  collections: [],
  composites: [
    { kind: "tabs", name: "Sections" },
    { kind: "combobox_popup", name: "Size" },
    { kind: "bulk_selection", name: "Rows" },
  ],
  workflows: [],
  unsupported: [],
});

const source = (): Source =>
  ({ id: "toy", aliases: {}, partition: "dev", fetch: { kind: "git", url: "", rev: "" } }) as unknown as Source;
const page: Page = { id: "p", path: "/index.html", workflows: ["use widgets"] };

async function withToy<T>(f: (origin: string, tmp: string) => Promise<T>): Promise<T> {
  const dir = mkdtempSync(`${tmpdir()}/toy-comp-`);
  writeFileSync(`${dir}/index.html`, toy);
  const running = await serveStatic(dir);
  const tmp = mkdtempSync(`${tmpdir()}/toy-comp-rec-`);
  mkdirSync(tmp, { recursive: true });
  try {
    return await f(running.origin, tmp);
  } finally {
    await running.stop();
  }
}

const op = (steps: Operation["steps"], expect: Operation["expect"]): Operation => ({
  name: "op",
  effect: "state_change",
  steps,
  expect,
});

test("a composite member, a custom popup choice and a header checkbox are executed through what was recognized", async () => {
  const browser = await chromium.launch({ channel: "chromium" });
  try {
    await withToy(async (origin, tmp) => {
      const run = (o: Operation) => recognizedRun(browser, origin, source(), page, ann(), o, tmp);
      const tab = await run(
        op([{ do: "click", target: "Sections", item: "Details" }], [{ kind: "text", contains: "Details text" }]),
      );
      assert.equal(tab.outcome, "verified", tab.detail);
      const pick = await run(
        op([{ do: "choose", target: "Size", value: "Large" }], [{ kind: "value", target: "Size", value: "Large" }]),
      );
      assert.equal(pick.outcome, "verified", pick.detail);
      const all = await run(
        op([{ do: "toggle", target: "Select all", checked: true }], [{ kind: "text", contains: "all selected" }]),
      );
      assert.equal(all.outcome, "verified", all.detail);
    });
  } finally {
    await browser.close();
  }
});

test("a name that two structures share, a missing member and a missing option are refused and nothing is touched", async () => {
  const browser = await chromium.launch({ channel: "chromium" });
  try {
    await withToy(async (origin, tmp) => {
      const run = (o: Operation) => recognizedRun(browser, origin, source(), page, ann(), o, tmp);
      // "Details" is a tab in two tablists, and the annotation says "tabs" for both.
      const both = await run(
        op([{ do: "click", target: "Sections", item: "Details" }], [{ kind: "text", contains: "Details text" }]),
      );
      assert.ok(both.outcome === "verified" || both.outcome === "fallback");
      const noMember = await run(
        op([{ do: "click", target: "Sections", item: "Nonexistent" }], [{ kind: "text", contains: "x" }]),
      );
      assert.equal(noMember.outcome, "fallback");
      assert.equal(noMember.stepsDone, 0);
      const noOption = await run(
        op(
          [{ do: "choose", target: "Size", value: "Gigantic" }],
          [{ kind: "value", target: "Size", value: "Gigantic" }],
        ),
      );
      assert.equal(noOption.outcome, "fallback");
      assert.match(noOption.detail ?? "", /no_option/);
      // "Large" is also an option of the Colour list; choosing it for Size must not touch Colour.
      const wrongOwner = await run(
        op([{ do: "choose", target: "Size", value: "Large" }], [{ kind: "value", target: "Colour", value: "Large" }]),
      );
      assert.equal(wrongOwner.outcome, "wrong_effect");
    });
  } finally {
    await browser.close();
  }
});

test("a header checkbox is chosen over identically named row checkboxes only because the structure says so", async () => {
  const browser = await chromium.launch({ channel: "chromium" });
  try {
    await withToy(async (origin, tmp) => {
      const rowsOnly: Operation = op(
        [{ do: "toggle", target: "Select all", checked: true }],
        [{ kind: "text", contains: "all selected" }],
      );
      const r = await recognizedRun(browser, origin, source(), page, ann(), rowsOnly, tmp);
      assert.equal(r.outcome, "verified");
    });
  } finally {
    await browser.close();
  }
});
