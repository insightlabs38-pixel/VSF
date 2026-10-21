import type { Locator, Page as PwPage } from "@playwright/test";
import type { Annotation } from "./annotation.ts";
import { norm } from "./match.ts";
import type { Expect, Step } from "./operations.ts";

const FIELD_ROLES: Record<string, string[]> = {
  text: ["textbox", "searchbox", "combobox", "spinbutton"],
  choice: ["combobox", "listbox", "radio", "menuitemradio", "option"],
  toggle: ["checkbox", "radio", "switch", "menuitemcheckbox"],
  range: ["slider", "spinbutton"],
  file: ["button", "textbox"],
  date: ["textbox", "combobox", "spinbutton"],
  other: ["textbox", "combobox", "checkbox", "button"],
};
const ACTION_ROLES = ["button", "link", "menuitem", "tab", "option", "menuitemcheckbox", "treeitem", "columnheader"];
const COLLECTION_ROLES = ["table", "grid", "treegrid", "list", "listbox", "tree"];

type Kind = "field" | "action" | "collection" | "composite";

/** The annotated item a name refers to; a name shared by a field and an action prefers the field. */
function itemOf(a: Annotation, name: string): { kind: Kind; fieldKind?: string } {
  const n = norm(name);
  const f = a.fields.find((x) => norm(x.name) === n);
  if (f) return { kind: "field", fieldKind: f.kind };
  if (a.actions.some((x) => norm(x.name) === n)) return { kind: "action" };
  if (a.collections.some((x) => norm(x.name) === n)) return { kind: "collection" };
  return { kind: "composite" };
}

/** Where a named item is, found by role and accessible name only, the way an assistive technology would. */
export function locateAll(page: PwPage, a: Annotation, name: string): Locator {
  const item = itemOf(a, name);
  const roles =
    item.kind === "field"
      ? (FIELD_ROLES[item.fieldKind ?? "other"] ?? FIELD_ROLES.other ?? [])
      : item.kind === "action"
        ? ACTION_ROLES
        : item.kind === "collection"
          ? COLLECTION_ROLES
          : [...ACTION_ROLES, "tablist", "dialog", "menu", "navigation", "region", "group"];
  let loc: Locator | null = null;
  for (const role of roles) {
    const l = page.getByRole(role as never, { name, exact: true });
    loc = loc ? loc.or(l) : l;
  }
  if (item.kind === "field" && loc) loc = loc.or(page.getByLabel(name, { exact: true }));
  if (item.kind === "action" && loc) loc = loc.or(page.getByText(name, { exact: true }));
  return loc as Locator;
}

export const locate = (page: PwPage, a: Annotation, name: string): Locator => locateAll(page, a, name).first();

const SETTLE_MS = 200;

/** Performs one annotated step through the page's accessibility surface alone. */
export async function referenceStep(page: PwPage, a: Annotation, s: Step): Promise<void> {
  if (s.do === "press") {
    await page.keyboard.press(s.key);
  } else {
    const loc = locate(page, a, s.target);
    const t = { timeout: 4000 };
    if (s.do === "fill") await loc.fill(s.value, t);
    else if (s.do === "click") await loc.click(t);
    else if (s.do === "toggle") await loc.setChecked(s.checked, t);
    else if (s.do === "choose") {
      const tag = await loc.evaluate((e) => e.tagName, undefined, t);
      if (tag === "SELECT") await loc.selectOption({ label: s.value }, t);
      else {
        await loc.click(t);
        await page.getByRole("option", { name: s.value, exact: true }).first().click(t);
      }
    } else if (s.do === "upload") {
      const file = { name: s.value, mimeType: "text/plain", buffer: Buffer.from(s.text) };
      const isInput = await loc.evaluate((e) => e instanceof HTMLInputElement && e.type === "file", undefined, t);
      if (isInput) await loc.setInputFiles(file, t);
      else {
        const [chooser] = await Promise.all([page.waitForEvent("filechooser", t), loc.click(t)]);
        await chooser.setFiles(file);
      }
    }
  }
  await page.waitForTimeout(SETTLE_MS);
}

async function rowCount(loc: Locator): Promise<number> {
  return loc.evaluate((root) => {
    const tabular = ["table", "grid", "treegrid"].includes(root.getAttribute("role") ?? root.tagName.toLowerCase());
    if (tabular) {
      const rows = [...root.querySelectorAll('tr, [role="row"]')];
      return rows.filter(
        (r) =>
          !r.querySelector('th, [role="columnheader"]') &&
          r.querySelector('td, [role="gridcell"], [role="cell"], [role="rowheader"]') &&
          !r.closest("thead, tfoot"),
      ).length;
    }
    return root.querySelectorAll('li, [role="listitem"], [role="option"], [role="treeitem"]').length;
  });
}

const escapeRe = (t: string): string => t.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

/** How many visible elements show the text, case-sensitively; content that is hidden from a person does not count. */
async function shown(page: PwPage, text: string): Promise<number> {
  return page
    .getByText(new RegExp(escapeRe(text)))
    .locator("visible=true")
    .count();
}

/** Reads one expectation from the page. Retries briefly so asynchronous updates are not misjudged. */
export async function holds(page: PwPage, a: Annotation, e: Expect): Promise<boolean> {
  const check = async (): Promise<boolean> => {
    if (e.kind === "text") return (await shown(page, e.contains)) > 0;
    if (e.kind === "absent") return (await shown(page, e.contains)) === 0;
    if (e.kind === "fragment") return page.url().includes(e.contains);
    const loc = locate(page, a, e.target);
    if (e.kind === "value") return (await loc.inputValue({ timeout: 1500 })) === e.value;
    if (e.kind === "checked") return (await loc.isChecked({ timeout: 1500 })) === e.checked;
    return (await rowCount(loc)) === e.count;
  };
  for (let i = 0; i < 6; i++) {
    try {
      if (await check()) return true;
    } catch {
      /* not readable yet */
    }
    await page.waitForTimeout(250);
  }
  return false;
}

export async function allHold(page: PwPage, a: Annotation, es: Expect[]): Promise<boolean> {
  for (const e of es) if (!(await holds(page, a, e))) return false;
  return true;
}
