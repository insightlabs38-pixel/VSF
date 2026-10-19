import { createServer, type Server } from "node:http";
import type { AddressInfo } from "node:net";
import type { Page } from "@playwright/test";
import { pageBundle } from "./bundle.ts";
import type { Boot } from "./page-entry.ts";

export interface Site {
  server: Server;
  url: string;
  pages: Map<string, string>;
  stop(): Promise<void>;
}

export async function startSite(): Promise<Site> {
  const pages = new Map<string, string>();
  const bundle = await pageBundle();
  const server = createServer((req, res) => {
    const path = (req.url ?? "/").split("?")[0] as string;
    if (path === "/bundle.js") {
      res.writeHead(200, { "content-type": "text/javascript" });
      res.end(bundle);
      return;
    }
    const body = pages.get(path);
    if (body === undefined) {
      res.writeHead(404, { "content-type": "text/plain" });
      res.end("not found");
      return;
    }
    res.writeHead(200, { "content-type": "text/html; charset=utf-8" });
    res.end(body);
  });
  return new Promise((resolve) => {
    server.listen(0, "127.0.0.1", () => {
      const url = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
      resolve({ server, url, pages, stop: () => new Promise((r) => server.close(() => r())) });
    });
  });
}

/** A source page: its own markup and CSS, an empty host for the renderer and the bundle. */
export function sourcePage(source = "", head = "", attrs = ""): string {
  return `<!doctype html><html ${attrs}><head><meta charset="utf-8"><title>Source</title>${head}</head><body>${source}<div id="vsf-host"></div><script src="/bundle.js"></script></body></html>`;
}

export async function open(page: Page, site: Site, path: string, cfg: Boot, html = sourcePage()): Promise<void> {
  site.pages.set(path, html);
  await page.goto(`${site.url}${path}`);
  await page.evaluate((c) => (window as unknown as { VSF: { boot(c: unknown): void } }).VSF.boot(c), cfg);
}
