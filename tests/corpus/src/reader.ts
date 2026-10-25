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
export function locateAll(page: PwPage, a: Annotation, name: string, withText = true): Locator {
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
  if (item.kind === "action" && loc && withText) loc = loc.or(page.getByText(name, { exact: true }));
  return loc as Locator;
}

const FIELD_CONTROL =
  '*[self::input[not(@type="hidden")] or self::select or self::textarea or @role="textbox" or @role="combobox" or @role="listbox" or @role="spinbutton" or @role="searchbox" or @role="slider" or @role="checkbox" or @role="radio" or @role="switch"]';
const COLLECTION_CONTROL = '*[self::table or @role="table" or @role="grid" or @role="treegrid" or @role="tree"]';

/** The control a person reads next to a visible caption: the first matching element after the caption's text. */
function byCaption(page: PwPage, name: string, control: string): Locator {
  return page
    .locator(
      `xpath=//*[not(self::script or self::style)][normalize-space(text())=${xpathString(name)} or normalize-space(text())=${xpathString(`${name}:`)}]`,
    )
    .locator("visible=true")
    .first()
    .locator(`xpath=(descendant::${control} | following::${control})`)
    .locator("visible=true")
    .first();
}

const xpathString = (t: string): string =>
  t.includes("'")
    ? `concat(${t
        .split("'")
        .map((x) => `'${x}'`)
        .join(`, "'", `)})`
    : `'${t}'`;

/** An annotated item, found by accessible name, else by the caption a person would read beside it. */
export const locate = async (page: PwPage, a: Annotation, name: string): Promise<Locator> => {
  for (const found of [locateAll(page, a, name, false), locateAll(page, a, name)]) {
    const shown = found.locator("visible=true");
    if ((await shown.count()) > 0) return shown.first();
  }
  const byName = locateAll(page, a, name);
  if ((await byName.count()) > 0) return byName.first();
  const kind = itemOf(a, name).kind;
  if (kind === "field" || kind === "collection") {
    const cap = byCaption(
      page,
      name.replace(/[:*]\s*$/, "").trim(),
      kind === "field" ? FIELD_CONTROL : COLLECTION_CONTROL,
    );
    if ((await cap.count()) > 0) return cap;
    if (kind === "field") {
      const inTitled = page
        .getByTitle(name, { exact: true })
        .locator(`xpath=descendant::${FIELD_CONTROL}`)
        .locator("visible=true")
        .first();
      if ((await inTitled.count()) > 0) return inTitled;
    }
    if (kind === "collection") {
      const heading = page
        .locator(
          `xpath=//*[self::h1 or self::h2 or self::h3 or self::h4 or self::caption or self::legend][contains(normalize-space(.), ${xpathString(name)})]`,
        )
        .locator("visible=true")
        .first()
        .locator(`xpath=(descendant::${COLLECTION_CONTROL} | following::${COLLECTION_CONTROL})`)
        .locator("visible=true")
        .first();
      if ((await heading.count()) > 0) return heading;
      const only = page.locator(`xpath=//${COLLECTION_CONTROL}`).locator("visible=true");
      if ((await only.count()) === 1) return only.first();
    }
  }
  return byName.first();
};

const MEMBER_ROLES = [
  "tab",
  "menuitem",
  "menuitemcheckbox",
  "menuitemradio",
  "treeitem",
  "button",
  "link",
  "option",
  "columnheader",
  "row",
];

/** One member of a composite, named as the page shows it. */
export const locateItem = (page: PwPage, item: string): Locator => {
  let loc: Locator | null = null;
  for (const role of MEMBER_ROLES) {
    const l = page.getByRole(role as never, { name: item, exact: true });
    loc = loc ? loc.or(l) : l;
  }
  return (loc as Locator).first();
};

const SETTLE_MS = 200;

const choiceOption = (page: PwPage, value: string): Locator =>
  page
    .getByRole("option", { name: value, exact: true })
    .or(page.getByRole("menuitem", { name: value, exact: true }))
    .or(page.locator("li, [role=listitem], div, span").getByText(value, { exact: true }))
    .locator("visible=true")
    .first();

/** Types the way a person does, so key handlers fire; inputs that take no typing are filled. */
async function typeInto(loc: Locator, value: string, t: { timeout: number }): Promise<void> {
  const typable = await loc.evaluate(
    (e) =>
      (e instanceof HTMLElement && e.isContentEditable) ||
      e instanceof HTMLTextAreaElement ||
      (e instanceof HTMLInputElement &&
        !["date", "datetime-local", "time", "month", "week", "color", "range", "file"].includes(e.type)),
    undefined,
    t,
  );
  if (!typable) return loc.fill(value, t);
  await loc.click({ timeout: 2000 }).catch(() => loc.focus(t));
  await loc.press("ControlOrMeta+a", t);
  await loc.press("Delete", t);
  if (value) await loc.pressSequentially(value, { delay: 15, timeout: 8000 });
}

