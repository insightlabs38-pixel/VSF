import { expect, test } from "@playwright/test";
import { openTerminal, ready } from "../support/page.ts";
import { startWorld, type World } from "../support/world.ts";

let w: World;
test.beforeAll(async () => {
  w = await startWorld();
});
test.afterAll(async () => w.stop());

const write = (page: import("@playwright/test").Page, data: string) =>
  page.evaluate(async (d) => window.term?.write(d), data);
const lines = (page: import("@playwright/test").Page) => page.evaluate(() => window.term?.lines());
const inputs = (page: import("@playwright/test").Page) => page.evaluate(() => window.inputs);

test.beforeEach(async ({ page }) => {
  await ready(page, w);
  await openTerminal(page);
});

test("output from the source is shown as written, line by line", async ({ page }) => {
  await write(page, "hello\r\nworld");
  expect(await lines(page)).toEqual(["hello", "world"]);
  expect(await page.evaluate(() => window.term?.cursor())).toEqual({ x: 5, y: 1 });
});

test("keys, control keys, arrows and enter produce the bytes a process expects", async ({ page }) => {
  await page.evaluate(() => window.term?.focus());
  await page.keyboard.type("ls");
  await page.keyboard.press("Enter");
  await page.keyboard.press("ArrowUp");
  await page.keyboard.press("Control+c");
  await page.keyboard.press("Tab");
  expect((await inputs(page))?.join("")).toBe("ls\r\x1b[A\x03\t");
});

test("wide, combining and astral characters occupy the cells a terminal gives them", async ({ page }) => {
  await write(page, "日本");
  expect(await page.evaluate(() => window.term?.cursor())).toEqual({ x: 4, y: 0 });
  await write(page, "\r\né");
  expect(await page.evaluate(() => window.term?.cursor())).toEqual({ x: 1, y: 1 });
  await write(page, "\r\n😀x");
  expect(await page.evaluate(() => window.term?.cursor())).toEqual({ x: 3, y: 2 });
  const all = await lines(page);
  expect(all?.[0]).toBe("日本");
  expect(all?.[1]).toBe("é");
  expect(all?.[2]).toBe("😀x");
});

test("bytes split inside a UTF-8 sequence are joined, and invalid bytes are dropped without desynchronizing", async ({
  page,
}) => {
  await page.evaluate(async () => {
    const bytes = new TextEncoder().encode("é日");
    await window.term?.write(bytes.slice(0, 1));
    await window.term?.write(bytes.slice(1, 5));
    await window.term?.write(new Uint8Array([0xff, 0x41]));
  });
  expect(await lines(page)).toEqual(["é日A"]);
});

test("pasted text reaches the process once, with line ends as carriage returns", async ({ page }) => {
  await page.evaluate(() => window.term?.focus());
  await page.evaluate(() => {
    const ta = document.querySelector(".xterm-helper-textarea") as HTMLTextAreaElement;
    const dt = new DataTransfer();
    dt.setData("text/plain", "a\nb");
    ta.dispatchEvent(new ClipboardEvent("paste", { clipboardData: dt, bubbles: true, cancelable: true }));
  });
  expect(await inputs(page)).toEqual(["a\rb"]);
});

test("a program that asks for bracketed paste gets the paste wrapped and nothing else is changed", async ({ page }) => {
  await write(page, "\x1b[?2004h");
  await page.evaluate(() => window.term?.focus());
  await page.evaluate(() => {
    const ta = document.querySelector(".xterm-helper-textarea") as HTMLTextAreaElement;
    const dt = new DataTransfer();
    dt.setData("text/plain", "rm -rf /\n");
    ta.dispatchEvent(new ClipboardEvent("paste", { clipboardData: dt, bubbles: true, cancelable: true }));
  });
  expect(await inputs(page)).toEqual(["\x1b[200~rm -rf /\r\x1b[201~"]);
});

test("an input method's committed text is sent once and the composition is not sent", async ({ page }) => {
  await page.evaluate(() => window.term?.focus());
  const cdp = await page.context().newCDPSession(page);
  await cdp.send("Input.imeSetComposition", { text: "に", selectionStart: 1, selectionEnd: 1 });
  await cdp.send("Input.imeSetComposition", { text: "にほ", selectionStart: 2, selectionEnd: 2 });
  expect(await inputs(page)).toEqual([]);
  await cdp.send("Input.insertText", { text: "日本" });
  await page.waitForFunction(() => window.inputs.length > 0);
  expect((await inputs(page))?.join("")).toBe("日本");
});

