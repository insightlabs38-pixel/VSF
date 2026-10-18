import { spawnSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { join } from "node:path";

export interface ToolchainManifest {
  schema: string;
  rust: string;
  rust_targets: string[];
  go: string;
  node: string;
  pnpm: string;
  playwright: string;
  playwright_chromium_revision: string;
}

export interface Finding {
  id: string;
  message: string;
}

const read = (root: string, rel: string): string => readFileSync(join(root, rel), "utf8");

export function loadManifest(root: string): ToolchainManifest {
  return JSON.parse(read(root, "toolchain/versions.json")) as ToolchainManifest;
}

/** Cross-checks every file that pins a version against the single manifest. */
export function checkPins(root: string): Finding[] {
  const m = loadManifest(root);
  const out: Finding[] = [];
  const expect = (id: string, actual: string | undefined, want: string): void => {
    if (actual !== want) out.push({ id, message: `${id}: expected ${want}, found ${actual ?? "nothing"}` });
  };
  expect("rust-toolchain.channel", /^channel\s*=\s*"([^"]+)"/m.exec(read(root, "rust-toolchain.toml"))?.[1], m.rust);
  for (const target of m.rust_targets) {
    if (!read(root, "rust-toolchain.toml").includes(`"${target}"`))
      out.push({ id: "rust-toolchain.targets", message: `missing target ${target}` });
  }
  expect("go.mod.go", /^go\s+(\S+)/m.exec(read(root, "backend/go.mod"))?.[1], m.go);
  expect(".node-version", read(root, ".node-version").trim(), m.node);
  const pkg = JSON.parse(read(root, "package.json")) as {
    packageManager?: string;
    engines?: Record<string, string>;
    devDependencies?: Record<string, string>;
  };
  expect("package.packageManager", pkg.packageManager, `pnpm@${m.pnpm}`);
  expect("package.engines.node", pkg.engines?.node, m.node);
  expect("package.engines.pnpm", pkg.engines?.pnpm, m.pnpm);
  expect("package.playwright", pkg.devDependencies?.["@playwright/test"], m.playwright);
  for (const [name, range] of Object.entries(pkg.devDependencies ?? {})) {
    if (!/^\d+\.\d+\.\d+$/.test(range))
      out.push({ id: "package.devDependencies", message: `${name}@${range} is not an exact pin` });
  }
  return out;
}

export interface ProbeResult {
  tool: string;
  found: boolean;
  version?: string;
}

function probe(tool: string, args: string[], cwd: string, env: NodeJS.ProcessEnv): ProbeResult {
  const r = spawnSync(tool, args, { cwd, env, encoding: "utf8" });
  if (r.error || r.status !== 0) return { tool, found: false };
  return { tool, found: true, version: `${r.stdout}${r.stderr}`.trim() };
}

/** Probes installed tools; a missing tool is reported rather than silently skipped. */
export function checkInstalled(root: string, env: NodeJS.ProcessEnv = process.env): Finding[] {
  const m = loadManifest(root);
  const out: Finding[] = [];
  const need: [string, string[], RegExp, string][] = [
    ["rustc", ["--version"], /rustc (\S+)/, m.rust],
    ["cargo", ["--version"], /cargo (\S+)/, m.rust],
    ["go", ["version"], /go version go(\S+)/, m.go],
    ["node", ["--version"], /v(\S+)/, m.node],
    ["pnpm", ["--version"], /^(\S+)/, m.pnpm],
  ];
  for (const [tool, args, re, want] of need) {
    const p = probe(tool, args, root, env);
    if (!p.found) {
      out.push({ id: `missing-toolchain.${tool}`, message: `${tool} not found on PATH` });
      continue;
    }
    const got = re.exec(p.version ?? "")?.[1];
    if (got !== want)
      out.push({ id: `toolchain.${tool}`, message: `${tool}: expected ${want}, found ${got ?? p.version}` });
  }
  return out;
}
