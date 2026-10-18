import { chmodSync, mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { test as base, expect } from "@playwright/test";
import { startServer } from "@vsf/fixture-apps/server";
import { createEnv, type Env } from "../support/env.ts";

const HOST = "com.varifold.probe";
const hostPath = fileURLToPath(new URL("../support/native-host.mjs", import.meta.url));

const test = base.extend<{ env: Env }>({
  // biome-ignore lint/correctness/noEmptyPattern: Playwright requires a destructuring first argument
  env: async ({}, use) => {
    const a = await startServer(0);
    const { env, dispose } = await createEnv(
      { a: Number(new URL(a.url).port), b: 0 },
      {
        extraPermissions: ["nativeMessaging"],
        setupProfile(profile, extId) {
          const dir = join(profile, "NativeMessagingHosts");
          mkdirSync(dir, { recursive: true });
          chmodSync(hostPath, 0o755);
          writeFileSync(
            join(dir, `${HOST}.json`),
            JSON.stringify({
              name: HOST,
              description: "probe",
              path: hostPath,
              type: "stdio",
              allowed_origins: [`chrome-extension://${extId}/`],
            }),
          );
        },
      },
    );
    await use(env);
    await dispose();
    a.server.close();
  },
});

const send = (env: Env, msg: object): Promise<string> =>
  env.panel.evaluate(
    (args) =>
      new Promise<string>((resolve) => {
        chrome.runtime.sendNativeMessage(args.host, args.msg, (reply) => {
          resolve(
            chrome.runtime.lastError
              ? `error: ${chrome.runtime.lastError.message}`
              : JSON.stringify(reply).slice(0, 60),
          );
        });
      }),
    { host: HOST, msg },
  );

test("native messaging size limits are measured in both directions", async ({ env }, info) => {
  test.setTimeout(120_000);
  const toHost: Record<string, string> = {};
  for (const bytes of [1_000, 1_000_000, 8_000_000, 32_000_000]) {
    toHost[String(bytes)] = await send(env, { op: "echo", pad: "p".repeat(bytes) });
  }
  const fromHost: Record<string, string> = {};
  for (const bytes of [500_000, 1_000_000 - 100, 1_048_576, 2_000_000]) {
    fromHost[String(bytes)] = await send(env, { op: "blob", n: bytes });
  }
  info.annotations.push({
    type: "observed",
    description: JSON.stringify({ browserToHost: toHost, hostToBrowser: fromHost }),
  });
  expect(toHost["1000"]).toContain("received");
  expect(toHost["8000000"]).toContain("received");
  expect(fromHost["500000"]).toContain("blob");
  expect(fromHost["2000000"]).toMatch(/^error/);
});
