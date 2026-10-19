import { expect, test } from "@playwright/test";
import type { CanonNode } from "@vsf/observer";
import { labelsPage, PRIVATE_VALUE } from "../support/pages.ts";
import { attach, startWorld, truthOf } from "../support/world.ts";

const flat = (nodes: CanonNode[]): CanonNode[] => nodes.flatMap((n) => [n, ...flat(n[7])]);
const FLAG = { visible: 1, interactive: 2, editable: 4, disabled: 16, rtl: 64 };

async function load(page: import("@playwright/test").Page) {
  const world = await startWorld();
  world.a.pages.set("/", labelsPage);
  await page.addInitScript(() => {
    const w = window as unknown as { __layoutReads: number };
    w.__layoutReads = 0;
    const count = () => {
      w.__layoutReads++;
    };
    const rect = Element.prototype.getBoundingClientRect;
    Element.prototype.getBoundingClientRect = function (this: Element) {
      count();
      return rect.call(this);
    };
    const style = window.getComputedStyle;
    window.getComputedStyle = function (this: Window, ...a: Parameters<typeof style>) {
      count();
      return style.apply(this, a);
    };
  });
  const h = await attach(page, { allow: [world.a.url] });
  await page.goto(`${world.a.url}/`);
  await h.settle();
  const all = () => flat(h.collector.replica.canonical());
  return { world, h, all };
}

const named = (nodes: CanonNode[], name: string) => nodes.find((n) => n[2] === name);

test("names, labels and relations survive RTL, nesting and disabled controls", async ({ page }) => {
  const { world, h, all } = await load(page);
  const nodes = all();
  expect(h.collector.replica.state).toBe("synced");
  const arabic = named(nodes, "الاسم");
  expect(arabic?.[1]).toBe("textbox");
  expect((arabic?.[4] ?? 0) & FLAG.editable).toBeTruthy();
  expect((arabic?.[4] ?? 0) & FLAG.rtl).toBeTruthy();
  expect(named(nodes, "Wrapped")?.[1]).toBe("textbox");
  const disabled = named(nodes, "disabled child");
  expect((disabled?.[4] ?? 0) & FLAG.disabled).toBeTruthy();
  expect((named(nodes, "Go")?.[4] ?? 0) & FLAG.disabled).toBeTruthy();
  expect((arabic?.[4] ?? 0) & FLAG.disabled).toBe(0);
  const labelled = named(nodes, "First Second");
  expect(labelled?.[1]).toBe("button");
  expect(labelled?.[5].map((r) => r[0])).toEqual(["labelledby", "labelledby"]);
  const order = nodes.map((n) => n[2]);
  expect(labelled?.[5].every(([, pos]) => pos >= 0 && pos < nodes.length)).toBe(true);
  expect(nodes.filter((n) => n[3] === "First" || n[3] === "Second")).toHaveLength(2);
  expect(order).not.toContain("ignored content");
  const described = named(nodes, "described");
  expect(described?.[5].map((r) => r[0])).toEqual(["describedby"]);
  const ltr = nodes.find((n) => n[3] === "English in rtl document");
  expect((ltr?.[4] ?? 0) & FLAG.rtl).toBe(0);
  expect(nodes.some((n) => n[1] === "button" && n[2] === "Nested bold name")).toBe(true);
  expect((nodes.find((n) => n[2] === "hidden button" || n[3] === "hidden button")?.[4] ?? 1) & FLAG.visible).toBe(0);
  expect(named(nodes, "Image button")?.[1]).toBe("button");
  expect(named(nodes, "Pick")?.[1]).toBe("combobox");
  await world.stop();
});

test("bidirectional marks and Unicode form are kept byte for byte and values are never read", async ({ page }) => {
  const { world, h, all } = await load(page);
  const nodes = all();
  expect(named(nodes, "‏مرحبا‎ Hello")).toBeDefined();
  expect(JSON.stringify(h.collector.batches)).not.toContain(PRIVATE_VALUE);
  const wrapped = nodes.find((n) => n[2] === "Wrapped");
  expect(wrapped?.[3]).toBeNull();
  await world.stop();
});

