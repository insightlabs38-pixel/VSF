import { expect, test } from "@playwright/test";
import { measureSize } from "../support/overhead.ts";

test("collector overhead is measured for cold and steady state and stays inside regression guards", async () => {
  test.setTimeout(180_000);
  const r = await measureSize({ nodes: 3000, coldRuns: 3, cycles: 100, rounds: 3 });
  expect(r.nodes).toBeGreaterThan(2900);
  for (const d of [r.cold, r.warm, r.steadyFlush, r.steadyMutate, r.coldTransport]) {
    expect(d.n).toBeGreaterThan(0);
    expect(d.p50).toBeGreaterThanOrEqual(0);
    expect(d.p50).toBeLessThanOrEqual(d.p95);
    expect(d.p95).toBeLessThanOrEqual(d.max);
  }
  expect(r.baselineBytes).toBeGreaterThan(0);
  expect(r.baselineOps).toBeGreaterThanOrEqual(r.nodes - 1);
  expect(r.rssObserved).toBeGreaterThan(0);
  expect(r.peakRssObserved).toBeGreaterThanOrEqual(r.rssObserved);
  expect(r.cpuMsPerCycleObserved).toBeGreaterThan(r.cpuMsPerCycleControl);
  expect(r.steadyFlush.p95).toBeLessThan(10);
  expect(r.cpuMsPerCycleObserved).toBeLessThan(5);
  expect(r.cold.p95).toBeLessThan(3000);
  expect(r.heapGrowthBytes).toBeLessThan(2 * 1024 * 1024);
  expect(r.heapRounds).toHaveLength(3);
});
