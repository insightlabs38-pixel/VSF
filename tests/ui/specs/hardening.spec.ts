import { expect, type Page, test } from "@playwright/test";
import { formBoot } from "../support/config.ts";
import { open, type Site, sourcePage, startSite } from "../support/world.ts";

let site: Site;
test.beforeAll(async () => {
  site = await startSite();
});
test.afterAll(async () => {
  await site.stop();
});

const ev = <T>(page: Page, fn: string): Promise<T> =>
  page.evaluate(`(() => { const app = window.app; return (${fn})(app); })()`) as Promise<T>;

const tryBoot = (page: Page, cfg: unknown): Promise<string> =>
  page.evaluate((c) => {
    try {
      (window as unknown as { VSF: { boot(c: unknown): void } }).VSF.boot(c);
      return "booted";
    } catch (e) {
      return (e as Error).message;
    }
  }, cfg);

test("a presentation compiled for another semantic is refused before anything is drawn", async ({ page }) => {
  site.pages.set("/d", sourcePage());
  await page.goto(`${site.url}/d`);
  const msg = await tryBoot(page, { ...formBoot(), runtimeDigest: `sha256:${"ff".repeat(32)}` });
  expect(msg).toContain("semantic.digest_mismatch");
  expect(await page.evaluate(() => document.querySelector("#vsf-host")?.shadowRoot?.childElementCount ?? 0)).toBe(0);
});

test("the visible name of an operation comes from the semantic, not from the presentation", async ({ page }) => {
  await open(page, site, "/l", formBoot());
  await ev(
    page,
    `(a) => { const b = structuredClone(a.body()); for (const c of b.components) if (c.binding?.action === 'delete_order') c.accessibility.name = 'Archive'; a.renderer.update(b); }`,
  );
  const del = page.getByRole("button", { name: "Delete order" });
  await expect(del).toHaveCount(1);
  await expect(del).toHaveAttribute("data-label-mismatch", "true");
  await expect(page.getByRole("button", { name: "Archive" })).toHaveCount(0);
});

test("the shadow root is closed by default so page scripts cannot reach the controls", async ({ page }) => {
  await open(page, site, "/c", formBoot({ shadow: "closed" }));
  expect(await page.evaluate(() => document.querySelector("#vsf-host")?.shadowRoot)).toBeNull();
  expect(await ev<number>(page, "(a) => a.renderer.root.querySelectorAll('button').length")).toBeGreaterThan(0);
});

test("a script-generated click never runs a write; a real key press does; a read-only click is harmless", async ({
  page,
}) => {
  await open(page, site, "/s", formBoot({ shadow: "closed" }));
  await ev(page, "(a) => a.renderer.root.querySelector('[data-effect=destructive]').click()");
  await ev(
    page,
    "(a) => a.renderer.root.querySelector('[data-effect=conditional_write], [data-effect=idempotent_write], [data-effect=non_idempotent_write]')?.click()",
  );
  expect(await ev<unknown[]>(page, "(a) => a.dispatches")).toEqual([]);
  const said = await ev<string[]>(page, "(a) => a.renderer.announcer.log.map((l) => l.message)");
  expect(said.some((m) => /did not come from the user/.test(m))).toBe(true);
  await ev(page, "(a) => a.renderer.root.querySelector('[data-effect=destructive]').focus()");
  await page.keyboard.press("Enter");
  expect(await ev<{ action: string }[]>(page, "(a) => a.dispatches.map((d) => ({ action: d.action }))")).toEqual([
    { action: "delete_order" },
  ]);
  await ev(page, "(a) => a.renderer.root.querySelector('[data-effect=read_only]').click()");
  expect((await ev<unknown[]>(page, "(a) => a.dispatches")).length).toBe(2);
});

test("a harness may opt in to synthetic activations, and only then", async ({ page }) => {
  await open(page, site, "/y", formBoot({ synthetic: true }));
  await ev(page, "(a) => a.renderer.root.querySelector('[data-effect=destructive]').click()");
  expect((await ev<unknown[]>(page, "(a) => a.dispatches")).length).toBe(1);
});

test("cyclic, over-deep, dangling and oversized presentations are refused and leave the screen unchanged", async ({
  page,
}) => {
  await open(page, site, "/x", formBoot());
  const before = await ev<string>(page, "(a) => a.renderer.root.innerHTML");
  const attempts: Record<string, string> = {
    cycle: `(a) => { const b = structuredClone(a.body()); const f = b.components.find((c) => c.kind === 'form'); const g = b.components.find((c) => c.kind === 'group'); g.children.push(f.id); return b; }`,
    dangling: `(a) => { const b = structuredClone(a.body()); b.components[0].children.push('nope'); return b; }`,
    deep: `(a) => { const b = structuredClone(a.body()); let parent = b.components.find((c) => c.kind === 'group'); for (let i = 0; i < 80; i++) { const id = 'deep_' + i; b.components.push({ id, kind: 'group', children: [], accessibility: { role: 'group', name_source: 'explicit', name: 'd' } }); parent.children.push(id); parent = b.components[b.components.length - 1]; } return b; }`,
    huge: `(a) => { const b = structuredClone(a.body()); for (let i = 0; i < 10001; i++) b.components.push({ id: 'h' + i, kind: 'text', children: [], accessibility: { role: 'text', name_source: 'explicit', name: 'x' } }); return b; }`,
  };
  for (const [name, make] of Object.entries(attempts)) {
    const msg = await page.evaluate(
      `(() => { const app = window.app; try { app.renderer.update((${make})(app)); return 'accepted'; } catch (e) { return e.message; } })()`,
    );
    expect(msg, name).toContain("presentation rejected");
  }
  expect(await ev<string>(page, "(a) => a.renderer.root.innerHTML")).toBe(before);
});
