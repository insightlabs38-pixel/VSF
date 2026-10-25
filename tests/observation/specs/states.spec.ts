import { expect, test } from "@playwright/test";
import { attach, startWorld } from "../support/world.ts";

const page1 = `<!doctype html><title>s</title><main>
<button id="b" aria-pressed="true">Bold</button>
<button aria-haspopup="menu" aria-expanded="false" aria-controls="m">Options</button>
<ul id="m" role="menu"><li role="menuitem">One</li></ul>
<input type="file" multiple aria-label="Attachment">
<input type="text" required value="secret-value" aria-label="Name">
<div role="radio" aria-checked="false">Monthly</div>
<span aria-pressed="javascript:alert(1)" role="button">Bad</span>
<details open><summary>More</summary>x</details>
</main>`;

test("states are allow-listed attribute tokens, never values or free text", async ({ page }) => {
  const world = await startWorld();
  world.a.pages.set("/", page1);
  const h = await attach(page, { allow: [world.a.url] });
  await page.goto(`${world.a.url}/`);
  await h.settle();
  const nodes = [...h.collector.replica.nodes.values()];
  const by = (pred: (n: (typeof nodes)[number]) => boolean) => nodes.find(pred);
  expect(by((n) => n.name === "Bold")?.states).toEqual(["pressed=true"]);
  const options = by((n) => n.name === "Options");
  expect(options?.states).toEqual(["expanded=false", "haspopup=menu"]);
  expect(options?.rels.some(([k]) => k === "controls")).toBe(true);
  expect(by((n) => n.name === "Attachment")?.states).toEqual(["multiple=true", "type=file"]);
  expect(by((n) => n.name === "Name")?.states).toEqual(["required=true", "type=text"]);
  expect(by((n) => n.role === "radio")?.states).toEqual(["checked=false"]);
  expect(by((n) => n.text === "Bad")?.states).toEqual([]);
  expect(by((n) => n.tag === "details")?.states).toEqual(["expanded=true"]);
  expect(JSON.stringify(h.collector.batches)).not.toContain("secret-value");
  await world.stop();
});

test("a changed state reaches the replica as a delta", async ({ page }) => {
  const world = await startWorld();
  world.a.pages.set("/", page1);
  const h = await attach(page, { allow: [world.a.url] });
  await page.goto(`${world.a.url}/`);
  await h.settle();
  await page.locator("#b").evaluate((el) => el.setAttribute("aria-pressed", "false"));
  await h.settle();
  const bold = [...h.collector.replica.nodes.values()].find((n) => n.name === "Bold");
  expect(bold?.states).toEqual(["pressed=false"]);
  await world.stop();
});

const virtual = `<!doctype html><title>v</title><main>
<div id="vp" style="height:100px;overflow:auto"><div role="grid" aria-label="Rows" id="g"></div></div>
<div id="vp2" style="height:100px;overflow:auto"><table id="t"><tbody></tbody></table></div>
<script>
const g = document.getElementById("g");
const render = (top) => {
  const first = Math.floor(top / 20);
  g.replaceChildren(...Array.from({ length: 6 }, (_, k) => {
    const row = document.createElement("div"); row.setAttribute("role", "row");
    const cell = document.createElement("div"); cell.setAttribute("role", "gridcell"); cell.textContent = "Row " + (first + k);
    row.append(cell); return row;
  }));
};
g.style.height = "20000px";
document.getElementById("vp").addEventListener("scroll", (e) => render(e.target.scrollTop));
render(0);
const tb = document.querySelector("#t tbody");
for (let i = 0; i < 8; i++) { const r = tb.insertRow(); r.insertCell().textContent = "Static " + i; }
</script></main>`;

test("a scroll probe moves any scrollable region and restores it, and refuses unknown ids", async ({ page }) => {
  const world = await startWorld();
  world.a.pages.set("/", virtual);
  const h = await attach(page, { allow: [world.a.url] });
  await page.goto(`${world.a.url}/`);
  await h.settle();
  const nodes = [...h.collector.replica.nodes.values()];
  const grid = nodes.find((n) => n.role === "grid");
  const table = nodes.find((n) => n.tag === "table");
  const probe = (id: number | undefined) =>
    page.evaluate((i) => {
      const s = (
        window as unknown as {
          __vsf: { session: { scrollProbe(i: number): { moved: boolean; restore(): void } | null } };
        }
      ).__vsf.session;
      const r = s.scrollProbe(i as number);
      const top = document.getElementById("vp")?.scrollTop ?? -1;
      r?.restore();
      return { moved: r?.moved ?? null, top, back: document.getElementById("vp")?.scrollTop };
    }, id);
  const g = await probe(grid?.id);
  expect(g.moved).toBe(true);
  expect(g.top).toBeGreaterThan(0);
  expect(g.back).toBe(0);
  expect((await probe(table?.id)).moved).toBe(true);
  expect(
    await page.evaluate(() =>
      (window as unknown as { __vsf: { session: { scrollProbe(i: number): unknown } } }).__vsf.session.scrollProbe(-5),
    ),
  ).toBe(null);
  await world.stop();
});
