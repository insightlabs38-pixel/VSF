import type { Page } from "@playwright/test";
import { type Apps, startApps } from "../support/apps.ts";
import { expect, test } from "../support/harness.ts";
import { stopWorkers } from "../support/lifecycle.ts";

let apps: Apps;
let other: Apps;
test.beforeAll(async () => {
  apps = await startApps();
  other = await startApps();
});
test.afterAll(async () => {
  await apps.close();
  await other.close();
});

const dump = async (
  env: Parameters<typeof stopWorkers>[0] extends never ? never : import("../support/harness.ts").Env,
) =>
  (await env.worker()).evaluate(async () =>
    JSON.stringify({ local: await chrome.storage.local.get(null), session: await chrome.storage.session.get(null) }),
  );

const hostAttrs = (page: Page): Promise<{ scheme: string | null; density: string | null }> =>
  page.evaluate(() => {
    const overlay = document.querySelector("[data-vsf-overlay]");
    const host = overlay?.shadowRoot?.querySelector("[data-vsf-host]");
    return { scheme: host?.getAttribute("data-scheme") ?? null, density: host?.getAttribute("data-density") ?? null };
  });

async function open(env: import("../support/harness.ts").Env, site: Apps, name = "table-orders") {
  const page = await env.context.newPage();
  await page.goto(`${site.origin}/apps/${name}`);
  await env.send({ type: "grant", origin: site.origin });
  const tabId = await env.tabIdOf(page);
  return { page, tabId };
}

test("preferences are kept per origin, sealed at rest, and applied again after a restart of everything", async ({
  env,
}) => {
  const a = await open(env, apps);
  expect(await env.send({ type: "transform", tabId: a.tabId })).toMatchObject({ ok: true, settings: "default" });
  expect(await hostAttrs(a.page)).toEqual({ scheme: "system", density: "comfortable" });

  await a.page.getByLabel("Theme").press("d");
  await a.page.getByLabel("Density").press("c");
  await expect.poll(() => hostAttrs(a.page)).toEqual({ scheme: "dark", density: "compact" });
  await expect.poll(async () => (await dump(env)).includes("vault:settings.")).toBe(true);

  const text = await dump(env);
  expect(text).not.toContain('"density"');
  expect(text).not.toContain("compact");
  expect(text).not.toContain('"scheme"');

  await env.send({ type: "restore", tabId: a.tabId });
  expect(await stopWorkers(env.panel)).toBeGreaterThan(0);
  expect(await env.send({ type: "transform", tabId: a.tabId })).toMatchObject({ ok: true, settings: "restored" });
  expect(await hostAttrs(a.page)).toEqual({ scheme: "dark", density: "compact" });
  await expect(a.page.getByLabel("Density")).toHaveValue("compact");

  const b = await open(env, other, "plain-form");
  expect(await env.send({ type: "transform", tabId: b.tabId })).toMatchObject({ ok: true, settings: "default" });
  expect(await hostAttrs(b.page)).toEqual({ scheme: "system", density: "comfortable" });
});

test("a settings record that was altered is not trusted: defaults apply and the user is told", async ({ env }) => {
  const a = await open(env, apps);
  await env.send({ type: "transform", tabId: a.tabId });
  await a.page.getByLabel("Theme").press("d");
  await expect.poll(async () => (await dump(env)).includes("vault:settings.")).toBe(true);
  await env.send({ type: "restore", tabId: a.tabId });

  await (await env.worker()).evaluate(async () => {
    const all = await chrome.storage.local.get(null);
    const key = Object.keys(all).find((k) => k.startsWith("vault:settings."));
    if (!key) throw new Error("no settings record");
    const rec = all[key] as { ct: string };
    rec.ct = rec.ct.startsWith("A") ? `B${rec.ct.slice(1)}` : `A${rec.ct.slice(1)}`;
    await chrome.storage.local.set({ [key]: rec });
  });
  expect(await env.send({ type: "transform", tabId: a.tabId })).toMatchObject({ ok: true, settings: "unreadable" });
  expect(await hostAttrs(a.page)).toEqual({ scheme: "system", density: "comfortable" });
});

test("a page script cannot change preferences by sending the controls events", async ({ env }) => {
  const a = await open(env, apps);
  await env.send({ type: "transform", tabId: a.tabId });
  await a.page.evaluate(() => {
    const select = document.querySelector("[data-vsf-overlay]")?.shadowRoot?.querySelector<HTMLSelectElement>("select");
    if (!select) throw new Error("no select");
    select.value = "dark";
    select.dispatchEvent(new Event("change", { bubbles: true }));
  });
  await a.page.waitForTimeout(500);
  expect(await hostAttrs(a.page)).toEqual({ scheme: "system", density: "comfortable" });
  expect((await dump(env)).includes("vault:settings.")).toBe(false);
});
