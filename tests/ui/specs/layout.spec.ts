import { expect, type Page, test } from "@playwright/test";
import { compositions } from "../support/compositions.ts";
import { formBoot, orderRows, semantic } from "../support/config.ts";
import { open, type Site, startSite } from "../support/world.ts";

let site: Site;
test.beforeAll(async () => {
  site = await startSite();
});
test.afterAll(async () => {
  await site.stop();
});

const WIDTHS = [
  { name: "desktop", width: 1280, height: 800 },
  { name: "tablet", width: 768, height: 1024 },
  { name: "phone", width: 390, height: 844 },
] as const;

const ev = <T>(page: Page, fn: string): Promise<T> =>
  page.evaluate(`(() => { const app = window.app; return (${fn})(app); })()`) as Promise<T>;

async function show(page: Page, name: string, width: number, height: number): Promise<string[]> {
  const errors: string[] = [];
  page.on("pageerror", (e) => errors.push(e.message));
  page.on("console", (m) => m.type() === "error" && errors.push(m.text()));
  await page.setViewportSize({ width, height });
  const c = compositions[name];
  if (!c) throw new Error(name);
  await open(
    page,
    site,
    `/layout-${name}`,
    formBoot({
      views: c.views,
      layout: c.layout,
      semantic: semantic(),
      rows: { order: orderRows(12), customer: [{ key: "c1", values: { customer_name: "Ada Lovelace" } }] },
      ...(c.surfaces ? { surfaces: c.surfaces } : {}),
    }),
  );
  return errors;
}

const box = (page: Page, region: string) =>
  ev<{ x: number; y: number; w: number; h: number } | null>(
    page,
    `(a) => { const e = a.renderer.root.querySelector('[data-region="${region}"]'); if (!e) return null; const r = e.getBoundingClientRect(); return { x: r.x, y: r.y, w: r.width, h: r.height }; }`,
  );

for (const name of Object.keys(compositions)) {
  for (const v of WIDTHS) {
    test(`${name} fits ${v.name} without overflow, errors or lost operations`, async ({ page }) => {
      const errors = await show(page, name, v.width, v.height);
      expect(errors).toEqual([]);
      const overflow = await page.evaluate(
        "document.documentElement.scrollWidth - document.documentElement.clientWidth",
      );
      expect(overflow).toBeLessThanOrEqual(0);
      const outside = await ev<string[]>(
        page,
        `(a) => [...a.renderer.root.querySelectorAll('[data-region]')].filter((e) => { const r = e.getBoundingClientRect(); return r.width > 0 && (r.right > innerWidth + 1 || r.left < -1); }).map((e) => e.getAttribute('data-region'))`,
      );
      expect(outside).toEqual([]);
      expect(await ev<number>(page, "(a) => a.renderer.root.querySelectorAll('main').length")).toBe(1);
      const rendered = await ev<string[]>(
        page,
        "(a) => [...a.renderer.root.querySelectorAll('button[data-effect]')].map((b) => b.getAttribute('data-vsf-id')).sort()",
      );
      const wanted = await ev<string[]>(
        page,
        "(a) => [...new Set(a.body().components.filter((c) => c.binding && c.binding.action).map((c) => c.binding.action))].sort()",
      );
      for (const action of wanted)
        expect(
          rendered.some((id) => id?.startsWith(`act:${action}`) ?? false),
          `${action} is missing from the page`,
        ).toBe(true);
      const hidden = await ev<string[]>(
        page,
        `(a) => [...a.renderer.root.querySelectorAll('button[data-effect]')].filter((b) => b.getClientRects().length === 0).map((b) => b.getAttribute('data-vsf-id'))`,
      );
      expect(hidden, "an operation is rendered but not visible").toEqual([]);
    });
  }
}

