import type { Page } from "@playwright/test";
import { gridBoot } from "./config.ts";
import { open, type Site, sourcePage } from "./world.ts";

export interface Stat {
  ms: number;
  writes: number;
  created: number;
  removed: number;
  moved: number;
}

export interface Report {
  rows: number;
  sourceNodes: number;
  render: Stat & { domNodes: number };
  filter: Stat & { visible: number };
  filterClear: Stat;
  sort: Stat;
  cellUpdate: Stat;
  insert: Stat;
  remove: Stat;
  swap: Stat;
  replaceAll: Stat;
  keyboard: { keys: number; msPerKey: number; tabStops: number; focusInGrid: boolean };
  palette: { openMs: number; commands: number };
  operation: {
    sourceDelayMs: number;
    uiBusyMs: number;
    uiInteractiveWhilePendingMs: number;
    totalMs: number;
    dispatches: number;
  };
  memory: {
    before: number;
    after: number;
    growthBytes: number;
    domBefore: number;
    domAfter: number;
    subsBefore: number;
    subsAfter: number;
  };
}

const call = <T>(page: Page, fn: string): Promise<T> =>
  page.evaluate(`(() => { const app = window.app; return (${fn})(app); })()`) as Promise<T>;

/** Runs `body` in the page and returns elapsed milliseconds and the DOM-write counters it caused. */
const timed = (page: Page, body: string): Promise<Stat> =>
  call<Stat>(
    page,
    `(a) => { const s0 = { ...a.renderer.stats }; const t0 = performance.now(); (${body})(a); const ms = performance.now() - t0; const s1 = a.renderer.stats; return { ms, writes: s1.writes - s0.writes, created: s1.created - s0.created, removed: s1.removed - s0.removed, moved: s1.moved - s0.moved }; }`,
  );

function heavySource(n: number): string {
  const items = Array.from(
    { length: n },
    (_, i) => `<li class="x${i % 7}"><a href="#${i}">item ${i}</a><span>${i}</span></li>`,
  ).join("");
  return `<style>${Array.from({ length: 200 }, (_, i) => `.x${i % 7} a:nth-child(${i}){color:rgb(${i % 255},0,0)}`).join("\n")}</style><ul id="big">${items}</ul>`;
}

