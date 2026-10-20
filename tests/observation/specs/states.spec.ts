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