/** Picks an option from a popup the way a person does: open it, or type to narrow it, then click the entry. */
async function pickFromPopup(page: PwPage, loc: Locator, value: string, t: { timeout: number }): Promise<void> {
  await loc.click(t);
  if ((await choiceOption(page, value).count()) === 0) {
    await loc.pressSequentially(value, { delay: 20 }).catch(() => undefined);
    await page.waitForTimeout(300);
  }
  if ((await choiceOption(page, value).count()) === 0) {
    const open = page
      .getByRole("button", { name: /show all|open|toggle|select/i })
      .locator("visible=true")
      .first();
    if ((await open.count()) > 0) await open.click(t).catch(() => undefined);
  }
  await choiceOption(page, value).click(t);
}

/** Sets a toggle; when the input itself is covered (a styled replacement), a person clicks its visible label. */
async function setToggle(loc: Locator, checked: boolean, t: { timeout: number }): Promise<void> {
  try {
    await loc.setChecked(checked, { timeout: 1500 });
  } catch {
    const now = await loc.evaluate((e) => (e as HTMLInputElement).checked === true);
    if (now === checked) return;
    const label = await loc.evaluateHandle(
      (e) =>
        (e as HTMLInputElement).labels?.[0] ??
        e.closest("label") ??
        (e.id ? document.querySelector(`label[for="${e.id}"]`) : null),
    );
    const el = label.asElement();
    if (!el) throw new Error("no visible label to click");
    await el.click(t);
  }
}

/** Performs one annotated step through the page's accessibility surface alone. */
export async function referenceStep(page: PwPage, a: Annotation, s: Step): Promise<void> {
  if (s.do === "press") {
    await page.keyboard.press(s.key);
  } else if (s.do === "click" && s.item) {
    await locateItem(page, s.item).click({ timeout: 4000 });
  } else {
    const loc = await locate(page, a, s.target);
    const t = { timeout: 4000 };
    if (s.do === "fill") await typeInto(loc, s.value, t);
    else if (s.do === "click") await loc.click(t);
    else if (s.do === "toggle") await setToggle(loc, s.checked, t);
    else if (s.do === "choose") {
      const tag = await loc.evaluate((e) => e.tagName, undefined, t);
      if (tag === "SELECT") await loc.selectOption({ label: s.value }, t);
      else await pickFromPopup(page, loc, s.value, t);
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

/** What the page shows as a control's value: an input's value, a select's chosen label, else the widget's own text. */
async function shownValue(loc: Locator): Promise<string> {
  return loc.evaluate((e) => {
    if (e instanceof HTMLSelectElement) return e.selectedOptions[0]?.label ?? "";
    if (e.getAttribute("role") === "listbox") {
      const picked = [...e.querySelectorAll('[role="option"][aria-selected="true"]')];
      if (picked.length > 0) return picked.map((o) => (o.textContent ?? "").replace(/\s+/g, " ").trim()).join(", ");
    }
    if (e instanceof HTMLInputElement || e instanceof HTMLTextAreaElement) return e.value;
    return (
      e.getAttribute("aria-valuetext") ??
      e.getAttribute("aria-valuenow") ??
      (e.textContent ?? "").replace(/\s+/g, " ").trim()
    );
  });
}

async function rowCount(loc: Locator): Promise<number> {
  return loc.evaluate((root) => {
    const tabular = ["table", "grid", "treegrid"].includes(root.getAttribute("role") ?? root.tagName.toLowerCase());
    if (tabular) {
      const rows = [...root.querySelectorAll('tr, [role="row"]')];
      const headerCell = 'th:not([scope="row"]), [role="columnheader"]';
      const dataCell = 'td, th[scope="row"], [role="gridcell"], [role="cell"], [role="rowheader"]';
      return rows.filter(
        (r) =>
          (r as HTMLElement).offsetParent !== null &&
          !r.closest("thead, tfoot") &&
          !(r.children.length === 1 && Number(r.children[0]?.getAttribute("colspan")) > 1) &&
          r.querySelector(dataCell) &&
          !(r.querySelector(headerCell) && !r.querySelector('td, [role="gridcell"], [role="cell"]')),
      ).length;
    }
    return root.querySelectorAll('li, [role="listitem"], [role="option"], [role="treeitem"]').length;
  });
}

const escapeRe = (t: string): string => t.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

/** How many visible elements show the text, case-sensitively; hidden content and a page's own source listings do not count. */
async function shown(page: PwPage, text: string): Promise<number> {
  return page
    .getByText(new RegExp(escapeRe(text).replace(/ +/g, "\\s+")))
    .locator("visible=true")
    .evaluateAll((els) => els.filter((e) => !e.closest("pre, code")).length);
}

/** Reads one expectation from the page. Retries briefly so asynchronous updates are not misjudged. */
export async function holds(page: PwPage, a: Annotation, e: Expect): Promise<boolean> {
  const check = async (): Promise<boolean> => {
    if (e.kind === "text") return (await shown(page, e.contains)) > 0;
    if (e.kind === "absent") return (await shown(page, e.contains)) === 0;
    if (e.kind === "fragment") return page.url().includes(e.contains);
    const loc = await locate(page, a, e.target);
    if (e.kind === "value") return (await shownValue(loc)) === e.value;
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
