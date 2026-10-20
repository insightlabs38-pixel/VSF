import { type Apps, startApps } from "../support/apps.ts";
import { type Companion, startCompanion } from "../support/companion.ts";
import { expect, test } from "../support/harness.ts";
import { status } from "../support/transform.ts";

let apps: Apps;
test.beforeAll(async () => {
  apps = await startApps();
});
test.afterAll(async () => {
  await apps.close();
});

const bridgeOf = async (env: Parameters<typeof status>[0]): Promise<unknown> => (await status(env)).sessions[0]?.bridge;
const hits = (c: Companion, path: string, code: number): number =>
  c.log.filter((l) => l.path === path && l.status === code && l.origin.startsWith("chrome-extension://")).length;

test("a paired extension gets its session bound by the companion and ends it on restore", async ({ env }) => {
  const companion = await startCompanion(`chrome-extension://${env.extId}`);
  try {
    expect(await env.send({ type: "pair", endpoint: companion.endpoint, code: companion.code })).toMatchObject({
      ok: true,
    });
    const page = await env.context.newPage();
    await page.goto(`${apps.origin}/apps/table-orders`);
    await env.send({ type: "grant", origin: apps.origin });
    const tabId = await env.tabIdOf(page);
    expect(await env.send({ type: "transform", tabId })).toMatchObject({ ok: true });
    const bound = (await bridgeOf(env)) as { id: string; expiresAt: number };
    expect(bound.id).toMatch(/^bs_[0-9a-f]{24}$/);
    expect(bound.expiresAt * 1000).toBeGreaterThan(Date.now());
    expect(hits(companion, "/v1/session", 200)).toBe(1);

    expect(await env.send({ type: "restore", tabId })).toMatchObject({ ok: true, restored: true });
    await expect.poll(() => hits(companion, "/v1/session/end", 200)).toBe(1);
  } finally {
    await companion.stop();
  }
});

test("without a pairing, or with the companion gone, the page is still transformed and the reason is recorded", async ({
  env,
}) => {
  const page = await env.context.newPage();
  await page.goto(`${apps.origin}/apps/plain-form`);
  await env.send({ type: "grant", origin: apps.origin });
  const tabId = await env.tabIdOf(page);
  expect(await env.send({ type: "transform", tabId })).toMatchObject({ ok: true });
  expect(await bridgeOf(env)).toBeFalsy();

  const companion = await startCompanion(`chrome-extension://${env.extId}`);
  expect(await env.send({ type: "pair", endpoint: companion.endpoint, code: companion.code })).toMatchObject({
    ok: true,
  });
  await companion.stop();
  expect(await env.send({ type: "transform", tabId })).toMatchObject({ ok: true });
  expect(await bridgeOf(env)).toEqual({ error: "bridge_unreachable" });
});

test("unpairing revokes the pairing on the companion and later pages are not bound", async ({ env }) => {
  const companion = await startCompanion(`chrome-extension://${env.extId}`);
  try {
    await env.send({ type: "pair", endpoint: companion.endpoint, code: companion.code });
    expect(await env.send({ type: "link" })).toMatchObject({ ok: true, paired: true });
    expect(await env.send({ type: "unpair" })).toMatchObject({ ok: true, companionNotified: true });
    expect(hits(companion, "/v1/pair/revoke", 200)).toBe(1);
    expect(await env.send({ type: "link" })).toMatchObject({ ok: true, paired: false });
    const page = await env.context.newPage();
    await page.goto(`${apps.origin}/apps/table-orders`);
    await env.send({ type: "grant", origin: apps.origin });
    expect(await env.send({ type: "transform", tabId: await env.tabIdOf(page) })).toMatchObject({ ok: true });
    expect(await bridgeOf(env)).toBeFalsy();
  } finally {
    await companion.stop();
  }
});

test("the stored credentials are sealed: no secret appears in any extension storage area", async ({ env }) => {
  const companion = await startCompanion(`chrome-extension://${env.extId}`);
  try {
    await env.send({ type: "pair", endpoint: companion.endpoint, code: companion.code });
    const dump = await (await env.worker()).evaluate(async () =>
      JSON.stringify({ local: await chrome.storage.local.get(null), session: await chrome.storage.session.get(null) }),
    );
    expect(dump).not.toContain(companion.code.replaceAll("-", ""));
    expect(dump).toContain("vault:bridge");
    expect(dump).not.toMatch(/"(refresh|access)"\\s*:/);
    const sealed = JSON.parse(dump) as { local: Record<string, { ct?: string }> };
    expect(sealed.local["vault:bridge"]?.ct).toBeTruthy();
  } finally {
    await companion.stop();
  }
});
