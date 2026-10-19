import { expect, test } from "@playwright/test";
import type { CanonNode } from "@vsf/observer";
import { framePage, framesPage, mpaPage, spaPage } from "../support/pages.ts";
import { normalise, runScenario } from "../support/runner.ts";
import { attach, startWorld } from "../support/world.ts";

const frameLoad = (page: import("@playwright/test").Page, id: string, attr: "src" | "srcdoc", value: string) =>
  page.evaluate(
    ([i, a, v]) =>
      new Promise<void>((resolve) => {
        const f = document.getElementById(i as string) as HTMLIFrameElement;
        f.addEventListener("load", () => resolve(), { once: true });
        f.setAttribute(a as string, v as string);
      }),
    [id, attr, value],
  );

const flat = (nodes: CanonNode[]): CanonNode[] => nodes.flatMap((n) => [n, ...flat(n[7])]);

async function setup(page: import("@playwright/test").Page) {
  const world = await startWorld();
  for (const p of ["/spa", "/spa/orders", "/spa/customers"]) world.a.pages.set(p, spaPage);
  for (const n of [1, 2, 3, 4]) world.a.pages.set(`/mpa/${n}`, mpaPage(n));
  world.a.pages.set("/frames", framesPage(world.b.url));
  world.a.pages.set("/frames/a", framePage("A"));
  world.a.pages.set("/frames/b", framePage("B"));
  world.b.pages.set("/frames/x", framePage("X"));
  world.b.pages.set("/frames/y", framePage("Y"));
  const h = await attach(page, { allow: [world.a.url, world.b.url] });
  return { world, h };
}

async function spaScenario(page: import("@playwright/test").Page) {
  const { world, h } = await setup(page);
  const steps = [
    { name: "load", run: async () => void (await page.goto(`${world.a.url}/spa`)) },
    { name: "to orders", run: () => page.click("#go-orders") },
    { name: "to customers", run: () => page.click("#go-customers") },
    { name: "back", run: async () => void (await page.goBack()) },
    { name: "forward", run: async () => void (await page.goForward()) },
    { name: "hash", run: () => page.evaluate(() => void (location.hash = "#section")) },
  ];
  const results = await runScenario(page, h, steps);
  const stats = await page.evaluate(
    () => (window as unknown as { __vsf: { session: { stats: { fullSyncs: number } } } }).__vsf.session.stats.fullSyncs,
  );
  await world.stop();
  return { results, stats };
}

test("single-page navigation keeps one epoch and publishes the new route", async ({ page }) => {
  const { results, stats } = await spaScenario(page);
  expect(results.every((r) => r.equalsPage && r.state === "synced")).toBe(true);
  expect(new Set(results.map((r) => r.epoch)).size).toBe(1);
  expect(stats).toBe(1);
  expect(results.map((r) => new URL(r.url ?? "http://x").pathname)).toEqual([
    "/spa",
    "/spa/orders",
    "/spa/customers",
    "/spa/orders",
    "/spa/customers",
    "/spa/customers",
  ]);
  expect(new Set(results.map((r) => r.nodes)).size).toBeGreaterThan(1);
});

test("traditional navigation and reloads start a new, larger epoch every time", async ({ page }) => {
  const { world, h } = await setup(page);
  const steps = [
    { name: "load 1", run: async () => void (await page.goto(`${world.a.url}/mpa/1`)) },
    { name: "next", run: () => page.click("#next") },
    { name: "next again", run: () => page.click("#next") },
    { name: "reload", run: async () => void (await page.reload()) },
    { name: "back", run: async () => void (await page.goBack()) },
    {
      name: "cleared storage, reload",
      run: async () => {
        await page.evaluate(() => sessionStorage.clear());
        await page.reload();
      },
    },
    { name: "other origin", run: async () => void (await page.goto(`${world.b.url}/frames/x`)) },
  ];
  const results = await runScenario(page, h, steps);
  expect(results.every((r) => r.equalsPage && r.state === "synced")).toBe(true);
  const epochs = results.map((r) => r.epoch);
  expect(epochs.slice(0, 4)).toEqual([...epochs.slice(0, 4)].sort((a, b) => a - b));
  expect(new Set(epochs.slice(0, 3)).size).toBe(3);
  for (let i = 1; i < epochs.length; i++)
    expect(epochs[i] as number, results[i]?.name).toBeGreaterThan(epochs[i - 1] as number);
  await world.stop();
});

