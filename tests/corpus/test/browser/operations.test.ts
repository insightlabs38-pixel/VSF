import assert from "node:assert/strict";
import { mkdirSync, mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { test } from "node:test";
import { chromium } from "@playwright/test";
import type { Annotation } from "../../src/annotation.ts";
import {
  diagnosticOf,
  type OperationResult,
  recognizedRun,
  referenceRun,
  resolve,
  summarizeOperations,
} from "../../src/execute.ts";
import type { Page, Source } from "../../src/manifest.ts";
import type { RecUnit } from "../../src/match.ts";
import type { Operation } from "../../src/operations.ts";
import { serveStatic } from "../../src/serve.ts";

const html = `<!doctype html><title>t</title><main>
<h1>Todo list</h1>
<label>New item <input id="n"></label>
<button id="add">Add item</button>
<button id="noop">Do nothing</button>
<ul id="l"></ul>
<p id="s" role="status"></p>
<script>
document.getElementById("add").addEventListener("click", () => {
  const v = document.getElementById("n").value;
  if (!v) return;
  const li = document.createElement("li");
  li.textContent = v;
  document.getElementById("l").append(li);
});
</script></main>`;

const ann = (): Annotation => ({
  schema: "vsf-corpus-annotation-v1",
  source: "toy",
  page: "p",
  dossierSha256: "0".repeat(64),
  annotator: { kind: "human", id: "test", sawRecognizerOutput: false, basis: ["test"] },
  fields: [{ name: "New item", kind: "text" }],
  actions: [
    { name: "Add item", effect: "state_change" },
    { name: "Do nothing", effect: "read" },
  ],
  collections: [],
  composites: [],
  workflows: [],
  unsupported: [],
});

const source = (): Source =>
  ({ id: "toy", aliases: {}, partition: "dev", fetch: { kind: "git", url: "", rev: "" } }) as unknown as Source;
const page: Page = { id: "p", path: "/index.html", workflows: ["add an item"] };

const add: Operation = {
  name: "add an item",
  effect: "state_change",
  steps: [
    { do: "fill", target: "New item", value: "Buy milk" },
    { do: "click", target: "Add item" },
  ],
  expect: [{ kind: "text", contains: "Buy milk" }],
};

async function withToy<T>(f: (origin: string, tmp: string) => Promise<T>): Promise<T> {
  const dir = mkdtempSync(`${tmpdir()}/toy-`);
  writeFileSync(`${dir}/index.html`, html);
  const running = await serveStatic(dir);
  const tmp = mkdtempSync(`${tmpdir()}/toy-rec-`);
  mkdirSync(tmp, { recursive: true });
  try {
    return await f(running.origin, tmp);
  } finally {
    await running.stop();
  }
}

test("the reference driver confirms real operations and drops vacuous and unconfirmable ones", async () => {
  const browser = await chromium.launch({ channel: "chromium" });
  try {
    await withToy(async (origin) => {
      assert.equal((await referenceRun(browser, origin, source(), page, ann(), add)).status, "confirmed");
      const vacuous: Operation = { ...add, expect: [{ kind: "text", contains: "Todo list" }] };
      assert.equal((await referenceRun(browser, origin, source(), page, ann(), vacuous)).status, "vacuous");
      const nothing: Operation = {
        name: "claims an effect nothing has",
        effect: "read",
        steps: [{ do: "click", target: "Do nothing" }],
        expect: [{ kind: "text", contains: "Done" }],
      };
      assert.equal((await referenceRun(browser, origin, source(), page, ann(), nothing)).status, "unconfirmed");
    });
  } finally {
    await browser.close();
  }
});

test("an operation done only through recognized units is verified, and a missing control falls back untouched", async () => {
  const browser = await chromium.launch({ channel: "chromium" });
  try {
    await withToy(async (origin, tmp) => {
      const done = await recognizedRun(browser, origin, source(), page, ann(), add, tmp);
      assert.equal(done.outcome, "verified");
      assert.equal(done.stepsDone, 2);
      const absent: Operation = { ...add, steps: [{ do: "click", target: "Remove everything" }] };
      const none = await recognizedRun(browser, origin, source(), page, ann(), absent, tmp);
      assert.equal(none.outcome, "fallback");
      assert.equal(none.stepsDone, 0);
      const late: Operation = {
        ...add,
        steps: [
          { do: "fill", target: "New item", value: "x" },
          { do: "click", target: "Remove everything" },
        ],
      };
      assert.equal((await recognizedRun(browser, origin, source(), page, ann(), late, tmp)).outcome, "abandoned");
    });
  } finally {
    await browser.close();
  }
});

const unit = (o: Partial<RecUnit>): RecUnit => ({ type: "field", node: 1, usable: true, ...o }) as RecUnit;

test("a step resolves only to one usable unit of a suitable kind, never a guess", () => {
  const field = (node: number, text: string, kind = "text", usable = true) =>
    unit({ node, kind, usable, binding: { state: "bound", text } });
  const step = { do: "fill", target: "Email", value: "a@b" } as const;
  assert.equal(resolve(step, [field(1, "Email")]).unit?.node, 1);
  assert.equal(resolve(step, [field(1, "Email"), field(2, "Email")]).why, "ambiguous");
  assert.equal(resolve(step, [field(1, "Email"), field(2, "Email", "text", false)]).unit?.node, 1);
  assert.equal(resolve(step, [field(1, "Email", "toggle")]).why, "kind_mismatch");
  assert.equal(resolve(step, [field(1, "Phone")]).why, "no_unit");
  assert.equal(resolve(step, [field(1, "Email", "text", false)]).why, "no_unit");
  const click = { do: "click", target: "Save" } as const;
  assert.equal(resolve(click, [unit({ type: "action", node: 5, name: "Save" })]).unit?.node, 5);
  assert.equal(resolve(click, [unit({ type: "action", node: 5, name: "Save", usable: false })]).why, "no_unit");
});

test("a recognizer that does nothing scores no verified operation and no false claim", () => {
  const rows: OperationResult[] = ["a", "b", "c"].map((operation) => ({
    source: "toy",
    page: "p",
    partition: "dev",
    operation,
    negative: false,
    reference: "confirmed",
    origin: "frozen",
    diagnostic: "recognition_limit",
    outcome: "fallback",
  }));
  const s = summarizeOperations(rows);
  assert.equal(s.verified, 0);
  assert.equal(s.falseExecutableClaims, 0);
  assert.equal(s.fallback, 3);
  assert.equal(summarizeOperations([{ ...rows[0], outcome: "misbound" } as OperationResult]).falseExecutableClaims, 1);
});

test("operations the reference cannot confirm stay in the denominator and never count as verified", () => {
  const base = { source: "toy", page: "p", partition: "dev", negative: false, origin: "frozen" } as const;
  const rows: OperationResult[] = [
    { ...base, operation: "a", reference: "confirmed", outcome: "verified", diagnostic: "verified" },
    { ...base, operation: "b", reference: "unconfirmed", probe: "verified", diagnostic: "reference_limit" },
    { ...base, operation: "c", reference: "vacuous", diagnostic: "annotation_defect" },
    { ...base, operation: "d", reference: "confirmed", outcome: "wrong_effect", diagnostic: "incorrect_effect" },
    { ...base, operation: "e", reference: "confirmed", outcome: "fallback", diagnostic: "recognition_limit" },
    { ...base, origin: "added", operation: "f", reference: "confirmed", outcome: "verified", diagnostic: "verified" },
  ];
  const s = summarizeOperations(rows);
  assert.equal(s.intended, 5);
  assert.equal(s.addedAfterFreeze, 1);
  assert.equal(s.verified, 1);
  assert.equal(s.verifiedOfIntended, 0.2);
  assert.equal(s.verifiedOfReferenceConfirmed, 0.333);
  assert.deepEqual(s.unconfirmedProbes, { run: 1, expectationsHeld: 1 });
  assert.equal(s.diagnostics.annotation_defect, 1);
  assert.equal(diagnosticOf("confirmed", "misbound"), "incorrect_effect");
  assert.equal(diagnosticOf("unconfirmed", undefined), "reference_limit");
});
