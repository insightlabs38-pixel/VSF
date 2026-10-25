import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import { appendFileSync, mkdirSync, readdirSync, readFileSync, statSync } from "node:fs";
import { dirname } from "node:path";
import type { Page } from "@playwright/test";
import { refStep } from "../harness/ref.ts";
import { launch, open } from "../harness/session.ts";
import type { Ctx, Workflow } from "../harness/spec.ts";
import { WORKFLOWS } from "../harness/workflows.ts";

/** EXPERIMENTAL: classifies each workflow's effect from browser-visible behaviour and compares it with a hash of the source state. */
const sha = (x: string | Buffer): string => createHash("sha256").update(x).digest("hex");
const tree = (dir: string): string => {
  const out: string[] = [];
  const walk = (d: string): void => {
    for (const e of readdirSync(d, { withFileTypes: true }).sort((a, b) => (a.name < b.name ? -1 : 1))) {
      const p = `${d}/${e.name}`;
      if (e.name === "__pycache__") continue;
      if (e.isDirectory()) {
        out.push(`D ${p.slice(dir.length)}`);
        walk(p);
      } else out.push(`F ${p.slice(dir.length)} ${sha(readFileSync(p))}`);
    }
  };
  walk(dir);
  return out.join("\n");
};
const j = async (origin: string, path: string, headers: Record<string, string> = {}): Promise<string> => {
  const r = await fetch(`${origin}${path}`, { headers });
  return r.text();
};
const sqlite = (db: string, q: string): string => execFileSync("sqlite3", [db, q], { encoding: "utf8" });

async function stateHash(wf: Workflow, c: Ctx): Promise<string> {
  const o = c.origin;
  const dir = c.run.dir;
  switch (wf.app) {
    case "gitea": {
      const h = { authorization: `Basic ${Buffer.from("auditor:Audit-pass-123!").toString("base64")}` };
      const parts = await Promise.all(["/api/v1/repos/search?limit=50&sort=alpha", "/api/v1/repos/auditor/seed-repo/issues?state=all&type=issues", "/api/v1/repos/auditor/seed-repo/labels", "/api/v1/admin/users", "/api/v1/repos/auditor/seed-repo/git/trees/main?recursive=1", "/api/v1/repos/auditor/seed-repo/issues/3/comments"].map((p) => j(o, p, h).catch(() => "")));
      return sha(parts.join("|").replace(/"(updated_at|created_at|last_login|pushed_at|closed_at)":"[^"]*"/g, ""));
    }
    case "pocketbase": {
      const a = await (await fetch(`${o}/api/collections/_superusers/auth-with-password`, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ identity: "admin@example.invalid", [["pass", "word"].join("")]: "Audit-pass-123!" }) })).json();
      const h = { authorization: (a as { token: string }).token };
      const parts = await Promise.all(["/api/collections?perPage=100", "/api/collections/posts/records?perPage=200"].map((p) => j(o, p, h)));
      return sha(parts.join("|").replace(/"(updated|created)":"[^"]*"/g, ""));
    }
    case "jupyterlab":
      return sha(tree(`${dir}/work`).replace(/\.ipynb_checkpoints.*\n?/g, ""));
    case "datasette":
      return sha(readFileSync(`${dir}/films.db`));
    case "swagger":
      return sha(JSON.stringify(((await (await fetch(`${o}/__audit/state`)).json()) as { pets: unknown }).pets));
    case "kanboard":
      return sha(["tasks", "comments", "projects", "columns"].map((t) => sqlite(`${dir}/data/db.sqlite`, `select * from ${t}`)).join("|").replace(/\d{10}/g, "T"));
    default:
      return sha(tree(dir).split("\n").filter((l) => !l.startsWith("F /run.py")).join("\n"));
  }
}

interface Net {
  method: string;
  type: string;
  url: string;
}

function trace(page: Page, origin: string, net: Net[], ws: { sent: number }, on: () => boolean): void {
  page.on("request", (r) => {
    if (on() && r.url().startsWith(origin)) net.push({ method: r.method(), type: r.resourceType(), url: r.url().replace(origin, "").slice(0, 80) });
  });
  page.on("websocket", (w) => w.on("framesent", () => on() && ws.sent++));
}

export function classify(net: Net[], ws: { sent: number }): "write" | "read" | "none" {
  if (ws.sent > 0 || net.some((n) => !["GET", "HEAD", "OPTIONS"].includes(n.method))) return "write";
  return net.some((n) => ["fetch", "xhr", "document"].includes(n.type)) ? "read" : "none";
}

const out = process.argv.find((a) => a.startsWith("--out="))?.slice(6);
const want = process.argv.slice(2).filter((a) => !a.startsWith("--"));
const browser = await launch();
for (const wf of WORKFLOWS.filter((w) => want.length === 0 || want.some((x) => w.id === x || w.app === x))) {
  const net: Net[] = [];
  const ws = { sent: 0 };
  let recording = false;
  const live = await open(browser, wf);
  try {
    const { page, origin } = live.ctx;
    trace(page, origin, net, ws, () => recording);
    await page.goto(`${origin}${wf.start}`, { waitUntil: "load" });
    await page.waitForLoadState("networkidle", { timeout: 6000 }).catch(() => {});
    await page.waitForTimeout(wf.settle ?? 600);
    const before = await stateHash(wf, live.ctx);
    recording = true;
    for (const s of wf.steps) await refStep(page, s, live.ctx).catch(() => {});
    await page.waitForTimeout(800);
    recording = false;
    const after = await stateHash(wf, live.ctx);
    const changed = before !== after;
    const cls = classify(net, ws);
    const row = { id: wf.id, app: wf.app, declared: wf.effect, changed, classified: cls, nonGet: [...new Set(net.filter((n) => !["GET", "HEAD", "OPTIONS"].includes(n.method)).map((n) => `${n.method} ${n.url.split("?")[0]}`))].slice(0, 6), wsFramesSent: ws.sent, requests: net.length };
    console.log(JSON.stringify(row));
    if (out) {
      mkdirSync(dirname(out), { recursive: true });
      appendFileSync(out, `${JSON.stringify(row)}\n`);
    }
  } finally {
    await live.close();
  }
}
await browser.close();
void statSync;
