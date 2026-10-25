import { expect, test } from "@playwright/test";
import { openEditor, openTerminal, ready } from "../support/page.ts";
import { plainPage, startWorld, type World } from "../support/world.ts";

let w: World;
test.beforeAll(async () => {
  w = await startWorld();
  w.pages.set("/plain", plainPage);
});
test.afterAll(async () => w.stop());

/** The library chunks only; a small chunk of shared code is loaded with the entry. */
const chunks = (w2: World) => w2.requests.filter((r) => /^\/dist\/chunk-(terminal|editor)-/.test(r));

test("a page that shows neither a terminal nor an editor fetches no adapter code", async ({ page }) => {
  w.requests.length = 0;
  await page.goto(`${w.url}/plain`);
  await page.waitForFunction(() => "vsf" in window);
  expect(
    w.requests.filter((r) => r !== "/dist/chunk-chunk-E227AJMC.js" && !r.startsWith("/dist/chunk-chunk-")),
  ).toEqual(["/plain", "/dist/entry.js"]);
  expect(chunks(w)).toEqual([]);
  expect(await page.evaluate(() => window.vsf.loadedAdapters())).toEqual([]);
});

test("opening a terminal fetches the terminal library and not the editor library", async ({ page }) => {
  w.requests.length = 0;
  await ready(page, w);
  expect(chunks(w)).toEqual([]);
  await openTerminal(page);
  expect(await page.evaluate(() => window.vsf.loadedAdapters())).toEqual(["terminal"]);
  const got = chunks(w);
  expect(got.some((c) => c.includes("terminal"))).toBe(true);
  expect(got.some((c) => c.includes("editor"))).toBe(false);
});

test("opening an editor fetches the editor library and not the terminal library", async ({ page }) => {
  w.requests.length = 0;
  await ready(page, w);
  await openEditor(page, { value: "x" });
  const got = chunks(w);
  expect(got.some((c) => c.includes("editor"))).toBe(true);
  expect(got.some((c) => c.includes("terminal"))).toBe(false);
});

test("a second adapter of the same kind reuses the loaded module", async ({ page }) => {
  w.requests.length = 0;
  await ready(page, w);
  await openTerminal(page);
  const first = chunks(w).length;
  await page.evaluate(async () => {
    const host = document.createElement("div");
    document.body.append(host);
    const t = await window.vsf.openAdapter("terminal", host, {});
    t.dispose();
  });
  expect(chunks(w).length).toBe(first);
});

test("a failed library load is reported, leaves the page usable, and works after a reload", async ({ page }) => {
  await ready(page, w);
  await page.route("**/dist/chunk-terminal-*", (route) => route.abort());
  const first = await page.evaluate(async () => {
    const host = document.getElementById("host") as HTMLElement;
    try {
      await window.vsf.openAdapter("terminal", host, {});
      return "opened";
    } catch {
      return "failed";
    }
  });
  expect(first).toBe("failed");
  expect(await page.evaluate(() => window.vsf.loadedAdapters())).toEqual([]);
  // The other adapter is unaffected by the failure.
  await openEditor(page, { value: "still works" });
  expect(await page.evaluate(() => window.editor?.getValue())).toBe("still works");
  // Browsers keep a failed module fetch for that URL until the page is reloaded.
  await page.unroute("**/dist/chunk-terminal-*");
  await ready(page, w);
  await openTerminal(page);
  expect(await page.evaluate(() => window.vsf.loadedAdapters())).toEqual(["terminal"]);
});