export async function measure(page: Page, site: Site, rows: number, sourceNodes = 0, cycles = 30): Promise<Report> {
  const html = sourcePage(sourceNodes > 0 ? heavySource(sourceNodes) : "");
  const boot = gridBoot(rows);
  site.pages.set("/perf", html);
  await page.goto(`${site.url}/perf`);
  const render = await page.evaluate((c) => {
    const w = window as unknown as {
      VSF: { boot(c: unknown): void };
      app: { renderer: { stats: Record<string, number>; root: ShadowRoot } };
    };
    const t0 = performance.now();
    w.VSF.boot(c);
    const ms = performance.now() - t0;
    const s = w.app.renderer.stats;
    return {
      ms,
      writes: s.writes ?? 0,
      created: s.created ?? 0,
      removed: s.removed ?? 0,
      moved: s.moved ?? 0,
      domNodes: w.app.renderer.root.querySelectorAll("*").length,
    };
  }, boot);

  const filter = await timed(
    page,
    "(a) => { const i = a.renderer.root.querySelector('input[type=text]'); i.value = 'SO-0005'; i.dispatchEvent(new Event('input')); }",
  );
  const visible = await call<number>(
    page,
    "(a) => [...a.renderer.root.querySelectorAll('tbody tr')].filter((r) => !r.hidden).length",
  );
  const filterClear = await timed(
    page,
    "(a) => { const i = a.renderer.root.querySelector('input[type=text]'); i.value = ''; i.dispatchEvent(new Event('input')); }",
  );
  const sort = await timed(page, "(a) => a.renderer.root.querySelector('button[data-col=total]').click()");
  const cellUpdate = await timed(page, "(a) => a.store.rows('order').setCell('o00007', 'total', 123.45)");
  const insert = await timed(
    page,
    "(a) => a.store.rows('order').insert('onew', { order_id: 'SO-NEW', total: 1, placed_on: '2026-01-01' }, 500)",
  );
  const remove = await timed(page, "(a) => a.store.rows('order').remove('onew')");
  const swap = await timed(
    page,
    "(a) => { const r = a.store.rows('order'); const o = r.order(); const rows = o.map((k) => ({ key: k, values: { order_id: r.cell(k, 'order_id'), total: r.cell(k, 'total'), placed_on: r.cell(k, 'placed_on') } })); [rows[10], rows[20]] = [rows[20], rows[10]]; r.replace(rows); }",
  );
  const replaceAll = await timed(
    page,
    "(a) => { const r = a.store.rows('order'); const o = r.order(); r.replace(o.map((k, i) => ({ key: k, values: { order_id: r.cell(k, 'order_id'), total: i, placed_on: r.cell(k, 'placed_on') } }))); }",
  );

  await page.evaluate(() => {
    const w = window as unknown as { app: { renderer: { root: ShadowRoot } } };
    (w.app.renderer.root.querySelector("tbody td") as HTMLElement).focus();
  });
  const keys = 120;
  const t0 = Date.now();
  for (let i = 0; i < keys; i++) await page.keyboard.press(i % 12 === 11 ? "PageDown" : "ArrowDown");
  const keyMs = Date.now() - t0;
  const kb = await call<{ tabStops: number; focusInGrid: boolean }>(
    page,
    "(a) => ({ tabStops: a.renderer.root.querySelectorAll('table [tabindex=\"0\"]').length, focusInGrid: !!a.renderer.root.activeElement?.closest('table') })",
  );

  const palette = await call<{ openMs: number; commands: number }>(
    page,
    "(a) => { const t = performance.now(); a.renderer.openPalette(); const ms = performance.now() - t; const n = a.renderer.root.querySelectorAll('[role=option]').length; a.renderer.closePalette(); return { openMs: ms, commands: n }; }",
  );

  const delay = 300;
  const operation = await call<{
    uiBusyMs: number;
    uiInteractiveWhilePendingMs: number;
    totalMs: number;
    dispatches: number;
  }>(
    page,
    `(a) => new Promise((resolve) => {
      const row = a.renderer.root.querySelector('tbody tr');
      const btn = row.querySelector('button[data-effect=read_only]');
      a.rt.dispatch = (action, ctx) => { a.dispatches.push({ action, entity: ctx.entity, rowKey: ctx.rowKey }); return new Promise((r) => setTimeout(() => r({ ok: true, message: 'done' }), ${delay})); };
      const t0 = performance.now();
      btn.click();
      const busy = btn.getAttribute('aria-busy') === 'true';
      const uiBusyMs = performance.now() - t0;
      const t1 = performance.now();
      const input = a.renderer.root.querySelector('input[type=text]');
      input.value = 'SO-0009'; input.dispatchEvent(new Event('input'));
      const uiInteractiveWhilePendingMs = performance.now() - t1;
      input.value = ''; input.dispatchEvent(new Event('input'));
      const poll = () => { if (btn.getAttribute('aria-busy') !== 'true') resolve({ uiBusyMs: busy ? uiBusyMs : -1, uiInteractiveWhilePendingMs, totalMs: performance.now() - t0, dispatches: a.dispatches.length }); else setTimeout(poll, 5); };
      poll();
    })`,
  );

  const mem = await memory(page, cycles);
  return {
    rows,
    sourceNodes,
    render,
    filter: { ...filter, visible },
    filterClear,
    sort,
    cellUpdate,
    insert,
    remove,
    swap,
    replaceAll,
    keyboard: { keys, msPerKey: keyMs / keys, ...kb },
    palette,
    operation: { sourceDelayMs: delay, ...operation },
    memory: mem,
  };
}

/** Growth after many full cycles of layout churn, filtering and row replacement, measured after a forced collection. */
export async function memory(page: Page, cycles = 30): Promise<Report["memory"]> {
  const cdp = await page.context().newCDPSession(page);
  const heap = async (): Promise<number> => {
    await cdp.send("HeapProfiler.collectGarbage");
    await cdp.send("HeapProfiler.collectGarbage");
    const m = (await cdp.send("Performance.getMetrics")) as { metrics: { name: string; value: number }[] };
    return m.metrics.find((x) => x.name === "JSHeapUsedSize")?.value ?? 0;
  };
  await cdp.send("Performance.enable");
  const cycle = `(a) => {
    a.relayout([{ entity: 'order', layout: 'form' }]);
    a.relayout([{ entity: 'order', layout: 'grid' }]);
    const i = a.renderer.root.querySelector('input[type=text]');
    if (i) { i.value = 'SO-001'; i.dispatchEvent(new Event('input')); i.value = ''; i.dispatchEvent(new Event('input')); }
    const r = a.store.rows('order'); const o = r.order();
    r.replace(o.map((k, n) => ({ key: k, values: { order_id: r.cell(k, 'order_id'), total: n, placed_on: r.cell(k, 'placed_on') } })));
  }`;
  for (let i = 0; i < 5; i++) await call(page, cycle);
  const nodes = () => call<number>(page, "(a) => a.renderer.root.querySelectorAll('*').length");
  const subs = () => call<number>(page, "(a) => a.store.subscriptionCount()");
  const before = await heap();
  const domBefore = await nodes();
  const subsBefore = await subs();
  for (let i = 0; i < cycles; i++) await call(page, cycle);
  const after = await heap();
  const domAfter = await nodes();
  const subsAfter = await subs();
  await cdp.detach();
  return { before, after, growthBytes: after - before, domBefore, domAfter, subsBefore, subsAfter };
}

export async function openSite(page: Page, site: Site): Promise<void> {
  await open(page, site, "/warm", gridBoot(10));
}
