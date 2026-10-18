import type { Server } from "node:http";
import { test as base } from "@playwright/test";
import { startServer } from "@vsf/fixture-apps/server";
import { createEnv, type Env } from "./env.ts";

export type { Env } from "./env.ts";

interface Worlds {
  servers: { a: Server; b: Server };
  ports: { a: number; b: number };
}

export const test = base.extend<{ env: Env }, { worlds: Worlds }>({
  worlds: [
    // biome-ignore lint/correctness/noEmptyPattern: Playwright requires a destructuring first argument
    async ({}, use) => {
      const a = await startServer(0);
      const b = await startServer(0);
      const port = (u: string): number => Number(new URL(u).port);
      await use({ servers: { a: a.server, b: b.server }, ports: { a: port(a.url), b: port(b.url) } });
      a.server.close();
      b.server.close();
    },
    { scope: "worker" },
  ],
  env: async ({ worlds }, use) => {
    const { env, dispose } = await createEnv(worlds.ports);
    await use(env);
    await dispose();
  },
});

export { expect } from "@playwright/test";
