import { expect, test } from "@playwright/test";
import { contrast, PALETTE, parseHex } from "@vsf/design-system";
import { formBoot, gridBoot, semantic } from "../support/config.ts";
import { active, live, tabNames } from "../support/page.ts";
import { open, type Site, startSite } from "../support/world.ts";

let site: Site;
test.beforeAll(async () => {
  site = await startSite();
});
test.afterAll(async () => {
  await site.stop();
});

const evalApp = <T>(page: import("@playwright/test").Page, fn: string): Promise<T> =>
  page.evaluate(`(() => { const app = window.app; return (${fn})(app); })()`) as Promise<T>;

test("form: Tab order follows reading order, Shift+Tab reverses it, and every stop has a name", async ({ page }) => {
  await open(page, site, "/f", formBoot());
  const forward = await tabNames(page, 7);
  expect(forward.slice(0, 7)).toEqual([
    "Skip to content",
    "Order number",
    "Total",
    "Placed on",
    "Open order",
    "Submit order",
    "Delete order",
  ]);
  const back = await tabNames(page, 3, true);
  expect(back).toEqual(["Submit order", "Open order", "Placed on"]);
});

test("form: the accessibility tree has landmarks, a labelled form and named controls", async ({ page }) => {
  await open(page, site, "/f", {
    ...formBoot(),
    navigation: true,
    views: [
      { entity: "order", layout: "form" },
      { entity: "customer", layout: "form" },
    ],
  });
  await expect(page.locator("#vsf-host")).toMatchAriaSnapshot(`
    - link "Skip to content"
    - status
    - alert
    - navigation "Views":
      - list:
        - listitem:
          - link "Order"
        - listitem:
          - link "Customer"
    - main:
      - form "Order":
        - heading "Order" [level=2]
        - textbox "Order number"
        - textbox "Total"
        - textbox "Placed on"
        - group "Order actions":
          - button "Open order"
          - button "Submit order"
          - button "Delete order"
  `);
});

test("no interactive element lacks an accessible name, in the form and in the grid", async ({ page }) => {
  for (const [path, cfg] of [
    ["/f", formBoot()],
    ["/g", gridBoot(20)],
  ] as const) {
    await open(page, site, path, cfg);
    const unnamed = await evalApp<string[]>(
      page,
      `(app) => [...app.renderer.root.querySelectorAll('button,input,select,textarea,a[href],[role=gridcell],[role=columnheader]')].filter((e) => {
        const label = e.getAttribute('aria-label') || (e.id && app.renderer.root.querySelector('label[for="' + e.id + '"]')?.textContent) || e.textContent;
        return !(label || '').trim();
      }).map((e) => e.outerHTML.slice(0, 80))`,
    );
    expect(unnamed, path).toEqual([]);
  }
});

test("Enter in a field never runs an operation; Enter and Space on a button run it exactly once", async ({ page }) => {
  await open(page, site, "/f", formBoot());
  await page.getByRole("textbox", { name: "Total" }).focus();
  await page.keyboard.press("Enter");
  expect(await evalApp<unknown[]>(page, "(a) => a.dispatches")).toEqual([]);
  await page.getByRole("button", { name: "Submit order" }).focus();
  await page.keyboard.press("Enter");
  await page.keyboard.press("Space");
  expect(await evalApp<unknown[]>(page, "(a) => a.dispatches")).toEqual([
    { action: "submit_order", entity: "order" },
    { action: "submit_order", entity: "order" },
  ]);
});

test("focus is visible, 3px, and has at least 3:1 contrast with the page", async ({ page }) => {
  await open(page, site, "/f", formBoot({ theme: { scheme: "light", accent: "blue", density: "comfortable" } }));
  await page.keyboard.press("Tab");
  await page.keyboard.press("Tab");
  const ring = await evalApp<{ w: string; style: string; color: string }>(
    page,
    `(app) => { const e = app.renderer.root.activeElement; const c = getComputedStyle(e); return { w: c.outlineWidth, style: c.outlineStyle, color: c.outlineColor }; }`,
  );
  expect(ring.style).toBe("solid");
  expect(ring.w).toBe("3px");
  const [r, g, b] = parseHex(PALETTE.light.focus);
  expect(ring.color).toBe(`rgb(${r}, ${g}, ${b})`);
  expect(contrast(PALETTE.light.focus, PALETTE.light.bg)).toBeGreaterThanOrEqual(3);
});

