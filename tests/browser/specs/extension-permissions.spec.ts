import type { CDPSession, Page } from "@playwright/test";
import { expect, test } from "../support/harness.ts";

interface Ctx {
  id: number;
  name: string;
  origin: string;
  type: string | undefined;
}

/** Independent witness: lists the script execution contexts the browser created in a page. */
async function trackContexts(page: Page): Promise<{ session: CDPSession; contexts: Ctx[] }> {
  const session = await page.context().newCDPSession(page);
  const contexts: Ctx[] = [];
  session.on("Runtime.executionContextCreated", (e) => {
    const c = e.context as { id: number; name: string; origin: string; auxData?: { type?: string } };
    contexts.push({ id: c.id, name: c.name, origin: c.origin, type: c.auxData?.type });
  });
  await session.send("Runtime.enable");
  return { session, contexts };
}

const extWorlds = (contexts: Ctx[], extName: string): Ctx[] => contexts.filter((c) => c.name === extName);

test("per-origin grant and revoke gate observation, verified by browser-side execution contexts", async ({ env }) => {
  const pageA = await env.context.newPage();
  const pageB = await env.context.newPage();
  const watchA = await trackContexts(pageA);
  const watchB = await trackContexts(pageB);
  await pageA.goto(`${env.a}/forms/`);
  await pageB.goto(`${env.b}/forms/`);
  const [ta, tb] = [await env.tabIdOf(pageA), await env.tabIdOf(pageB)];
  const EXT = "Varifold (development build)";

  await env.send({ type: "grant", origin: env.a });
  expect(await env.send({ type: "capture", tabId: ta })).toMatchObject({ ok: true });
  expect(await env.send({ type: "capture", tabId: tb })).toMatchObject({ ok: false, error: "origin_not_granted" });
  expect(extWorlds(watchA.contexts, EXT).length).toBeGreaterThan(0);
  expect(extWorlds(watchB.contexts, EXT)).toHaveLength(0);

  const revoked = await env.send({ type: "revoke", origin: env.a });
  expect(revoked).toMatchObject({ ok: true });
  const before = extWorlds(watchA.contexts, EXT).length;
  expect(await env.send({ type: "capture", tabId: ta })).toMatchObject({ ok: false, error: "origin_not_granted" });
  expect(extWorlds(watchA.contexts, EXT)).toHaveLength(before);

  await env.send({ type: "grant", origin: env.b });
  expect(await env.send({ type: "capture", tabId: tb })).toMatchObject({ ok: true, origin: env.b });
  expect(await env.send({ type: "capture", tabId: ta })).toMatchObject({ ok: false, error: "origin_not_granted" });
});

test("malformed grants and commands are rejected without changing state", async ({ env }) => {
  const bad = [
    { type: "grant", origin: "http://127.0.0.1:1/path" },
    { type: "grant", origin: "https://*.example.test" },
    { type: "grant", origin: "javascript:alert(1)" },
    { type: "grant", origin: ["http://user", ":", "pw", "@127.0.0.1:1"].join("") },
    { type: "grant", origin: 42 },
    { type: "capture", tabId: -1 },
    { type: "capture", tabId: 1.5 },
    { type: "capture" },
    { type: "nope" },
    "string",
    null,
  ];
  for (const m of bad) expect(await env.send(m), JSON.stringify(m)).toMatchObject({ ok: false, error: "bad_request" });
  const status = (await env.send({ type: "status" })) as { grants: string[] };
  expect(status.grants).toEqual([]);
});

test("page-origin misuse cannot grant, observe or reach the extension", async ({ env }) => {
  const hostile = await env.context.newPage();
  await hostile.goto(`${env.a}/hostile/?ext=${env.extId}`);
  await hostile.waitForSelector("html[data-attempts=done]");
  const attempts = JSON.parse((await hostile.locator("#log").textContent()) ?? "[]") as {
    name: string;
    outcome: string;
  }[];
  const byName = Object.fromEntries(attempts.map((a) => [a.name, a.outcome]));
  expect(byName["chrome.runtime.sendMessage available"]).toBe("false");
  expect(byName["fetch chrome-extension panel"]).toMatch(/^rejected/);
  expect(byName["fetch chrome-extension manifest"]).toMatch(/^rejected/);
  await hostile.waitForTimeout(300);
  const status = (await env.send({ type: "status" })) as {
    grants: string[];
    stats: { captures: number; executeCalls: object };
  };
  expect(status.grants).toEqual([]);
  expect(status.stats.captures).toBe(0);
  expect(status.stats.executeCalls).toEqual({});
});

test("a compromised content-script context cannot issue commands", async ({ env }) => {
  await env.send({ type: "grant", origin: env.a });
  const page = await env.context.newPage();
  await page.goto(`${env.a}/forms/`);
  const tabId = await env.tabIdOf(page);
  const worker = await env.worker();
  const reply = await worker.evaluate(async (id) => {
    const [r] = await chrome.scripting.executeScript({
      target: { tabId: id },
      func: () => chrome.runtime.sendMessage({ type: "grant", origin: "http://evil.example.test" }),
    });
    return r?.result;
  }, tabId);
  expect(reply).toMatchObject({ ok: false, error: "sender_not_allowed" });
  const status = (await env.send({ type: "status" })) as { grants: string[] };
  expect(status.grants).toEqual([env.a]);
});

test("finding: browser host permissions ignore the port component", async ({ env }, info) => {
  const page = await env.context.newPage();
  await page.goto(`${env.b}/forms/`);
  const tabId = await env.tabIdOf(page);
  const worker = await env.worker();
  const outcome = await worker.evaluate(async (id) => {
    try {
      await chrome.scripting.executeScript({ target: { tabId: id }, func: () => 1 });
      return "injected";
    } catch (e) {
      return `blocked: ${(e as Error).message.slice(0, 80)}`;
    }
  }, tabId);
  info.annotations.push({
    type: "finding",
    description: `same host, other port, scripting.executeScript -> ${outcome}`,
  });
  expect(outcome).toBe("injected");
});
