import { readFileSync } from "node:fs";
import { createServer, type Server } from "node:http";
import type { AddressInfo } from "node:net";
import { extname, join, normalize } from "node:path";
import { buildGallery } from "@vsf/gallery/build";

const TYPES: Record<string, string> = {
  ".html": "text/html; charset=utf-8",
  ".js": "text/javascript",
  ".woff2": "font/woff2",
  ".css": "text/css",
};

export interface GalleryServer {
  url: string;
  stop(): Promise<void>;
}

/** Serves the built gallery exactly as a static host would. */
export async function startGallery(): Promise<GalleryServer> {
  const dir = await buildGallery();
  const server: Server = createServer((req, res) => {
    const path = normalize(decodeURIComponent((req.url ?? "/").split("?")[0] ?? "/")).replace(/^(\.\.[/\\])+/, "");
    const file = join(dir, path === "/" ? "index.html" : path);
    try {
      const body = readFileSync(file);
      res
        .writeHead(200, {
          "content-type": TYPES[extname(file)] ?? "application/octet-stream",
          "cache-control": "no-store",
        })
        .end(body);
    } catch {
      res.writeHead(404, { "content-type": "text/plain" }).end("not found");
    }
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  return {
    url: `http://127.0.0.1:${(server.address() as AddressInfo).port}`,
    stop: () => {
      server.closeAllConnections();
      return new Promise((resolve) => server.close(() => resolve()));
    },
  };
}