test("geometry is read only for the nodes asked about and carries a freshness stamp", async ({ page }) => {
  const { world, h } = await load(page);
  const reads = () => page.evaluate(() => (window as unknown as { __layoutReads: number }).__layoutReads);
  expect(await reads()).toBe(0);
  await page.evaluate(() => {
    const p = document.createElement("p");
    p.textContent = "added later";
    document.body.append(p);
  });
  await h.settle();
  expect(await reads()).toBe(0);
  const id = flat(h.collector.replica.canonical()).findIndex((n) => n[3] === "added later");
  expect(id).toBeGreaterThanOrEqual(0);
  const target = [...h.collector.replica.nodes.values()].find((n) => n.text === "added later");
  const probed = await page.evaluate(
    (ids) => (window as unknown as { __vsf: { session: { probe(i: number[]): unknown } } }).__vsf.session.probe(ids),
    [target?.id ?? -1, 999999],
  );
  const result = probed as { epoch: number; seq: number; items: { id: number; rect: { width: number } | null }[] };
  expect(await reads()).toBe(1);
  expect(result.items[0]?.rect?.width).toBeGreaterThan(0);
  expect(result.items[1]?.rect).toBeNull();
  expect(result.epoch).toBe(h.collector.replica.epoch);
  expect(result.seq).toBe(h.collector.replica.seq);
  await world.stop();
});

test("a label that changes updates every control named by it", async ({ page }) => {
  const { world, h } = await load(page);
  await page.evaluate(() => {
    (document.getElementById("t1") as HTMLElement).textContent = "Changed";
  });
  await h.settle();
  const nodes = flat(h.collector.replica.canonical());
  expect(named(nodes, "Changed Second")?.[1]).toBe("button");
  expect(named(nodes, "First Second")).toBeUndefined();
  await page.evaluate(() => {
    (document.getElementById("n") as HTMLElement).setAttribute("disabled", "");
  });
  await h.settle();
  const after = flat(h.collector.replica.canonical());
  expect((named(after, "الاسم")?.[4] ?? 0) & FLAG.disabled).toBeTruthy();
  await world.stop();
});

test("labels that appear, disappear or retarget rename the controls they name", async ({ page }) => {
  const { world, h, all } = await load(page);
  await page.evaluate(() => {
    const b = document.createElement("button");
    b.id = "late-btn";
    b.type = "button";
    document.getElementById("f")?.append(b);
  });
  await h.settle();
  expect(all().some((n) => n[1] === "button" && n[2] === null && n[3] === null)).toBe(true);
  await page.evaluate(() => {
    const l = document.createElement("label");
    l.htmlFor = "late-btn";
    l.textContent = "Late label";
    document.getElementById("f")?.append(l);
  });
  await h.settle();
  expect(named(all(), "Late label")?.[1]).toBe("button");
  await page.evaluate(() => {
    const other = document.createElement("input");
    other.id = "other-in";
    document.getElementById("f")?.append(other);
    (document.querySelector('label[for="late-btn"]') as HTMLLabelElement).htmlFor = "other-in";
  });
  await h.settle();
  const after = all();
  expect(named(after, "Late label")?.[1]).toBe("textbox");
  expect(after.some((n) => n[1] === "button" && n[2] === "Late label")).toBe(false);
  await page.evaluate(() => document.querySelector('label[for="other-in"]')?.remove());
  await h.settle();
  expect(named(all(), "Late label")).toBeUndefined();
  expect(h.collector.replica.canonical()).toEqual(await truthOf(page));
  await world.stop();
});

test("naming thousands of controls that have ids stays linear in the page size", async ({ page }) => {
  const world = await startWorld();
  const rows = 16000;
  world.a.pages.set(
    "/",
    `<!doctype html><html lang="en"><body><label for="b0">first</label>${Array.from({ length: rows }, (_, i) => `<button id="b${i}" type="button">B${i}</button>`).join("")}</body></html>`,
  );
  const h = await attach(page, { allow: [world.a.url] });
  await page.goto(`${world.a.url}/`);
  await page.waitForLoadState("load");
  await h.settle();
  const ms = await page.evaluate(() => (window as unknown as { __vsfColdMs: number }).__vsfColdMs);
  expect(h.collector.replica.size).toBeGreaterThan(rows);
  expect(ms).toBeLessThan(2500);
  const labelled = [...h.collector.replica.nodes.values()].find((n) => n.tag === "button" && n.name === "first");
  expect(labelled).toBeDefined();
  await world.stop();
});
