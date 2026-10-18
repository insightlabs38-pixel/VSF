import { createHash } from "node:crypto";
import { readFile } from "node:fs/promises";
import { createServer, type IncomingMessage, type Server, type ServerResponse } from "node:http";
import { dirname, extname, join, normalize, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const repo = resolve(here, "../../..");
const MAX_BODY = 16 * 1024;

const MOUNTS: [string, string][] = [
  ["/assets/client/", join(here, "../dist")],
  ["/assets/schema/", join(repo, "frontend/packages/schema/dist")],
  ["/assets/wasm-host/", join(repo, "frontend/packages/wasm-host/dist")],
  ["/artifacts/wasm/", join(repo, "artifacts/wasm")],
  ["/", join(here, "../public")],
];

const TYPES: Record<string, string> = {
  ".html": "text/html; charset=utf-8",
  ".css": "text/css; charset=utf-8",
  ".js": "text/javascript; charset=utf-8",
  ".json": "application/json; charset=utf-8",
  ".wasm": "application/wasm",
};

const HEADERS = {
  "x-content-type-options": "nosniff",
  "referrer-policy": "no-referrer",
  "cache-control": "no-store",
  "content-security-policy":
    "default-src 'self'; script-src 'self' 'wasm-unsafe-eval'; style-src 'self' 'unsafe-inline'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'",
};

export function escapeHtml(text: string): string {
  return text.replace(/[&<>"']/g, (c) => `&#${c.charCodeAt(0)};`);
}

function send(
  res: ServerResponse,
  status: number,
  type: string,
  body: string | Uint8Array,
  scriptHashes: string[] = [],
): void {
  const csp = scriptHashes.length
    ? HEADERS["content-security-policy"].replace("script-src 'self'", `script-src 'self' ${scriptHashes.join(" ")}`)
    : HEADERS["content-security-policy"];
  res.writeHead(status, { ...HEADERS, "content-security-policy": csp, "content-type": type });
  res.end(body);
}

/** Inline import maps are allowed only by exact content hash, never by 'unsafe-inline'. */
export function importMapHashes(html: string): string[] {
  const hashes: string[] = [];
  for (const m of html.matchAll(/<script type="importmap">([\s\S]*?)<\/script>/g)) {
    hashes.push(
      `'sha256-${createHash("sha256")
        .update(m[1] ?? "")
        .digest("base64")}'`,
    );
  }
  return hashes;
}

export interface FormErrors {
  name?: string;
  email?: string;
  quantity?: string;
}

export function validateForm(fields: URLSearchParams): { errors: FormErrors; values: Record<string, string> } {
  const values = {
    name: fields.get("name") ?? "",
    email: fields.get("email") ?? "",
    quantity: fields.get("quantity") ?? "",
  };
  const errors: FormErrors = {};
  if (values.name.trim() === "" || values.name.length > 64) errors.name = "Name is required (max 64 characters).";
  if (!/^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(values.email) || values.email.length > 254)
    errors.email = "Enter a valid email address.";
  if (!/^[0-9]{1,3}$/.test(values.quantity) || Number(values.quantity) < 1 || Number(values.quantity) > 100) {
    errors.quantity = "Quantity must be a whole number from 1 to 100.";
  }
  return { errors, values };
}

export function renderForm(values: Record<string, string>, errors: FormErrors): string {
  const err = (k: keyof FormErrors) =>
    errors[k] ? `<p id="${k}-error" class="error">${escapeHtml(errors[k])}</p>` : "";
  const attr = (k: keyof FormErrors) => (errors[k] ? ` aria-invalid="true" aria-describedby="${k}-error"` : "");
  const summary = Object.keys(errors).length
    ? `<div role="alert" class="error"><h2>There are ${Object.keys(errors).length} problems</h2></div>`
    : "";
  return `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>Order form</title><link rel="stylesheet" href="/styles.css"></head>
<body><main><h1>Place an order</h1>${summary}
<form method="post" action="/forms/submit" novalidate>
<label for="name">Name</label><input id="name" name="name" value="${escapeHtml(values.name ?? "")}" required${attr("name")}>${err("name")}
<label for="email">Email</label><input id="email" name="email" type="email" value="${escapeHtml(values.email ?? "")}" required${attr("email")}>${err("email")}
<label for="quantity">Quantity</label><input id="quantity" name="quantity" inputmode="numeric" value="${escapeHtml(values.quantity ?? "")}" required${attr("quantity")}>${err("quantity")}
<p><button type="submit">Submit order</button></p>
</form></main></body></html>`;
}

async function readBody(req: IncomingMessage): Promise<string | null> {
  const chunks: Buffer[] = [];
  let size = 0;
  for await (const chunk of req) {
    size += (chunk as Buffer).length;
    if (size > MAX_BODY) return null;
    chunks.push(chunk as Buffer);
  }
  return Buffer.concat(chunks).toString("utf8");
}

async function serveStatic(pathname: string, res: ServerResponse): Promise<void> {
  const mount = MOUNTS.find(([prefix]) => pathname.startsWith(prefix));
  if (!mount) return send(res, 404, "text/plain; charset=utf-8", "not found");
  let rel: string;
  try {
    rel = decodeURIComponent(pathname.slice(mount[0].length));
  } catch {
    return send(res, 400, "text/plain; charset=utf-8", "bad request");
  }
  if (rel === "" || rel.endsWith("/")) rel += "index.html";
  const base = resolve(mount[1]);
  const target = resolve(base, normalize(rel));
  if (rel.includes("\0") || (target !== base && !target.startsWith(base + sep))) {
    return send(res, 403, "text/plain; charset=utf-8", "forbidden");
  }
  try {
    const bytes = await readFile(target);
    const type = TYPES[extname(target)] ?? "application/octet-stream";
    send(res, 200, type, bytes, type.startsWith("text/html") ? importMapHashes(bytes.toString("utf8")) : []);
  } catch {
    send(res, 404, "text/plain; charset=utf-8", "not found");
  }
}

async function handle(req: IncomingMessage, res: ServerResponse): Promise<void> {
  const url = new URL(req.url ?? "/", "http://fixture.invalid");
  if (url.pathname === "/forms/" || url.pathname === "/forms") {
    if (req.method !== "GET") return send(res, 405, "text/plain; charset=utf-8", "method not allowed");
    return send(res, 200, TYPES[".html"] as string, renderForm({}, {}));
  }
  if (url.pathname === "/forms/submit") {
    if (req.method !== "POST") return send(res, 405, "text/plain; charset=utf-8", "method not allowed");
    const body = await readBody(req);
    if (body === null) return send(res, 413, "text/plain; charset=utf-8", "payload too large");
    const { errors, values } = validateForm(new URLSearchParams(body));
    if (Object.keys(errors).length > 0) return send(res, 422, TYPES[".html"] as string, renderForm(values, errors));
    return send(
      res,
      200,
      TYPES[".html"] as string,
      `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Order accepted</title><link rel="stylesheet" href="/styles.css"></head><body><main><h1>Order accepted</h1><p id="confirmation">${escapeHtml(values.quantity ?? "")} x order for ${escapeHtml(values.name ?? "")}</p></main></body></html>`,
    );
  }
  if (req.method !== "GET" && req.method !== "HEAD")
    return send(res, 405, "text/plain; charset=utf-8", "method not allowed");
  return serveStatic(url.pathname, res);
}

export function startServer(port = 0, host = "127.0.0.1"): Promise<{ server: Server; url: string }> {
  const server = createServer((req, res) => {
    handle(req, res).catch(() => send(res, 500, "text/plain; charset=utf-8", "internal error"));
  });
  return new Promise((resolveListen) => {
    server.listen(port, host, () => {
      const address = server.address();
      const actual = typeof address === "object" && address ? address.port : port;
      resolveListen({ server, url: `http://${host}:${actual}` });
    });
  });
}
