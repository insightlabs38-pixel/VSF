import { createServer, type Server } from "node:http";
import type { AddressInfo } from "node:net";
import { fileURLToPath } from "node:url";
import type { Page } from "@playwright/test";
import { type Batch, Replica } from "@vsf/observer";
import { build } from "esbuild";

const entry = fileURLToPath(new URL("../../../frontend/packages/observer/src/index.ts", import.meta.url));

let cached: string | null = null;

/** The observer as one script defining `VSFObserver`, exactly what a content script would carry. */
export async function observerBundle(): Promise<string> {
  if (cached) return cached;
  const out = await build({
    entryPoints: [entry],
    bundle: true,
    write: false,
    format: "iife",
    globalName: "VSFObserver",
    target: "chrome120",
    logLevel: "error",
  });
  cached = `${out.outputFiles[0]?.text ?? ""}\nwindow.VSFObserver = VSFObserver;`;
  return cached;
}

export interface Origin {
  server: Server;
  url: string;
  pages: Map<string, string>;
  hits: string[];
}

function listen(host: string): Promise<Origin> {
  const pages = new Map<string, string>();
  const hits: string[] = [];
  const server = createServer((req, res) => {
    const path = (req.url ?? "/").split("?")[0] as string;
    hits.push(req.url ?? "/");
    const body = pages.get(path);
    if (body === undefined) {
      res.writeHead(404, { "content-type": "text/plain" });
      res.end("not found");
      return;
    }
    res.writeHead(200, { "content-type": path.endsWith(".js") ? "text/javascript" : "text/html; charset=utf-8" });
    res.end(body);
  });
  return new Promise((resolve) => {
    server.listen(0, host, () => {
      const port = (server.address() as AddressInfo).port;
      resolve({ server, url: `http://${host}:${port}`, pages, hits });
    });
  });
}

/** Two unrelated origins on loopback: the second can never be read from the first. */
export async function startWorld(): Promise<{ a: Origin; b: Origin; stop(): Promise<void> }> {
  const a = await listen("127.0.0.1");
  const b = await listen("localhost");
  return {
    a,
    b,
    stop: () =>
      Promise.all(
        [a, b].map(
          (o) =>
            new Promise<void>((r) => {
              o.server.close(() => r());
              o.server.closeAllConnections();
            }),
        ),
      ).then(() => {}),
  };
}

export type ResyncRequest = { kind: "full" } | { kind: "subtree"; id: number } | { kind: "replay"; from: number };

export interface HarnessOptions {
  allow: string[];
  batchMs?: number | null;
  closedShadowAccess?: boolean;
  limits?: Record<string, number>;
  flags?: string[];
}

/** Receives every batch from the page the way the extension panel would, and can lose some on purpose. */
export class Collector {
  readonly replica = new Replica();
  readonly batches: Batch[] = [];
  readonly results: string[] = [];
  readonly epochs = new Set<number>();
  dropNext = 0;

  receive(batch: Batch): void {
    this.batches.push(batch);
    this.epochs.add(batch.epoch);
    if (this.dropNext > 0) {
      this.dropNext--;
      this.results.push(`dropped:${batch.epoch}.${batch.seq}`);
      return;
    }
    const r = this.replica.apply(batch);
    this.results.push(r.status);
  }
}

export interface Harness {
  page: Page;
  collector: Collector;
  /** Waits for every batch the page has produced to be delivered. */
  drain(): Promise<void>;
  /** Forces the page to publish what it has queued, then drains. */
  flush(): Promise<void>;
  /** Drains, then repairs any gap or divergence the way a collector would, until consistent. */
  settle(): Promise<void>;
  resync(req: ResyncRequest): Promise<void>;
  start(): Promise<void>;
}

const BOOT = (opts: HarnessOptions): string => `
(() => {
  if (window !== window.top) return;
  const opts = ${JSON.stringify(opts)};
  const begin = () => {
    if (window.__vsf) window.__vsf.session.stop();
    const sends = [];
    const diag = new VSFObserver.Diagnostics();
    const caps = {};
    if (opts.closedShadowAccess) caps.openOrClosedShadowRoot = (el) => window.__vsfClosed?.get(el) ?? el.shadowRoot;
    const session = VSFObserver.Session.start({
      doc: document,
      policy: { allowOrigin: (o) => opts.allow.includes(o) },
      batchMs: opts.batchMs === undefined ? null : opts.batchMs,
      nextEpoch: VSFObserver.persistentEpochs(window),
      caps,
      limits: opts.limits,
      diagnostics: diag,
      flagProbes: (opts.flags ?? []).map((n) => VSFObserver.dataFlagProbe(n)),
      sink: (b) => sends.push(window.__vsfSink(b)),
    });
    window.__vsf = { session, diag, drain: async () => { await Promise.all(sends.splice(0)); } };
  };
  window.__vsfBegin = begin;
  const go = () => { try { begin(); } catch (e) { window.__vsfError = String(e && e.code || e); } };
  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", go, { once: true });
  else go();
})();`;

export async function attach(page: Page, opts: HarnessOptions): Promise<Harness> {
  const collector = new Collector();
  await page.exposeFunction("__vsfSink", (b: Batch) => collector.receive(b));
  await page.addInitScript(await observerBundle());
  await page.addInitScript(BOOT(opts));
  const h: Harness = {
    page,
    collector,
    async drain() {
      await page.evaluate(() => (window as unknown as { __vsf: { drain(): Promise<void> } }).__vsf.drain());
    },
    async flush() {
      await page.evaluate(() => (window as unknown as { __vsf: { session: { flush(): void } } }).__vsf.session.flush());
      await h.drain();
    },
    async resync(req) {
      await page.evaluate(
        (r) => (window as unknown as { __vsf: { session: { resync(r: unknown): void } } }).__vsf.session.resync(r),
        req,
      );
      await h.drain();
    },
    async settle() {
      await h.flush();
      for (let i = 0; i < 4 && collector.replica.state !== "synced"; i++) {
        const s = collector.replica;
        await h.resync(s.state === "gap" ? { kind: "replay", from: s.seq + 1 } : { kind: "full" });
      }
    },
    async start() {
      await page.evaluate(() => (window as unknown as { __vsfBegin(): void }).__vsfBegin());
      await h.drain();
    },
  };
  return h;
}
