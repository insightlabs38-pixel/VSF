import { expect, type Page, test } from "@playwright/test";
import { formBoot, gridBoot } from "../support/config.ts";
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

const mark = (page: Page): Promise<number> =>
  ev(
    page,
    `(a) => { let n = 0; for (const e of a.renderer.root.querySelectorAll('*')) { e.__mark = ++n; } return n; }`,
  );

const unmarked = (page: Page): Promise<number> =>
  ev(page, `(a) => [...a.renderer.root.querySelectorAll('*')].filter((e) => e.__mark === undefined).length`);

const stats = (page: Page): Promise<{ writes: number; created: number; removed: number; moved: number }> =>
  ev(page, "(a) => ({ ...a.renderer.stats })");

const delta = async (page: Page, fn: string) => {
  const before = await stats(page);
  await ev(page, fn);
  const after = await stats(page);
  return {
    writes: after.writes - before.writes,
    created: after.created - before.created,
    removed: after.removed - before.removed,
    moved: after.moved - before.moved,
  };
};

const trace = (page: Page): Promise<void> =>
  ev(
    page,
    `(a) => { a.records = []; a.mo = new MutationObserver((r) => a.records.push(...r)); a.mo.observe(a.renderer.root, { subtree: true, childList: true, attributes: true, characterData: true }); }`,
  );

const records = (page: Page): Promise<{ type: string; target: string }[]> =>
  ev(
    page,
    `(a) => { a.records.push(...a.mo.takeRecords()); return a.records.map((r) => ({ type: r.type, target: r.target.nodeName + (r.target.getAttribute ? '[' + (r.target.getAttribute('data-vsf-row') || r.target.getAttribute('data-vsf-component') || '') + ']' : '') })); }`,
  );

test("an update to another field leaves focus, caret and selection of the focused input untouched", async ({
  page,
}) => {
  await open(page, site, "/f", formBoot());
  const total = page.getByRole("textbox", { name: "Total" });
  await total.focus();
  await total.evaluate((el: HTMLInputElement) => el.setSelectionRange(1, 4));
  await mark(page);
  const d = await delta(page, "(a) => a.store.set('order.placed_on', '2027-01-01')");
  expect(d).toEqual({ writes: 1, created: 0, removed: 0, moved: 0 });
  expect(await total.evaluate((el: HTMLInputElement) => [el.selectionStart, el.selectionEnd])).toEqual([1, 4]);
  expect((await ev<{ tag: string } | null>(page, "(a) => ({ tag: a.renderer.root.activeElement.tagName })"))?.tag).toBe(
    "INPUT",
  );
  expect(await unmarked(page)).toBe(0);
});

test("setting the focused input to its current value writes nothing", async ({ page }) => {
  await open(page, site, "/f", formBoot());
  const total = page.getByRole("textbox", { name: "Total" });
  await total.focus();
  await total.evaluate((el: HTMLInputElement) => el.setSelectionRange(2, 2));
  const d = await delta(page, "(a) => a.store.set('order.total', '129.95')");
  expect(d.writes).toBe(0);
  expect(await total.evaluate((el: HTMLInputElement) => el.selectionStart)).toBe(2);
});

test("an outside change to the focused input keeps the caret where the user is typing", async ({ page }) => {
  await open(page, site, "/f", formBoot());
  const total = page.getByRole("textbox", { name: "Total" });
  await total.focus();
  await total.evaluate((el: HTMLInputElement) => el.setSelectionRange(2, 2));
  await ev(page, "(a) => a.store.set('order.total', '129.9500')");
  expect(await total.evaluate((el: HTMLInputElement) => [el.value, el.selectionStart])).toEqual(["129.9500", 2]);
  await total.evaluate((el: HTMLInputElement) => el.setSelectionRange(8, 8));
  await ev(page, "(a) => a.store.set('order.total', '1299.9500')");
  expect(await total.evaluate((el: HTMLInputElement) => [el.value, el.selectionStart])).toEqual(["1299.9500", 9]);
});

