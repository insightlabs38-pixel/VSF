import { expect, type Page, test } from "@playwright/test";
import { heapBytes, openEditor, openTerminal, ready, record } from "../support/page.ts";
import { startWorld, type World } from "../support/world.ts";

let w: World;
test.beforeAll(async () => {
  w = await startWorld();
});
test.afterAll(async () => w.stop());

test.beforeEach(async ({ page }) => {
  await ready(page, w);
});

const MB = 1 << 20;

async function flood(page: Page, lines: number): Promise<void> {
  await page.evaluate(async (n) => {
    const chunk = `${Array.from({ length: 1000 }, (_, i) => `line ${i} ${"x".repeat(60)}`).join("\r\n")}\r\n`;
    for (let i = 0; i < n / 1000; i++) await window.term?.write(chunk);
  }, lines);
}

test("a flood of output keeps the scrollback at its limit and memory within budget", async ({ page }) => {
  await openTerminal(page, { scrollback: 1000 });
  const before = await heapBytes(page);
  const started = Date.now();
  await flood(page, 200_000);
  const ms = Date.now() - started;
  const after = await heapBytes(page);
  const rows = await page.evaluate(() => window.term?.lines().length ?? 0);
  record("terminal_flood_200k_lines", { ms, heapGrowthBytes: after - before, rows });
  expect(rows).toBeLessThanOrEqual(1000 + 30);
  expect(after - before).toBeLessThan(16 * MB);
  expect(ms).toBeLessThan(60_000);
  await page.evaluate(async () => window.term?.write("\r\nstill responsive"));
  expect((await page.evaluate(() => window.term?.lines()))?.at(-1)).toBe("still responsive");
});

test("hostile output cannot wedge the terminal, grow it without bound, or reach the page", async ({ page }) => {
  await openTerminal(page, { scrollback: 500 });
  const before = await heapBytes(page);
  await page.evaluate(async () => {
    const t = window.term;
    if (!t) return;
    // One enormous line, invalid bytes, absurd cursor moves, alternate screen toggles, and a title flood.
    await t.write("a".repeat(2 << 20));
    await t.write(new Uint8Array(1 << 20).fill(0xff));
    await t.write("\x1b[999999999;999999999H\x1b[999999999A\x1b[999999999B\x1b[999999999C\x1b[999999999D");
    await t.write("\x1b[?1049h\x1b[?1049l".repeat(5000));
    await t.write("\x1b]0;t\x07".repeat(50_000));
    await t.write("\x1b[1;1;1;1;1;1;1;1;1;1;1;1;1;1;1;1;1;1;1;1;1;1;1;1;1;1;1;1;1;1;1;1m".repeat(20_000));
    await t.write("\x1bc");
    await t.write("ok");
  });
  expect(await page.evaluate(() => window.term?.lines())).toEqual(["ok"]);
  const hostileHeap = await heapBytes(page);
  record("terminal_hostile_output", { heapGrowthBytes: hostileHeap - before });
  expect(hostileHeap).toBeLessThan(before + 16 * MB);
  expect(await page.title()).toBe("adapters");
});

test("opening and disposing adapters over and over does not leak", async ({ page }) => {
  await openTerminal(page);
  await page.evaluate(() => window.term?.dispose());
  await openEditor(page, { value: "x" });
  await page.evaluate(() => window.editor?.dispose());
  const before = await heapBytes(page);
  await page.evaluate(async () => {
    const host = document.getElementById("host") as HTMLElement;
    for (let i = 0; i < 40; i++) {
      const t = await window.vsf.openAdapter("terminal", host, {});
      await t.write(`cycle ${i}\r\n`);
      t.dispose();
      const e = await window.vsf.openAdapter("editor", host, { value: `cycle ${i}` });
      e.dispose();
    }
  });
  const after = await heapBytes(page);
  expect(await page.evaluate(() => document.getElementById("host")?.childElementCount)).toBe(0);
  record("adapter_open_dispose_40_cycles", { heapGrowthBytes: after - before });
  expect(after - before).toBeLessThan(8 * MB);
});

test("a very large document is shown without drawing every line, and edits stay exact", async ({ page }) => {
  await openEditor(page, { value: "" });
  const before = await heapBytes(page);
  const set = await page.evaluate(() => {
    const doc = Array.from({ length: 100_000 }, (_, i) => `row ${i} ${"y".repeat(24)}`).join("\n");
    const t0 = performance.now();
    window.editor?.setValue(doc);
    return { ms: performance.now() - t0, length: doc.length };
  });
  record("editor_100k_lines", { setValueMs: set.ms, chars: set.length });
  expect(set.ms).toBeLessThan(5000);
  expect(await page.evaluate(() => window.editor?.getValue().length)).toBe(set.length);
  expect(await page.locator(".cm-line").count()).toBeLessThan(400);
  await page.evaluate(() => window.editor?.focus());
  await page.keyboard.press("Control+End");
  await page.keyboard.type("END");
  expect(await page.evaluate(() => window.editor?.getValue().endsWith("yyyEND"))).toBe(true);
  const growth = (await heapBytes(page)) - before;
  record("editor_100k_lines_heap", { heapGrowthBytes: growth, linesDrawn: await page.locator(".cm-line").count() });
  expect(growth).toBeLessThan(64 * MB);
});

test("odd text survives the editor unchanged: one huge line, stacked combining marks, bidirectional controls", async ({
  page,
}) => {
  await openEditor(page, { value: "" });
  const cases = {
    hugeLine: "z".repeat(1 << 20),
    stacked: `e${"́".repeat(5000)}x`,
    bidi: "abc ‮evil‬ ⁧ rtl ⁩ end",
    controls: "tab\there\u0000nul​zero",
    astral: "😀".repeat(50_000),
  };
  for (const [name, text] of Object.entries(cases)) {
    const ok = await page.evaluate((t) => {
      window.editor?.setValue(t);
      return window.editor?.getValue() === t;
    }, text);
    expect(ok, name).toBe(true);
  }
  await page.evaluate(() => window.editor?.setValue("final"));
  await page.evaluate(() => window.editor?.focus());
  await page.keyboard.press("End");
  await page.keyboard.type("!");
  expect(await page.evaluate(() => window.editor?.getValue())).toBe("final!");
});
