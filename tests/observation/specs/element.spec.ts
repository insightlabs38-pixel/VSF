import { expect, test } from "@playwright/test";
import { attach, startWorld } from "../support/world.ts";

const html = `<!doctype html><title>e</title><main>
<button id="go">Go</button>
<input id="q" aria-label="Query">
<template><button>Never</button></template>
</main>`;

test("an observed node id leads back to its live element, and a stale id to nothing", async ({ page }) => {
  const world = await startWorld();
  world.a.pages.set("/", html);
  const h = await attach(page, { allow: [world.a.url] });
  await page.goto(`${world.a.url}/`);
  await h.settle();
  const nodes = [...h.collector.replica.nodes.values()];
  const go = nodes.find((n) => n.name === "Go" || n.text === "Go");
  const q = nodes.find((n) => n.name === "Query");
  if (!go || !q) throw new Error("fixture not observed");
  const idOf = (id: number) =>
    page.evaluate((i) => {
      const el = (
        window as unknown as { __vsf: { session: { elementOf(i: number): Element | null } } }
      ).__vsf.session.elementOf(i);
      return el ? `${el.tagName}#${el.id}` : null;
    }, id);
  expect(await idOf(go.id)).toBe("BUTTON#go");
  expect(await idOf(q.id)).toBe("INPUT#q");
  expect(await idOf(999999)).toBeNull();
  await page.locator("#go").evaluate((el) => el.remove());
  await h.settle();
  expect(await idOf(go.id)).toBeNull();
  await world.stop();
});
