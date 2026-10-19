import type { Page } from "@playwright/test";
import { expect, test } from "../support/harness.ts";
import { stopWorkers } from "../support/lifecycle.ts";
import { type Service, startService } from "../support/service.ts";
import { axNodes, clickInShell } from "../support/shell.ts";

interface Status {
  bootId: string;
  ops: Record<string, { state: string; reason?: string; result?: { ok: boolean; message?: string } }>;
  sessions: number[];
  transforms: Record<string, { epoch: string }>;
}

async function setup(env: Parameters<Parameters<typeof test>[2]>[0]["env"]) {
  const service = await startService(`chrome-extension://${env.extId}`);
  await env.send({ type: "grant", origin: env.a });
  expect(await env.send({ type: "connect", url: service.url })).toMatchObject({ ok: true });
  const page = await env.context.newPage();
  await page.goto(`${env.a}/ui/orders/o1/`);
  const tabId = await env.tabIdOf(page);
  expect(await env.send({ type: "transform", tabId })).toMatchObject({ ok: true, app: "orders-stage", state: "ready" });
  return { service, page, tabId };
}

/** Wraps sendMessage in the extension's isolated world so the test can see exactly what the shell sent. */
async function record(env: { panel: Page }, tabId: number): Promise<void> {
  await env.panel.evaluate(async (t) => {
    await chrome.scripting.executeScript({
      target: { tabId: t },
      func: () => {
        const w = globalThis as unknown as { __sent?: unknown[]; __wrapped?: boolean };
        if (w.__wrapped) return;
        w.__wrapped = true;
        w.__sent = [];
        const orig = chrome.runtime.sendMessage.bind(chrome.runtime);
        (chrome.runtime as unknown as { sendMessage: unknown }).sendMessage = (m: unknown) => {
          w.__sent?.push(structuredClone(m));
          return (orig as (m: unknown) => Promise<unknown>)(m);
        };
      },
    });
  }, tabId);
}

async function sent(env: { panel: Page }, tabId: number): Promise<Record<string, unknown>[]> {
  return env.panel.evaluate(async (t) => {
    const [r] = await chrome.scripting.executeScript({
      target: { tabId: t },
      func: () => (globalThis as unknown as { __sent?: unknown[] }).__sent ?? [],
    });
    return r?.result as Record<string, unknown>[];
  }, tabId);
}

async function resend(env: { panel: Page }, tabId: number, message: unknown) {
  return env.panel.evaluate(
    async ({ t, m }) => {
      const [r] = await chrome.scripting.executeScript({
        target: { tabId: t },
        func: (msg) => chrome.runtime.sendMessage(msg),
        args: [m],
      });
      return r?.result as Record<string, unknown>;
    },
    { t: tabId, m: message },
  );
}

const status = async (env: { send(m: unknown): Promise<Record<string, unknown>> }): Promise<Status> =>
  (await env.send({ type: "status" })) as unknown as Status;

const names = async (page: Page): Promise<string[]> => (await axNodes(page)).map((n) => n.name);

test("a click reaches the service once with a durable record behind it", async ({ env }) => {
  const { service, page } = await setup(env);
  try {
    expect((await axNodes(page)).find((n) => n.role === "button" && n.name === "Update stage")?.disabled).toBe(false);
    await clickInShell(page, "button", "Update stage");
    await expect.poll(() => service.seen.length).toBe(1);
    expect(service.seen[0]?.body).toMatchObject({ action: "set_stage", entity: "order", origin: env.a });
    const s = await status(env);
    const op = Object.values(s.ops);
    expect(op).toHaveLength(1);
    expect(op[0]).toMatchObject({ state: "done", result: { ok: true, message: "ran set_stage" } });
    await expect.poll(async () => (await names(page)).some((n) => n.includes("ran set_stage"))).toBe(true);
  } finally {
    await service.stop();
  }
});

