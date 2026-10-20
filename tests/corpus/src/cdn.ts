import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { existsSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { CACHE } from "./manifest.ts";

export interface NpmRef {
  name: string;
  version: string;
  path: string;
}

const NPM_CDN =
  /^https:\/\/(?:cdn\.jsdelivr\.net\/npm|unpkg\.com)\/((?:@[^/@]+\/)?[^/@]+)@(\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?)\/(.+)$/;

/** Maps a pinned-version CDN address to the npm package file it names; an unpinned or other address is not mirrored. */
export function parseCdn(url: string): NpmRef | null {
  const jq = /^https:\/\/code\.jquery\.com\/jquery-(\d+\.\d+\.\d+)(\.min)?\.js$/.exec(url.split("?")[0] ?? "");
  if (jq) return { name: "jquery", version: jq[1] ?? "", path: `dist/jquery${jq[2] ?? ""}.js` };
  const m = NPM_CDN.exec(url.split("?")[0] ?? "");
  if (!m) return null;
  const path = m[3] ?? "";
  if (path.split("/").some((p) => p === ".." || p === "")) return null;
  return { name: m[1] ?? "", version: m[2] ?? "", path };
}

const dirFor = (r: NpmRef): string => `${CACHE}npm/${r.name.replace("/", "__")}@${r.version}/`;

function sh(cmd: string, args: string[]): string {
  const r = spawnSync(cmd, args, { encoding: "utf8" });
  if (r.status !== 0) throw new Error(`${cmd} failed: ${r.stderr.trim().slice(0, 200)}`);
  return r.stdout;
}

/** Downloads the exact package version once, checks the registry's integrity value and unpacks it. */
export function ensurePackage(r: NpmRef): string {
  const dir = dirFor(r);
  if (existsSync(`${dir}.ok`)) return dir;
  rmSync(dir, { recursive: true, force: true });
  mkdirSync(dir, { recursive: true });
  const meta = JSON.parse(
    sh("curl", ["-sS", "--fail", `https://registry.npmjs.org/${r.name.replace("/", "%2f")}/${r.version}`]),
  ) as {
    dist: { tarball: string; integrity?: string; shasum: string };
  };
  const tgz = `${dir}pkg.tgz`;
  sh("curl", ["-sSL", "--fail", "-o", tgz, meta.dist.tarball]);
  const bytes = readFileSync(tgz);
  const sha1 = createHash("sha1").update(bytes).digest("hex");
  const integrity = meta.dist.integrity?.startsWith("sha512-")
    ? `sha512-${createHash("sha512").update(bytes).digest("base64")}` === meta.dist.integrity
    : sha1 === meta.dist.shasum;
  if (!integrity) throw new Error(`${r.name}@${r.version}: tarball does not match the registry integrity`);
  sh("tar", ["xzf", tgz, "-C", dir, "--strip-components=1"]);
  writeFileSync(`${dir}.ok`, sha1);
  return dir;
}

export function mirrorFile(url: string): { body: Buffer; type: string } | null {
  const ref = parseCdn(url);
  if (!ref) return null;
  const file = `${ensurePackage(ref)}${ref.path}`;
  if (!existsSync(file)) return null;
  const ext = ref.path.split(".").pop() ?? "";
  const types: Record<string, string> = {
    js: "text/javascript",
    mjs: "text/javascript",
    css: "text/css",
    woff2: "font/woff2",
    woff: "font/woff",
    svg: "image/svg+xml",
    json: "application/json",
    map: "application/json",
  };
  return { body: readFileSync(file), type: types[ext] ?? "application/octet-stream" };
}
