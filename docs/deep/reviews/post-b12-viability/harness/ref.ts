import type { ElementHandle, Locator, Page } from "@playwright/test";
import { setToggle, typeInto } from "../../../../../tests/corpus/src/actuate.ts";
import type { Ctx, Step } from "./spec.ts";

const T = { timeout: 5000 };

export function locate(page: Page, s: Step): Locator {
  const root = s.within ? page.locator(s.within) : page;
  const base = s.sel
    ? root.locator(s.sel)
    : s.role
      ? root.getByRole(s.role as never, { name: s.target ?? "", exact: true })
      : root.getByLabel(s.target ?? "", { exact: true });
  return s.nth !== undefined ? base.nth(s.nth) : base;
}

/** Acts on one element the way a person would; shared by the reference and the VSF run except for custom popups. */
export async function act(
  page: Page,
  el: ElementHandle<Element>,
  s: Step,
  ctx: Ctx,
  popup: (el: ElementHandle<Element>, value: string) => Promise<void>,
): Promise<void> {
  if (s.do === "fill") await typeInto(page, el, s.value ?? "");
  else if (s.do === "click") await el.click(T);
  else if (s.do === "toggle") await setToggle(el, s.checked ?? true);
  else if (s.do === "choose") {
    const tag = await el.evaluate((e) => e.tagName);
    if (tag === "SELECT") await el.selectOption({ label: s.value ?? "" }, T);
    else await popup(el, s.value ?? "");
  } else if (s.do === "upload") {
    const file = { name: s.value ?? "f.txt", mimeType: "text/plain", buffer: Buffer.from(s.text ?? "") };
    const isInput = await el.evaluate((e) => e instanceof HTMLInputElement && e.type === "file");
    if (isInput) await el.setInputFiles(file, T);
    else {
      const [chooser] = await Promise.all([page.waitForEvent("filechooser", T), el.click(T)]);
      await chooser.setFiles(file);
    }
  } else if (s.do === "drag") {
    const to = page.locator(s.to ?? "body").first();
    const a = await el.boundingBox();
    const b = await to.boundingBox();
    if (!a || !b) throw new Error("drag: no box");
    await page.mouse.move(a.x + a.width / 2, a.y + a.height / 2);
    await page.mouse.down();
    await page.mouse.move(b.x + b.width / 2, b.y + b.height / 2, { steps: 12 });
    await page.mouse.up();
  }
  await page.waitForTimeout(s.wait ?? 200);
  void ctx;
}

/** Opens a custom popup control the way a person does and clicks the listed entry. */
export async function refPopup(page: Page, el: ElementHandle<Element>, value: string): Promise<void> {
  await el.click(T);
  await page.waitForTimeout(250);
  const byRole = page.getByRole("option", { name: value, exact: true });
  if ((await byRole.count()) > 0) return void (await byRole.first().click(T));
  const byItem = page.getByRole("menuitem", { name: value, exact: true });
  if ((await byItem.count()) > 0) return void (await byItem.first().click(T));
  const byButton = page.getByRole("button", { name: value, exact: true });
  if ((await byButton.count()) > 0) return void (await byButton.first().click(T));
  await page.getByText(value, { exact: true }).first().click(T);
}

export async function refStep(page: Page, s: Step, ctx: Ctx): Promise<void> {
  if (s.do === "press") {
    await page.keyboard.press(s.key ?? "Enter");
    await page.waitForTimeout(s.wait ?? 200);
    return;
  }
  if (s.do === "type") {
    await page.keyboard.type(s.text ?? "", { delay: 20 });
    await page.waitForTimeout(s.wait ?? 200);
    return;
  }
  const l = locate(page, s);
  const el = await l.elementHandle(T);
  if (!el) throw new Error(`no element for ${s.target ?? s.sel}`);
  await act(page, el, s, ctx, (e, v) => refPopup(page, e, v));
}
