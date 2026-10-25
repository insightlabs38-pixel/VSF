import { mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { type Browser, chromium, type ElementHandle, type Page as PwPage } from "@playwright/test";
import type { Harness } from "../../observation/support/world.ts";
import { setToggle, typeInto } from "./actuate.ts";
import type { Annotation } from "./annotation.ts";
import { openState } from "./capture.ts";
import { recognizeSnapshot } from "./evaluate.ts";
import type { Page, Partition, Source } from "./manifest.ts";
import { COMPOSITE_KINDS, norm, type RecComposite, type RecUnit } from "./match.ts";
import {
  annotatedNames,
  loadFrozen,
  loadTriage,
  type Operation,
  type Operations,
  type Step,
  type Triage,
  targetsOf,
} from "./operations.ts";
import { allHold, locate, locateAll, locateItem, referenceStep } from "./reader.ts";
import { annotationPath, operationsPath, readIf, start } from "./run.ts";

export type Reference = "confirmed" | "vacuous" | "unconfirmed" | "invalid";
export type Outcome = "verified" | "fallback" | "abandoned" | "wrong_effect" | "misbound";
/** Why an intended workflow is or is not a verified complete success; separate from the executable outcome. */
export type Diagnostic =
  | "verified"
  | "recognition_limit"
  | "incorrect_effect"
  | "reference_limit"
  | "annotation_defect"
  | "information_limit";

export interface OperationResult {
  source: string;
  page: string;
  partition: Partition;
  operation: string;
  negative: boolean;
  reference: Reference;
  referenceDetail?: string;
  /** Written in the first annotation round; the denominator of every coverage figure. */
  origin: "frozen" | "added";
  diagnostic: Diagnostic;
  /** Triage reason for a limitation that is not the recognizer's. */
  triage?: string;
  outcome?: Outcome;
  /** What the recognized path did on an operation the reference could not confirm; never counted as verified. */
  probe?: Outcome;
  probeDetail?: string;
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
  before.page.setDefaultTimeout(5000);
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

/** What the browser itself calls a file input, whatever the page labels it. */
const BROWSER_FILE_NAMES = ["choose file", "choose files", "browse", "no file chosen"];

/** The member of a recognized composite a step names, when exactly one composite of the annotated kind has it. */
function resolveItem(
  step: Extract<Step, { do: "click" }>,
  composites: RecComposite[],
  a: Annotation | undefined,
): { unit?: RecUnit; why?: string } {
  const kind = a?.composites.find((c) => norm(c.name) === norm(step.target))?.kind;
  const kinds = kind ? (COMPOSITE_KINDS[kind] ?? []) : [];
  const want = norm(step.item);
  const hits = new Map<number, string>();
  for (const c of composites)
    if (kinds.includes(c.kind)) for (const m of c.members) if (norm(m.name) === want) hits.set(m.node, c.kind);
  if (hits.size === 0) return { why: "no_unit" };
  if (hits.size > 1) return { why: "ambiguous" };
  const [node] = [...hits.keys()];
  return { unit: { type: "action", node: node as number, name: step.item ?? "", usable: true } };
}

/** The one usable unit a step names, or why none can be used. Several matches are never guessed between. */
export function resolve(
  step: Step,
  units: RecUnit[],
  composites: RecComposite[] = [],
  a?: Annotation,
): { unit?: RecUnit; why?: string } {
  if (step.do === "press") return {};
  if (step.do === "click" && step.item) return resolveItem(step, composites, a);
  const n = norm(step.target);
  const hitNodes = new Set(
    units
      .filter((u) => u.type === "field" && u.binding?.state === "bound" && norm(u.binding.text) === n && u.usable)
      .map((u) => u.node),
  );
  const fieldKinds: Record<string, string[]> = {
    fill: ["text", "date", "other", "range"],
    choose: ["choice"],
    toggle: ["toggle"],
    upload: ["file"],
  };
  const nativeFileName = step.do === "upload" && BROWSER_FILE_NAMES.includes(n);
  // A control that is only a part of another hit's widget (the select a combobox enhances) is not a second choice.
  const covered = new Set(
    composites.filter((c) => c.owner !== null && hitNodes.has(c.owner)).flatMap((c) => c.covers ?? []),
  );
  const fieldHits = units.filter(
    (u) =>
      !covered.has(u.node) &&
      (nativeFileName
        ? u.type === "field" && u.kind === "file" && u.usable
        : u.type === "field" && u.binding?.state === "bound" && norm(u.binding.text) === n && u.usable),
  );
  // Row checkboxes inherit their column's name; the one in the header of the same structure is the select-all.
  const pickHeader = (hits: RecUnit[]): RecUnit[] => {
    for (const c of composites) {
      if (c.kind !== "bulk_selection") continue;
      const part = new Map(c.members.map((m) => [m.node, m.part]));
      if (hits.every((h) => part.has(h.node))) {
        const head = hits.filter((h) => part.get(h.node) === "header");
        if (head.length === 1 && hits.length > 1) return head;
      }
    }
    return hits;
  };
  if (step.do === "click") {
    const actionHits = units.filter(
      (u) => u.type === "action" && (norm(u.name) === n || norm(u.shown) === n) && u.usable,
    );
    const hits = pickHeader(
      actionHits.length > 0 ? actionHits : fieldHits.filter((u) => ["toggle", "choice"].includes(u.kind ?? "")),
    );
    if (hits.length === 0) return { why: "no_unit" };
    return hits.length > 1 ? { why: "ambiguous" } : { unit: hits[0] as RecUnit };
  }
  if (fieldHits.length === 0) return { why: "no_unit" };
  const ok = fieldHits.filter(
    (u) =>
      (fieldKinds[step.do] ?? []).includes(u.kind ?? "") ||
      (u.editable === true &&
        ((step.do === "fill" && u.kind === "choice") || (step.do === "choose" && u.kind === "text"))),
  );
  if (ok.length === 0) return { why: "kind_mismatch" };
  const chosen = pickHeader(ok);
  return chosen.length > 1 ? { why: "ambiguous" } : { unit: chosen[0] as RecUnit };
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

async function sameElement(
  page: PwPage,
  a: Annotation,
  step: Extract<Step, { target: string }>,
  el: ElementHandle<Element>,
  widget: boolean,
): Promise<boolean> {
  const item = step.do === "click" ? step.item : undefined;
  const found = item
    ? [locateItem(page, item)]
    : [
        locateAll(page, a, step.target),
        ...((await locateAll(page, a, step.target).count()) > 0 ? [] : [await locate(page, a, step.target)]),
      ];
  let any = false;
  for (const loc of found) {
    if ((await loc.count()) === 0) continue;
    for (const c of await loc.elementHandles()) {
      any = true;
      if (await el.evaluate((x, a) => x === a.y || (a.w && x.contains(a.y)), { y: c, w: widget })) return true;
    }
  }
  return !any;
}

interface Context {
  page: PwPage;
  h: Harness;
  a: Annotation;
  tmp: string;
  label: string;
}

const norms = (xs: (string | null)[]): string[] => xs.map(norm);

/** Opens a combobox the way a person does and clicks the entry it lists, finding that entry among what is recognized after opening. */
async function chooseFromPopup(cx: Context, unit: RecUnit, el: ElementHandle<Element>, value: string): Promise<void> {
  const inside = await el.evaluateHandle(
    (root, want) =>
      root.getAttribute("role") === "listbox"
        ? ([...root.querySelectorAll('[role="option"]')].find(
            (o) => (o.textContent ?? "").replace(/\s+/g, " ").trim() === want,
          ) ?? null)
        : null,
    value,
  );
  const listed = inside.asElement();
  if (listed) return void (await listed.click({ timeout: 4000 }));
  await el.click({ timeout: 2000 }).catch(() => el.focus());
  for (let attempt = 0; attempt < 2; attempt++) {
    await cx.page.waitForTimeout(250);
    await cx.h.settle();
    const rec = recognizeSnapshot(cx.h.collector.replica.toRecognizerInput(), cx.label, cx.tmp).result;
    const hits = new Map<number, string>();
    for (const c of rec.composites ?? [])
      if (c.kind === "combobox_popup" && c.owner === unit.node)
        for (const m of c.members) if (norms([m.name])[0] === norm(value)) hits.set(m.node, c.kind);
    if (hits.size > 1) throw new Error("unsupported:ambiguous_option");
    const [node] = [...hits.keys()];
    if (node !== undefined) {
      const option = await elementFor(cx.page, node);
      if (!option) throw new Error("unsupported:option_gone");
      await option.click({ timeout: 4000 });
      return;
    }
    if (attempt === 0 && unit.editable) await typeInto(cx.page, el, value);
  }
  throw new Error("unsupported:no_option");
}

async function perform(cx: Context, step: Step, el: ElementHandle<Element> | null, unit?: RecUnit): Promise<void> {
  const { page } = cx;
  const t = { timeout: 4000 };
  if (step.do === "press") await page.keyboard.press(step.key);
  else if (!el) throw new Error("unsupported:element_gone");
  else if (step.do === "fill") await typeInto(page, el, step.value);
  else if (step.do === "click") await el.click(t);
  else if (step.do === "toggle") await setToggle(el, step.checked);
  else if (step.do === "choose") {
    const tag = await el.evaluate((e) => e.tagName);
    if (tag === "SELECT") await el.selectOption({ label: step.value }, t);
    else await chooseFromPopup(cx, unit as RecUnit, el, step.value);
  } else if (step.do === "upload") {
    const file = { name: step.value, mimeType: "text/plain", buffer: Buffer.from(step.text) };
    const isInput = await el.evaluate((e) => e instanceof HTMLInputElement && e.type === "file");
    if (!isInput) throw new Error("unsupported:file_chooser");
    await el.setInputFiles(file, t);
  }
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
  page.setDefaultTimeout(5000);
  let done = 0;
  const stop = (i: number, why: string): { outcome: Outcome; detail: string; stepsDone: number } => ({
    outcome: done === 0 ? "fallback" : "abandoned",
    detail: `step ${i + 1}: ${why}`,
    stepsDone: done,
  });
  try {
    for (const [i, step] of op.steps.entries()) {
      if (!h) throw new Error("observer missing");
      await h.settle();
      let unit: RecUnit | undefined;
      let el: ElementHandle<Element> | null = null;
      const cx: Context = { page, h, a, tmp, label: `${s.id}__${p.id}__op` };
      if (step.do !== "press") {
        const rec = recognizeSnapshot(h.collector.replica.toRecognizerInput(), cx.label, tmp).result;
        const r = resolve(step, rec.units, rec.composites ?? [], a);
        if (!r.unit) return stop(i, r.why ?? "no_unit");
        unit = r.unit;
        el = await elementFor(page, unit.node);
        if (!el) return stop(i, "element_gone");
        if (
          !(await sameElement(
            page,
            a,
            step,
            el,
            (rec.composites ?? []).some((c) => c.owner === unit?.node),
          ))
        )
          return {
            outcome: "misbound",
            detail: `step ${i + 1}: acted on an element other than "${step.target}"`,
            stepsDone: done,
          };
      }
      try {
        await perform(cx, step, el, unit);
      } catch (e) {
        const m = String((e as Error).message);
        return stop(
          i,
          m.startsWith("unsupported:") ? m.slice(12) : `could not act (${m.split("\n")[0]?.slice(0, 100)})`,
        );
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
  /** Sources measured at the same time, each in its own server and browser contexts. */
  jobs?: number;
}

export const diagnosticOf = (ref: Reference, outcome: Outcome | undefined): Diagnostic => {
  if (ref === "vacuous" || ref === "invalid") return "annotation_defect";
  if (ref === "unconfirmed") return "reference_limit";
  if (outcome === "verified") return "verified";
  if (outcome === "wrong_effect" || outcome === "misbound") return "incorrect_effect";
  return "recognition_limit";
};

async function runSource(
  browser: Browser,
  s: Source,
  plan: Plan,
  tmp: string,
  frozen: Set<string>,
  triage: Triage,
): Promise<OperationResult[]> {
  const out: OperationResult[] = [];
  const files = s.pages.filter((p) => !plan.only || plan.only.some((o) => o === s.id || o === `${s.id}/${p.id}`));
  const todo = files
    .map((p) => ({
      p,
      ops: readIf<Operations>(operationsPath(s.id, p.id)),
      a: readIf<Annotation>(annotationPath(s.id, p.id)),
    }))
    .filter((x) => x.ops && x.a);
  if (todo.length === 0) return out;
  const running = await start(s);
  try {
    for (const { p, ops, a } of todo) {
      const names = annotatedNames(a as Annotation);
      for (const op of (ops as Operations).operations) {
        const key = `${s.id}/${p.id}/${op.name}`;
        const unknown = targetsOf(op).filter((t) => !names.has(norm(t)));
        const ref =
          unknown.length > 0
            ? { status: "invalid" as Reference, detail: `unknown target ${unknown[0]}` }
            : await referenceRun(browser, running.origin, s, p, a as Annotation, op);
        const row: OperationResult = {
          source: s.id,
          page: p.id,
          partition: s.partition,
          operation: op.name,
          negative: op.negative === true,
          reference: ref.status,
          origin: frozen.has(key) ? "frozen" : "added",
          diagnostic: "reference_limit",
          ...(ref.detail ? { referenceDetail: ref.detail } : {}),
        };
        if (plan.recognized && (ref.status === "confirmed" || ref.status === "unconfirmed")) {
          const r = await recognizedRun(browser, running.origin, s, p, a as Annotation, op, tmp);
          if (ref.status === "confirmed") {
            row.outcome = r.outcome;
            row.stepsDone = r.stepsDone;
            if (r.detail) row.detail = r.detail;
          } else {
            row.probe = r.outcome;
            if (r.detail) row.probeDetail = r.detail;
          }
        }
        row.diagnostic = diagnosticOf(ref.status, row.outcome);
        const t = triage[key];
        if (t && row.diagnostic !== "verified" && row.diagnostic !== "incorrect_effect") {
          row.diagnostic = t.class;
          row.triage = t.reason;
        }
        out.push(row);
        if (process.env.VSF_CORPUS_PROGRESS)
          console.error(`${key}: ${row.reference} ${row.outcome ?? row.probe ?? ""}`);
      }
    }
  } finally {
    await running.stop();
  }
  return out;
}

export async function runOperations(plan: Plan): Promise<OperationResult[]> {
  const browser = await chromium.launch({ channel: "chromium" });
  const tmp = mkdtempSync(`${tmpdir()}/corpus-ops-`);
  const frozen = loadFrozen();
  const triage = loadTriage();
  const queue = [...plan.sources];
  const results = new Map<string, OperationResult[]>();
  try {
    await Promise.all(
      Array.from({ length: Math.max(1, Math.min(plan.jobs ?? 1, queue.length)) }, async () => {
        for (let s = queue.shift(); s; s = queue.shift())
          results.set(s.id, await runSource(browser, s, plan, tmp, frozen, triage));
      }),
    );
  } finally {
    await browser.close();
  }
  return plan.sources.flatMap((s) => results.get(s.id) ?? []);
}

export interface OperationSummary {
  /** Frozen intended workflows: the denominator of every coverage figure. */
  intended: number;
  addedAfterFreeze: number;
  referenceConfirmed: number;
  referenceUnconfirmed: number;
  annotationDefects: number;
  executed: number;
  verified: number;
  fallback: number;
  abandoned: number;
  wrongEffect: number;
  misbound: number;
  falseExecutableClaims: number;
  /** verified / intended: the figure that stands for workflow coverage. */
  verifiedOfIntended: number | null;
  /** verified / reference-confirmed: a rate on the subset the reference driver could demonstrate; not coverage. */
  verifiedOfReferenceConfirmed: number | null;
  diagnostics: Record<Diagnostic, number>;
  /** Unconfirmed operations the recognized path happened to perform with the expectations holding; not counted anywhere above. */
  unconfirmedProbes: { run: number; expectationsHeld: number };
}

export function summarizeOperations(rows: OperationResult[]): OperationSummary {
  const frozen = rows.filter((r) => r.origin === "frozen");
  const n = (f: (r: OperationResult) => boolean, set = frozen): number => set.filter(f).length;
  const confirmed = n((r) => r.reference === "confirmed");
  const verified = n((r) => r.outcome === "verified");
  const wrong = n((r) => r.outcome === "wrong_effect");
  const mis = n((r) => r.outcome === "misbound");
  const diagnostics: Record<Diagnostic, number> = {
    verified: 0,
    recognition_limit: 0,
    incorrect_effect: 0,
    reference_limit: 0,
    annotation_defect: 0,
    information_limit: 0,
  };
  for (const r of frozen) diagnostics[r.diagnostic]++;
  const ratio = (a: number, b: number): number | null => (b === 0 ? null : Math.round((a / b) * 1000) / 1000);
  return {
    intended: frozen.length,
    addedAfterFreeze: rows.length - frozen.length,
    referenceConfirmed: confirmed,
    referenceUnconfirmed: n((r) => r.reference === "unconfirmed"),
    annotationDefects: n((r) => r.reference === "vacuous" || r.reference === "invalid"),
    executed: n((r) => r.outcome !== undefined),
    verified,
    fallback: n((r) => r.outcome === "fallback"),
    abandoned: n((r) => r.outcome === "abandoned"),
    wrongEffect: wrong,
    misbound: mis,
    falseExecutableClaims: wrong + mis,
    verifiedOfIntended: ratio(verified, frozen.length),
    verifiedOfReferenceConfirmed: ratio(verified, confirmed),
    diagnostics,
    unconfirmedProbes: { run: n((r) => r.probe !== undefined), expectationsHeld: n((r) => r.probe === "verified") },
  };
}
