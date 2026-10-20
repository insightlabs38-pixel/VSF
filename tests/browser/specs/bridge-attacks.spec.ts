import { request } from "node:http";
import { test as base, expect } from "@playwright/test";
import { startServer } from "@vsf/fixture-apps/server";
import { type Apps, startApps } from "../support/apps.ts";
import { type Companion, startCompanion } from "../support/companion.ts";
import { createEnv, type Env } from "../support/env.ts";

interface World {
  env: Env;
  companion: Companion;
  /** The attacker's own site: it sets no policy against itself. */
  attacker: Apps;
  /** An origin that resolves to loopback only inside this browser, standing in for an attacker's rebinding domain. */
  rebind: string;
}

const test = base.extend<{ world: World }>({
  // biome-ignore lint/correctness/noEmptyPattern: Playwright requires a destructuring first argument
  world: async ({}, use) => {
    const a = await startServer(0);
    const b = await startServer(0);
    const attacker = await startApps();
    attacker.pages.set("attacker", "<!doctype html><html lang=en><title>Attacker</title><body>hello</body></html>");
    const { env, dispose } = await createEnv(
      { a: Number(new URL(a.url).port), b: Number(new URL(b.url).port) },
      { args: ["--host-resolver-rules=MAP rebind.test 127.0.0.1"] },
    );
    const companion = await startCompanion(`chrome-extension://${env.extId}`, { devToken: true });
    await use({
      env,
      companion,
      attacker,
      rebind: `http://rebind.test:${new URL(attacker.origin).port}`,
    });
    await companion.stop();
    await dispose();
    await attacker.close();
    a.server.close();
    b.server.close();
  },
});

const hostile = (w: World) => ({
  base: `http://127.0.0.1:${w.companion.port}`,
  token: w.companion.token as string,
});

interface Attempt {
  name: string;
  outcome: string;
}

/** Runs inside a page the attacker controls; every attempt reports what the browser let through. */
async function barrage(page: import("@playwright/test").Page, base: string, token: string): Promise<Attempt[]> {
  return page.evaluate(
    async ({ base, token }) => {
      const out: { name: string; outcome: string }[] = [];
      const note = (name: string, outcome: string): void => void out.push({ name, outcome });
      const body = JSON.stringify({ nonce: "AAAAAAAAAAAAAAAAAAAAAA" });
      const tryFetch = async (name: string, url: string, init: RequestInit): Promise<void> => {
        try {
          const r = await fetch(url, init);
          note(name, `status ${r.status} type ${r.type}`);
        } catch (e) {
          note(name, `rejected ${(e as Error).name}`);
        }
      };
      await tryFetch("cors get", `${base}/v1/ping`, {});
      await tryFetch("cors get with token", `${base}/v1/ping`, { headers: { Authorization: `Bearer ${token}` } });
      await tryFetch("cors json post with csrf header", `${base}/v1/pair/begin`, {
        method: "POST",
        headers: { "content-type": "application/json", "x-vsf-bridge": "1" },
        body,
      });
      await tryFetch("no-cors text post", `${base}/v1/pair/begin`, {
        method: "POST",
        mode: "no-cors",
        headers: { "content-type": "text/plain" },
        body,
      });
      await tryFetch("no-cors get with credentials", `${base}/v1/ping?token=${token}`, {
        mode: "no-cors",
        credentials: "include",
      });
      await new Promise<void>((resolve) => {
        const f = document.createElement("form");
        f.method = "POST";
        f.action = `${base}/v1/pair/begin`;
        f.enctype = "text/plain";
        f.target = "hidden-frame";
        const i = document.createElement("input");
        i.name = body;
        i.value = "";
        f.append(i);
        const frame = document.createElement("iframe");
        frame.name = "hidden-frame";
        frame.onload = () => resolve();
        document.body.append(frame, f);
        f.submit();
        setTimeout(resolve, 1500);
      });
      note("form post", "submitted");
      for (const tag of ["img", "script", "link"] as const) {
        await new Promise<void>((resolve) => {
          const el = document.createElement(tag);
          el.onload = el.onerror = () => resolve();
          if (tag === "link") (el as HTMLLinkElement).rel = "stylesheet";
          (el as HTMLImageElement).src = (el as HTMLLinkElement).href = `${base}/v1/ping?token=${token}`;
          if (tag === "img" || tag === "script") (el as HTMLImageElement).src = `${base}/v1/ping?token=${token}`;
          document.head.append(el);
          setTimeout(resolve, 1500);
        });
        note(`${tag} element`, "requested");
      }
      await new Promise<void>((resolve) => {
        try {
          const ws = new WebSocket(`${base.replace("http", "ws")}/v1/ws`);
          ws.onopen = () => {
            note("websocket", "OPEN");
            resolve();
          };
          ws.onerror = () => {
            note("websocket", "error");
            resolve();
          };
          setTimeout(resolve, 1500);
        } catch (e) {
          note("websocket", `threw ${(e as Error).name}`);
          resolve();
        }
      });
      return out;
    },
    { base, token },
  );
}

