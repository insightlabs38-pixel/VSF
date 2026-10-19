import { readFileSync } from "node:fs";
import { type Browser, type CDPSession, chromium, type Page } from "@playwright/test";
import type { Batch } from "@vsf/observer";
import { listPage } from "./pages.ts";
import { attach, type Harness, startWorld } from "./world.ts";

export interface Dist {
  n: number;
  p50: number;
  p95: number;
  max: number;
}

export function dist(values: number[]): Dist {
  const s = [...values].sort((a, b) => a - b);
  const at = (q: number) => s[Math.min(s.length - 1, Math.ceil(q * s.length) - 1)] as number;
  return { n: s.length, p50: at(0.5), p95: at(0.95), max: s[s.length - 1] as number };
}

interface ProcInfo {
  type: string;
  id: number;
  cpuTime: number;
}

async function rendererProcess(browser: Browser): Promise<ProcInfo | null> {
  const b = await browser.newBrowserCDPSession();
  const { processInfo } = (await b.send("SystemInfo.getProcessInfo")) as { processInfo: ProcInfo[] };
  await b.detach();
  const renderers = processInfo.filter((p) => p.type === "renderer");
  const rss = (p: ProcInfo) => procStatus(p.id).rss;
  return renderers.sort((x, y) => rss(y) - rss(x))[0] ?? null;
}

function procStatus(pid: number): { rss: number; hwm: number } {
  const text = readFileSync(`/proc/${pid}/status`, "utf8");
  const kb = (key: string) => Number(new RegExp(`${key}:\\s+(\\d+) kB`).exec(text)?.[1] ?? 0);
  return { rss: kb("VmRSS") * 1024, hwm: kb("VmHWM") * 1024 };
}

async function heapUsed(cdp: CDPSession): Promise<number> {
  await cdp.send("HeapProfiler.collectGarbage");
  const { metrics } = (await cdp.send("Performance.getMetrics")) as { metrics: { name: string; value: number }[] };
  return metrics.find((m) => m.name === "JSHeapUsedSize")?.value ?? 0;
}

async function metrics(cdp: CDPSession): Promise<Record<string, number>> {
  const { metrics } = (await cdp.send("Performance.getMetrics")) as { metrics: { name: string; value: number }[] };
  return Object.fromEntries(metrics.map((m) => [m.name, m.value]));
}

const WORK = `
window.__work = (cycles, observed) => {
  const list = document.getElementById("list");
  const rows = list.children.length;
  const flushTimes = [];
  const mutateTimes = [];
  const added = [];
  for (let i = 0; i < cycles; i++) {
    const t0 = performance.now();
    const row = list.children[(i * 7919) % rows];
    const span = row.querySelector(".n");
    if (span) span.textContent = "Row changed " + i;
    if (i % 3 === 0) {
      const li = document.createElement("li");
      li.textContent = "added " + i;
      list.append(li);
      added.push(li);
    }
    if (i % 5 === 0 && added.length > 0) added.shift().remove();
    const t1 = performance.now();
    if (observed) window.__vsf.session.flush();
    const t2 = performance.now();
    mutateTimes.push(t1 - t0);
    flushTimes.push(t2 - t1);
  }
  return { flushTimes, mutateTimes };
};`;

export interface SizeResult {
  nodes: number;
  baselineBytes: number;
  baselineOps: number;
  cold: Dist;
  warm: Dist;
  coldTransport: Dist;
  steadyFlush: Dist;
  steadyMutate: Dist;
  cpuSecondsObserved: number;
  cpuSecondsControl: number;
  cpuMsPerCycleObserved: number;
  cpuMsPerCycleControl: number;
  cycles: number;
  scriptSecondsObserved: number;
  scriptSecondsControl: number;
  rssObserved: number;
  rssControl: number;
  peakRssObserved: number;
  peakRssControl: number;
  heapObserved: number;
  heapControl: number;
  heapPerNode: number;
  heapRounds: number[];
  heapGrowthBytes: number;
}

export interface OverheadConfig {
  nodes: number;
  coldRuns: number;
  cycles: number;
  rounds: number;
}

async function oneBrowser<T>(fn: (browser: Browser) => Promise<T>): Promise<T> {
  const browser = await chromium.launch({ channel: "chromium", args: ["--enable-precise-memory-info"] });
  try {
    return await fn(browser);
  } finally {
    await browser.close();
  }
}

async function open(
  browser: Browser,
  rows: number,
  observed: boolean,
): Promise<{ page: Page; h: Harness | null; stop(): Promise<void> }> {
  const world = await startWorld();
  world.a.pages.set("/", listPage(rows));
  const ctx = await browser.newContext();
  const page = await ctx.newPage();
  const h = observed ? await attach(page, { allow: [world.a.url], batchMs: null }) : null;
  await page.addInitScript(WORK);
  await page.goto(`${world.a.url}/`);
  if (h) await h.settle();
  return {
    page,
    h,
    stop: async () => {
      await ctx.close();
      await world.stop();
    },
  };
}