test("an unavailable operation stays focusable, explains itself and does nothing", async ({ page }) => {
  await open(
    page,
    site,
    "/u",
    formBoot({ availability: { delete_order: { state: "conflicted", reason: "API and page disagree" } } }),
  );
  const del = page.getByRole("button", { name: "Delete order" });
  await expect(del).toHaveAttribute("aria-disabled", "true");
  await expect(del).toHaveAccessibleDescription(/Unavailable, conflicted: API and page disagree/);
  await del.focus();
  expect((await active(page))?.name).toContain("Delete order");
  await page.keyboard.press("Enter");
  expect(await evalApp<unknown[]>(page, "(a) => a.dispatches")).toEqual([]);
  expect(await live(page, "alert")).toBe("Delete order is unavailable: API and page disagree");
});

test("results are announced: success politely, failure and refusal as alerts, repeats again", async ({ page }) => {
  await open(
    page,
    site,
    "/a",
    formBoot({
      results: {
        submit_order: { ok: true, message: "Order submitted" },
        open_order: { ok: false, message: "Order is locked" },
      },
    }),
  );
  await page.getByRole("button", { name: "Submit order" }).click();
  await expect.poll(() => live(page, "status")).toBe("Order submitted");
  const first = await evalApp<string>(page, "(a) => a.renderer.root.querySelector('[role=status]').textContent");
  await page.getByRole("button", { name: "Submit order" }).click();
  await expect
    .poll(() => evalApp<string>(page, "(a) => a.renderer.root.querySelector('[role=status]').textContent"))
    .not.toBe(first);
  expect(await live(page, "status")).toBe("Order submitted");
  await page.getByRole("button", { name: "Open order" }).click();
  await expect.poll(() => live(page, "alert")).toBe("Order is locked");
  const log = await evalApp<{ level: string; message: string }[]>(page, "(a) => a.renderer.announcer.log");
  expect(log.map((l) => l.level)).toEqual(["polite", "polite", "assertive"]);
});

test("grid: one tab stop, arrow keys, Home/End, Ctrl+Home/End and paging move a roving focus", async ({ page }) => {
  await open(page, site, "/g", gridBoot(40));
  await page.getByRole("textbox", { name: "Filter rows" }).focus();
  await page.keyboard.press("Tab");
  expect((await active(page))?.name).toBe("Order number");
  await page.keyboard.press("ArrowDown");
  expect((await active(page))?.id).toBe("cell:o00000/order_id");
  await page.keyboard.press("ArrowRight");
  expect((await active(page))?.id).toBe("cell:o00000/total");
  await page.keyboard.press("ArrowDown");
  await page.keyboard.press("ArrowDown");
  expect((await active(page))?.id).toBe("cell:o00002/total");
  await page.keyboard.press("End");
  expect((await active(page))?.name).toContain("Delete order");
  await page.keyboard.press("Home");
  expect((await active(page))?.id).toBe("cell:o00002/order_id");
  await page.keyboard.press("Control+End");
  expect((await active(page))?.name).toContain("SO-00040");
  await page.keyboard.press("Control+Home");
  expect((await active(page))?.id).toBe("cell:o00000/order_id");
  await page.keyboard.press("PageDown");
  expect((await active(page))?.id).toBe("cell:o00010/order_id");
  await page.keyboard.press("PageUp");
  expect((await active(page))?.id).toBe("cell:o00000/order_id");
  const stops = await evalApp<number>(
    page,
    "(a) => a.renderer.root.querySelectorAll('[data-stop][tabindex=\"0\"]').length",
  );
  expect(stops).toBe(1);
});

test("grid: leaving with Tab and returning with Shift+Tab lands on the last cell", async ({ page }) => {
  await open(page, site, "/g", gridBoot(30));
  await page.getByRole("textbox", { name: "Filter rows" }).focus();
  await page.keyboard.press("Tab");
  await page.keyboard.press("ArrowDown");
  await page.keyboard.press("ArrowDown");
  await page.keyboard.press("ArrowRight");
  const here = (await active(page))?.id;
  expect(here).toBe("cell:o00001/total");
  await page.keyboard.press("Tab");
  expect(await active(page)).toBeNull();
  await page.keyboard.press("Shift+Tab");
  expect((await active(page))?.id).toBe(here);
});

test("grid: row actions are keyboard operable and carry the row in their name", async ({ page }) => {
  await open(page, site, "/g", gridBoot(10));
  await page.getByRole("button", { name: "Submit order: SO-00003" }).focus();
  await page.keyboard.press("Enter");
  expect(await evalApp<unknown[]>(page, "(a) => a.dispatches")).toEqual([
    { action: "submit_order", entity: "order", rowKey: "o00002" },
  ]);
  await expect.poll(() => live(page, "status")).toBe("submit_order sent");
});

