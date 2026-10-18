import { expect, test } from "@playwright/test";
import type { CanonNode } from "@vsf/observer";
import { innerPage, SECRET_INNER, scopesPage } from "../support/pages.ts";
import { attach, startWorld } from "../support/world.ts";

async function open(
  allow: (w: Awaited<ReturnType<typeof startWorld>>) => string[],
  opts: Record<string, unknown> = {},
) {
  const world = await startWorld();
  world.a.pages.set("/", scopesPage(world.b.url));
  world.a.pages.set("/inner", innerPage);
  world.a.pages.set("/other", "<!doctype html><html lang='en'><body><h2>other page</h2></body></html>");
  world.b.pages.set("/inner", innerPage);
  return { world, allow: allow(world), opts };
}

const flat = (nodes: CanonNode[]): CanonNode[] => nodes.flatMap((n) => [n, ...flat(n[7])]);

test("every document, frame and shadow root is enumerated and inaccessible ones are placeholders", async ({ page }) => {
  const { world, allow } = await open((w) => [w.a.url]);
  await attach(page, { allow });
  await page.goto(`${world.a.url}/`);
  await page.waitForLoadState("load");
  const scopes = await page.evaluate(() => {
    const O = (window as unknown as { VSFObserver: typeof import("@vsf/observer") }).VSFObserver;
    return O.enumerateScopes(document, {}, { allowOrigin: () => true });
  });
  const byReason = (r: string) => scopes.filter((s) => s.reason === r);
  expect(scopes[0]).toMatchObject({ kind: "document", access: "accessible", parent: -1 });
  expect(scopes.filter((s) => s.kind === "shadow" && s.access === "accessible")).toHaveLength(2);
  expect(scopes.filter((s) => s.kind === "frame" && s.access === "accessible")).toHaveLength(2);
  expect(byReason("cross_origin_frame")).toMatchObject([{ origin: world.b.url, hostTag: "iframe" }]);
  expect(byReason("sandboxed_frame")).toHaveLength(1);
  expect(byReason("canvas_content")).toHaveLength(1);
  expect(byReason("native_widget")).toHaveLength(1);
  const nested = scopes.find((s) => s.kind === "shadow" && s.depth === 2);
  expect(nested).toBeDefined();
  await world.stop();
});

test("the baseline carries opaque placeholders with their origin and never the inaccessible content", async ({
  page,
}) => {
  const { world, allow } = await open((w) => [w.a.url]);
  const h = await attach(page, { allow });
  await page.goto(`${world.a.url}/`);
  await page.waitForLoadState("load");
  await h.settle();
  const r = h.collector.replica;
  expect(r.state).toBe("synced");
  const nodes = flat(r.canonical());
  const cross = nodes.find((n) => n[6]?.opaque === "cross_origin_frame");
  expect(cross).toBeDefined();
  expect(cross?.[6]?.origin).toBe(world.b.url);
  expect(cross?.[7]).toEqual([]);
  expect(cross?.[0]).toBe("#opaque");
  expect((cross?.[4] ?? 0) & 32).toBe(32);
  const wire = JSON.stringify(h.collector.batches);
  const crossFrames = nodes.filter((n) => n[6]?.kind === "frame" && n[6].origin === world.b.url && !n[6].opaque);
  expect(crossFrames).toHaveLength(0);
  expect(wire.split(SECRET_INNER).length - 1).toBe(1);
  expect(nodes.filter((n) => n[3] === SECRET_INNER)).toHaveLength(1);
  await world.stop();
});

test("open shadow roots are entered at any depth and closed ones only when the runtime can see them", async ({
  page,
}) => {
  const { world, allow } = await open((w) => [w.a.url]);
  const h = await attach(page, { allow });
  await page.goto(`${world.a.url}/`);
  await page.waitForLoadState("load");
  await h.settle();
  let texts = flat(h.collector.replica.canonical()).map((n) => n[2]);
  expect(texts).toContain("deep button");
  expect(texts).not.toContain("closed content");
  const stats = await page.evaluate(
    () =>
      (window as unknown as { __vsf: { session: { stats: { scopes: { possiblyClosed: number } } } } }).__vsf.session
        .stats.scopes,
  );
  expect(stats.possiblyClosed).toBeGreaterThan(0);
  await world.stop();

  const page2 = await page.context().newPage();
  const { world: w2, allow: allow2 } = await open((w) => [w.a.url]);
  const h2 = await attach(page2, { allow: allow2, closedShadowAccess: true });
  await page2.goto(`${w2.a.url}/`);
  await page2.waitForLoadState("load");
  await h2.settle();
  const nodes = flat(h2.collector.replica.canonical());
  texts = nodes.map((n) => n[2]);
  expect(texts).toContain("closed content");
  expect(nodes.some((n) => n[6]?.kind === "shadow" && n[6].mode === "closed")).toBe(true);
  await w2.stop();
});