test("typing is the single source of truth: store, input and a second subscriber agree", async ({ page }) => {
  await open(page, site, "/f", formBoot());
  await ev(page, "(a) => { a.seen = []; a.store.subscribe('order.total', (v) => a.seen.push(v)); }");
  await page.getByRole("textbox", { name: "Total" }).fill("77");
  expect(await ev(page, "(a) => a.store.get('order.total')")).toBe("77");
  expect(await ev(page, "(a) => a.seen")).toEqual(["77"]);
});

test("grid: changing one cell writes one text node and nothing else", async ({ page }) => {
  await open(page, site, "/g", gridBoot(200));
  await mark(page);
  await trace(page);
  const d = await delta(page, "(a) => a.store.rows('order').setCell('o00100', 'total', 5)");
  expect(d).toEqual({ writes: 1, created: 0, removed: 0, moved: 0 });
  const rec = await records(page);
  expect(rec).toEqual([{ type: "childList", target: "TD[]" }]);
  expect(await unmarked(page)).toBe(0);
});

test("grid: inserting a row creates that row only and moves nothing", async ({ page }) => {
  await open(page, site, "/g", gridBoot(200));
  await mark(page);
  const d = await delta(
    page,
    "(a) => a.store.rows('order').insert('new1', { order_id: 'SO-NEW', total: 1, placed_on: '2027-01-01' }, 50)",
  );
  expect(d.moved).toBe(0);
  expect(d.removed).toBe(0);
  expect(d.created).toBeLessThan(20);
  expect(await page.locator("tbody tr").count()).toBe(201);
  const fresh = await ev<number>(
    page,
    "(a) => [...a.renderer.root.querySelectorAll('tbody tr')].filter((r) => [...r.querySelectorAll('*')].some((e) => e.__mark === undefined)).length",
  );
  expect(fresh).toBe(1);
  expect(await page.locator("tbody tr").nth(50).getAttribute("data-vsf-row")).toBe("new1");
});

test("grid: removing a row removes that row only", async ({ page }) => {
  await open(page, site, "/g", gridBoot(200));
  await mark(page);
  const d = await delta(page, "(a) => a.store.rows('order').remove('o00007')");
  expect(d.removed).toBe(1);
  expect(d.created).toBe(0);
  expect(d.moved).toBe(0);
  expect(await unmarked(page)).toBe(0);
});

test("grid: swapping two rows moves two nodes, and a full reversal reuses every row", async ({ page }) => {
  await open(page, site, "/g", gridBoot(100));
  await mark(page);
  const swap = await delta(
    page,
    `(a) => { const rs = a.store.rows('order'); const o = [...rs.order()]; const rows = o.map((k) => ({ key: k, values: { ...rs.row(k) } })); [rows[10], rows[60]] = [rows[60], rows[10]]; rs.replace(rows); }`,
  );
  expect(swap.moved).toBeLessThanOrEqual(2);
  expect(swap.created).toBe(0);
  const rev = await delta(page, "(a) => a.store.rows('order').sortBy('order_id', -1)");
  expect(rev.created).toBe(0);
  expect(rev.removed).toBe(0);
  expect(rev.moved).toBeLessThanOrEqual(99);
  expect(await unmarked(page)).toBe(0);
  const first = await page.locator("tbody tr").first().getAttribute("data-vsf-row");
  expect(first).toBe("o00099");
});