test("focus restoration: closing the palette, deleting the focused row and relaying out never strand focus", async ({
  page,
}) => {
  await open(page, site, "/g", gridBoot(10));
  await page.getByRole("button", { name: "Delete order: SO-00004" }).focus();
  await page.keyboard.press("Control+k");
  await expect(page.getByRole("dialog", { name: "Command palette" })).toBeVisible();
  expect((await active(page))?.name).toBe("Search commands");
  await page.keyboard.press("Escape");
  expect((await active(page))?.name).toContain("Delete order: SO-00004");
  await evalApp(page, "(a) => a.store.rows('order').remove('o00003')");
  const after = await active(page);
  expect(after?.name).toContain("Delete order: SO-00005");
  await evalApp(page, "(a) => a.relayout([{ entity: 'order', layout: 'form' }])");
  const landed = await active(page);
  expect(landed).not.toBeNull();
});

test("focus and caret survive a theme change that rebuilds nothing", async ({ page }) => {
  await open(page, site, "/f", formBoot());
  const total = page.getByRole("textbox", { name: "Total" });
  await total.focus();
  await total.press("End");
  await page.keyboard.type("0");
  await evalApp(page, "(a) => a.retheme({ scheme: 'dark', accent: 'mint', density: 'compact' })");
  expect((await active(page))?.name).toBe("Total");
  await page.keyboard.type("1");
  await expect(total).toHaveValue("129.9501");
});

test("RTL: direction follows the locale, columns mirror and arrow keys follow reading direction", async ({ page }) => {
  const sem = semantic();
  const order = sem.entities.find((e) => e.id === "order");
  if (!order) throw new Error("fixture");
  (order.fields[0] as { name: string }).name = "رقم الطلب";
  (order.fields[1] as { name: string }).name = "الإجمالي";
  await open(page, site, "/rtl", gridBoot(5, { semantic: sem, locale: "ar-SA", direction: "auto" }));
  const meta = await evalApp<{ dir: string; lang: string }>(
    page,
    "(a) => ({ dir: a.host.getAttribute('dir'), lang: a.host.getAttribute('lang') })",
  );
  expect(meta).toEqual({ dir: "rtl", lang: "ar-SA" });
  const xs = await evalApp<number[]>(
    page,
    "(a) => [...a.renderer.root.querySelectorAll('th')].slice(0, 3).map((e) => Math.round(e.getBoundingClientRect().right))",
  );
  expect(xs[0]).toBeGreaterThan(xs[1] as number);
  expect(xs[1]).toBeGreaterThan(xs[2] as number);
  await page.getByRole("button", { name: "رقم الطلب" }).focus();
  await page.keyboard.press("ArrowDown");
  expect((await active(page))?.id).toBe("cell:o00000/order_id");
  await page.keyboard.press("ArrowLeft");
  expect((await active(page))?.id).toBe("cell:o00000/total");
  await page.keyboard.press("ArrowRight");
  expect((await active(page))?.id).toBe("cell:o00000/order_id");
});

test("RTL: form labels and inputs align to the inline start (right) edge", async ({ page }) => {
  await open(page, site, "/rtlf", formBoot({ locale: "he-IL", direction: "auto" }));
  const m = await evalApp<{ label: number; input: number; host: number; align: string }>(
    page,
    `(a) => { const r = a.renderer.root; const l = r.querySelector('label'); const i = r.querySelector('input'); const h = a.host.getBoundingClientRect();
      return { label: Math.round(l.getBoundingClientRect().right), input: Math.round(i.getBoundingClientRect().right), host: Math.round(h.right), align: getComputedStyle(l).textAlign }; }`,
  );
  expect(m.label).toBeGreaterThan(m.host - 80);
  expect(m.input).toBeGreaterThan(m.host - 80);
  expect(["start", "right"]).toContain(m.align);
});

test("LTR stays LTR for an explicit direction even with an RTL locale", async ({ page }) => {
  await open(page, site, "/ltr", formBoot({ locale: "ar", direction: "ltr" }));
  expect(await evalApp<string>(page, "(a) => a.host.getAttribute('dir')")).toBe("ltr");
});

test("target size: every interactive control is at least 44 css px at the default density", async ({ page }) => {
  await open(page, site, "/t", gridBoot(5));
  const small = await evalApp<string[]>(
    page,
    `(a) => [...a.renderer.root.querySelectorAll('button,input,select')].map((e) => { const b = e.getBoundingClientRect(); return [e.textContent || e.id, Math.round(b.width), Math.round(b.height)]; }).filter(([, w, h]) => h < 44 || w < 44).map((x) => x.join(':'))`,
  );
  expect(small).toEqual([]);
});