test("resize reports the size the process must be told and fit follows the container", async ({ page }) => {
  const sizes = await page.evaluate(() => {
    const out: { cols: number; rows: number }[] = [];
    window.term?.onResize((s) => out.push(s));
    window.term?.resize({ cols: 100, rows: 30 });
    return out;
  });
  expect(sizes).toEqual([{ cols: 100, rows: 30 }]);
  const small = await page.evaluate(() => window.term?.fit());
  await page.evaluate(() => {
    (document.getElementById("host") as HTMLElement).style.width = "1000px";
  });
  const big = await page.evaluate(() => window.term?.fit());
  expect(big?.cols).toBeGreaterThan(small?.cols ?? 0);
  expect(await page.evaluate(() => [window.term?.fit().cols, window.term?.fit().rows])).toEqual([big?.cols, big?.rows]);
});

test("sizes that are not whole, too small or too large are refused and change nothing", async ({ page }) => {
  const codes = await page.evaluate(() => {
    const out: string[] = [];
    for (const size of [
      { cols: 1, rows: 5 },
      { cols: 80, rows: 0 },
      { cols: 80.5, rows: 24 },
      { cols: 5000, rows: 24 },
      { cols: Number.NaN, rows: 24 },
    ]) {
      try {
        window.term?.resize(size);
        out.push("accepted");
      } catch (e) {
        out.push((e as { code?: string }).code ?? "other");
      }
    }
    return out;
  });
  expect(codes).toEqual(Array(5).fill("adapter.bad_size"));
  expect(await page.evaluate(() => window.term?.fit().cols)).toBeGreaterThan(2);
});

test("selecting text by word or line reads back exactly what is shown", async ({ page }) => {
  await write(page, "alpha beta\r\ngamma");
  const cell = await page.evaluate(() => {
    const screen = document.querySelector(".xterm-screen") as HTMLElement;
    const r = screen.getBoundingClientRect();
    const t = window.term?.fit() ?? { cols: 1, rows: 1 };
    return { x: r.left, y: r.top, w: r.width / t.cols, h: r.height / t.rows };
  });
  await page.mouse.dblclick(cell.x + cell.w * 2.5, cell.y + cell.h * 0.5);
  expect(await page.evaluate(() => window.term?.selection())).toBe("alpha");
  await page.mouse.click(cell.x + cell.w * 2.5, cell.y + cell.h * 1.5, { clickCount: 3 });
  expect(await page.evaluate(() => window.term?.selection())).toBe("gamma");
});

test("focus is reported truthfully and moves with the page's focus", async ({ page }) => {
  expect(await page.evaluate(() => window.term?.hasFocus())).toBe(false);
  await page.evaluate(() => window.term?.focus());
  expect(await page.evaluate(() => window.term?.hasFocus())).toBe(true);
  await page.locator("#other").click();
  expect(await page.evaluate(() => window.term?.hasFocus())).toBe(false);
});

test("disposing releases the page, stops callbacks, and refuses further use", async ({ page }) => {
  const result = await page.evaluate(async () => {
    const t = window.term;
    if (!t) return null;
    const seen: string[] = [];
    t.onInput((d) => seen.push(d));
    t.dispose();
    t.dispose();
    const codes: string[] = [];
    for (const f of [() => t.write("x"), () => t.fit(), () => t.selection(), () => t.focus(), () => t.lines()]) {
      try {
        await f();
        codes.push("ok");
      } catch (e) {
        codes.push((e as { code?: string }).code ?? "other");
      }
    }
    return { lifecycle: t.lifecycle, children: document.getElementById("host")?.childElementCount, codes, seen };
  });
  expect(result?.lifecycle).toBe("disposed");
  expect(result?.children).toBe(0);
  expect(result?.codes).toEqual(Array(5).fill("adapter.disposed"));
  // A new terminal on the same host starts clean.
  await openTerminal(page);
  expect(await lines(page)).toEqual([]);
});

test("escape sequences in output cannot retitle the page, navigate, or open windows", async ({ page }) => {
  const popups: string[] = [];
  page.on("popup", (p) => popups.push(p.url()));
  const before = page.url();
  await write(page, "\x1b]0;evil\x07\x1b]8;;https://example.invalid/\x07link\x1b]8;;\x07\x1b]52;c;ZXZpbA==\x07");
  expect(await page.title()).toBe("adapters");
  expect(page.url()).toBe(before);
  expect(popups).toEqual([]);
});

test("a terminal answers a program's status query through its input, which is how a real terminal behaves", async ({
  page,
}) => {
  await write(page, "\x1b[c");
  const reply = (await inputs(page))?.join("") ?? "";
  expect(reply.startsWith("\x1b[?") && reply.endsWith("c")).toBe(true);
});
