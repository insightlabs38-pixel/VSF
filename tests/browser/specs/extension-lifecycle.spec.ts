import { decodePack } from "@vsf/observer";
import { findSecretShapes } from "@vsf/redact";
import { expect, test } from "../support/harness.ts";
import { stopWorkers } from "../support/lifecycle.ts";

test("worker termination and restart reconstruct state without losing grants", async ({ env }) => {
  await env.send({ type: "grant", origin: env.a });
  const page = await env.context.newPage();
  await page.goto(`${env.a}/forms/`);
  const tabId = await env.tabIdOf(page);
  expect(await env.send({ type: "capture", tabId })).toMatchObject({ ok: true });
  const before = (await env.send({ type: "status" })) as {
    bootId: string;
    grants: string[];
    stats: { captures: number };
  };

  const stopped = await stopWorkers(env.panel);
  expect(stopped).toBeGreaterThan(0);

  const after = (await env.send({ type: "status" })) as {
    bootId: string;
    grants: string[];
    stats: { captures: number };
  };
  expect(after.bootId).not.toBe(before.bootId);
  expect(after.grants).toEqual(before.grants);
  expect(after.stats.captures).toBe(before.stats.captures);
  expect(await env.send({ type: "capture", tabId })).toMatchObject({ ok: true, origin: env.a });
  const final = (await env.send({ type: "status" })) as { stats: { captures: number } };
  expect(final.stats.captures).toBe(before.stats.captures + 1);
});

test("a capture interrupted by termination is reported, never silently lost", async ({ env }, info) => {
  await env.send({ type: "grant", origin: env.a });
  const page = await env.context.newPage();
  await page.goto(`${env.a}/large/?n=50000&seed=3`);
  await page.waitForSelector("html[data-built]");
  const tabId = await env.tabIdOf(page);
  const inflight = env
    .send({ type: "capture", tabId })
    .catch((e: Error) => ({ ok: false, error: `message_failed:${e.message.slice(0, 40)}` }));
  await new Promise((r) => setTimeout(r, 40));
  await stopWorkers(env.panel);
  const outcome = await inflight;
  const status = (await env.send({ type: "status" })) as {
    stats: { captures: number };
    pending: Record<string, { state: string }>;
  };
  const completed = status.stats.captures === 1;
  info.annotations.push({ type: "observed", description: JSON.stringify({ outcome, status }) });
  const interrupted = Object.values(status.pending).some((p) => p.state === "interrupted");
  expect(completed || interrupted || (outcome as { ok: boolean }).ok === false).toBe(true);
  expect(completed || interrupted, JSON.stringify({ outcome, status })).toBe(true);
});

test("no secret appears in the pack, worker globals, storage, logs or replies", async ({ env }) => {
  await env.send({ type: "grant", origin: env.a });
  const page = await env.context.newPage();
  await page.goto(`${env.a}/secrets/?user=ada`);
  const secrets = await page.evaluate(() => (window as unknown as { __secrets: Record<string, string> }).__secrets);
  const values = Object.values(secrets);
  expect(values.length).toBe(3);
  const tabId = await env.tabIdOf(page);

  const worker = await env.worker();
  await worker.evaluate(() => {
    const g = globalThis as unknown as { __logs: string[] };
    g.__logs = [];
    for (const level of ["log", "info", "warn", "error", "debug"] as const) {
      console[level] = (...a: unknown[]) => void g.__logs.push(a.map(String).join(" "));
    }
    console.log("hook-installed");
  });
  const reply = await env.send({ type: "capture", tabId });
  expect(reply).toMatchObject({ ok: true });

  const b64 = await worker.evaluate(async (id) => {
    const [r] = await chrome.scripting.executeScript({ target: { tabId: id }, files: ["content.js"] });
    return (r?.result as { b64: string } | undefined)?.b64 ?? "";
  }, tabId);
  const pack = decodePack(Uint8Array.from(Buffer.from(b64, "base64")));
  const joined = pack.strings.join("\n");
  for (const v of values) expect(joined).not.toContain(v);
  expect(findSecretShapes(joined)).toEqual([]);
  expect(joined).toContain("[REDACTED:github_token]");
  expect(joined).toContain("[REDACTED:bearer_token]");
  expect(joined).toContain("Password");

  const dump = await worker.evaluate(async () => ({
    globals: Object.keys(globalThis).filter((k) => k !== "__logs"),
    local: await chrome.storage.local.get(null),
    session: await chrome.storage.session.get(null),
  }));
  const logs = await worker.evaluate(() => (globalThis as unknown as { __logs: string[] }).__logs);
  expect(logs).toEqual(["hook-installed"]);
  const haystack = JSON.stringify({ dump, reply, logs });
  for (const v of values) expect(haystack).not.toContain(v);
  expect(findSecretShapes(haystack)).toEqual([]);
});

test("the packaged scripts contain no logging calls", async () => {
  const { readFileSync } = await import("node:fs");
  const { join } = await import("node:path");
  const { buildExtension } = await import("@vsf/extension/build");
  const { mkdtempSync } = await import("node:fs");
  const { tmpdir } = await import("node:os");
  const dir = await buildExtension(
    { name: "static", hostPermissions: [] },
    join(mkdtempSync(join(tmpdir(), "vsf-static-")), "ext"),
  );
  for (const f of ["background.js", "content.js", "panel.js"]) {
    expect(readFileSync(join(dir, f), "utf8"), f).not.toMatch(/\bconsole\./);
  }
});
