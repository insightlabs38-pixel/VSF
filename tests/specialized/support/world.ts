import { mkdtempSync, readdirSync, readFileSync } from "node:fs";
import { createServer, type IncomingMessage, type Server, type ServerResponse } from "node:http";
import type { AddressInfo } from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { build } from "esbuild";
import { accept, type Socket } from "./ws.ts";

const entry = fileURLToPath(new URL("../page/entry.ts", import.meta.url));

export interface World {
  url: string;
  /** Every path the browser requested, in order. */
  requests: string[];
  pages: Map<string, string>;
  /** WebSocket handler; called for each upgrade on /ws. */
  onSocket: (s: Socket, req: IncomingMessage) => void;
  /** Server-sent events handler; called for each GET /events. */
  onEvents: (req: IncomingMessage, res: ServerResponse) => void;
  stop(): Promise<void>;
}

let cachedDir: string | null = null;

/** The adapters as split ES modules, so a page fetches a library only when it opens that adapter. */
async function bundle(): Promise<string> {
  if (cachedDir) return cachedDir;
  const out = mkdtempSync(join(tmpdir(), "vsf-specialized-"));
  await build({
    entryPoints: [entry],
    bundle: true,
    splitting: true,
    format: "esm",
    outdir: out,
    target: "chrome120",
    conditions: ["source"],
    logLevel: "error",
    chunkNames: "chunk-[name]-[hash]",
  });
  cachedDir = out;
  return out;
}

export function bundleFiles(): string[] {
  return cachedDir ? readdirSync(cachedDir) : [];
}

export async function startWorld(): Promise<World> {
  const dir = await bundle();
  const pages = new Map<string, string>();
  const requests: string[] = [];
  const w: World = {
    url: "",
    requests,
    pages,
    onSocket: (s) => s.close(),
    onEvents: (_req, res) => res.end(),
    stop: () => new Promise((resolve) => server.close(() => resolve())),
  };
  const server: Server = createServer((req, res) => {
    const path = (req.url ?? "/").split("?")[0] as string;
    requests.push(path);
    if (path === "/events") return w.onEvents(req, res);
    if (path.startsWith("/dist/")) {
      const name = path.slice(6);
      if (!/^[\w.-]+$/.test(name)) {
        res.writeHead(404).end();
        return;
      }
      try {
        res.writeHead(200, { "content-type": "text/javascript" }).end(readFileSync(join(dir, name)));
      } catch {
        res.writeHead(404).end();
      }
      return;
    }
    const body = pages.get(path);
    if (body === undefined) {
      res.writeHead(404, { "content-type": "text/plain" }).end("not found");
      return;
    }
    res.writeHead(200, { "content-type": "text/html; charset=utf-8" }).end(body);
  });
  server.on("upgrade", (req, socket) => {
    const s = accept(req, socket);
    if (s) w.onSocket(s, req);
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  w.url = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
  return w;
}

export const adapterPage = `<!doctype html><meta charset=utf-8><title>adapters</title>
<style>#host{width:640px;height:320px}</style>
<div id="host"></div><input id="other" aria-label="other">
<script type="module" src="/dist/entry.js"></script>`;

export const plainPage = `<!doctype html><meta charset=utf-8><title>plain</title><main><p>Nothing special here.</p></main>
<script type="module" src="/dist/entry.js"></script>`;
