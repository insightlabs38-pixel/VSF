import { mkdirSync, writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { chromium } from "@playwright/test";
import { measure, type Report } from "./support/perf.ts";
import { startSite } from "./support/world.ts";

const runs = Number(process.env.VSF_BENCH_RUNS ?? 5);
const out = resolve(process.argv[2] ?? "../../artifacts/bench/ui.json");

const median = (xs: number[]): number => [...xs].sort((a, b) => a - b)[Math.floor(xs.length / 2)] as number;

type Flat = Record<string, number>;
function flat(r: Report): Flat {
  return {
    render_ms: r.render.ms,
    render_writes: r.render.writes,
    render_dom_nodes: r.render.domNodes,
    filter_ms: r.filter.ms,
    filter_writes: r.filter.writes,
    filter_clear_ms: r.filterClear.ms,
    filter_clear_writes: r.filterClear.writes,
    sort_ms: r.sort.ms,
    sort_moved: r.sort.moved,
    cell_update_ms: r.cellUpdate.ms,
    cell_update_writes: r.cellUpdate.writes,
    insert_ms: r.insert.ms,
    insert_created: r.insert.created,
    remove_ms: r.remove.ms,
    swap_ms: r.swap.ms,
    swap_moved: r.swap.moved,
    replace_all_ms: r.replaceAll.ms,
    replace_all_created: r.replaceAll.created,
    key_ms: r.keyboard.msPerKey,
    tab_stops: r.keyboard.tabStops,
    palette_open_ms: r.palette.openMs,
    op_ui_busy_ms: r.operation.uiBusyMs,
    op_ui_interactive_ms: r.operation.uiInteractiveWhilePendingMs,
    op_total_ms: r.operation.totalMs,
    heap_growth_bytes: r.memory.growthBytes,
    dom_growth: r.memory.domAfter - r.memory.domBefore,
    subs_growth: r.memory.subsAfter - r.memory.subsBefore,
  };
}

const site = await startSite();
const browser = await chromium.launch();
const result: Record<string, { median: Flat; runs: Flat[] }> = {};
for (const [name, rows, source] of [
  ["rows_1000_source_small", 1000, 0],
  ["rows_1000_source_heavy", 1000, 20000],
  ["rows_100_source_small", 100, 0],
] as const) {
  const all: Flat[] = [];
  for (let i = 0; i < runs; i++) {
    const page = await browser.newPage();
    all.push(flat(await measure(page, site, rows, source)));
    await page.close();
  }
  const keys = Object.keys(all[0] as Flat);
  result[name] = {
    median: Object.fromEntries(keys.map((k) => [k, median(all.map((f) => f[k] as number))])),
    runs: all,
  };
  console.log(name, JSON.stringify(result[name]?.median));
}
await browser.close();
await site.stop();
mkdirSync(dirname(out), { recursive: true });
writeFileSync(
  out,
  `${JSON.stringify({ schema: "vsf-ui-bench-v1", runs, browser: "chromium (headless, playwright)", note: "source delay of 300 ms is injected in the operation scenario and reported separately from UI time", result }, null, 1)}\n`,
);
console.log(`wrote ${out}`);
