import { expect, test } from "@playwright/test";
import { measure, type Report } from "../support/perf.ts";
import { type Site, startSite } from "../support/world.ts";

let site: Site;
test.beforeAll(async () => {
  site = await startSite();
});
test.afterAll(async () => {
  await site.stop();
});

test.describe.configure({ timeout: 180_000 });

const cache = new Map<string, Report[]>();

async function best(page: import("@playwright/test").Page, rows: number, source: number, n = 2): Promise<Report[]> {
  const key = `${rows}:${source}`;
  const have = cache.get(key) ?? [];
  while (have.length < n) have.push(await measure(page, site, rows, source, 12));
  cache.set(key, have);
  return have.slice(0, n);
}
const min = (rs: Report[], f: (r: Report) => number): number => Math.min(...rs.map(f));

test("a 1,000-row view renders and answers pure UI operations within budget, with exact DOM write counts", async ({
  page,
}) => {
  const rs = await best(page, 1000, 0);
  const r = rs[0] as Report;
  expect(min(rs, (x) => x.render.ms)).toBeLessThan(1500);
  expect(min(rs, (x) => x.filter.ms)).toBeLessThan(150);
  expect(min(rs, (x) => x.filterClear.ms)).toBeLessThan(150);
  expect(min(rs, (x) => x.sort.ms)).toBeLessThan(400);
  expect(min(rs, (x) => x.cellUpdate.ms)).toBeLessThan(25);
  expect(min(rs, (x) => x.insert.ms)).toBeLessThan(100);
  expect(min(rs, (x) => x.remove.ms)).toBeLessThan(100);
  expect(min(rs, (x) => x.swap.ms)).toBeLessThan(400);
  expect(min(rs, (x) => x.replaceAll.ms)).toBeLessThan(400);
  expect(min(rs, (x) => x.palette.openMs)).toBeLessThan(150);
  expect(min(rs, (x) => x.keyboard.msPerKey)).toBeLessThan(150);
  for (const x of rs) {
    expect(x.cellUpdate.writes).toBe(1);
    expect(x.cellUpdate.created + x.cellUpdate.removed + x.cellUpdate.moved).toBe(0);
    expect(x.filter.visible).toBe(10);
    expect(x.filter.writes).toBe(1000 - 10 + 1);
    expect(x.filter.created + x.filterClear.created).toBe(0);
    expect(x.filterClear.writes).toBe(1000 - 10 + 1);
    expect(x.swap.moved).toBe(2);
    expect(x.swap.created + x.swap.removed).toBe(0);
    expect(x.replaceAll.created + x.replaceAll.removed).toBe(0);
    expect(x.remove.removed).toBeGreaterThan(0);
    expect(x.keyboard.tabStops).toBe(1);
    expect(x.keyboard.focusInGrid).toBe(true);
  }
  expect(r.render.domNodes).toBeLessThan(15 * 1000);
});

test("source and network delay are reported apart from UI time: the UI answers while the source is still working", async ({
  page,
}) => {
  const rs = await best(page, 1000, 0);
  for (const x of rs) {
    expect(x.operation.dispatches).toBe(1);
    expect(x.operation.totalMs).toBeGreaterThanOrEqual(x.operation.sourceDelayMs);
    expect(x.operation.uiBusyMs).toBeGreaterThanOrEqual(0);
    expect(x.operation.uiBusyMs).toBeLessThan(50);
    expect(x.operation.uiInteractiveWhilePendingMs).toBeLessThan(250);
    expect(x.operation.uiBusyMs + x.operation.uiInteractiveWhilePendingMs).toBeLessThan(x.operation.sourceDelayMs);
  }
});

test("repeated layout, filter and row-replacement cycles leave no growth in nodes, subscriptions or heap", async ({
  page,
}) => {
  const rs = await best(page, 1000, 0);
  for (const x of rs) {
    expect(x.memory.domAfter).toBe(x.memory.domBefore);
    expect(x.memory.subsAfter).toBe(x.memory.subsBefore);
    expect(x.memory.growthBytes).toBeLessThan(2 * 1024 * 1024);
  }
});

test("UI timing does not depend on the size or weight of the source page", async ({ page }) => {
  const small = await best(page, 1000, 0);
  const heavy = await best(page, 1000, 20000);
  const pairs: [string, (r: Report) => number][] = [
    ["render", (r) => r.render.ms],
    ["filter", (r) => r.filter.ms],
    ["sort", (r) => r.sort.ms],
    ["replaceAll", (r) => r.replaceAll.ms],
    ["cellUpdate", (r) => r.cellUpdate.ms],
  ];
  for (const [name, f] of pairs) {
    const a = min(small, f);
    const b = min(heavy, f);
    expect(b, `${name}: heavy ${b.toFixed(1)} ms vs small ${a.toFixed(1)} ms`).toBeLessThan(Math.max(a * 3, a + 40));
  }
  expect(heavy[0]?.render.writes).toBe(small[0]?.render.writes);
  expect(heavy[0]?.render.domNodes).toBe(small[0]?.render.domNodes);
});

test("cost scales with what changed, not with the size of the view", async ({ page }) => {
  const [a] = await best(page, 100, 0, 1);
  const [b] = await best(page, 1000, 0, 1);
  expect(a?.cellUpdate.writes).toBe(1);
  expect(b?.cellUpdate.writes).toBe(1);
  expect(b?.insert.created).toBe(a?.insert.created);
  expect(b?.swap.moved).toBe(a?.swap.moved);
});
