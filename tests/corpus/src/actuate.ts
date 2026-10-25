import type { ElementHandle, Page } from "@playwright/test";

const T = { timeout: 4000 };
const UNTYPABLE = ["date", "datetime-local", "time", "month", "week", "color", "range", "file"];

/** Types the way a person does, so key handlers fire; inputs that take no typing are filled. */
export async function typeInto(page: Page, el: ElementHandle<Element>, value: string): Promise<void> {
  const typable = await el.evaluate(
    (e, untypable) =>
      (e instanceof HTMLElement && e.isContentEditable) ||
      e instanceof HTMLTextAreaElement ||
      (e instanceof HTMLInputElement && !untypable.includes(e.type)),
    UNTYPABLE,
  );
  if (!typable) return el.fill(value, T);
  await el.click({ timeout: 2000 }).catch(() => el.focus());
  await page.keyboard.press("ControlOrMeta+a");
  await page.keyboard.press("Delete");
  if (value) await page.keyboard.type(value, { delay: 15 });
}

/** Sets a toggle; when the input itself is covered by a styled replacement, a person clicks its visible label. */
export async function setToggle(el: ElementHandle<Element>, checked: boolean): Promise<void> {
  try {
    await el.setChecked(checked, { timeout: 1500 });
  } catch {
    const role = await el.evaluate((e) => e.getAttribute("role"));
    const now =
      role === "checkbox" || role === "switch" || role === "radio"
        ? (await el.evaluate((e) => e.getAttribute("aria-checked"))) === "true"
        : await el.evaluate((e) => (e as HTMLInputElement).checked === true);
    if (now === checked) return;
    const label = await el.evaluateHandle(
      (e) =>
        (e as HTMLInputElement).labels?.[0] ??
        e.closest("label") ??
        (e.id ? document.querySelector(`label[for="${e.id}"]`) : null),
    );
    const target = label.asElement() ?? el;
    await target.click(T);
  }
}
