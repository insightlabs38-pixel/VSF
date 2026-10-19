import { mkdirSync, writeFileSync } from "node:fs";
import { dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { measureSize, type SizeResult } from "./support/overhead.ts";

const out = fileURLToPath(new URL("../../artifacts/observation/overhead.json", import.meta.url));
const sizes = (process.env.VSF_BENCH_SIZES ?? "1000,10000,50000").split(",").map(Number);
const cycles = Number(process.env.VSF_BENCH_CYCLES ?? 300);
const mib = (b: number) => (b / 1048576).toFixed(0);

const results: SizeResult[] = [];
for (const nodes of sizes) {
  const r = await measureSize({ nodes, coldRuns: nodes >= 50000 ? 5 : 12, cycles, rounds: 4 });
  results.push(r);
  console.log(
    `${r.nodes} nodes | baseline ${(r.baselineBytes / 1024).toFixed(0)} KiB, ${r.baselineOps} ops | cold (first layout included) p50 ${r.cold.p50.toFixed(1)} p95 ${r.cold.p95.toFixed(1)} ms, warm p50 ${r.warm.p50.toFixed(1)} p95 ${r.warm.p95.toFixed(1)} ms, hand-off p50 ${r.coldTransport.p50.toFixed(1)} ms | ` +
      `steady flush p50 ${r.steadyFlush.p50.toFixed(2)} p95 ${r.steadyFlush.p95.toFixed(2)} ms | cpu/cycle ${r.cpuMsPerCycleObserved.toFixed(2)} vs ${r.cpuMsPerCycleControl.toFixed(2)} ms | ` +
      `rss ${mib(r.rssObserved)} vs ${mib(r.rssControl)} MiB, peak ${mib(r.peakRssObserved)} vs ${mib(r.peakRssControl)} | heap/node ${r.heapPerNode.toFixed(0)} B | growth ${r.heapGrowthBytes} B`,
  );
}
mkdirSync(dirname(out), { recursive: true });
writeFileSync(out, `${JSON.stringify({ schema: "vsf-observation-overhead-v1", cycles, results }, null, 2)}\n`);
console.log(`wrote ${out}`);
