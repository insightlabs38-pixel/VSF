import { chromium } from "@playwright/test";
import { listPage } from "./support/pages.ts";
import { attach, startWorld } from "./support/world.ts";

interface ProfileNode {
  id: number;
  callFrame: { functionName: string; url: string; lineNumber: number };
}

const rows = Math.round(Number(process.env.VSF_PROFILE_NODES ?? 10000) / 3);
const world = await startWorld();
world.a.pages.set("/", listPage(rows));
const browser = await chromium.launch({ channel: "chromium" });
const ctx = await browser.newContext();
const page = await ctx.newPage();
await attach(page, { allow: [world.a.url], batchMs: null });
await page.goto(`${world.a.url}/`);
const cdp = await ctx.newCDPSession(page);
await cdp.send("Profiler.enable");
await cdp.send("Profiler.setSamplingInterval", { interval: 100 });
await cdp.send("Profiler.start");
const ms = await page.evaluate(() => {
  const t = performance.now();
  (window as unknown as { __vsfBegin(): void }).__vsfBegin();
  return performance.now() - t;
});
const { profile } = (await cdp.send("Profiler.stop")) as {
  profile: { nodes: ProfileNode[]; samples: number[]; timeDeltas: number[] };
};
const self = new Map<number, number>();
profile.samples.forEach((id, i) => self.set(id, (self.get(id) ?? 0) + (profile.timeDeltas[i] ?? 0)));
const byName = new Map<string, number>();
for (const n of profile.nodes) {
  const key = `${n.callFrame.functionName || "(anon)"} ${n.callFrame.url.split("/").pop()}:${n.callFrame.lineNumber}`;
  byName.set(key, (byName.get(key) ?? 0) + (self.get(n.id) ?? 0));
}
console.log(`start() took ${ms.toFixed(1)} ms for ${rows * 3} nodes`);
for (const [k, v] of [...byName.entries()].sort((a, b) => b[1] - a[1]).slice(0, 22))
  console.log(`${(v / 1000).toFixed(1).padStart(8)} ms  ${k}`);
await browser.close();
await world.stop();
