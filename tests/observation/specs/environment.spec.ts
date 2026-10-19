import { expect, test } from "@playwright/test";
import { cacheOverlay, type EnvironmentObservation } from "@vsf/observer";
import { attach, startWorld } from "../support/world.ts";

const frames = (page: import("@playwright/test").Page) =>
  page.evaluate(() => new Promise<void>((r) => requestAnimationFrame(() => requestAnimationFrame(() => r()))));

const page1 = (lang: string, extra = "") =>
  `<!doctype html><html${lang ? ` lang="${lang}"` : ""}><head>${extra}</head><body><p>x</p></body></html>`;

async function boot(page: import("@playwright/test").Page, html: string, flags: string[] = []) {
  const world = await startWorld();
  world.a.pages.set("/", html);
  const h = await attach(page, { allow: [world.a.url], flags });
  await page.goto(`${world.a.url}/`);
  await h.settle();
  const env = (): EnvironmentObservation => h.collector.replica.env as EnvironmentObservation;
  return { world, h, env };
}

test("the locale is what the page declares and never the browser's own language", async ({ browser }) => {
  const ctx = await browser.newContext({ locale: "de-DE" });
  const page = await ctx.newPage();
  const { world, env } = await boot(page, page1(""));
  expect(await page.evaluate(() => navigator.language)).toBe("de-DE");
  expect(env().locale).toEqual({ state: "unknown" });
  await world.stop();
  await ctx.close();
});

test("a declared language is normalised and a content-language meta is the fallback", async ({ page }) => {
  const a = await boot(page, page1("EN_us"));
  expect(a.env().locale).toEqual({ state: "observed", value: "en-US" });
  await a.world.stop();
  const p2 = await page.context().newPage();
  const b = await boot(p2, page1("", '<meta http-equiv="content-language" content="fr-CA">'));
  expect(b.env().locale).toEqual({ state: "observed", value: "fr-CA" });
  await b.world.stop();
  const p3 = await page.context().newPage();
  const c = await boot(p3, page1("not a locale"));
  expect(c.env().locale).toEqual({ state: "unknown" });
  await c.world.stop();
});

test("a locale change on the page is published and moves the cache overlay", async ({ page }) => {
  const { world, h, env } = await boot(page, page1("en"));
  const needs = { locale: true, viewport: false, flags: [] };
  const before = cacheOverlay(env(), needs);
  await page.evaluate(() => document.documentElement.setAttribute("lang", "ja"));
  await h.settle();
  const after = cacheOverlay(env(), needs);
  expect(env().locale).toEqual({ state: "observed", value: "ja" });
  expect(after.content).not.toBe(before.content);
  expect(after.shareable).toBe(true);
  await page.evaluate(() => document.documentElement.removeAttribute("lang"));
  await h.settle();
  const gone = cacheOverlay(env(), needs);
  expect(gone.shareable).toBe(false);
  expect(gone.unknown).toEqual(["locale"]);
  await world.stop();
});

test("the viewport is observed, resized and bucketed, and the overlay only moves across a bucket", async ({ page }) => {
  await page.setViewportSize({ width: 1000, height: 700 });
  const { world, h, env } = await boot(page, page1("en"));
  const needs = { locale: false, viewport: true, flags: [] };
  const first = env().viewport;
  expect(first.state === "observed" && first.value).toMatchObject({ width: 1000, height: 700, bucket: "lg" });
  const lg = cacheOverlay(env(), needs).content;
  await page.setViewportSize({ width: 1100, height: 700 });
  await frames(page);
  await h.settle();
  const second = env().viewport;
  expect(second.state === "observed" && second.value.width).toBe(1100);
  expect(cacheOverlay(env(), needs).content).toBe(lg);
  await page.setViewportSize({ width: 500, height: 700 });
  await frames(page);
  await h.settle();
  expect(cacheOverlay(env(), needs).content).not.toBe(lg);
  await world.stop();
});

test("only configured flag probes are read and a flag that is not visible stays unknown", async ({ page }) => {
  const html = `<!doctype html><html lang="en" data-flag-beta="on"><head><meta name="flag:new-nav" content="1"></head><body><p>x</p></body></html>`;
  const { world, h, env } = await boot(page, html, ["beta", "new-nav", "server-side-only"]);
  expect(env().flags).toEqual({
    beta: { state: "observed", value: "on" },
    "new-nav": { state: "observed", value: "1" },
    "server-side-only": { state: "unknown" },
  });
  const needs = { locale: true, viewport: false, flags: ["beta", "server-side-only"] };
  expect(cacheOverlay(env(), needs).shareable).toBe(false);
  await page.evaluate(() => document.documentElement.setAttribute("data-flag-beta", "off"));
  await h.settle();
  expect(env().flags.beta).toEqual({ state: "observed", value: "off" });
  await page.evaluate(() => document.querySelector('meta[name="flag:new-nav"]')?.setAttribute("content", "2"));
  await h.settle();
  expect(env().flags["new-nav"]).toEqual({ state: "observed", value: "2" });
  await world.stop();
});

test("an environment change does not force a new epoch", async ({ page }) => {
  const { world, h } = await boot(page, page1("en"));
  const epoch = h.collector.replica.epoch;
  await page.evaluate(() => document.documentElement.setAttribute("lang", "de"));
  await h.settle();
  expect(h.collector.replica.epoch).toBe(epoch);
  expect(h.collector.batches.at(-1)?.ops.map((o) => o.op)).toEqual(["env"]);
  await world.stop();
});
