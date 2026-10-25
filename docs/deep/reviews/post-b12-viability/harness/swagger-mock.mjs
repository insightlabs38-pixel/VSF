import { createServer } from "node:http";
import { readFileSync, existsSync, appendFileSync, writeFileSync } from "node:fs";
import { extname, join } from "node:path";

const [, , dir, port] = process.argv;
const cache = process.env.AUDIT_CACHE ?? new URL("../../../../../.audit-cache/", import.meta.url).pathname;
const dist = join(cache, "swagger/node_modules/swagger-ui-dist");
const yaml = await import(join(cache, "swagger/node_modules/js-yaml/dist/js-yaml.mjs"));
const spec = yaml.load(readFileSync(join(cache, "swagger/petstore-src/src/main/resources/openapi.yaml"), "utf8"));
spec.servers = [{ url: `http://127.0.0.1:${port}/api/v3` }];
const specJson = JSON.stringify(spec);
const initializer = `window.onload = function () { window.ui = SwaggerUIBundle({ url: "/openapi.json", dom_id: "#swagger-ui", deepLinking: false, presets: [SwaggerUIBundle.presets.apis, SwaggerUIStandalonePreset], plugins: [SwaggerUIBundle.plugins.DownloadUrl], layout: "StandaloneLayout" }); };`;
const log = join(dir, "requests.jsonl");
const pets = new Map([
  [1, { id: 1, name: "Rex", category: { id: 1, name: "Dogs" }, photoUrls: [], tags: [], status: "available" }],
  [2, { id: 2, name: "Tom", category: { id: 2, name: "Cats" }, photoUrls: [], tags: [], status: "pending" }],
  [3, { id: 3, name: "Nemo", category: { id: 3, name: "Fish" }, photoUrls: [], tags: [], status: "sold" }],
]);
const types = { ".js": "text/javascript", ".css": "text/css", ".html": "text/html", ".png": "image/png", ".map": "application/json" };

const send = (res, code, body, type = "application/json") => {
  res.writeHead(code, { "content-type": type, "access-control-allow-origin": "*", "access-control-allow-headers": "*", "access-control-allow-methods": "*" });
  res.end(typeof body === "string" || Buffer.isBuffer(body) ? body : JSON.stringify(body));
};

createServer((req, res) => {
  const url = new URL(req.url ?? "/", "http://x");
  const chunks = [];
  req.on("data", (c) => chunks.push(c));
  req.on("end", () => {
    const raw = Buffer.concat(chunks).toString("utf8");
    const p = url.pathname;
    if (p.startsWith("/api/v3/")) appendFileSync(log, `${JSON.stringify({ method: req.method, path: p, query: Object.fromEntries(url.searchParams), headers: { api_key: req.headers.api_key ?? null, authorization: req.headers.authorization ?? null, "content-type": req.headers["content-type"] ?? null }, body: raw })}\n`);
    if (req.method === "OPTIONS") return send(res, 204, "");
    if (p === "/__audit/state") return send(res, 200, { pets: [...pets.values()], requests: existsSync(log) ? readFileSync(log, "utf8").trim().split("\n").filter(Boolean).map((l) => JSON.parse(l)) : [] });
    if (p === "/openapi.json") return send(res, 200, specJson);
    if (p === "/swagger-initializer.js") return send(res, 200, initializer, "text/javascript");
    if (p.startsWith("/api/v3/")) {
      const a = p.slice(8).split("/");
      if (a[0] === "pet" && req.method === "POST" && a.length === 1) { const b = JSON.parse(raw || "{}"); pets.set(b.id, b); return send(res, 200, b); }
      if (a[0] === "pet" && req.method === "PUT" && a.length === 1) { const b = JSON.parse(raw || "{}"); pets.set(b.id, b); return send(res, 200, b); }
      if (a[0] === "pet" && a[1] === "findByStatus") return send(res, 200, [...pets.values()].filter((x) => x.status === (url.searchParams.get("status") ?? "available")));
      if (a[0] === "pet" && a.length === 2 && req.method === "GET") { const x = pets.get(Number(a[1])); return x ? send(res, 200, x) : send(res, 404, { message: "Pet not found" }); }
      if (a[0] === "pet" && a.length === 2 && req.method === "DELETE") { pets.delete(Number(a[1])); return send(res, 200, ""); }
      if (a[0] === "store" && a[1] === "inventory") { const inv = {}; for (const x of pets.values()) inv[x.status] = (inv[x.status] ?? 0) + 1; return send(res, 200, inv); }
      return send(res, 404, { message: "not implemented in the audit mock" });
    }
    const f = join(dist, p === "/" ? "index.html" : p);
    if (f.startsWith(dist) && existsSync(f)) return send(res, 200, readFileSync(f), types[extname(f)] ?? "application/octet-stream");
    return send(res, 404, "not found", "text/plain");
  });
}).listen(Number(port), "127.0.0.1");