export async function measureSize(cfg: OverheadConfig): Promise<SizeResult> {
  const rows = Math.max(1, Math.round(cfg.nodes / 3));
  const cold: number[] = [];
  const transport: number[] = [];
  const warm: number[] = [];
  let baselineBytes = 0;
  let baselineOps = 0;
  let nodes = 0;
  await oneBrowser(async (browser) => {
    for (let i = 0; i < cfg.coldRuns; i++) {
      const o = await open(browser, rows, true);
      const ms = await o.page.evaluate(() => (window as unknown as { __vsfColdMs: number }).__vsfColdMs);
      cold.push(ms);
      await o.page.waitForLoadState("load");
      for (let w = 0; w < 3; w++) {
        warm.push(
          await o.page.evaluate(() => {
            const t = performance.now();
            (window as unknown as { __vsfBegin(): void }).__vsfBegin();
            return performance.now() - t - ((window as unknown as { __vsfSendMs: number }).__vsfSendMs ?? 0);
          }),
        );
      }
      transport.push(await o.page.evaluate(() => (window as unknown as { __vsfSendMs: number }).__vsfSendMs));
      if (i === 0 && o.h) {
        const full = o.h.collector.batches.find((b: Batch) => b.kind === "full");
        baselineBytes = JSON.stringify(full).length;
        baselineOps = full?.ops.length ?? 0;
        nodes = o.h.collector.replica.size;
      }
      await o.stop();
    }
  });
  const run = async (observed: boolean) =>
    oneBrowser(async (browser) => {
      const o = await open(browser, rows, observed);
      const cdp = await o.page.context().newCDPSession(o.page);
      await cdp.send("Performance.enable");
      const proc = await rendererProcess(browser);
      const heap0 = await heapUsed(cdp);
      const rss0 = proc ? procStatus(proc.id) : { rss: 0, hwm: 0 };
      const m0 = await metrics(cdp);
      const cpu0 = (await rendererProcess(browser))?.cpuTime ?? 0;
      const first = await o.page.evaluate(
        ([n, ob]) =>
          (
            window as unknown as { __work(n: number, o: boolean): { flushTimes: number[]; mutateTimes: number[] } }
          ).__work(n as number, ob as boolean),
        [cfg.cycles, observed],
      );
      if (o.h) await o.h.drain();
      const m1 = await metrics(cdp);
      const cpu1 = (await rendererProcess(browser))?.cpuTime ?? 0;
      const rounds: number[] = [];
      for (let r = 0; r < cfg.rounds; r++) {
        await o.page.evaluate(
          ([n, ob]) =>
            (window as unknown as { __work(n: number, o: boolean): unknown }).__work(n as number, ob as boolean),
          [cfg.cycles, observed],
        );
        if (o.h) await o.h.drain();
        rounds.push(await heapUsed(cdp));
      }
      const rss1 = proc ? procStatus(proc.id) : { rss: 0, hwm: 0 };
      const out = {
        flush: first.flushTimes,
        mutate: first.mutateTimes,
        cpu: cpu1 - cpu0,
        script: (m1.ScriptDuration ?? 0) - (m0.ScriptDuration ?? 0),
        heap0,
        rss: rss1.rss,
        hwm: rss1.hwm,
        rss0: rss0.rss,
        rounds,
      };
      await o.stop();
      return out;
    });
  const control = await run(false);
  const observed = await run(true);
  const growth = (observed.rounds.at(-1) ?? 0) - (observed.rounds[0] ?? 0);
  return {
    nodes,
    baselineBytes,
    baselineOps,
    cold: dist(cold),
    warm: dist(warm),
    coldTransport: dist(transport),
    steadyFlush: dist(observed.flush),
    steadyMutate: dist(observed.mutate),
    cpuSecondsObserved: observed.cpu,
    cpuSecondsControl: control.cpu,
    cpuMsPerCycleObserved: (observed.cpu * 1000) / cfg.cycles,
    cpuMsPerCycleControl: (control.cpu * 1000) / cfg.cycles,
    cycles: cfg.cycles,
    scriptSecondsObserved: observed.script,
    scriptSecondsControl: control.script,
    rssObserved: observed.rss,
    rssControl: control.rss,
    peakRssObserved: observed.hwm,
    peakRssControl: control.hwm,
    heapObserved: observed.heap0,
    heapControl: control.heap0,
    heapPerNode: nodes > 0 ? (observed.heap0 - control.heap0) / nodes : 0,
    heapRounds: observed.rounds,
    heapGrowthBytes: growth,
  };
}
