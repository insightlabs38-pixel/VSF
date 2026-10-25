import { expect, type Page, test } from "@playwright/test";
import { openEditor, ready } from "../support/page.ts";
import { startWorld, type World } from "../support/world.ts";

let w: World;
test.beforeAll(async () => {
  w = await startWorld();
});
test.afterAll(async () => w.stop());

const value = (page: Page) => page.evaluate(() => window.editor?.getValue());
const selections = (page: Page) => page.evaluate(() => window.editor?.selections());
const changes = (page: Page) => page.evaluate(() => window.changes);

test.beforeEach(async ({ page }) => {
  await ready(page, w);
});

test("typing is a user edit and reports the whole new value", async ({ page }) => {
  await openEditor(page, { value: "ab" });
  await page.evaluate(() => window.editor?.focus());
  await page.keyboard.press("End");
  await page.keyboard.type("c");
  expect(await value(page)).toBe("abc");
  expect(await changes(page)).toEqual([{ value: "abc", userEdit: true }]);
});

test("a value set from the source is not a user edit and replaces the whole document", async ({ page }) => {
  await openEditor(page, { value: "old" });
  await page.evaluate(() => window.editor?.setValue("new text"));
  expect(await value(page)).toBe("new text");
  expect(await changes(page)).toEqual([{ value: "new text", userEdit: false }]);
});

test("read-only refuses typing and deleting but still takes the source's updates", async ({ page }) => {
  await openEditor(page, { value: "fixed", readOnly: true });
  await page.evaluate(() => window.editor?.focus());
  await page.keyboard.press("End");
  await page.keyboard.type("x");
  await page.keyboard.press("Backspace");
  expect(await value(page)).toBe("fixed");
  expect(await changes(page)).toEqual([]);
  await page.evaluate(() => window.editor?.setValue("from source"));
  expect(await value(page)).toBe("from source");
  await page.evaluate(() => window.editor?.readOnly(false));
  await page.keyboard.press("End");
  await page.keyboard.type("!");
  expect(await value(page)).toBe("from source!");
});

test("selections use UTF-16 offsets and moving over an emoji or a combined letter moves a whole character", async ({
  page,
}) => {
  await openEditor(page, { value: "😀éx" });
  await page.evaluate(() => window.editor?.focus());
  await page.keyboard.press("Home");
  await page.keyboard.press("ArrowRight");
  expect(await selections(page)).toEqual([{ from: 2, to: 2 }]);
  await page.keyboard.press("ArrowRight");
  expect(await selections(page)).toEqual([{ from: 4, to: 4 }]);
  // Backspace removes the combining mark first, as native text fields do.
  await page.keyboard.press("Backspace");
  expect(await value(page)).toBe("😀ex");
});

test("selecting a range from the source selects exactly it, and bad ranges are refused", async ({ page }) => {
  await openEditor(page, { value: "hello 😀" });
  await page.evaluate(() => window.editor?.select({ from: 6, to: 8 }));
  expect(await selections(page)).toEqual([{ from: 6, to: 8 }]);
  const codes = await page.evaluate(() => {
    const out: string[] = [];
    for (const r of [
      { from: -1, to: 2 },
      { from: 3, to: 2 },
      { from: 0, to: 99 },
      { from: 0.5, to: 1 },
    ]) {
      try {
        window.editor?.select(r);
        out.push("accepted");
      } catch (e) {
        out.push((e as { code?: string }).code ?? "other");
      }
    }
    return out;
  });
  expect(codes).toEqual(Array(4).fill("adapter.bad_range"));
  expect(await selections(page)).toEqual([{ from: 6, to: 8 }]);
});

test("composing text is reported while it is open and only the committed text remains", async ({ page }) => {
  await openEditor(page, { value: "" });
  await page.evaluate(() => window.editor?.focus());
  const cdp = await page.context().newCDPSession(page);
  expect(await page.evaluate(() => window.editor?.composing())).toBe(false);
  await cdp.send("Input.imeSetComposition", { text: "に", selectionStart: 1, selectionEnd: 1 });
  await cdp.send("Input.imeSetComposition", { text: "にほ", selectionStart: 2, selectionEnd: 2 });
  expect(await page.evaluate(() => window.editor?.composing())).toBe(true);
  await cdp.send("Input.insertText", { text: "日本" });
  await page.waitForFunction(() => window.editor?.composing() === false);
  expect(await value(page)).toBe("日本");
  expect(await selections(page)).toEqual([{ from: 2, to: 2 }]);
});

test("focus is reported truthfully and moves with the page's focus", async ({ page }) => {
  await openEditor(page, { value: "x" });
  expect(await page.evaluate(() => window.editor?.hasFocus())).toBe(false);
  await page.evaluate(() => window.editor?.focus());
  expect(await page.evaluate(() => window.editor?.hasFocus())).toBe(true);
  await page.locator("#other").click();
  expect(await page.evaluate(() => window.editor?.hasFocus())).toBe(false);
});

test("disposing releases the page, stops callbacks, and refuses further use", async ({ page }) => {
  await openEditor(page, { value: "x" });
  const result = await page.evaluate(() => {
    const e = window.editor;
    if (!e) return null;
    e.dispose();
    e.dispose();
    const codes: string[] = [];
    for (const f of [
      () => e.getValue(),
      () => e.setValue("y"),
      () => e.selections(),
      () => e.focus(),
      () => e.select({ from: 0, to: 0 }),
    ]) {
      try {
        f();
        codes.push("ok");
      } catch (err) {
        codes.push((err as { code?: string }).code ?? "other");
      }
    }
    return { lifecycle: e.lifecycle, children: document.getElementById("host")?.childElementCount, codes };
  });
  expect(result?.lifecycle).toBe("disposed");
  expect(result?.children).toBe(0);
  expect(result?.codes).toEqual(Array(5).fill("adapter.disposed"));
  await openEditor(page, { value: "again" });
  expect(await value(page)).toBe("again");
});
