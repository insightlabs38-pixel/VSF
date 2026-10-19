import { expect, type Page, test } from "@playwright/test";
import { formBoot, gridBoot, orderRows } from "../support/config.ts";
import { active, live } from "../support/page.ts";
import { open, type Site, startSite } from "../support/world.ts";

let site: Site;
test.beforeAll(async () => {
  site = await startSite();
});
test.afterAll(async () => {
  await site.stop();
});

const ev = <T>(page: Page, fn: string): Promise<T> =>
  page.evaluate(`(() => { const app = window.app; return (${fn})(app); })()`) as Promise<T>;

const two = () =>
  formBoot({
    navigation: true,
    views: [
      { entity: "order", layout: "form" },
      { entity: "customer", layout: "form" },
    ],
  });

const operations = (page: Page) =>
  ev<string[]>(
    page,
    `(a) => [...a.renderer.root.querySelectorAll('button[data-effect]')].map((b) => b.getAttribute('data-vsf-id') + '|' + b.getAttribute('data-effect') + '|' + b.getAttribute('data-state')).sort()`,
  );
const dispatched = (page: Page) => ev<number>(page, "(a) => a.dispatches.length");
const mark = (page: Page) =>
  ev<number>(
    page,
    `(a) => { let n = 0; for (const e of a.renderer.root.querySelectorAll('*')) e.__mark = ++n; return n; }`,
  );
const unmarked = (page: Page) =>
  ev<number>(page, `(a) => [...a.renderer.root.querySelectorAll('*')].filter((e) => e.__mark === undefined).length`);
const hostAttrs = (page: Page) =>
  ev<Record<string, string | null>>(
    page,
    `(a) => Object.fromEntries(['data-scheme','data-accent','data-density','dir','lang'].map((k) => [k, a.host.getAttribute(k)]))`,
  );

test("density, scheme, accent and navigation change independently and reset exactly, without remounting anything", async ({
  page,
}) => {
  await open(page, site, "/p", two());
  const ops = await operations(page);
  const base = await hostAttrs(page);
  await mark(page);
  const target = () =>
    page
      .getByRole("button", { name: "Submit order" })
      .boundingBox()
      .then((b) => b?.height ?? 0);
  const comfortable = await target();
  await ev(page, "(a) => a.renderer.setPreference({ density: 'compact' })");
  const compact = await target();
  expect(compact).toBeGreaterThanOrEqual(24);
  expect(compact).toBeLessThan(comfortable);
  await ev(page, "(a) => a.renderer.setPreference({ density: 'spacious' })");
  expect(await target()).toBeGreaterThanOrEqual(comfortable);
  await ev(page, "(a) => a.renderer.setPreference({ scheme: 'dark', accent: 'mint', nav: 'side' })");
  expect(await hostAttrs(page)).toMatchObject({
    "data-scheme": "dark",
    "data-accent": "mint",
    "data-density": "spacious",
  });
  expect(await ev<string>(page, "(a) => a.renderer.root.querySelector('.app').getAttribute('data-nav')")).toBe("side");
  expect(await operations(page)).toEqual(ops);
  await ev(page, "(a) => a.renderer.setPreference({ density: 'comfortable' })");
  expect(await hostAttrs(page)).toMatchObject({
    "data-scheme": "dark",
    "data-accent": "mint",
    "data-density": "comfortable",
  });
  await ev(page, "(a) => a.renderer.resetPreferences()");
  expect(await hostAttrs(page)).toEqual(base);
  expect(await ev<string>(page, "(a) => a.renderer.root.querySelector('.app').getAttribute('data-nav')")).toBe("top");
  expect(await operations(page)).toEqual(ops);
  expect(await unmarked(page)).toBe(0);
  expect(await dispatched(page)).toBe(0);
  const log = await ev<object[]>(page, "(a) => a.prefsLog");
  expect(log.at(-1)).toEqual({});
  expect(log.length).toBe(5);
});

test("switching layouts and back keeps every operation, value and effect, and never reauthorizes anything", async ({
  page,
}) => {
  await open(page, site, "/l", formBoot({ rows: { order: orderRows(3) } }));
  const total = page.getByRole("textbox", { name: "Total" });
  await total.fill("77.50");
  const ops = await operations(page);
  const availability = await ev<string>(page, "(a) => JSON.stringify(a.rt.availability('submit_order'))");
  for (const layout of ["grid", "form", "grid", "form"]) {
    await ev(page, `(a) => a.relayout([{ entity: 'order', layout: '${layout}' }])`);
    const names = await ev<string[]>(
      page,
      `(a) => [...new Set([...a.renderer.root.querySelectorAll('button[data-effect]')].map((b) => b.getAttribute('data-vsf-id').split('@')[0]))].sort()`,
    );
    expect(names).toEqual(["act:delete_order", "act:open_order", "act:submit_order"]);
  }
  expect(await operations(page)).toEqual(ops);
  expect(await total.inputValue()).toBe("77.50");
  expect(await ev<string>(page, "(a) => JSON.stringify(a.rt.availability('submit_order'))")).toBe(availability);
  expect(await dispatched(page)).toBe(0);
  expect(await ev<unknown[]>(page, "(a) => a.problems()")).toEqual([]);
});