test("the enterprise grid docks navigation and a rail beside the grid, and stacks them when narrow", async ({
  page,
}) => {
  await show(page, "enterprise-grid", 1280, 800);
  const [nav, main, rail] = [
    await box(page, "shell_nav"),
    await box(page, "shell_main"),
    await box(page, "shell_rail"),
  ];
  expect(nav && main && rail).toBeTruthy();
  if (!nav || !main || !rail) return;
  expect(nav.x + nav.w).toBeLessThanOrEqual(main.x + 1);
  expect(main.x + main.w).toBeLessThanOrEqual(rail.x + 1);
  expect(nav.w).toBeGreaterThanOrEqual(16 * 6);
  await page.setViewportSize({ width: 390, height: 844 });
  const [n2, m2] = [await box(page, "shell_nav"), await box(page, "shell_main")];
  expect(n2 && m2).toBeTruthy();
  if (!n2 || !m2) return;
  expect(n2.y).toBeLessThan(m2.y);
  expect(await page.getByRole("button", { name: "Show Navigation" }).count()).toBe(1);
});

test("a collapsed region is reachable again by its own button", async ({ page }) => {
  await show(page, "enterprise-grid", 390, 844);
  const toggle = page.getByRole("button", { name: /Show Navigation/ });
  await expect(toggle).toHaveAttribute("aria-expanded", "false");
  expect(
    await ev<number>(
      page,
      "(a) => [...a.renderer.root.querySelectorAll('[data-region=shell_nav] a')].filter((e) => e.getClientRects().length > 0).length",
    ),
  ).toBe(0);
  await toggle.click();
  await expect(page.getByRole("button", { name: /Hide Navigation/ })).toHaveAttribute("aria-expanded", "true");
  expect(
    await ev<number>(
      page,
      "(a) => [...a.renderer.root.querySelectorAll('[data-region=shell_nav] a')].filter((e) => e.getClientRects().length > 0).length",
    ),
  ).toBeGreaterThan(0);
});

test("a split becomes tabs below its breakpoint and the tabs work from the keyboard", async ({ page }) => {
  await show(page, "workbench-split", 1280, 800);
  const [a, b] = [await box(page, "bench_form"), await box(page, "bench_who")];
  if (!a || !b) throw new Error("panels missing");
  expect(Math.abs(a.y - b.y)).toBeLessThan(2);
  expect(a.x).toBeLessThan(b.x);
  expect(await page.getByRole("tab").count()).toBe(0);
  await page.setViewportSize({ width: 600, height: 800 });
  const tabs = page.getByRole("tab");
  await expect(tabs).toHaveCount(2);
  await expect(tabs.first()).toHaveAttribute("aria-selected", "true");
  expect(await ev<number>(page, "(a) => a.renderer.root.querySelectorAll('[role=tabpanel]').length")).toBe(2);
  await tabs.first().focus();
  await page.keyboard.press("ArrowRight");
  await expect(tabs.nth(1)).toHaveAttribute("aria-selected", "true");
  await expect(tabs.nth(1)).toBeFocused();
  await page.keyboard.press("Home");
  await expect(tabs.first()).toHaveAttribute("aria-selected", "true");
  expect(
    await ev<number>(
      page,
      "(a) => [...a.renderer.root.querySelectorAll('[role=tabpanel]')].filter((e) => e.getClientRects().length > 0).length",
    ),
  ).toBe(1);
  await page.setViewportSize({ width: 1280, height: 800 });
  await expect(page.getByRole("tab")).toHaveCount(0);
  expect(await ev<number>(page, "(a) => a.renderer.root.querySelectorAll('[role=tabpanel]').length")).toBe(0);
});

