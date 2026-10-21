import { mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { type Browser, chromium, type ElementHandle, type Page as PwPage } from "@playwright/test";
import type { Annotation } from "./annotation.ts";
import { openState } from "./capture.ts";
import { recognizeSnapshot } from "./evaluate.ts";
import type { Page, Partition, Source } from "./manifest.ts";
import { norm, type RecUnit } from "./match.ts";
import type { Operation, Operations, Step } from "./operations.ts";
import { allHold, locateAll, referenceStep } from "./reader.ts";
import { annotationPath, operationsPath, readIf, start } from "./run.ts";

export type Reference = "confirmed" | "vacuous" | "unconfirmed";
export type Outcome = "verified" | "fallback" | "abandoned" | "wrong_effect" | "misbound";

export interface OperationResult {
  source: string;
  page: string;
  partition: Partition;
  operation: string;
  negative: boolean;
  reference: Reference;
  referenceDetail?: string;
  outcome?: Outcome;
  detail?: string;
  stepsDone?: number;
}

/** Runs one operation through the page's accessibility surface alone and says whether it demonstrably works. */
export async function referenceRun(
  browser: Browser,
  origin: string,
  s: Source,
  p: Page,
  a: Annotation,
  op: Operation,
): Promise<{ status: Reference; detail?: string }> {
  const before = await openState(browser, origin, s, p, false);
  try {
    if (!op.negative && (await allHold(before.page, a, op.expect.slice(0, 1))) && op.expect.length === 1)
      return { status: "vacuous", detail: "the expectation already holds before any step" };
    for (const [i, st] of op.steps.entries())
      await referenceStep(before.page, a, st).catch((e) =>
        Promise.reject(new Error(`step ${i + 1}: ${String(e.message).split("\n")[0]}`)),
      );
    if (await allHold(before.page, a, op.expect)) return { status: "confirmed" };
    return { status: "unconfirmed", detail: "an expectation did not hold after the steps" };
  } catch (e) {
    return { status: "unconfirmed", detail: String((e as Error).message).slice(0, 160) };
  } finally {
    await before.close();
  }
}

/** The one usable unit a step names, or why none can be used. Several matches are never guessed between. */
export function resolve(step: Step, units: RecUnit[]): { unit?: RecUnit; why?: string } {
  if (step.do === "press") return {};
  const n = norm(step.target);
  const fieldKinds: Record<string, string[]> = {
    fill: ["text", "date", "other", "range"],
    choose: ["choice"],
    toggle: ["toggle"],
    upload: ["file"],
  };
  const fieldHits = units.filter(
    (u) => u.type === "field" && u.binding?.state === "bound" && norm(u.binding.text) === n && u.usable,
  );
  if (step.do === "click") {
    const actionHits = units.filter((u) => u.type === "action" && norm(u.name) === n && u.usable);
    const hits =
      actionHits.length > 0 ? actionHits : fieldHits.filter((u) => ["toggle", "choice"].includes(u.kind ?? ""));
    if (hits.length === 0) return { why: "no_unit" };
    return hits.length > 1 ? { why: "ambiguous" } : { unit: hits[0] as RecUnit };
  }
  if (fieldHits.length === 0) return { why: "no_unit" };
  const ok = fieldHits.filter((u) => (fieldKinds[step.do] ?? []).includes(u.kind ?? ""));
  if (ok.length === 0) return { why: "kind_mismatch" };
  return ok.length > 1 ? { why: "ambiguous" } : { unit: ok[0] as RecUnit };
}

async function elementFor(page: PwPage, node: number): Promise<ElementHandle<Element> | null> {
  const h = await page.evaluateHandle(
    (id) =>
      (window as unknown as { __vsf: { session: { elementOf(i: number): Element | null } } }).__vsf.session.elementOf(
        id,
      ),
    node,
  );
  return h.asElement() as ElementHandle<Element> | null;
}

async function sameElement(page: PwPage, a: Annotation, target: string, el: ElementHandle<Element>): Promise<boolean> {
  const all = await locateAll(page, a, target).elementHandles();
  for (const c of all) if (await el.evaluate((x, y) => x === y, c)) return true;
  return all.length === 0;
}

async function perform(page: PwPage, step: Step, el: ElementHandle<Element>): Promise<void> {
  const t = { timeout: 4000 };
  if (step.do === "fill") await el.fill(step.value, t);
  else if (step.do === "click") await el.click(t);
  else if (step.do === "toggle") {
    const role = await el.evaluate((e) => e.getAttribute("role"));
    if (role === "switch" || role === "checkbox") {
      const now = (await el.evaluate((e) => e.getAttribute("aria-checked"))) === "true";
      if (now !== step.checked) await el.click(t);
    } else await el.setChecked(step.checked, t);
  } else if (step.do === "choose") {
    const tag = await el.evaluate((e) => e.tagName);
    if (tag !== "SELECT") throw new Error("unsupported:choice_popup");
    await el.selectOption({ label: step.value }, t);
  } else if (step.do === "upload") {
    const file = { name: step.value, mimeType: "text/plain", buffer: Buffer.from(step.text) };
    const isInput = await el.evaluate((e) => e instanceof HTMLInputElement && e.type === "file");
    if (!isInput) throw new Error("unsupported:file_chooser");
    await el.setInputFiles(file, t);
  } else await page.keyboard.press(step.key);
  await page.waitForTimeout(200);
}

/** Executes an operation using only what the recognizer found, observing again after every step. */
export async function recognizedRun(
  browser: Browser,
  origin: string,
  s: Source,
  p: Page,
  a: Annotation,
  op: Operation,
  tmp: string,
): Promise<{ outcome: Outcome; detail?: string; stepsDone: number }> {
  const opened = await openState(browser, origin, s, p, true);
  const { page, h } = opened;
  let done = 0;
  try {
    for (const [i, step] of op.steps.entries()) {
      if (!h) throw new Error("observer missing");
      await h.settle();
      let unit: RecUnit | undefined;
      let el: ElementHandle<Element> | null = null;
      if (step.do !== "press") {
        const snap = h.collector.replica.toRecognizerInput();
        const rec = recognizeSnapshot(snap, `${s.id}__${p.id}__op`, tmp).result;
        const r = resolve(step, rec.units);
        if (!r.unit)
          return { outcome: done === 0 ? "fallback" : "abandoned", detail: `step ${i + 1}: ${r.why}`, stepsDone: done };
        unit = r.unit;
        el = await elementFor(page, unit.node);
        if (!el)
          return {
            outcome: done === 0 ? "fallback" : "abandoned",
            detail: `step ${i + 1}: element_gone`,
            stepsDone: done,
          };
        if (!(await sameElement(page, a, step.target, el)))
          return {
            outcome: "misbound",
            detail: `step ${i + 1}: acted on an element other than "${step.target}"`,
            stepsDone: done,
          };
      }
      try {
        await perform(page, step, el as ElementHandle<Element>);
      } catch (e) {
        const m = String((e as Error).message);
        if (m.startsWith("unsupported:"))
          return {
            outcome: done === 0 ? "fallback" : "abandoned",
            detail: `step ${i + 1}: ${m.slice(12)}`,
            stepsDone: done,
          };
        return {
          outcome: "wrong_effect",
          detail: `step ${i + 1}: could not act (${m.split("\n")[0]?.slice(0, 100)})`,
          stepsDone: done,
        };
      }
      done++;
    }
    return (await allHold(page, a, op.expect))
      ? { outcome: "verified", stepsDone: done }
      : { outcome: "wrong_effect", detail: "steps done, an expectation does not hold", stepsDone: done };
  } finally {
    await opened.close();
  }
}

export interface Plan {
  sources: Source[];
  only?: string[] | undefined;
  /** Run the recognized-unit path as well as the reference gate. */
  recognized: boolean;
}

export async function runOperations(plan: Plan): Promise<OperationResult[]> {
  const out: OperationResult[] = [];
  const browser = await chromium.launch({ channel: "chromium" });
  const tmp = mkdtempSync(`${tmpdir()}/corpus-ops-`);
  try {
    for (const s of plan.sources) {
      const files = s.pages.filter((p) => !plan.only || plan.only.some((o) => o === s.id || o === `${s.id}/${p.id}`));
      const todo = files
        .map((p) => ({
          p,
          ops: readIf<Operations>(operationsPath(s.id, p.id)),
          a: readIf<Annotation>(annotationPath(s.id, p.id)),
        }))
        .filter((x) => x.ops && x.a);
      if (todo.length === 0) continue;
      const running = await start(s);
      try {
        for (const { p, ops, a } of todo) {
          for (const op of (ops as Operations).operations) {
            const ref = await referenceRun(browser, running.origin, s, p, a as Annotation, op);
            const row: OperationResult = {
              source: s.id,
              page: p.id,
              partition: s.partition,
              operation: op.name,
              negative: op.negative === true,
              reference: ref.status,
              ...(ref.detail ? { referenceDetail: ref.detail } : {}),
            };
            if (plan.recognized && ref.status === "confirmed") {
              const r = await recognizedRun(browser, running.origin, s, p, a as Annotation, op, tmp);
              row.outcome = r.outcome;
              row.stepsDone = r.stepsDone;
              if (r.detail) row.detail = r.detail;
            }
            out.push(row);
          }
        }
      } finally {
        await running.stop();
      }
    }
  } finally {
    await browser.close();
  }
  return out;
}

export interface OperationSummary {
  annotated: number;
  referenceConfirmed: number;
  droppedVacuous: number;
  droppedUnconfirmed: number;
  executed: number;
  verified: number;
  fallback: number;
  abandoned: number;
  wrongEffect: number;
  misbound: number;
  falseExecutableClaims: number;
  verifiedShare: number | null;
}

export function summarizeOperations(rows: OperationResult[]): OperationSummary {
  const n = (f: (r: OperationResult) => boolean): number => rows.filter(f).length;
  const confirmed = n((r) => r.reference === "confirmed");
  const verified = n((r) => r.outcome === "verified");
  const wrong = n((r) => r.outcome === "wrong_effect");
  const mis = n((r) => r.outcome === "misbound");
  return {
    annotated: rows.length,
    referenceConfirmed: confirmed,
    droppedVacuous: n((r) => r.reference === "vacuous"),
    droppedUnconfirmed: n((r) => r.reference === "unconfirmed"),
    executed: n((r) => r.outcome !== undefined),
    verified,
    fallback: n((r) => r.outcome === "fallback"),
    abandoned: n((r) => r.outcome === "abandoned"),
    wrongEffect: wrong,
    misbound: mis,
    falseExecutableClaims: wrong + mis,
    verifiedShare: confirmed === 0 ? null : Math.round((verified / confirmed) * 1000) / 1000,
  };
}
