import { createServer, type Server } from "node:http";
import type { AddressInfo } from "node:net";

export interface Seen {
  requestId: string;
  body: Record<string, unknown>;
  at: number;
}

export interface Service {
  url: string;
  seen: Seen[];
  delayMs: number;
  stop(): Promise<void>;
}

/** A stand-in local service: counts what reaches it and can be slowed down to catch a request in flight. */
export async function startService(allowOrigin: string): Promise<Service> {
  const state: Service = { url: "", seen: [], delayMs: 0, stop: async () => {} };
  const server: Server = createServer((req, res) => {
    res.setHeader("Access-Control-Allow-Origin", allowOrigin);
    res.setHeader("Access-Control-Allow-Headers", "Content-Type, X-VSF-Bridge, X-VSF-Request");
    if (req.method === "OPTIONS") {
      res.writeHead(204).end();
      return;
    }
    const chunks: Buffer[] = [];
    req.on("data", (c: Buffer) => chunks.push(c));
    req.on("end", () => {
      const body = JSON.parse(Buffer.concat(chunks).toString("utf8") || "{}") as Record<string, unknown>;
      state.seen.push({ requestId: String(req.headers["x-vsf-request"]), body, at: Date.now() });
      setTimeout(() => {
        res.writeHead(200, { "content-type": "application/json" });
        res.end(JSON.stringify({ ok: true, message: `ran ${String(body.action)}` }));
      }, state.delayMs);
    });
  });
  await new Promise<void>((r) => server.listen(0, "127.0.0.1", r));
  state.url = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
  state.stop = () =>
    new Promise((r) => {
      server.closeAllConnections();
      server.close(() => r());
    });
  return state;
}