test("navigation can be hidden and every view stays reachable from the palette", async ({ page }) => {
  await open(page, site, "/n", two());
  await ev(page, "(a) => a.renderer.setPreference({ nav: 'hidden' })");
  await page.keyboard.press("Control+k");
  await expect.poll(() => ev<boolean>(page, "(a) => !!a.renderer.root.querySelector('[role=dialog]')")).toBe(true);
  await page.keyboard.type("customer");
  await page.keyboard.press("Enter");
  await expect(page.getByRole("heading", { name: "Customer" })).toBeVisible();
  expect(await ev<string>(page, "(a) => a.renderer.activeEntity")).toBe("customer");
  await expect.poll(() => live(page, "status")).toContain("Showing");
});

test("palette: filter, arrows, Enter and Escape; focus returns to where it came from and the page behind is inert", async ({
  page,
}) => {
  await open(page, site, "/k", two());
  await page.getByRole("textbox", { name: "Total" }).focus();
  await page.keyboard.press("Control+k");
  expect((await active(page))?.tag).toBe("input");
  expect(await ev<boolean>(page, "(a) => a.renderer.root.querySelector('.app').inert")).toBe(true);
  const all = await ev<string[]>(
    page,
    "(a) => [...a.renderer.root.querySelectorAll('[role=option]')].map((o) => o.textContent)",
  );
  expect(all.some((t) => t.startsWith("Go to Customer"))).toBe(true);
  expect(all.some((t) => t.startsWith("Delete order"))).toBe(true);
  await page.keyboard.press("Tab");
  expect((await active(page))?.tag).toBe("input");
  await page.keyboard.type("go to");
  await page.keyboard.press("ArrowDown");
  const selected = await ev<string>(page, "(a) => a.renderer.root.querySelector('[aria-selected=true]').textContent");
  expect(selected.startsWith("Go to")).toBe(true);
  await page.keyboard.press("Escape");
  expect(await ev<boolean>(page, "(a) => !!a.renderer.root.querySelector('[role=dialog]')")).toBe(false);
  expect((await active(page))?.name).toBe("Total");
  expect(await ev<boolean>(page, "(a) => a.renderer.root.querySelector('.app').inert")).toBe(false);
  expect(await dispatched(page)).toBe(0);
});

test("palette: an unavailable operation is listed with its reason and cannot run; an available one runs once", async ({
  page,
}) => {
  await open(
    page,
    site,
    "/u",
    formBoot({ availability: { delete_order: { state: "unusable", reason: "locked by owner" } } }),
  );
  await page.keyboard.press("Control+k");
  await page.keyboard.type("delete");
  const row = await ev<{ text: string; disabled: string | null }>(
    page,
    "(a) => { const o = a.renderer.root.querySelector('[role=option]'); return { text: o.textContent, disabled: o.getAttribute('aria-disabled') }; }",
  );
  expect(row.disabled).toBe("true");
  expect(row.text).toContain("locked by owner");
  await page.keyboard.press("Enter");
  expect(await dispatched(page)).toBe(0);
  expect(await live(page, "alert")).toContain("unavailable");
  await ev(page, "(a) => a.renderer.root.querySelector('input[role=combobox]').select()");
  await page.keyboard.type("submit");
  await page.keyboard.press("Enter");
  await expect.poll(() => dispatched(page)).toBe(1);
  expect(await ev<{ action: string }[]>(page, "(a) => a.dispatches.map((d) => ({ action: d.action }))")).toEqual([
    { action: "submit_order" },
  ]);
});

test("palette: a script-generated key cannot run a write, a real one can", async ({ page }) => {
  await open(page, site, "/s", formBoot({ synthetic: false }));
  await ev(page, "(a) => a.renderer.openPalette()");
  await page.keyboard.type("delete");
  await ev(
    page,
    "(a) => a.renderer.root.querySelector('input[role=combobox]').dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))",
  );
  expect(await dispatched(page)).toBe(0);
  expect(await live(page, "alert")).toContain("did not come from the user");
  await page.keyboard.press("Enter");
  await expect.poll(() => dispatched(page)).toBe(1);
});

test("palette in a grid: operations need a selected row and then act on it", async ({ page }) => {
  await open(page, site, "/g", gridBoot(12));
  await ev(page, "(a) => a.renderer.openPalette()");
  await page.keyboard.type("open");
  expect(await ev<string>(page, "(a) => a.renderer.root.querySelector('[role=option]').textContent")).toContain(
    "select a row first",
  );
  await page.keyboard.press("Escape");
  await page.getByRole("gridcell").first().focus();
  await page.keyboard.press("ArrowDown");
  await ev(page, "(a) => a.renderer.openPalette()");
  await page.keyboard.type("open");
  await page.keyboard.press("Enter");
  await expect.poll(() => dispatched(page)).toBe(1);
  const d = await ev<{ rowKey?: string }[]>(page, "(a) => a.dispatches");
  expect(d[0]?.rowKey).toMatch(/^o\d{5}$/);
});
