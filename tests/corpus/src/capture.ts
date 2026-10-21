import { createHash } from "node:crypto";
import type { Browser, Page as PwPage } from "@playwright/test";
import type { RecognizerInput } from "@vsf/observer";
import { attach, type Harness } from "../../observation/support/world.ts";
import { mirrorFile } from "./cdn.ts";
import { dirOf } from "./fetch.ts";
import type { Page, Source } from "./manifest.ts";

export interface Capture {
  source: string;
  page: string;
  url: string;
  title: string;
  snapshot: RecognizerInput;
  /** The browser's own accessibility tree, taken without the observer, for independent annotation. */
  aria: string;
  blocked: string[];
  mirrored: string[];
  captureMs: number;
  snapshotSha256: string;
  /** Digest of the tree ignoring node ids, which shift when a late script injects nodes in a different order. */
  structureSha256: string;
}

const sha = (s: string): string => createHash("sha256").update(s).digest("hex");

/** Pages that fill themselves with random sample data must show the same data on every capture. */
const SEEDED_RANDOM = `(() => { let a = 0x9e3779b9; Math.random = () => { a = (a + 0x6d2b79f5) | 0; let t = Math.imul(a ^ (a >>> 15), 1 | a); t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t; return ((t ^ (t >>> 14)) >>> 0) / 4294967296; }; })();`;

const VOLATILE = /\d+(?:\.\d+)? [KMGT]?i?B of \d+(?:\.\d+)? [KMGT]?i?B used/;

/** Host disk usage shown by a page differs between machines and says nothing about structure. */
const stable = (text: string | null): string | null => (text === null ? null : text.replace(VOLATILE, "<usage>"));

export function structureDigest(
  nodes: {
    id: number;
    parent: number;
    tag: string;
    role: string | null;
    name: string | null;
    text: string | null;
    flags: number;
  }[],
): string {
  const at = new Map(nodes.map((n, i) => [n.id, i]));
  return sha(
    JSON.stringify(nodes.map((n) => [at.get(n.parent) ?? -1, n.tag, n.role, n.name, stable(n.text), n.flags])),
  );
}

export interface Opened {
  page: PwPage;
  /** Present when the observer is attached. */
  h?: Harness | undefined;
  blocked: string[];
  mirrored: string[];
  close(): Promise<void>;
}

/** Opens one page state in a hermetic context and replays the steps that reach it; the observer is attached on request. */
export async function openState(
  browser: Browser,
  origin: string,
  s: Source,
  p: Page,
  observe: boolean,
): Promise<Opened> {
  const ctx = await browser.newContext({
    viewport: { width: 1280, height: 800 },
    locale: "en-US",
    timezoneId: "UTC",
    reducedMotion: "reduce",
    serviceWorkers: "block",
  });
  const blocked: string[] = [];
  const mirrored: string[] = [];
  await ctx.addInitScript(SEEDED_RANDOM);
  await ctx.route("**/*", async (route) => {
    const url = route.request().url();
    if (url.startsWith(origin) || url.startsWith("data:") || url.startsWith("blob:") || url.startsWith("about:"))
      return route.continue();
    try {
      const hit = mirrorFile(url, s.aliases, dirOf(s));
      if (hit) {
        mirrored.push(url);
        return route.fulfill({
          status: 200,
          body: hit.body,
          contentType: hit.type,
          headers: { "access-control-allow-origin": "*" },
        });
      }
    } catch {
      /* fall through to blocked */
    }
    blocked.push(url);
    return route.abort();
  });
  try {
    const page = await ctx.newPage();
    const h = observe ? await attach(page, { allow: [origin] }) : undefined;
    await page.goto(`${origin}${p.path}`, { waitUntil: "load" });
    await page.waitForLoadState("networkidle", { timeout: 5000 }).catch(() => {});
    for (const step of p.steps ?? []) {
      if (step.click) await page.locator(step.click).first().click({ timeout: 5000 });
      if (step.fill) await page.locator(step.fill.selector).first().fill(step.fill.value, { timeout: 5000 });
      if (step.press) await page.keyboard.press(step.press);
      if (step.upload)
        await page
          .locator(step.upload.selector)
          .first()
          .setInputFiles(
            step.upload.files.map((f) => ({
              name: f.name,
              mimeType: f.mimeType ?? "text/plain",
              buffer: Buffer.from(f.text),
            })),
          );
      await page.waitForTimeout(step.waitMs ?? 250);
    }
    if (h) await h.settle();
    return { page, h, blocked, mirrored, close: () => ctx.close() };
  } catch (err) {
    await ctx.close();
    throw err;
  }
}

/** Captures one page state with the real observer, as the extension would, in a hermetic browser context. */
export async function capturePage(browser: Browser, origin: string, s: Source, p: Page): Promise<Capture> {
  const t0 = performance.now();
  const opened = await openState(browser, origin, s, p, true);
  const { page, blocked, mirrored } = opened;
  const h = opened.h as Harness;
  try {
    const snapshot = h.collector.replica.toRecognizerInput();
    const aria = await page
      .locator("body")
      .ariaSnapshot({ timeout: 10_000 })
      .catch(() => "");
    const text = `${JSON.stringify(snapshot)}`.replaceAll(origin, "http://corpus.invalid");
    return {
      source: s.id,
      page: p.id,
      url: `${origin}${p.path}`,
      title: await page.title(),
      snapshot: JSON.parse(text) as RecognizerInput,
      aria,
      blocked,
      mirrored,
      captureMs: Math.round(performance.now() - t0),
      snapshotSha256: sha(text),
      structureSha256: structureDigest(snapshot.nodes as never),
    };
  } finally {
    await opened.close();
  }
}