test("the runtime advertises what it cannot see and the baseline reports the matching gaps", async ({ page }) => {
  const { world, allow } = await open((w) => [w.a.url]);
  const h = await attach(page, { allow });
  await page.goto(`${world.a.url}/`);
  await page.waitForLoadState("load");
  await h.settle();
  const report = await page.evaluate(() => {
    const O = (window as unknown as { VSFObserver: typeof import("@vsf/observer") }).VSFObserver;
    return O.captureCapabilities({}, window);
  });
  const state = (id: string) => report.find((c) => c.id === id)?.state;
  expect(state("observe.frames.cross_origin")).toBe("unavailable");
  expect(state("observe.shadow.closed")).toBe("unavailable");
  expect(state("observe.shadow.open")).toBe("available");
  expect(report.every((c) => /^[a-z][a-z0-9_.-]{0,95}$/.test(c.id))).toBe(true);
  const missing = await page.evaluate(() => {
    const w = window as unknown as {
      VSFObserver: typeof import("@vsf/observer");
      __vsf: { session: { stats: { scopes: { possiblyClosed: number } } } };
    };
    return { stats: w.__vsf.session.stats.scopes };
  });
  const { missingSignals } = await import("@vsf/observer");
  const gaps = missingSignals(h.collector.replica, missing.stats, {});
  const sig = (s: string, r: string) => gaps.some((g) => g.signal === s && g.reason === r);
  expect(sig("cross_origin_frame", "not_permitted")).toBe(true);
  expect(sig("closed_shadow_root", "unknown")).toBe(true);
  expect(sig("canvas_content", "not_supported")).toBe(true);
  expect(sig("native_widget", "not_supported")).toBe(true);
  expect(sig("accessibility_tree", "not_supported")).toBe(true);
  await world.stop();
});

test("an origin without a grant is refused before any content is read", async ({ page }) => {
  const { world, allow } = await open((w) => [w.b.url]);
  const h = await attach(page, { allow });
  await page.goto(`${world.a.url}/`);
  await page.waitForLoadState("load");
  expect(await page.evaluate(() => (window as unknown as { __vsfError?: string }).__vsfError)).toBe(
    "origin_not_granted",
  );
  expect(h.collector.batches).toHaveLength(0);
  await world.stop();
});

test("scope depth and count limits turn the excess into explicit placeholders", async ({ page }) => {
  const { world, allow } = await open((w) => [w.a.url]);
  const h = await attach(page, { allow, limits: { maxScopeDepth: 1 } });
  await page.goto(`${world.a.url}/`);
  await page.waitForLoadState("load");
  await h.settle();
  const nodes = flat(h.collector.replica.canonical());
  expect(nodes.filter((n) => n[6]?.opaque === "limit_exceeded").length).toBeGreaterThan(0);
  expect(nodes.map((n) => n[2])).not.toContain("deep button");
  await world.stop();
});

test("a frame that navigates is replaced in place under a new generation", async ({ page }) => {
  const { world, allow } = await open((w) => [w.a.url]);
  const h = await attach(page, { allow });
  await page.goto(`${world.a.url}/`);
  await page.waitForLoadState("load");
  await h.settle();
  const before = flat(h.collector.replica.canonical());
  expect(before.map((n) => n[3])).toContain(SECRET_INNER);
  const epoch = h.collector.replica.epoch;
  await page.evaluate(() => {
    (document.getElementById("same-src") as HTMLIFrameElement).src = "/other";
  });
  await page.waitForFunction(() =>
    (document.getElementById("same-src") as HTMLIFrameElement).contentDocument?.querySelector("h2"),
  );
  await h.settle();
  const after = flat(h.collector.replica.canonical());
  expect(after.map((n) => n[3])).not.toContain(SECRET_INNER);
  expect(after.map((n) => n[3])).toContain("other page");
  expect(h.collector.replica.epoch).toBe(epoch);
  await world.stop();
});
