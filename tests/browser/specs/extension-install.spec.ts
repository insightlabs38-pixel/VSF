import { startServer } from "@vsf/fixture-apps/server";
import { createEnv } from "../support/env.ts";
import { expect, test } from "../support/harness.ts";

interface Held {
  permissions?: string[];
  origins?: string[];
}

const held = (panel: import("@playwright/test").Page): Promise<Held> =>
  panel.evaluate(() => chrome.permissions.getAll());

test("the product install holds no site access and observation is impossible until the user grants it", async () => {
  const site = await startServer(0);
  const port = Number(new URL(site.url).port);
  const { env, dispose } = await createEnv({ a: port, b: port }, { hostPermissions: [] });
  try {
    const now = await held(env.panel);
    expect([...(now.permissions ?? [])].sort()).toEqual(["activeTab", "scripting", "storage"]);
    expect(now.origins ?? []).toEqual([]);

    const page = await env.context.newPage();
    await page.goto(`${env.a}/forms/`);
    const tabId = await env.tabIdOf(page);
    expect(await env.send({ type: "grant", origin: env.a })).toMatchObject({ ok: false, error: "permission_not_held" });
    const refused = { ok: false, error: "origin_unobservable" };
    expect(await env.send({ type: "capture", tabId })).toMatchObject(refused);
    expect(await env.send({ type: "observe", tabId })).toMatchObject(refused);
    const status = (await env.send({ type: "status" })) as { grants: string[]; stats: { executeCalls: object } };
    expect(status.grants).toEqual([]);
    expect(status.stats.executeCalls).toEqual({});
  } finally {
    await dispose();
    site.server.close();
  }
});

test("a grant needs the matching browser permission, not only the request", async ({ env }) => {
  expect(await env.send({ type: "grant", origin: env.a })).toMatchObject({ ok: true });
  expect(await env.send({ type: "grant", origin: env.c })).toMatchObject({ ok: false, error: "permission_not_held" });
  const status = (await env.send({ type: "status" })) as { grants: string[] };
  expect(status.grants).toEqual([env.a]);
});

test("the installed package serves only packaged pages and its worker is a module with no static content scripts", async ({
  env,
}) => {
  const manifest = await env.panel.evaluate(() => chrome.runtime.getManifest());
  expect(manifest.manifest_version).toBe(3);
  expect("content_scripts" in manifest).toBe(false);
  expect("externally_connectable" in manifest).toBe(false);
  expect("web_accessible_resources" in manifest).toBe(false);
  const page = await env.context.newPage();
  await page.goto(`${env.a}/forms/`);
  const reach = await page.evaluate(async (id) => {
    try {
      await fetch(`chrome-extension://${id}/background.js`);
      return "fetched";
    } catch {
      return "blocked";
    }
  }, env.extId);
  expect(reach).toBe("blocked");
});
