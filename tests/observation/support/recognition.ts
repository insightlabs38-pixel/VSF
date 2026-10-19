import { readdirSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { chromium } from "@playwright/test";
import type { RecognizerInput } from "@vsf/observer";
import { attach, startWorld } from "./world.ts";

export const APPS_DIR = fileURLToPath(new URL("../../../fixtures/recognition/apps/", import.meta.url));
export const SNAPSHOTS_DIR = fileURLToPath(new URL("../../../fixtures/recognition/snapshots/", import.meta.url));

export const appNames = (): string[] =>
  readdirSync(APPS_DIR)
    .filter((f) => f.endsWith(".html"))
    .map((f) => f.replace(/\.html$/, ""))
    .sort();

/** Every fixture app captured by the real observer in Chromium, as the Rust recognizer reads it. */
export async function captureApps(): Promise<Map<string, RecognizerInput>> {
  const world = await startWorld();
  const browser = await chromium.launch({ channel: "chromium" });
  const out = new Map<string, RecognizerInput>();
  try {
    for (const name of appNames()) {
      world.a.pages.set(`/${name}`, readFileSync(`${APPS_DIR}${name}.html`, "utf8"));
      const page = await (await browser.newContext({ viewport: { width: 1280, height: 800 } })).newPage();
      const h = await attach(page, { allow: [world.a.url] });
      await page.goto(`${world.a.url}/${name}`);
      await page.waitForLoadState("load");
      await h.settle();
      out.set(name, h.collector.replica.toRecognizerInput());
      await page.close();
    }
  } finally {
    await browser.close();
    await world.stop();
  }
  return out;
}
