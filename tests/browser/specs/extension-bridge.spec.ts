import { createServer, request } from "node:http";
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

test("a program pretending to be the companion cannot get the code or be believed", async ({ env }) => {
  const received: string[] = [];
  const impostor = createServer((req, res) => {
    let body = "";
    req.on("data", (d: Buffer) => {
      body += d.toString();
    });
    req.on("end", () => {
      received.push(`${req.method} ${req.url} ${body}`);
      res.setHeader("content-type", "application/json");
      res.setHeader("access-control-allow-origin", req.headers.origin ?? "*");
      res.setHeader("access-control-allow-headers", "content-type, x-vsf-bridge, authorization");
      if (req.method === "OPTIONS") return res.writeHead(204).end();
      const rnd = (n: number): string =>
        Buffer.from(Array.from({ length: n }, () => Math.floor(Math.random() * 256))).toString("base64url");
      if (req.url === "/v1/pair/begin")
        return res
          .writeHead(200)
          .end(
            JSON.stringify({ offer_id: "o1", salt: rnd(16), nonce_s: rnd(16), iterations: 100000, expires_at: 9e9 }),
          );
      res
        .writeHead(200)
        .end(
          JSON.stringify({
            client_id: "cli_fake",
            refresh: rnd(32),
            refresh_expires: 9e9,
            access: rnd(32),
            access_expires: 9e9,
            proof: rnd(32),
          }),
        );
    });
  });
  await new Promise<void>((r) => impostor.listen(0, "127.0.0.1", r));
  const port = (impostor.address() as { port: number }).port;
  try {
    const code = "ABCD-EFGH-JKMN-PQRS";
    const reply = await env.send({ type: "pair", endpoint: `127.0.0.1:${port}`, code });
    expect(reply).toMatchObject({ ok: false, error: "server_not_authentic" });
    expect(await env.send({ type: "link" })).toMatchObject({ ok: true, paired: false });
    const everything = received.join("\n");
    expect(everything).not.toContain("ABCD");
    expect(everything).not.toContain("ABCDEFGHJKMNPQRS");
    expect(received.some((r) => r.includes("/v1/pair/finish"))).toBe(true);
  } finally {
    impostor.close();
    impostor.closeAllConnections();
  }
});

test("a local program that claims the extension's origin but does not know the code is turned away", async ({
  env,
}) => {
  const companion = await startCompanion(`chrome-extension://${env.extId}`);
  try {
    const post = async (path: string, body: object): Promise<{ status: number; json: Record<string, unknown> }> => {
      const res = await new Promise<{ status: number; text: string }>((resolve, reject) => {
        const req = request(
          {
            host: "127.0.0.1",
            port: companion.port,
            path,
            method: "POST",
            headers: {
              "content-type": "application/json",
              "x-vsf-bridge": "1",
              origin: `chrome-extension://${env.extId}`,
            },
          },
          (r) => {
            let text = "";
            r.on("data", (d: Buffer) => {
              text += d.toString();
            });
            r.on("end", () => resolve({ status: r.statusCode ?? 0, text }));
          },
        );
        req.on("error", reject);
        req.end(JSON.stringify(body));
      });
      return { status: res.status, json: JSON.parse(res.text) as Record<string, unknown> };
    };
    for (let i = 0; i < 3; i++) {
      const begin = await post("/v1/pair/begin", { nonce: Buffer.alloc(24, i + 1).toString("base64url") });
      expect(begin.status).toBe(200);
      const guess = await post("/v1/pair/finish", {
        offer_id: begin.json.offer_id,
        proof: Buffer.alloc(32, 7).toString("base64url"),
        name: "spoof",
      });
      expect(guess.status).toBe(403);
    }
    expect(await env.send({ type: "pair", endpoint: companion.endpoint, code: companion.code })).toMatchObject({
      ok: false,
      error: "pair_denied",
    });
    expect(await env.send({ type: "link" })).toMatchObject({ ok: true, paired: false });
  } finally {
    await companion.stop();
  }
});
