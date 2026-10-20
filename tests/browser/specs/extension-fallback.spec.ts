import type { Page } from "@playwright/test";
import { type Apps, startApps } from "../support/apps.ts";
import { expect, test } from "../support/harness.ts";
import { stopWorkers } from "../support/lifecycle.ts";
import { overlays, sourceHtml, status } from "../support/transform.ts";

let apps: Apps;
test.beforeAll(async () => {
  apps = await startApps();
});
test.afterAll(async () => {
  await apps.close();
});

async function transformed(env: import("../support/harness.ts").Env, name = "table-orders") {
  const page = await env.context.newPage();
  await page.goto(`${apps.origin}/apps/${name}`);
  await env.send({ type: "grant", origin: apps.origin });
  const tabId = await env.tabIdOf(page);
  const original = await sourceHtml(page);
  expect(await env.send({ type: "transform", tabId })).toMatchObject({ ok: true });
  expect(await overlays(page)).toBe(1);
  return { page, tabId, original };
}

const restored = async (page: Page, original: string): Promise<void> => {
  await expect.poll(() => overlays(page)).toBe(0);
  expect(await page.evaluate(() => document.documentElement.outerHTML)).toBe(original);
};

test("a crash in the extension's own code restores the source page", async ({ env }) => {
  const { page, tabId, original } = await transformed(env);
  const worker = await env.worker();
  await worker.evaluate(async (id) => {
    await chrome.scripting.executeScript({
      target: { tabId: id },
      func: () => {
        setTimeout(() => {
          throw new Error("injected fault in the extension world");
        }, 0);
      },
    });
  }, tabId);
  await restored(page, original);
  await expect.poll(async () => (await status(env)).sessions.length).toBe(0);
});

test("errors raised by the page itself do not take the interface away", async ({ env }) => {
  const { page } = await transformed(env);
  await page.evaluate(() => {
    setTimeout(() => {
      throw new Error("the page's own bug");
    }, 0);
    Promise.reject(new Error("the page's own rejection"));
  });
  await page.waitForTimeout(800);
  expect(await overlays(page)).toBe(1);
});

test("the escape chord restores the source page from the keyboard", async ({ env }) => {
  const { page, original } = await transformed(env);
  await page.keyboard.press("Alt+Shift+KeyO");
  await restored(page, original);
  await expect.poll(async () => (await status(env)).sessions.length).toBe(0);
});

test("a script cannot fake the escape chord or the buttons", async ({ env }) => {
  const { page } = await transformed(env);
  await page.evaluate(() => {
    const e = new KeyboardEvent("keydown", { key: "O", code: "KeyO", altKey: true, shiftKey: true, bubbles: true });
    window.dispatchEvent(e);
    document.dispatchEvent(e);
    for (const b of document.querySelectorAll("[data-vsf-overlay]"))
      for (const x of b.shadowRoot?.querySelectorAll("button") ?? []) x.click();
  });
  await page.waitForTimeout(500);
  expect(await overlays(page)).toBe(1);
});

test("a page that removes the interface is noticed and the worker forgets the session", async ({ env }) => {
  const { page, original } = await transformed(env);
  await page.evaluate(() => document.querySelector("[data-vsf-overlay]")?.remove());
  await expect.poll(async () => (await status(env)).sessions.length).toBe(0);
  await page.evaluate(() => {
    document.body.removeAttribute("inert");
    document.body.removeAttribute("aria-hidden");
  });
  expect(await sourceHtml(page)).toBe(original);
});

test("a worker that lost the session restores the page at the next heartbeat", async ({ env }) => {
  const { page, original } = await transformed(env);
  const worker = await env.worker();
  await worker.evaluate(() => chrome.storage.session.set({ sessions: {} }));
  await restored(page, original);
});

test("a session record written by a newer version is not trusted and the page is restored", async ({ env }) => {
  const { page, original } = await transformed(env);
  const worker = await env.worker();
  await worker.evaluate(async () => {
    const { sessions } = (await chrome.storage.session.get("sessions")) as { sessions: Record<string, { v?: number }> };
    for (const s of Object.values(sessions)) s.v = 99;
    await chrome.storage.session.set({ sessions });
  });
  await restored(page, original);
  await expect.poll(async () => (await status(env)).sessions.length).toBe(0);
});

test("the interface survives the worker being stopped and keeps working after it restarts", async ({ env }) => {
  const { page } = await transformed(env);
  const before = (await status(env)).sessions[0]?.id;
  expect(await stopWorkers(env.panel)).toBeGreaterThan(0);
  await page.getByRole("button", { name: "Refresh from source" }).click();
  await expect(page.getByRole("status").first()).toContainText(/Up to date|rows from the source/);
  const after = await status(env);
  expect(after.sessions.map((s) => s.id)).toEqual([before]);
  expect(await overlays(page)).toBe(1);
});

test("reloading the extension orphans the page script, which restores the page by itself", async ({ env }) => {
  const { page, original } = await transformed(env);
  const worker = await env.worker();
  await worker.evaluate(() => chrome.runtime.reload()).catch(() => undefined);
  await restored(page, original);
});
