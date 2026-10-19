import { expect, test } from "@playwright/test";
import { decodeDocument, familyDigest, typedDigest } from "@vsf/ir";
import { encodePack, freshnessOf, type HostInfo, observationBody, REDACTION_PATTERNS } from "@vsf/observer";
import { parse, toCanonical } from "@vsf/schema";
import { innerPage, scopesPage } from "../support/pages.ts";
import { attach, startWorld } from "../support/world.ts";

const enc = new TextEncoder();

function documentBytes(body: Record<string, unknown>): Uint8Array {
  const doc = {
    schema: "vsf.observation/1.0.0",
    schema_digest: familyDigest("observation"),
    body,
  };
  return toCanonical(parse(enc.encode(JSON.stringify(doc))));
}

async function observe(page: import("@playwright/test").Page) {
  const world = await startWorld();
  world.a.pages.set("/", scopesPage(world.b.url));
  world.a.pages.set("/inner", innerPage);
  world.b.pages.set("/inner", innerPage);
  const h = await attach(page, { allow: [world.a.url] });
  await page.goto(`${world.a.url}/`);
  await page.waitForLoadState("load");
  await h.settle();
  const stats = await page.evaluate(
    () =>
      (window as unknown as { __vsf: { session: { stats: { scopes: { possiblyClosed: number } } } } }).__vsf.session
        .stats.scopes,
  );
  const pack = encodePack(h.collector.replica.toSnapshot());
  const host: HostInfo = {
    sourceAppId: "vendor-console:app.vendor.test",
    variantResolution: "unknown",
    runtimeId: "rt_ext01",
    principalRef: `prin_${"b2".repeat(16)}`,
    capturedAt: 1_792_368_100_000,
    staleAfterMs: 30_000,
    packDigest: await typedDigest("observation.pack", pack),
    packBytes: pack.length,
    patternsDigest: await typedDigest("redaction.patterns", enc.encode(JSON.stringify(REDACTION_PATTERNS))),
  };
  return { world, h, stats, host, pack };
}

test("an observation built from the replica is a valid ObservationIR with its gaps stated", async ({ page }) => {
  const { world, h, stats, host, pack } = await observe(page);
  const body = observationBody(h.collector.replica, stats, {}, host);
  const decoded = decodeDocument(documentBytes(body));
  expect(decoded.family).toBe("observation");
  expect(body.observation_epoch).toBe(h.collector.replica.epoch);
  expect(body.origin).toBe(world.a.url);
  expect(body.mode).toBe("full");
  expect((body.payload as { nodes: number; bytes: number }).nodes).toBe(h.collector.replica.size);
  expect((body.payload as { bytes: number }).bytes).toBe(pack.length);
  const scope = body.scope as { frames: number; documents: number; full_document: boolean };
  expect(scope.frames).toBeGreaterThanOrEqual(4);
  expect(scope.documents).toBeGreaterThanOrEqual(2);
  expect(scope.full_document).toBe(true);
  const missing = body.missing_signals as { signal: string; reason: string }[];
  expect(missing.some((m) => m.signal === "cross_origin_frame" && m.reason === "not_permitted")).toBe(true);
  expect(missing.some((m) => m.signal === "closed_shadow_root" && m.reason === "unknown")).toBe(true);
  expect(missing.some((m) => m.signal === "accessibility_tree")).toBe(true);
  await world.stop();
});

test("a body the contract would reject is rejected before it leaves the observer", async ({ page }) => {
  const { world, h, stats, host } = await observe(page);
  const body = observationBody(h.collector.replica, stats, {}, host);
  for (const bad of [
    { ...body, origin: "ftp://x" },
    { ...body, runtime_id: "extension" },
    { ...body, mode: "partial" },
    { ...body, payload: { ...(body.payload as object), format: "json" } },
  ]) {
    expect(() => decodeDocument(documentBytes(bad))).toThrow();
  }
  await world.stop();
});

test("truncation is reported as a partial document", async ({ page }) => {
  const world = await startWorld();
  world.a.pages.set("/", scopesPage(world.b.url));
  world.a.pages.set("/inner", innerPage);
  const h = await attach(page, { allow: [world.a.url], limits: { maxNodes: 20 } });
  await page.goto(`${world.a.url}/`);
  await page.waitForLoadState("load");
  await h.settle();
  const pack = encodePack(h.collector.replica.toSnapshot());
  const host: HostInfo = {
    sourceAppId: "vendor-console:app.vendor.test",
    variantResolution: "unknown",
    runtimeId: "rt_ext01",
    principalRef: `prin_${"b2".repeat(16)}`,
    capturedAt: 1,
    staleAfterMs: 1000,
    packDigest: await typedDigest("observation.pack", pack),
    packBytes: pack.length,
    patternsDigest: await typedDigest("redaction.patterns", enc.encode("[]")),
  };
  const body = observationBody(h.collector.replica, { possiblyClosed: 0 }, {}, host);
  expect((body.scope as { full_document: boolean }).full_document).toBe(false);
  expect(() => decodeDocument(documentBytes(body))).not.toThrow();
  await world.stop();
});

test("evidence is usable only while the replica equals the page and the window has not passed", async ({ page }) => {
  const { world, h } = await observe(page);
  const r = h.collector.replica;
  expect(freshnessOf(r, 1000, 2000, 5000)).toBe("fresh");
  expect(freshnessOf(r, 1000, 6001, 5000)).toBe("stale");
  expect(freshnessOf(r, 1000, 6000, 5000)).toBe("fresh");
  h.collector.dropNext = 1;
  await page.evaluate(() => {
    (document.querySelector("h1") as HTMLElement).textContent = "first";
  });
  await h.flush();
  await page.evaluate(() => {
    (document.querySelector("h1") as HTMLElement).textContent = "second";
  });
  await h.flush();
  expect(r.state).toBe("gap");
  expect(freshnessOf(r, 1000, 1001, 5000)).toBe("unsynced");
  await h.settle();
  expect(freshnessOf(r, 1000, 1001, 5000)).toBe("fresh");
  await world.stop();
});
