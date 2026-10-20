import { readFileSync } from "node:fs";
import { createServer, type Server } from "node:http";
import type { AddressInfo } from "node:net";
import { fileURLToPath } from "node:url";

const DIR = fileURLToPath(new URL("../../../fixtures/recognition/apps/", import.meta.url));

export interface Apps {
  origin: string;
  /** Pages that exist only for one test, served at /apps/<name>. */
  pages: Map<string, string>;
  server: Server;
  close(): Promise<void>;
}

/** Serves the recognition fixture applications unchanged, one page per name. */
export async function startApps(): Promise<Apps> {
  const pages = new Map<string, string>();
  const server = createServer((req, res) => {
    const m = /^\/apps\/([a-z-]+)$/.exec((req.url ?? "").split("?")[0] ?? "");
    const name = m?.[1];
    let body: string | undefined;
    try {
      body = name ? (pages.get(name) ?? readFileSync(`${DIR}${name}.html`, "utf8")) : undefined;
    } catch {
      body = undefined;
    }
    if (body === undefined) {
      res.writeHead(404, { "content-type": "text/plain" }).end("not found");
      return;
    }
    res.writeHead(200, { "content-type": "text/html; charset=utf-8", "cache-control": "no-store" }).end(body);
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const origin = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
  return {
    origin,
    pages,
    server,
    close: () => {
      server.closeAllConnections();
      return new Promise((resolve) => server.close(() => resolve()));
    },
  };
}
