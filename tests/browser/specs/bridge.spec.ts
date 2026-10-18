import { type ChildProcess, spawn } from "node:child_process";
import { createServer } from "node:http";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { test as base, expect } from "@playwright/test";
import { startServer } from "@vsf/fixture-apps/server";
import { generateEnvelopeBytes } from "@vsf/schema";
import { createEnv, type Env } from "../support/env.ts";

interface Bridge {
  base: string;
  token: string;
  log: { method: string; path: string; status: number; origin: string; host: string }[];
  proc: ChildProcess;
}

const root = fileURLToPath(new URL("../../../", import.meta.url));

async function startBridge(origin: string): Promise<Bridge> {
  const proc = spawn(join(root, "artifacts/bin/vsfbridge"), ["-origins", origin], {
    stdio: ["ignore", "pipe", "pipe"],
  });
  const log: Bridge["log"] = [];
  let pending = "";
  proc.stderr?.on("data", (d: Buffer) => {
    pending += d.toString();
    const lines = pending.split("\n");
    pending = lines.pop() ?? "";
    for (const l of lines) if (l.startsWith("{")) log.push(JSON.parse(l));
  });
  const first = await new Promise<string>((resolve) => proc.stdout?.once("data", (d: Buffer) => resolve(d.toString())));
  const { addr, token } = JSON.parse(first) as { addr: string; token: string };
  return { base: `http://${addr}`, token, log, proc };
}

// No host permissions: the extension is subject to CORS exactly like any other origin.
const test = base.extend<{ world: { env: Env; bridge: Bridge; pageOrigin: string } }>({
  // biome-ignore lint/correctness/noEmptyPattern: Playwright requires a destructuring first argument
  world: async ({}, use) => {
    const a = await startServer(0);
    const b = await startServer(0);
    const { env, dispose } = await createEnv(
      { a: Number(new URL(a.url).port), b: Number(new URL(b.url).port) },
      { hostPermissions: [] },
    );
    const bridge = await startBridge(`chrome-extension://${env.extId}`);
    await use({ env, bridge, pageOrigin: env.a });
    bridge.proc.kill();
    await dispose();
    a.server.close();
    b.server.close();
  },
});

test("the paired extension origin passes CORS preflight and reaches the bridge", async ({ world: { env, bridge } }) => {
  const result = await env.panel.evaluate(
    async ({ url, token, body }) => {
      const headers = { Authorization: `Bearer ${token}`, "X-VSF-Bridge": "1", "Content-Type": "application/json" };
      const ping = await fetch(`${url}/v1/ping`, { headers });
      const verify = await fetch(`${url}/v1/envelope/verify`, { method: "POST", headers, body });
      return { ping: ping.status, verify: verify.status, verifyBody: await verify.text() };
    },
    { url: bridge.base, token: bridge.token, body: Buffer.from(generateEnvelopeBytes(42)).toString() },
  );
  expect(result).toMatchObject({ ping: 200, verify: 200 });
  const origin = `chrome-extension://${env.extId}`;
  await expect
    .poll(() => bridge.log.some((r) => r.method === "OPTIONS" && r.status === 204 && r.origin === origin), {
      message: JSON.stringify(bridge.log),
    })
    .toBe(true);
  await expect
    .poll(() => bridge.log.some((r) => r.method === "POST" && r.path === "/v1/envelope/verify" && r.status === 200))
    .toBe(true);
});

test("a hostile web origin holding the token still cannot read or write through the bridge", async ({
  world: { env, bridge },
}) => {
  const hostileServer = createServer((_req, res) => {
    res.setHeader("content-type", "text/html");
    res.end("<!doctype html><title>hostile</title>");
  });
  await new Promise<void>((r) => hostileServer.listen(0, "127.0.0.1", r));
  const hostileOrigin = `http://127.0.0.1:${(hostileServer.address() as { port: number }).port}`;
  const page = await env.context.newPage();
  await page.goto(`${hostileOrigin}/`);
  const attempts = await page.evaluate(
    async ({ url, token }) => {
      const out: Record<string, string> = {};
      const headers = { Authorization: `Bearer ${token}`, "X-VSF-Bridge": "1", "Content-Type": "application/json" };
      await fetch(`${url}/v1/ping`, { headers }).then(
        () => (out.authedRead = "resolved"),
        (e: Error) => (out.authedRead = `blocked ${e.name}`),
      );
      await fetch(`${url}/v1/upload/begin`, { method: "POST", headers, body: "{}" }).then(
        () => (out.authedWrite = "resolved"),
        (e: Error) => (out.authedWrite = `blocked ${e.name}`),
      );
      await fetch(`${url}/v1/envelope/verify`, {
        method: "POST",
        mode: "no-cors",
        headers: { "Content-Type": "text/plain" },
        body: "{}",
      }).then(
        () => (out.simpleForgery = "sent"),
        (e: Error) => (out.simpleForgery = `blocked ${e.name}`),
      );
      return out;
    },
    { url: bridge.base, token: bridge.token },
  );
  expect(attempts.authedRead).toMatch(/^blocked/);
  expect(attempts.authedWrite).toMatch(/^blocked/);
  await expect
    .poll(() => bridge.log.filter((r) => r.origin === hostileOrigin).length, { message: JSON.stringify(bridge.log) })
    .toBeGreaterThan(0);
  const hostile = bridge.log.filter((r) => r.origin === hostileOrigin);
  expect(
    hostile.every((r) => r.status === 403 || r.status === 401),
    JSON.stringify(hostile),
  ).toBe(true);
  expect(hostile.some((r) => r.method === "POST" && r.path === "/v1/upload/begin")).toBe(false);
  hostileServer.close();
});