test("a hostile web page cannot reach any privileged bridge action by fetch, form, element or socket", async ({
  world: w,
}) => {
  const { env, companion } = w;
  const page = await env.context.newPage();
  await page.goto(`${w.attacker.origin}/apps/attacker`);
  const h = hostile(w);
  const before = companion.log.length;
  const attempts = await barrage(page, h.base, h.token);
  await page.waitForTimeout(500);

  const byName = Object.fromEntries(attempts.map((a) => [a.name, a.outcome]));
  expect(byName["cors get"]).toMatch(/^rejected/);
  expect(byName["cors get with token"]).toMatch(/^rejected/);
  expect(byName["cors json post with csrf header"]).toMatch(/^rejected/);
  expect(byName["no-cors text post"]).toMatch(/type opaque|rejected/);
  expect(byName.websocket).not.toBe("OPEN");

  await expect
    .poll(() => companion.log.length - before, {
      message: "the companion must have seen the traffic for its silence to mean anything",
    })
    .toBeGreaterThan(5);
  const seen = companion.log.slice(before);
  const succeeded = seen.filter((l) => l.status < 400);
  expect(succeeded, JSON.stringify(succeeded)).toEqual([]);
  for (const l of seen) expect(l.origin === "" || l.origin === w.attacker.origin).toBe(true);
  const corsPreflights = seen.filter((l) => l.method === "OPTIONS");
  for (const l of corsPreflights) expect(l.status).toBe(403);

  const status = (await env.send({ type: "link" })) as Record<string, unknown>;
  expect(status).toMatchObject({ ok: true, paired: false });
});

test("the pairing offer survives hostile traffic and still completes for the extension", async ({ world: w }) => {
  const { env, companion } = w;
  const page = await env.context.newPage();
  await page.goto(`${w.attacker.origin}/apps/attacker`);
  const h = hostile(w);
  for (let i = 0; i < 3; i++) await barrage(page, h.base, h.token);
  const paired = await env.send({ type: "pair", endpoint: companion.endpoint, code: companion.code });
  expect(paired).toMatchObject({ ok: true });
  expect(await env.send({ type: "link" })).toMatchObject({ ok: true, paired: true, endpoint: companion.endpoint });
  const ours = companion.log.filter((l) => l.origin === `chrome-extension://${env.extId}`);
  expect(ours.filter((l) => l.path === "/v1/pair/finish" && l.status === 200)).toHaveLength(1);
});

test("a DNS-rebinding name that resolves to the bridge never gets past the Host check", async ({ world: w }) => {
  const { env, companion } = w;
  const page = await env.context.newPage();
  await page.goto(`${w.rebind}/apps/attacker`);
  const before = companion.log.length;
  const outcome = await page.evaluate(async (port) => {
    try {
      const r = await fetch(`http://rebind.test:${port}/v1/ping`, { headers: { Authorization: "Bearer x" } });
      return `status ${r.status}`;
    } catch (e) {
      return `rejected ${(e as Error).name}`;
    }
  }, companion.port);
  expect(outcome).toMatch(/^rejected/);
  await page.waitForTimeout(300);
  expect(companion.log.slice(before).every((l) => l.status === 403)).toBe(true);

  const sent = await new Promise<{ status: number; body: string }>((resolve, reject) => {
    const req = request(
      {
        host: "127.0.0.1",
        port: companion.port,
        path: "/v1/ping",
        headers: {
          Host: `rebind.test:${companion.port}`,
          Origin: w.rebind,
          Authorization: `Bearer ${companion.token}`,
        },
      },
      (res) => {
        let body = "";
        res.on("data", (d: Buffer) => {
          body += d.toString();
        });
        res.on("end", () => resolve({ status: res.statusCode ?? 0, body }));
      },
    );
    req.on("error", reject);
    req.end();
  });
  expect(sent.status).toBe(403);
  expect(sent.body).toContain("bridge.host_not_allowed");
  const logged = companion.log.filter((l) => l.host === `rebind.test:${companion.port}`);
  expect(logged.length).toBeGreaterThan(0);
  expect(logged.every((l) => l.status === 403)).toBe(true);
});

test("a content script running inside a hostile page speaks with the page's origin and gets nowhere", async ({
  world: w,
}) => {
  const { env, companion } = w;
  const page = await env.context.newPage();
  await page.goto(`${w.attacker.origin}/apps/attacker`);
  const tabId = await env.tabIdOf(page);
  const worker = await env.worker();
  await env.send({ type: "grant", origin: env.a }).catch(() => undefined);
  const before = companion.log.length;
  const h = hostile(w);
  const result = await worker.evaluate(
    async ({ id, base, token }) => {
      const [r] = await chrome.scripting.executeScript({
        target: { tabId: id },
        func: async (b: string, t: string) => {
          const out: string[] = [];
          for (const init of [
            { headers: { Authorization: `Bearer ${t}` } },
            {
              method: "POST",
              headers: { Authorization: `Bearer ${t}`, "content-type": "application/json", "x-vsf-bridge": "1" },
              body: "{}",
            },
          ]) {
            try {
              const res = await fetch(`${b}/v1/ping`, init);
              out.push(`status ${res.status}`);
            } catch (e) {
              out.push(`rejected ${(e as Error).name}`);
            }
          }
          const relay = await chrome.runtime.sendMessage({
            type: "pair",
            endpoint: "127.0.0.1:1234",
            code: "AAAA-AAAA-AAAA-AAAA",
          });
          out.push(`relay ${JSON.stringify(relay)}`);
          return out;
        },
        args: [base, token],
      });
      return r?.result as string[];
    },
    { id: tabId, base: h.base, token: h.token },
  );
  expect(result[0]).toMatch(/^rejected/);
  expect(result[1]).toMatch(/^rejected/);
  expect(result[2]).toContain("sender_not_allowed");
  await expect.poll(() => companion.log.length - before).toBeGreaterThan(0);
  const seen = companion.log.slice(before);
  expect(seen.filter((l) => l.status < 400)).toEqual([]);
  expect(seen.every((l) => l.origin === w.attacker.origin)).toBe(true);
});
