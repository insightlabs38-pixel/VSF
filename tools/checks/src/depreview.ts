import { readFileSync } from "node:fs";

export type Ecosystem = "cargo" | "npm" | "go";

export interface Locked {
  ecosystem: Ecosystem;
  name: string;
  version: string;
}

export interface Advisory {
  id: string;
  ecosystem: Ecosystem;
  name: string;
  below: string;
  reason: string;
}

export interface DepFinding {
  rule: string;
  package: string;
  detail: string;
}

export const REGISTRY = "registry+https://github.com/rust-lang/crates.io-index";

export function parseCargoLock(text: string): { locked: Locked[]; findings: DepFinding[] } {
  const locked: Locked[] = [];
  const findings: DepFinding[] = [];
  for (const block of text.split("[[package]]").slice(1)) {
    const field = (k: string): string | undefined => new RegExp(`^${k} = "([^"]*)"`, "m").exec(block)?.[1];
    const name = field("name") ?? "?";
    const version = field("version") ?? "?";
    const source = field("source");
    if (source === undefined) continue;
    locked.push({ ecosystem: "cargo", name, version });
    if (source !== REGISTRY)
      findings.push({ rule: "cargo.non-registry-source", package: `${name}@${version}`, detail: source });
    if (!field("checksum"))
      findings.push({ rule: "cargo.missing-checksum", package: `${name}@${version}`, detail: "no checksum" });
  }
  return { locked, findings };
}

export function parsePnpmLock(text: string): { locked: Locked[]; findings: DepFinding[] } {
  const locked: Locked[] = [];
  const findings: DepFinding[] = [];
  const start = text.indexOf("\npackages:\n");
  if (start < 0) return { locked, findings };
  const end = text.indexOf("\nsnapshots:\n", start);
  const section = text.slice(start + 1, end < 0 ? undefined : end);
  for (const entry of section.split(/\n\n(?= {2}\S)/).slice(1)) {
    const key = /^\s*'?([^'\n]+?)'?:\s*$/m.exec(entry)?.[1] ?? "";
    const at = key.lastIndexOf("@");
    const name = key.slice(0, at);
    const version = key.slice(at + 1);
    locked.push({ ecosystem: "npm", name, version });
    const resolution = /resolution: \{([^}]*)\}/.exec(entry)?.[1] ?? "";
    if (!/integrity: sha512-/.test(resolution))
      findings.push({ rule: "npm.missing-integrity", package: key, detail: resolution || "no resolution" });
    if (/tarball:|repo:|commit:|type: git/.test(resolution))
      findings.push({ rule: "npm.non-registry-source", package: key, detail: resolution });
  }
  return { locked, findings };
}

export function parseGoMod(mod: string, sum: string): { locked: Locked[]; findings: DepFinding[] } {
  const locked: Locked[] = [];
  const findings: DepFinding[] = [];
  const requires = [...mod.matchAll(/^\s*(?:require\s+)?([\w.\-/]+\.[\w.\-/]+)\s+(v\S+)(?:\s+\/\/ indirect)?\s*$/gm)];
  for (const [, name = "", version = ""] of requires) {
    locked.push({ ecosystem: "go", name, version: version.replace(/^v/, "") });
    if (!sum.includes(`${name} ${version} h1:`))
      findings.push({ rule: "go.missing-sum", package: `${name}@${version}`, detail: "no go.sum entry" });
  }
  for (const m of mod.matchAll(/^\s*replace\s+.*=>\s*(\S+)/gm)) {
    if (!/^\.{1,2}\//.test(m[1] ?? ""))
      findings.push({ rule: "go.non-local-replace", package: m[1] ?? "", detail: m[0].trim() });
  }
  return { locked, findings };
}

const num = (v: string): number[] =>
  v
    .split(/[.+-]/)
    .slice(0, 3)
    .map((p) => Number.parseInt(p, 10) || 0);

export function versionBelow(version: string, bound: string): boolean {
  const a = num(version);
  const b = num(bound);
  for (let i = 0; i < 3; i++) {
    if ((a[i] ?? 0) !== (b[i] ?? 0)) return (a[i] ?? 0) < (b[i] ?? 0);
  }
  return false;
}

export function matchAdvisories(locked: readonly Locked[], advisories: readonly Advisory[]): DepFinding[] {
  const out: DepFinding[] = [];
  for (const l of locked) {
    for (const a of advisories) {
      if (a.ecosystem === l.ecosystem && a.name === l.name && versionBelow(l.version, a.below)) {
        out.push({
          rule: "advisory",
          package: `${l.name}@${l.version}`,
          detail: `${a.id}: ${a.reason} (fixed in ${a.below})`,
        });
      }
    }
  }
  return out;
}

const ALLOWED = new Set([
  "MIT",
  "Apache-2.0",
  "BSD-2-Clause",
  "BSD-3-Clause",
  "ISC",
  "0BSD",
  "Unlicense",
  "Zlib",
  "MPL-2.0",
  "BlueOak-1.0.0",
  "CC0-1.0",
  "Python-2.0",
  "Unicode-3.0",
  "Unicode-DFS-2016",
  "MIT-0",
  "CC-BY-4.0",
]);

/** SPDX expression evaluated conservatively: OR needs one allowed arm, AND needs all. */
export function licenseAllowed(expr: string): boolean {
  const normalized = expr.replace(/[()]/g, " ").replace(/\//g, " OR ").trim();
  if (normalized === "") return false;
  return normalized
    .split(/\s+OR\s+/)
    .some((alt) => alt.split(/\s+AND\s+/).every((id) => ALLOWED.has(id.trim().replace(/\s+WITH\s+.*/, ""))));
}

export function checkLicenses(
  entries: readonly { name: string; version: string; license: string | undefined }[],
): DepFinding[] {
  const out: DepFinding[] = [];
  for (const e of entries) {
    if (!e.license)
      out.push({ rule: "license.missing", package: `${e.name}@${e.version}`, detail: "no license metadata" });
    else if (!licenseAllowed(e.license))
      out.push({ rule: "license.disallowed", package: `${e.name}@${e.version}`, detail: e.license });
  }
  return out;
}

export function loadAdvisories(path: string): Advisory[] {
  return (JSON.parse(readFileSync(path, "utf8")) as { advisories: Advisory[] }).advisories;
}
