import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { type BrowserContext, chromium, type Page, type Worker } from "@playwright/test";
import { buildExtension, extensionId } from "@vsf/extension/build";

export interface Env {
  /** Authorized fixture origin: host-permitted by the test build. */
  a: string;
  /** Same host, different port: browser-permitted but never VSF-granted. */
  b: string;
  /** Different host name: not covered by the build's host permissions. */
  c: string;
  extId: string;
  context: BrowserContext;
  panel: Page;
  send(message: unknown): Promise<Record<string, unknown>>;
  tabIdOf(page: Page): Promise<number>;
  worker(): Promise<Worker>;
}

export async function createEnv(
  ports: { a: number; b: number },
  options: {
    hostPermissions?: string[];
    extraPermissions?: string[];
    setupProfile?: (profileDir: string, extId: string) => void;
  } = {},
): Promise<{ env: Env; dispose(): Promise<void> }> {
  const dir = mkdtempSync(join(tmpdir(), "vsf-ext-"));
  const extDir = await buildExtension(
    {
      name: "test-browser",
      hostPermissions: options.hostPermissions ?? ["http://127.0.0.1/*"],
      extraPermissions: options.extraPermissions ?? [],
    },
    join(dir, "ext"),
  );
  const manifest = JSON.parse(readFileSync(join(extDir, "manifest.json"), "utf8")) as { key: string };
  const extId = extensionId(manifest.key);
  options.setupProfile?.(join(dir, "profile"), extId);
  const context = await chromium.launchPersistentContext(join(dir, "profile"), {
    channel: "chromium",
    headless: true,
    args: [`--disable-extensions-except=${extDir}`, `--load-extension=${extDir}`],
  });
  let sw = context.serviceWorkers()[0];
  if (!sw) sw = await context.waitForEvent("serviceworker", { timeout: 15_000 });
  const panel = await context.newPage();
  await panel.goto(`chrome-extension://${extId}/panel.html`);
  const env: Env = {
    a: `http://127.0.0.1:${ports.a}`,
    b: `http://127.0.0.1:${ports.b}`,
    c: `http://localhost:${ports.a}`,
    extId,
    context,
    panel,
    send: (message) => panel.evaluate((m) => chrome.runtime.sendMessage(m), message),
    async tabIdOf(page) {
      await page.bringToFront();
      return panel.evaluate(async () => {
        const tabs = await chrome.tabs.query({ active: true, lastFocusedWindow: true });
        return tabs[0]?.id ?? -1;
      });
    },
    async worker() {
      return context.serviceWorkers()[0] ?? (await context.waitForEvent("serviceworker", { timeout: 15_000 }));
    },
  };
  return {
    env,
    async dispose() {
      await context.close();
      rmSync(dir, { recursive: true, force: true });
    },
  };
}