test("grid: filtering hides rows without rebuilding them", async ({ page }) => {
  await open(page, site, "/g", gridBoot(300));
  await mark(page);
  await page.getByRole("textbox", { name: "Filter rows" }).fill("SO-0002");
  expect(
    await ev<number>(page, "(a) => [...a.renderer.root.querySelectorAll('tbody tr')].filter((r) => !r.hidden).length"),
  ).toBe(10);
  expect(await unmarked(page)).toBe(0);
  await trace(page);
  const d = await delta(
    page,
    "(a) => { const i = a.renderer.root.querySelector('input[type=text]'); i.value = ''; i.dispatchEvent(new Event('input')); }",
  );
  expect(d.created).toBe(0);
  expect(d.removed).toBe(0);
  expect(d.moved).toBe(0);
  // 290 rows are un-hidden, plus exactly one live-region announcement of the new count.
  expect(d.writes).toBe(291);
  const rec = await records(page);
  expect(rec.filter((r) => r.type === "attributes" && r.target.startsWith("TR[o")).length).toBe(290);
  expect(rec.filter((r) => !(r.type === "attributes" && r.target.startsWith("TR[o"))).length).toBe(1);
  expect(await unmarked(page)).toBe(0);
  expect(
    await ev<number>(page, "(a) => [...a.renderer.root.querySelectorAll('tbody tr')].filter((r) => r.hidden).length"),
  ).toBe(0);
  expect(await ev<string>(page, "(a) => a.renderer.announcer.log.at(-1).message")).toBe("300 of 300 rows shown");
});

test("grid: a filter that matches nothing hides every row and says so", async ({ page }) => {
  await open(page, site, "/g", gridBoot(300));
  await mark(page);
  await page.getByRole("textbox", { name: "Filter rows" }).fill("no-such-row");
  expect(
    await ev<number>(page, "(a) => [...a.renderer.root.querySelectorAll('tbody tr')].filter((r) => !r.hidden).length"),
  ).toBe(0);
  expect(await ev<string>(page, "(a) => a.renderer.announcer.log.at(-1).message")).toBe("0 of 300 rows shown");
  expect(await unmarked(page)).toBe(0);
});

test("layout changes keep shared components and rebuild only what differs", async ({ page }) => {
  await open(page, site, "/f", {
    ...formBoot(),
    navigation: true,
    views: [
      { entity: "order", layout: "form" },
      { entity: "customer", layout: "form" },
    ],
  });
  await mark(page);
  const same = await delta(
    page,
    "(a) => a.recompile({ navigation: true }, [{ entity: 'order', layout: 'form' }, { entity: 'customer', layout: 'form' }])",
  );
  expect(same).toEqual({ writes: 0, created: 0, removed: 0, moved: 0 });
  expect(await unmarked(page)).toBe(0);
  await ev(
    page,
    "(a) => a.relayout([{ entity: 'order', layout: 'form', preserved: [{ id: 'chart', name: 'Chart' }] }, { entity: 'customer', layout: 'form' }])",
  );
  const created = await unmarked(page);
  expect(created).toBeGreaterThan(0);
  expect(created).toBeLessThan(10);
});

test("availability changes repaint only the buttons of that operation", async ({ page }) => {
  await open(page, site, "/f", formBoot());
  await mark(page);
  await trace(page);
  const d = await delta(
    page,
    "(a) => a.setAvailability('delete_order', { state: 'unknown', reason: 'not demonstrated' })",
  );
  expect(d.created).toBe(0);
  expect(d.removed).toBe(0);
  const rec = await records(page);
  expect(
    rec.every(
      (r) =>
        r.target.startsWith("BUTTON") ||
        r.target.startsWith("SPAN") ||
        r.type === "childList" ||
        r.type === "attributes",
    ),
  ).toBe(true);
  expect(rec.length).toBeLessThanOrEqual(6);
  expect(await unmarked(page)).toBe(0);
});

test("subscriptions are released: layout churn and teardown leave nothing behind", async ({ page }) => {
  await open(page, site, "/g", gridBoot(100));
  const base = await ev<number>(page, "(a) => a.store.subscriptionCount()");
  expect(base).toBe(1 + 100 * 3 + 0);
  for (let i = 0; i < 10; i++) {
    await ev(page, "(a) => a.relayout([{ entity: 'order', layout: 'form' }])");
    await ev(page, "(a) => a.relayout([{ entity: 'order', layout: 'grid' }])");
  }
  expect(await ev<number>(page, "(a) => a.store.subscriptionCount()")).toBe(base);
  await ev(page, "(a) => a.dispose()");
  expect(await ev<number>(page, "(a) => a.store.subscriptionCount()")).toBe(0);
});