test("a preserved original surface sits in a panel without breaking the layout", async ({ page }) => {
  const errors = await show(page, "workbench-split", 1280, 800);
  expect(errors).toEqual([]);
  expect(
    await ev<number>(page, "(a) => a.renderer.root.querySelectorAll('[data-vsf-component^=surf_notebook]').length"),
  ).toBe(1);
  const surface = await ev<{ inside: boolean; w: number }>(
    page,
    "(a) => { const s = a.renderer.root.querySelector('[data-vsf-component^=surf_notebook]'); const p = s.closest('[data-region]'); const r = s.getBoundingClientRect(); return { inside: p.getAttribute('data-region') === 'bench_form', w: r.width }; }",
  );
  expect(surface.inside).toBe(true);
  expect(surface.w).toBeGreaterThan(100);
});

test("customization is reversible and cannot hide an operation or reorder components", async ({ page }) => {
  await show(page, "enterprise-grid", 1280, 800);
  const rev = (): Promise<number> => ev<number>(page, "(a) => a.renderer.layoutRevision");
  expect(await rev()).toBe(0);
  await ev(page, "(a) => a.renderer.customizeLayout({ collapsed: ['shell_rail', 'shell_main'] })");
  expect(
    await ev<boolean>(
      page,
      "(a) => a.renderer.root.querySelector('[data-region=shell_rail]').hasAttribute('data-collapsed')",
    ),
  ).toBe(true);
  expect(
    await ev<boolean>(
      page,
      "(a) => a.renderer.root.querySelector('[data-region=shell_main]').hasAttribute('data-collapsed')",
    ),
  ).toBe(false);
  expect(await page.getByRole("button", { name: "Delete order" }).first().isVisible()).toBe(true);
  const before = await ev<string>(
    page,
    "(a) => [...a.renderer.root.querySelectorAll('[data-region=shell_main] [data-vsf-component]')].map((e) => e.getAttribute('data-vsf-component')).join()",
  );
  await ev(
    page,
    "(a) => a.renderer.customizeLayout({ order: ['shell_rail', 'shell_main', 'shell_nav'], sizes: { shell_nav: 99 } })",
  );
  expect(await rev()).toBe(2);
  const after = await ev<string>(
    page,
    "(a) => [...a.renderer.root.querySelectorAll('[data-region=shell_main] [data-vsf-component]')].map((e) => e.getAttribute('data-vsf-component')).join()",
  );
  expect(after).toBe(before);
  const nav = await box(page, "shell_nav");
  expect(nav?.w ?? 0).toBeLessThanOrEqual(32 * 10.5);
  expect(await ev<boolean>(page, "(a) => a.renderer.undoLayout()")).toBe(true);
  expect(await ev<boolean>(page, "(a) => a.renderer.undoLayout()")).toBe(true);
  expect(await ev<boolean>(page, "(a) => a.renderer.undoLayout()")).toBe(false);
  expect(
    await ev<boolean>(
      page,
      "(a) => a.renderer.root.querySelector('[data-region=shell_rail]').hasAttribute('data-collapsed')",
    ),
  ).toBe(false);
});

test("a presentation that grants no customization ignores saved layout preferences", async ({ page }) => {
  await show(page, "master-detail", 1280, 800);
  await ev(
    page,
    "(a) => { const b = { ...a.body(), customization: undefined }; delete b.customization; a.renderer.update(b); }",
  );
  await ev(page, "(a) => a.renderer.customizeLayout({ order: ['md_detail', 'md_list'] })");
  const [list, detail] = [await box(page, "md_list"), await box(page, "md_detail")];
  if (!list || !detail) throw new Error("panels missing");
  expect(list.x).toBeLessThan(detail.x);
});

test("a rejected layout is never drawn", async ({ page }) => {
  await show(page, "master-detail", 1280, 800);
  const message = await ev<string>(
    page,
    "(a) => { const b = JSON.parse(JSON.stringify(a.body())); b.regions[1].parent = 'nowhere'; try { a.renderer.update(b); return ''; } catch (e) { return String(e.message); } }",
  );
  expect(message).toContain("region.unknown_parent");
  expect(await ev<number>(page, "(a) => a.renderer.root.querySelectorAll('[data-region]').length")).toBeGreaterThan(0);
});
