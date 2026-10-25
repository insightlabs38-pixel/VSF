import { expect, test } from "@playwright/test";
import type { Filter, SourceChart } from "@vsf/specialized";
import { ready } from "../support/page.ts";
import { startWorld, type World } from "../support/world.ts";

let w: World;
test.beforeAll(async () => {
  w = await startWorld();
});
test.afterAll(async () => w.stop());

const chart: SourceChart = {
  source: "sales",
  kind: "bar",
  title: "Revenue by quarter",
  xLabel: "Quarter",
  yLabel: "Revenue (USD)",
  series: [
    { id: "eu", name: "Europe", hidden: false },
    { id: "us", name: "United States", hidden: true },
  ],
  marks: [
    { name: "Q1, 12.50", series: "eu" },
    { name: "Q2, 0.30", series: "eu" },
    { name: "Q1, 1200000.125", series: "us" },
    { name: "Q2, -4", series: "us" },
  ],
  surface: "svg",
};

async function show(page: import("@playwright/test").Page, filters: Filter[]): Promise<void> {
  await page.evaluate(
    ([c, f]) => {
      const m = window.vsf.fromSource(c as never);
      if (!("points" in m)) throw new Error("unsupported");
      document.body.append(window.vsf.tableElement(document, window.vsf.toTable(m, f as never)));
    },
    [chart, filters] as const,
  );
}

test.beforeEach(async ({ page }) => {
  await ready(page, w);
});

test("the table alternative is a named table whose rows are exactly the chart's points, with exact values", async ({
  page,
}) => {
  await show(page, []);
  const table = page.getByRole("table", { name: "Revenue by quarter" });
  await expect(table.getByRole("columnheader")).toHaveText(["Series", "Quarter", "Revenue (USD)"]);
  await expect(table.getByRole("row")).toHaveCount(5);
  await expect(table.getByRole("cell", { name: "1200000.125" })).toBeVisible();
  await expect(table.getByRole("cell", { name: "12.50", exact: true })).toBeVisible();
  await expect(table.getByRole("rowheader", { name: "United States (hidden in the chart)" })).toHaveCount(2);
});

test("a filter removes the same points from the table and says how many it removed", async ({ page }) => {
  await show(page, [{ field: "series", op: "eq", value: "eu" }]);
  const table = page.getByRole("table", { name: "Revenue by quarter" });
  await expect(table.getByRole("row")).toHaveCount(3);
  await expect(page.getByText("2 points are not listed because of the active filters.")).toBeVisible();
  await expect(table.getByRole("cell", { name: "-4" })).toHaveCount(0);
});

test("source text cannot inject markup into the table", async ({ page }) => {
  await page.evaluate(() => {
    const m = window.vsf.fromSource({
      source: "x",
      kind: "bar",
      title: "<img src=x onerror=window.pwned=1>",
      xLabel: null,
      yLabel: null,
      series: [{ id: "s", name: "<b>bold</b>", hidden: false }],
      marks: [{ name: "<i>q</i>, 1", series: "s" }],
      surface: "html",
    });
    if ("points" in m) document.body.append(window.vsf.tableElement(document, window.vsf.toTable(m)));
  });
  expect(await page.locator("table img, table b, table i").count()).toBe(0);
  expect(await page.evaluate(() => (window as unknown as { pwned?: number }).pwned)).toBeUndefined();
});

test("a canvas chart is reported as unsupported with the reason and the source interface kept", async ({ page }) => {
  const r = await page.evaluate(
    ([c]) => window.vsf.fromSource({ ...(c as never as object), surface: "canvas" } as never),
    [chart] as const,
  );
  expect(r).toEqual({
    supported: false,
    reason: "canvas content is not in the page structure; values cannot be recovered",
    fallback: "retain_source_ui",
  });
});
