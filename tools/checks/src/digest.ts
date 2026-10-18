import { createHash } from "node:crypto";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative } from "node:path";

export interface DigestEntry {
  path: string;
  bytes: number;
  sha256: string;
}

function walk(dir: string, out: string[]): void {
  for (const name of readdirSync(dir).sort()) {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) walk(p, out);
    else out.push(p);
  }
}

/** Sorted, path-relative digests so two builds in different directories are directly comparable. */
export function digestTree(root: string, dirs: readonly string[]): DigestEntry[] {
  const files: string[] = [];
  for (const d of dirs) walk(join(root, d), files);
  return files.map((f) => {
    const bytes = readFileSync(f);
    return {
      path: relative(root, f).split("\\").join("/"),
      bytes: bytes.length,
      sha256: createHash("sha256").update(bytes).digest("hex"),
    };
  });
}

export function rollup(entries: readonly DigestEntry[]): string {
  const h = createHash("sha256");
  for (const e of entries) h.update(`${e.path}\0${e.sha256}\n`);
  return h.digest("hex");
}