test("the same request id is never forwarded twice, even with a fresh sequence number", async ({ env }) => {
  const { service, page, tabId } = await setup(env);
  try {
    await record(env, tabId);
    await clickInShell(page, "button", "Update stage");
    await expect.poll(() => service.seen.length).toBe(1);
    const [first] = await sent(env, tabId);
    expect(first).toMatchObject({ type: "dispatch", action: "set_stage" });
    expect(await resend(env, tabId, first)).toMatchObject({ ok: false, error: "replay" });
    const again = await resend(env, tabId, { ...first, seq: (first?.seq as number) + 10 });
    expect(again).toMatchObject({ ok: true, state: "done", replayed: true });
    expect(service.seen).toHaveLength(1);
  } finally {
    await service.stop();
  }
});

test("a worker killed mid-request leaves an unknown outcome and the service sees one request", async ({ env }) => {
  const { service, page } = await setup(env);
  try {
    service.delayMs = 2500;
    const before = (await status(env)).bootId;
    await clickInShell(page, "button", "Update stage");
    await expect.poll(() => service.seen.length).toBe(1);
    await stopWorkers(env.panel);
    await expect
      .poll(async () => (await names(page)).some((n) => n.includes("outcome is unknown")), { timeout: 20_000 })
      .toBe(true);
    const s = await status(env);
    expect(s.bootId).not.toBe(before);
    const op = Object.values(s.ops);
    expect(op).toHaveLength(1);
    expect(op[0]).toMatchObject({ state: "unknown", reason: "worker_restarted" });
    await new Promise((r) => setTimeout(r, 3000));
    expect(service.seen).toHaveLength(1);
  } finally {
    await service.stop();
  }
});

test("credentials from before a restart stop working and a reconnect issues new ones", async ({ env }) => {
  const { service, page, tabId } = await setup(env);
  try {
    await record(env, tabId);
    await clickInShell(page, "button", "Update stage");
    await expect.poll(() => service.seen.length).toBe(1);
    const [old] = await sent(env, tabId);
    await stopWorkers(env.panel);
    const stolen = { ...old, requestId: "req-stolen-0001", seq: 99 };
    expect(await resend(env, tabId, stolen)).toMatchObject({ ok: false, error: "stale_session" });
    expect((await status(env)).sessions).toEqual([]);

    // The shell reconnects by itself on its next action and gets a new key.
    await clickInShell(page, "button", "Update stage");
    await expect.poll(() => service.seen.length).toBe(2);
    expect((await status(env)).sessions).toEqual([tabId]);
    expect(await resend(env, tabId, { ...stolen, requestId: "req-stolen-0002", seq: 100 })).toMatchObject({
      ok: false,
      error: "stale_session",
    });
    expect(service.seen.map((s) => s.requestId)).not.toContain("req-stolen-0001");
    expect(service.seen.map((s) => s.requestId)).not.toContain("req-stolen-0002");
  } finally {
    await service.stop();
  }
});

test("a tab that moved to another origin cannot reconnect, and the page world cannot dispatch at all", async ({
  env,
}) => {
  const { service, page, tabId } = await setup(env);
  try {
    const epoch = (await status(env)).transforms[String(tabId)]?.epoch ?? "";
    const probe = await page.evaluate(
      () => typeof (window as unknown as { chrome?: { runtime?: unknown } }).chrome?.runtime,
    );
    expect(probe).toBe("undefined");
    await page.goto(`${env.b}/forms/`);
    const reply = await resend(env, tabId, { type: "reconnect", epoch });
    expect(reply).toMatchObject({ ok: false, error: "origin_changed" });
    expect(service.seen).toHaveLength(0);
  } finally {
    await service.stop();
  }
});

test("restore and revoke still work after a worker restart", async ({ env }) => {
  const { service, page, tabId } = await setup(env);
  try {
    await stopWorkers(env.panel);
    expect(await env.send({ type: "restore", tabId })).toMatchObject({ ok: true, restored: true });
    expect(await page.evaluate(() => document.querySelectorAll("[data-vsf-shell]").length)).toBe(0);
    expect(await env.send({ type: "transform", tabId })).toMatchObject({ ok: true, state: "ready" });
    expect(await env.send({ type: "revoke", origin: env.a })).toMatchObject({ ok: true });
    expect(await page.evaluate(() => document.querySelectorAll("[data-vsf-shell]").length)).toBe(0);
    expect((await status(env)).sessions).toEqual([]);
  } finally {
    await service.stop();
  }
});

export type { Service };