test("document replacement inside one page is a new epoch even though nothing navigated", async ({ page }) => {
  const { world, h } = await setup(page);
  const steps = [
    { name: "load", run: async () => void (await page.goto(`${world.a.url}/mpa/1`)) },
    { name: "mutate", run: () => page.evaluate(() => void document.body.append(document.createElement("hr"))) },
    {
      name: "replace",
      run: () =>
        page.evaluate(() => {
          document.open();
          document.write("<!doctype html><html lang='en'><body><p>replaced document</p></body></html>");
          document.close();
        }),
    },
    { name: "mutate again", run: () => page.evaluate(() => void document.body.append(document.createElement("hr"))) },
  ];
  const r = await runScenario(page, h, steps);
  expect(r.every((x) => x.equalsPage && x.state === "synced")).toBe(true);
  expect(r[1]?.epoch).toBe(r[0]?.epoch);
  expect(r[2]?.epoch).toBeGreaterThan(r[1]?.epoch as number);
  expect(r[3]?.epoch).toBe(r[2]?.epoch);
  await world.stop();
});

test("iframe transitions change the frame's generation and never the page's epoch", async ({ page }) => {
  const { world, h } = await setup(page);
  const frameDoc = () => flat(h.collector.replica.canonical()).filter((n) => n[6]?.kind === "frame");
  const steps = [
    { name: "load", run: async () => void (await page.goto(`${world.a.url}/frames`)) },
    {
      name: "same-origin frame navigates",
      run: () => page.evaluate(() => void ((document.getElementById("f1") as HTMLIFrameElement).src = "/frames/b")),
    },
    {
      name: "cross-origin frame navigates",
      run: () =>
        page.evaluate(
          (u) => void ((document.getElementById("f2") as HTMLIFrameElement).src = `${u}/frames/y`),
          world.b.url,
        ),
    },
    { name: "frame removed", run: () => page.evaluate(() => void document.getElementById("f1")?.remove()) },
    {
      name: "frame added",
      run: () =>
        page.evaluate(
          () =>
            new Promise<void>((resolve) => {
              const f = document.createElement("iframe");
              f.id = "f3";
              f.addEventListener("load", () => resolve(), { once: true });
              f.src = "/frames/a";
              document.body.append(f);
            }),
        ),
    },
    {
      name: "frame srcdoc replaced",
      run: () =>
        page.evaluate(() => void ((document.getElementById("f3") as HTMLIFrameElement).srcdoc = "<p>from srcdoc</p>")),
    },
  ];
  const seen: string[] = [];
  const results = [];
  for (const s of steps) {
    const part = await runScenario(page, h, [s]);
    results.push(...part);
    seen.push(
      JSON.stringify(
        frameDoc().map((n) => [n[6]?.opaque ?? "open", n[6]?.origin?.replace(/:\d+$/, ""), n[7].length > 0]),
      ),
    );
  }
  expect(results.every((r) => r.equalsPage && r.state === "synced")).toBe(true);
  expect(new Set(results.map((r) => r.epoch)).size).toBe(1);
  const texts = flat(h.collector.replica.canonical()).map((n) => n[3]);
  expect(texts).toContain("from srcdoc");
  expect(texts).not.toContain("frame A");
  expect(seen[0]).toContain("cross_origin_frame");
  expect(seen[3]).not.toBe(seen[2]);
  const gens = [...h.collector.replica.nodes.values()]
    .filter((n) => n.scope?.kind === "frame" && !n.scope.opaque)
    .map((n) => n.scope?.generation ?? 0);
  expect(Math.max(...gens)).toBeGreaterThanOrEqual(2);
  await world.stop();
});

test("the same scenarios give the same traces on every run", async ({ browser }) => {
  const traces: string[] = [];
  for (let run = 0; run < 3; run++) {
    const ctx = await browser.newContext();
    const page = await ctx.newPage();
    const { results } = await spaScenario(page);
    const { world, h } = await setup(await ctx.newPage());
    await world.stop();
    void h;
    traces.push(JSON.stringify(normalise(results)));
    await ctx.close();
  }
  expect(new Set(traces).size).toBe(1);
});
