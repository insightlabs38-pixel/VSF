import { mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import type { ElementHandle, Page } from "@playwright/test";
import { attach, type Harness } from "../../../../../tests/observation/support/world.ts";
import { recognizeSnapshot } from "../../../../../tests/corpus/src/evaluate.ts";
import { resolve } from "../../../../../tests/corpus/src/execute.ts";
import { norm, type RecComposite, type RecUnit } from "../../../../../tests/corpus/src/match.ts";
import { ORACLES } from "./oracles.ts";
import { act, locate, refStep } from "./ref.ts";
import { launch, open } from "./session.ts";
import type { Ctx, Step, Workflow } from "./spec.ts";

const TMP = mkdtempSync(`${tmpdir()}/vsf-audit-`);

export type Resolution = "ok" | "no_unit" | "ambiguous" | "kind_mismatch" | "passthrough" | "element_gone" | "act_error" | "misbound";

export interface StepRec {
  i: number;
  do: string;
  target: string;
  resolution: Resolution;
  /** A recognized unit exists at the control the reference driver acted on, whatever its name. */
  unitAtControl: boolean;
  unitKind?: string | undefined;
  usable?: boolean | undefined;
  verbs?: string[] | undefined;
  structural?: string | null | undefined;
  nameBound?: string | undefined;
  recMs: number;
  note?: string;
}

export type Outcome = "native_path" | "hybrid" | "preserved_source" | "incorrect_effect" | "unsafe_misbound" | "unsupported";

export interface VsfResult {
  id: string;
  app: string;
  outcome: Outcome;
  stoppedAt?: number;
  cause?: string;
  steps: StepRec[];
  oracle: { ok: boolean; detail: string };
  ms: number;
  recMsTotal: number;
  recCalls: number;
  stepsMs: number;
  recRssKb: number;
}

async function elementFor(page: Page, node: number): Promise<ElementHandle<Element> | null> {
  const h = await page.evaluateHandle(
    (id) => (window as unknown as { __vsf: { session: { elementOf(i: number): Element | null } } }).__vsf.session.elementOf(id),
    node,
  );
  return h.asElement() as ElementHandle<Element> | null;
}

/** Nodes of recognized units whose element is, contains or is contained by the reference element. */
async function unitsAt(page: Page, units: RecUnit[], ref: ElementHandle<Element>): Promise<RecUnit[]> {
  const ids = units.map((u) => u.node);
  const hit = await page.evaluate(
    ([r, nodes]) => {
      const s = (window as unknown as { __vsf: { session: { elementOf(i: number): Element | null } } }).__vsf.session;
      return (nodes as number[]).filter((n) => {
        const e = s.elementOf(n);
        return !!e && (e === r || e.contains(r as Element) || (r as Element).contains(e));
      });
    },
    [ref, ids] as const,
  );
  const set = new Set(hit);
  return units.filter((u) => set.has(u.node));
}

async function vsfPopup(page: Page, h: Harness, unit: RecUnit, el: ElementHandle<Element>, value: string): Promise<void> {
  await el.click({ timeout: 2500 }).catch(() => el.focus());
  for (let attempt = 0; attempt < 2; attempt++) {
    await page.waitForTimeout(300);
    await h.settle();
    const rec = recognizeSnapshot(h.collector.replica.toRecognizerInput(), "popup", TMP).result;
    const hits = new Map<number, string>();
    for (const c of rec.composites ?? [])
      if (c.kind === "combobox_popup" && c.owner === unit.node)
        for (const m of c.members) if (norm(m.name) === norm(value)) hits.set(m.node, c.kind);
    if (hits.size > 1) throw new Error("ambiguous_option");
    const [node] = [...hits.keys()];
    if (node !== undefined) {
      const o = await elementFor(page, node);
      if (!o) throw new Error("option_gone");
      return void (await o.click({ timeout: 4000 }));
    }
  }
  throw new Error("no_option");
}

export async function runVsf(wf: Workflow, browser: Awaited<ReturnType<typeof launch>>): Promise<VsfResult> {
  const t0 = Date.now();
  let h: Harness | undefined;
  const live = await open(browser, wf, async (page, origin) => {
    h = await attach(page, { allow: [origin] });
  });
  const { ctx } = live;
  const steps: StepRec[] = [];
  let recMsTotal = 0;
  let recCalls = 0;
  let recRssKb = 0;
  const tSteps = Date.now();
  let stoppedAt: number | undefined;
  let cause: string | undefined;
  let misbound = false;
  const oracle = ORACLES[wf.oracle];
  try {
    await ctx.page.goto(`${ctx.origin}${wf.start}`, { waitUntil: "load" });
    await ctx.page.waitForLoadState("networkidle", { timeout: 6000 }).catch(() => {});
    if (wf.settle) await ctx.page.waitForTimeout(wf.settle);
    for (const [i, s] of wf.steps.entries()) {
      const rec: StepRec = { i, do: s.do, target: s.target ?? s.key ?? s.text ?? "", resolution: "passthrough", unitAtControl: false, recMs: 0 };
      steps.push(rec);
      if (s.do === "press" || s.do === "type") {
        await refStep(ctx.page, s, ctx);
        continue;
      }
      await (h as Harness).settle();
      const t1 = performance.now();
      const snap = recognizeSnapshot((h as Harness).collector.replica.toRecognizerInput(), `${wf.id}-${i}`, TMP);
      rec.recMs = Math.round((performance.now() - t1) * 10) / 10;
      recMsTotal += rec.recMs;
      recRssKb = Math.max(recRssKb, snap.rssKb);
      recCalls++;
      const units = snap.result.units;
      const ref = await locate(ctx.page, s).elementHandle({ timeout: 4000 }).catch(() => null);
      if (ref) {
        const at = (await unitsAt(ctx.page, units, ref)).filter((u) => u.type !== "collection");
        rec.unitAtControl = at.length > 0;
        const u = at[0];
        if (u) {
          rec.unitKind = u.kind ?? u.type;
          rec.usable = u.usable;
          rec.verbs = u.verbs;
          rec.structural = u.structural;
          rec.nameBound = u.binding?.text ?? u.name;
        }
      }
      const r = resolve(s as never, units, snap.result.composites ?? [], undefined);
      if (r.unit && process.env.AUDIT_CONTROL === "shuffle") {
        const others = units.filter((o) => o.type === r.unit?.type && o.usable && o.node !== r.unit?.node);
        if (others.length) r.unit = others[(r.unit.node * 7) % others.length] as RecUnit;
      }
      if (!r.unit) {
        rec.resolution = (r.why as Resolution) ?? "no_unit";
        stoppedAt = i;
        cause = rec.unitAtControl ? (rec.usable === false ? "unit_withheld" : rec.resolution === "ambiguous" ? "ambiguous_name" : "name_binding_differs") : rec.resolution === "ambiguous" ? "ambiguous_name" : "no_unit_at_control";
        break;
      }
      const el = await elementFor(ctx.page, r.unit.node);
      if (!el) {
        rec.resolution = "element_gone";
        stoppedAt = i;
        cause = "element_gone";
        break;
      }
      if (ref) {
        const same = await el.evaluate((x, y) => x === y || x.contains(y) || y.contains(x), ref);
        if (!same) {
          rec.resolution = "misbound";
          misbound = true;
          stoppedAt = i;
          cause = "misbound";
          break;
        }
      }
      try {
        if (process.env.AUDIT_CONTROL !== "null") await act(ctx.page, el, s, ctx, (e, v) => vsfPopup(ctx.page, h as Harness, r.unit as RecUnit, e, v));
        rec.resolution = "ok";
      } catch (e) {
        rec.resolution = "act_error";
        rec.note = String((e as Error).message).split("\n")[0] ?? "";
        stoppedAt = i;
        cause = "act_error";
        break;
      }
    }
    const stepsMs = Date.now() - tSteps;
    let hybridDone = stoppedAt === undefined;
    if (stoppedAt !== undefined && !misbound) {
      for (const [i, s] of wf.steps.entries()) {
        if (i < stoppedAt) continue;
        try {
          await refStep(ctx.page, s, ctx);
        } catch {
          break;
        }
        if (i === wf.steps.length - 1) hybridDone = true;
      }
    }
    await ctx.page.waitForLoadState("load", { timeout: 6000 }).catch(() => {});
    await ctx.page.waitForTimeout(400);
    const post = oracle ? await oracle(ctx).catch((e: Error) => ({ ok: false, detail: `oracle error: ${String(e.message).split("\n")[0]}` })) : { ok: false, detail: "no oracle" };
    let outcome: Outcome;
    if (misbound) outcome = "unsafe_misbound";
    else if (stoppedAt === undefined) outcome = post.ok ? "native_path" : "incorrect_effect";
    else if (post.ok && hybridDone) outcome = stoppedAt === 0 ? "preserved_source" : "hybrid";
    else outcome = "unsupported";
    return { id: wf.id, app: wf.app, outcome, ...(stoppedAt !== undefined ? { stoppedAt } : {}), ...(cause ? { cause } : {}), steps, oracle: post, ms: Date.now() - t0, recMsTotal: Math.round(recMsTotal), recCalls, stepsMs, recRssKb };
  } finally {
    await live.close();
  }
}

export type { Ctx, Step };
export type { RecComposite };

export interface ObserveStep extends StepRec {
  /** What the frozen B11 resolver would pick for this step on this page state, judged against the control the reference driver acted on. */
  wouldResolve: "correct" | "misbound" | "none";
  committing: boolean;
}

export interface ObserveResult {
  id: string;
  app: string;
  steps: ObserveStep[];
  oracle: { ok: boolean; detail: string };
  recMsTotal: number;
  recCalls: number;
  recMsMax: number;
  recRssKb: number;
}

/** Drives the whole workflow with the reference driver while the observer and recognizer look at every state; VSF never acts. */
export async function runObserve(wf: Workflow, browser: Awaited<ReturnType<typeof launch>>): Promise<ObserveResult> {
  let h: Harness | undefined;
  const live = await open(browser, wf, async (page, origin) => {
    h = await attach(page, { allow: [origin] });
  });
  const { ctx } = live;
  const steps: ObserveStep[] = [];
  let recMsTotal = 0;
  let recCalls = 0;
  let recMsMax = 0;
  let recRssKb = 0;
  try {
    await ctx.page.goto(`${ctx.origin}${wf.start}`, { waitUntil: "load" });
    await ctx.page.waitForLoadState("networkidle", { timeout: 6000 }).catch(() => {});
    if (wf.settle) await ctx.page.waitForTimeout(wf.settle);
    const lastClick = wf.steps.map((s) => s.do).lastIndexOf("click");
    for (const [i, s] of wf.steps.entries()) {
      const rec: ObserveStep = { i, do: s.do, target: s.target ?? s.key ?? s.text ?? "", resolution: "passthrough", unitAtControl: false, recMs: 0, wouldResolve: "none", committing: i === lastClick && s.do === "click" };
      steps.push(rec);
      if (s.do !== "press" && s.do !== "type") {
        await (h as Harness).settle();
        const t1 = performance.now();
        const snap = recognizeSnapshot((h as Harness).collector.replica.toRecognizerInput(), `${wf.id}-obs-${i}`, TMP);
        rec.recMs = Math.round((performance.now() - t1) * 10) / 10;
        recMsTotal += rec.recMs;
        recMsMax = Math.max(recMsMax, rec.recMs);
        recRssKb = Math.max(recRssKb, snap.rssKb);
        recCalls++;
        const ref = await locate(ctx.page, s).elementHandle({ timeout: 4000 }).catch(() => null);
        if (ref) {
          const at = (await unitsAt(ctx.page, snap.result.units, ref)).filter((u) => u.type !== "collection");
          rec.unitAtControl = at.length > 0;
          const u = at[0];
          if (u) {
            rec.unitKind = u.kind ?? u.type;
            rec.usable = u.usable;
            rec.verbs = u.verbs;
            rec.structural = u.structural;
            rec.nameBound = u.binding?.text ?? u.name;
          }
          const r = resolve(s as never, snap.result.units, snap.result.composites ?? [], undefined);
          rec.resolution = (r.why as Resolution) ?? "ok";
          if (r.unit) {
            const el = await elementFor(ctx.page, r.unit.node);
            const same = el ? await el.evaluate((x, y) => x === y || x.contains(y) || y.contains(x), ref) : false;
            rec.wouldResolve = same ? "correct" : "misbound";
          }
        }
      }
      await refStep(ctx.page, s, ctx).catch(() => {});
    }
    await ctx.page.waitForLoadState("load", { timeout: 6000 }).catch(() => {});
    await ctx.page.waitForTimeout(400);
    const post = ORACLES[wf.oracle] ? await (ORACLES[wf.oracle] as NonNullable<(typeof ORACLES)[string]>)(ctx).catch((e: Error) => ({ ok: false, detail: `oracle error: ${String(e.message).split("\n")[0]}` })) : { ok: false, detail: "no oracle" };
    return { id: wf.id, app: wf.app, steps, oracle: post, recMsTotal: Math.round(recMsTotal), recCalls, recMsMax, recRssKb };
  } finally {
    await live.close();
  }
}
