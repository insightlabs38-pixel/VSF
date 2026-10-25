import { type ChildProcess, spawn } from "node:child_process";
import { existsSync, readFileSync, statSync } from "node:fs";
import { createServer, type Server } from "node:http";
import type { AddressInfo } from "node:net";
import { extname, join, normalize, sep } from "node:path";
import { dirOf } from "./fetch.ts";
import { ROOT, type Source } from "./manifest.ts";

const TYPES: Record<string, string> = {
  ".html": "text/html; charset=utf-8",
  ".htm": "text/html; charset=utf-8",
  ".js": "text/javascript",
  ".mjs": "text/javascript",
  ".css": "text/css",
  ".json": "application/json",
  ".svg": "image/svg+xml",
  ".png": "image/png",
  ".jpg": "image/jpeg",
  ".jpeg": "image/jpeg",
  ".gif": "image/gif",
  ".woff": "font/woff",
  ".woff2": "font/woff2",
  ".ico": "image/x-icon",
  ".txt": "text/plain",
  ".map": "application/json",
};

export interface Running {
  origin: string;
  stop(): Promise<void>;
}

/** Serves a directory read-only and refuses to leave it. */
export function serveStatic(root: string, mounts: Record<string, string> = {}): Promise<Running> {
  const base = normalize(root.endsWith(sep) ? root : root + sep);
  const server: Server = createServer((req, res) => {
    let path: string;
    try {
      path = decodeURIComponent((req.url ?? "/").split("?")[0] ?? "/");
    } catch {
      res.writeHead(400).end();
      return;
    }
    const mount = Object.keys(mounts).find((m) => path === m || path.startsWith(`${m}/`));
    const mountBase = mount
      ? normalize(mounts[mount]?.endsWith(sep) ? (mounts[mount] as string) : `${mounts[mount]}${sep}`)
      : base;
    let file = normalize(join(mountBase, mount ? path.slice(mount.length) : path));
    if (!file.startsWith(mountBase)) {
      res.writeHead(403).end();
      return;
    }
    if (existsSync(file) && statSync(file).isDirectory()) file = join(file, "index.html");
    if (!existsSync(file)) {
      res.writeHead(404, { "content-type": "text/plain" }).end("not found");
      return;
    }
    res
      .writeHead(200, { "content-type": TYPES[extname(file).toLowerCase()] ?? "application/octet-stream" })
      .end(readFileSync(file));
  });
  return new Promise((resolve) =>
    server.listen(0, "127.0.0.1", () => {
      const port = (server.address() as AddressInfo).port;
      resolve({
        origin: `http://127.0.0.1:${port}`,
        stop: () =>
          new Promise<void>((r) => {
            server.close(() => r());
            server.closeAllConnections();
          }),
      });
    }),
  );
}

async function healthy(url: string, ms: number): Promise<boolean> {
  const end = Date.now() + ms;
  while (Date.now() < end) {
    try {
      if ((await fetch(url)).ok) return true;
    } catch {
      /* not up yet */
    }
    await new Promise((r) => setTimeout(r, 200));
  }
  return false;
}

/** Starts an application as a disposable local process with its own state directory, and waits until it answers. */
export async function serveProcess(s: Source, work: string, port: number): Promise<Running> {
  if (s.serve?.kind !== "process") throw new Error(`${s.id} is not a process`);
  const dir = dirOf(s);
  const sub = (a: string): string =>
    a.replaceAll("{src}", dir).replaceAll("{work}", work).replaceAll("{port}", String(port)).replaceAll("{root}", ROOT);
  const env = { ...process.env, CORPUS_WORK: work, PYTHONDONTWRITEBYTECODE: "1" };
  for (const step of s.serve.setup ?? []) {
    const [cmd = "", ...args] = step.map(sub);
    await new Promise<void>((resolve, reject) => {
      const c = spawn(cmd, args, { cwd: work, stdio: "ignore", env });
      c.on("exit", (code) => (code === 0 ? resolve() : reject(new Error(`${s.id}: setup ${cmd} exited ${code}`))));
    });
  }
  const [cmd = "", ...args] = s.serve.command.map(sub);
  const child: ChildProcess = spawn(cmd, args, { cwd: work, stdio: "ignore", env });
  const origin = `http://127.0.0.1:${port}`;
  if (!(await healthy(`${origin}${s.serve.health}`, 20_000))) {
    child.kill("SIGKILL");
    throw new Error(`${s.id}: did not become healthy at ${s.serve.health}`);
  }
  return {
    origin,
    stop: () =>
      new Promise<void>((r) => {
        child.once("exit", () => r());
        child.kill("SIGTERM");
        setTimeout(() => child.kill("SIGKILL"), 3000).unref();
      }),
  };
}
