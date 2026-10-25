import { spawnSync } from "node:child_process";
import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { appendFileSync, mkdirSync } from "node:fs";
import { dirname } from "node:path";
import { attach } from "../../../../../tests/observation/support/world.ts";
import { ROOT } from "./apps.ts";
import { launch, open } from "./session.ts";
import type { Workflow } from "./spec.ts";
import { WORKFLOWS } from "./workflows.ts";

const REC = `${ROOT}compiler/target/release/vsf-recognizer`;
const FAB = `${ROOT}artifacts/bin/vsf-fabric`;
const TMP = mkdtempSync(`${tmpdir()}/vsf-perf-`);
const pct = (xs: number[], p: number): number => [...xs].sort((a, b) => a - b)[Math.min(xs.length - 1, Math.floor((p / 100) * xs.length))] as number;

const PROBE = "import json,resource,subprocess,sys,time;n=int(sys.argv[1]);cmd=sys.argv[2:];ms=[]\nfor _ in range(n):\n t=time.perf_counter();subprocess.run(cmd,capture_output=True);ms.append((time.perf_counter()-t)*1000)\nms.sort();print(json.dumps({'p50':round(ms[len(ms)//2],1),'p95':round(ms[min(len(ms)-1,int(len(ms)*0.95))],1),'rssKb':resource.getrusage(resource.RUSAGE_CHILDREN).ru_maxrss}))";

function timed(bin: string, args: string[], n: number): { p50: number; p95: number; rssKb: number } {
  const r = spawnSync("python3", ["-I", "-c", PROBE, String(n), bin, ...args], { encoding: "utf8", maxBuffer: 256 << 20 });
  return JSON.parse(r.stdout) as { p50: number; p95: number; rssKb: number };
}

const out = process.argv.find((a) => a.startsWith("--out="))?.slice(6);
const only = process.argv.slice(2).filter((a) => !a.startsWith("--"));
const first = new Map<string, Workflow>();
for (const w of WORKFLOWS) if (!first.has(w.variant ?? w.app) && (only.length === 0 || only.includes(w.app))) first.set(w.variant ?? w.app, w);
const browser = await launch();
for (const [name, wf] of first) {
  const row: Record<string, unknown> = { page: name, start: wf.start };
  for (const mode of ["plain", "observed"] as const) {
    const live = await open(browser, wf, mode === "observed" ? async (page, origin) => void (await attach(page, { allow: [origin] })) : undefined);
    try {
      const loads: number[] = [];
      for (let i = 0; i < 3; i++) {
        const t = performance.now();
        await live.ctx.page.goto(`${live.ctx.origin}${wf.start}`, { waitUntil: "load" });
        await live.ctx.page.waitForLoadState("networkidle", { timeout: 6000 }).catch(() => {});
        loads.push(performance.now() - t);
      }
      row[`${mode}LoadMs`] = Math.round(pct(loads, 50));
      if (mode === "observed") {
        await live.ctx.page.waitForTimeout(wf.app === "jupyterlab" ? 3500 : (wf.settle ?? 800));
        row.coldObserveMs = await live.ctx.page.evaluate(() => Math.round((window as unknown as { __vsfColdMs: number }).__vsfColdMs * 10) / 10);
        const h = await attach(await live.context.newPage(), { allow: [live.ctx.origin] });
        await h.page.goto(`${live.ctx.origin}${wf.start}`, { waitUntil: "load" });
        await h.page.waitForTimeout(wf.app === "jupyterlab" ? 3500 : (wf.settle ?? 800));
        await h.settle();
        const snap = h.collector.replica.toRecognizerInput();
        row.nodes = (snap.nodes as unknown[]).length;
        const f = `${TMP}/${name}.json`;
        writeFileSync(f, JSON.stringify(snap));
        row.recognizerRules3 = timed(REC, ["recognize", f, "--rules", "3"], 15);
        row.assemble = timed(FAB, ["assemble", f, "--origin", live.ctx.origin, "--variant", `var_${"0".repeat(32)}`, "--epoch", "1", "--observed-at", "1700000000000"], 15);
        const heapBefore = await h.page.evaluate(() => (performance as unknown as { memory?: { usedJSHeapSize: number } }).memory?.usedJSHeapSize ?? 0);
        row.pageHeapBytesObserved = heapBefore;
      } else {
        row.plainHeapBytes = await live.ctx.page.evaluate(() => (performance as unknown as { memory?: { usedJSHeapSize: number } }).memory?.usedJSHeapSize ?? 0);
      }
    } finally {
      await live.close();
    }
  }
  console.log(JSON.stringify(row));
  if (out) {
    mkdirSync(dirname(out), { recursive: true });
    appendFileSync(out, `${JSON.stringify(row)}\n`);
  }
}
await browser.close();
