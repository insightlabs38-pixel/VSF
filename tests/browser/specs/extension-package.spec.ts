process.env.PW_EXPERIMENTAL_SERVICE_WORKER_NETWORK_EVENTS = "1";

import { mkdtempSync, readFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { buildExtension } from "@vsf/extension/build";
import { auditPackage } from "../../../tools/checks/src/extaudit.ts";
import { startApps } from "../support/apps.ts";
import { expect, test } from "../support/harness.ts";

const sums = (dir: string): Record<string, string> => {
  const r = auditPackage(dir, "product");
  return Object.fromEntries(r.files.map((f) => [f.path, f.sha256]));
};

test("the product package builds identically twice and passes the distribution audit", async () => {
  const out = mkdtempSync(join(tmpdir(), "vsf-pkg-"));
  const a = await buildExtension({ name: "product", hostPermissions: [] }, join(out, "a"));
  const b = await buildExtension({ name: "product", hostPermissions: [] }, join(out, "b"));
  expect(sums(a)).toEqual(sums(b));
  expect(auditPackage(a, "product").findings).toEqual([]);
  const manifest = JSON.parse(readFileSync(join(a, "manifest.json"), "utf8")) as { host_permissions?: string[] };
  expect(manifest.host_permissions).toBeUndefined();
});

test("during a full transform every network request from extension contexts targets the package itself", async ({
  env,
}) => {
  const apps = await startApps();
  try {
    const seen: string[] = [];
    env.context.on("request", (r) => {
      if (r.serviceWorker() || r.frame()?.url().startsWith("chrome-extension://")) seen.push(r.url());
    });
    const page = await env.context.newPage();
    await page.goto(`${apps.origin}/apps/table-orders`);
    await env.send({ type: "grant", origin: apps.origin });
    const tabId = await env.tabIdOf(page);
    expect(await env.send({ type: "transform", tabId })).toMatchObject({ ok: true });
    await env.send({ type: "restore", tabId });
    expect(seen.length).toBeGreaterThan(0);
    for (const url of seen) expect(url.startsWith(`chrome-extension://${env.extId}/`), url).toBe(true);
  } finally {
    await apps.close();
  }
});
