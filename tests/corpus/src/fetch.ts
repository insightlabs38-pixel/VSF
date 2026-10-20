import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { existsSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { CACHE, type Source } from "./manifest.ts";

export const dirOf = (s: Source): string => `${CACHE}src/${s.id}/`;

function run(cwd: string, cmd: string, args: string[]): string {
  const r = spawnSync(cmd, args, { cwd, encoding: "utf8" });
  if (r.status !== 0) throw new Error(`${cmd} ${args.join(" ")} failed: ${r.stderr.trim().slice(0, 300)}`);
  return r.stdout.trim();
}

/** Fetches exactly the pinned revision or release and refuses anything else. */
export function fetchSource(s: Source): string {
  const dir = dirOf(s);
  const stamp = `${dir}.pinned`;
  const want = s.fetch.kind === "git" ? s.fetch.rev : s.fetch.sha256;
  if (existsSync(stamp) && readFileSync(stamp, "utf8") === want) return dir;
  rmSync(dir, { recursive: true, force: true });
  mkdirSync(dir, { recursive: true });
  if (s.fetch.kind === "git") {
    run(dir, "git", ["init", "-q"]);
    run(dir, "git", ["remote", "add", "origin", s.fetch.url]);
    if (s.fetch.sparse) run(dir, "git", ["sparse-checkout", "set", ...s.fetch.sparse]);
    run(dir, "git", ["fetch", "-q", "--depth", "1", "--filter=blob:none", "origin", s.fetch.rev]);
    run(dir, "git", ["checkout", "-q", "FETCH_HEAD"]);
    const got = run(dir, "git", ["rev-parse", "HEAD"]);
    if (got !== s.fetch.rev) throw new Error(`${s.id}: fetched ${got}, pinned ${s.fetch.rev}`);
  } else {
    const file = `${dir}download.bin`;
    run(dir, "curl", ["-sSL", "--fail", "-o", file, s.fetch.url]);
    const got = createHash("sha256").update(readFileSync(file)).digest("hex");
    if (got !== s.fetch.sha256) {
      rmSync(file);
      throw new Error(`${s.id}: release sha256 ${got}, pinned ${s.fetch.sha256}`);
    }
    run(dir, "tar", ["xzf", file]);
  }
  writeFileSync(stamp, want);
  return dir;
}
