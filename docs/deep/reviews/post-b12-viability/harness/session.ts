import { existsSync } from "node:fs";
import { type Browser, type BrowserContext, chromium, type Download, type Page } from "@playwright/test";
import { SEED, type Running, start } from "./apps.ts";
import type { Ctx, Workflow } from "./spec.ts";

export interface Live {
  ctx: Ctx;
  context: BrowserContext;
  close(): Promise<void>;
}

export async function launch(): Promise<Browser> {
  return chromium.launch({ channel: "chromium" });
}

/** A fresh disposable application and a fresh browser context, restored to the sealed seed state. */
export async function open(browser: Browser, wf: Workflow, observe?: (page: Page, origin: string) => Promise<void>): Promise<Live> {
  const run: Running = await start(wf.app, wf.variant);
  const storage = `${SEED}${wf.app}.storage.json`;
  const context = await browser.newContext({
    viewport: { width: 1280, height: 800 },
    locale: "en-US",
    timezoneId: "UTC",
    reducedMotion: "reduce",
    acceptDownloads: true,
    serviceWorkers: "block",
    ...(existsSync(storage) ? { storageState: storage } : {}),
  });
  const page = await context.newPage();
  const files = new Map<string, Buffer>();
  const downloads: Download[] = [];
  page.on("download", async (d) => {
    downloads.push(d);
    try {
      const path = await d.path();
      const { readFileSync } = await import("node:fs");
      files.set(d.suggestedFilename(), readFileSync(path));
    } catch {
      /* the oracle reports a missing file */
    }
  });
  if (observe) await observe(page, run.origin);
  const ctx: Ctx = { run, page, origin: run.origin, downloads, files };
  return {
    ctx,
    context,
    async close() {
      await context.close();
      await run.stop();
    },
  };
}
