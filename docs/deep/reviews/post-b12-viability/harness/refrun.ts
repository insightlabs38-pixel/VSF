import { appendFileSync, mkdirSync } from "node:fs";
import { dirname } from "node:path";
import { ORACLES } from "./oracles.ts";
import { refStep } from "./ref.ts";
import { launch, open } from "./session.ts";
import type { Workflow } from "./spec.ts";
import { CALIBRATION, WORKFLOWS } from "./workflows.ts";

export interface RefResult {
  id: string;
  app: string;
  status: "confirmed" | "unconfirmed" | "vacuous";
  detail: string;
  stepFailed?: number;
  ms: number;
  /** Milliseconds from the first step to the last, application start-up and oracle excluded. */
  stepsMs?: number;
}

export async function runReference(wf: Workflow, browser: Awaited<ReturnType<typeof launch>>): Promise<RefResult> {
  const t0 = Date.now();
  const live = await open(browser, wf);
  const { ctx } = live;
  const oracle = ORACLES[wf.oracle];
  try {
    if (!oracle) return { id: wf.id, app: wf.app, status: "unconfirmed", detail: `no oracle ${wf.oracle}`, ms: 0 };
    await ctx.page.goto(`${ctx.origin}${wf.start}`, { waitUntil: "load" });
    await ctx.page.waitForLoadState("networkidle", { timeout: 6000 }).catch(() => {});
    if (wf.settle) await ctx.page.waitForTimeout(wf.settle);
    const pre = await oracle(ctx).catch(() => ({ ok: false, detail: "pre-state oracle error" }));
    if (pre.ok) return { id: wf.id, app: wf.app, status: "vacuous", detail: `oracle already holds: ${pre.detail}`, ms: Date.now() - t0 };
    const tSteps = Date.now();
    for (const [i, s] of wf.steps.entries()) {
      try {
        await refStep(ctx.page, s, ctx);
      } catch (e) {
        return { id: wf.id, app: wf.app, status: "unconfirmed", detail: `step ${i + 1} (${s.do} ${s.target ?? s.sel ?? s.key ?? ""}): ${String((e as Error).message).split("\n")[0]}`, stepFailed: i + 1, ms: Date.now() - t0 };
      }
    }
    const stepsMs = Date.now() - tSteps;
    await ctx.page.waitForLoadState("load", { timeout: 6000 }).catch(() => {});
    await ctx.page.waitForTimeout(400);
    const post = await oracle(ctx).catch((e: Error) => ({ ok: false, detail: `oracle error: ${String(e.message).split("\n")[0]}` }));
    return { id: wf.id, app: wf.app, status: post.ok ? "confirmed" : "unconfirmed", detail: post.detail, ms: Date.now() - t0, stepsMs };
  } finally {
    await live.close();
  }
}

if (process.argv[1]?.endsWith("refrun.ts")) {
  const want = process.argv.slice(2).filter((a) => !a.startsWith("--"));
  const out = process.argv.find((a) => a.startsWith("--out="))?.slice(6);
  const picked = (process.argv.includes("--calib") ? CALIBRATION : WORKFLOWS).filter((w) => want.length === 0 || want.some((x) => w.id === x || w.app === x));
  const browser = await launch();
  for (const wf of picked) {
    const r = await runReference(wf, browser);
    console.log(`${r.status.padEnd(11)} ${wf.id.padEnd(12)} ${r.ms}ms ${r.detail}`);
    if (out) {
      mkdirSync(dirname(out), { recursive: true });
      appendFileSync(out, `${JSON.stringify(r)}\n`);
    }
  }
  await browser.close();
}
