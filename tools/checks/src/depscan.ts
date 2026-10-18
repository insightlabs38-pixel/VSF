import { spawnSync } from "node:child_process";
import { existsSync, readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import {
  checkLicenses,
  type DepFinding,
  type Locked,
  loadAdvisories,
  matchAdvisories,
  parseCargoLock,
  parseGoMod,
  parsePnpmLock,
} from "./depreview.ts";

export interface ScanOptions {
  root: string;
  advisoriesPath: string;
  licenses: boolean;
}

function cargoLicenses(root: string): { name: string; version: string; license: string | undefined }[] {
  const r = spawnSync(
    "cargo",
    ["metadata", "--locked", "--format-version", "1", "--manifest-path", join(root, "compiler/Cargo.toml")],
    {
      encoding: "utf8",
      maxBuffer: 256 * 1024 * 1024,
    },
  );
  if (r.status !== 0) throw new Error(`cargo metadata failed: ${r.stderr}`);
  const meta = JSON.parse(r.stdout) as {
    packages: { name: string; version: string; source: string | null; license: string | null }[];
  };
  return meta.packages
    .filter((p) => p.source !== null)
    .map((p) => ({ name: p.name, version: p.version, license: p.license ?? undefined }));
}

interface Override {
  name?: string;
  prefix?: string;
  version: string;
  license: string;
}

const label = (o: Override): string => `${o.name ?? `${o.prefix}*`}@${o.version}`;

type LicenseEntry = { name: string; version: string; license: string | undefined };

export function applyOverrides(
  entries: LicenseEntry[],
  overrides: readonly Override[],
): { entries: LicenseEntry[]; stale: DepFinding[] } {
  const used = new Set<string>();
  const merged = entries.map((e) => {
    const hit = overrides.find(
      (o) => o.version === e.version && (o.name === e.name || (o.prefix !== undefined && e.name.startsWith(o.prefix))),
    );
    if (hit && e.license === undefined) {
      used.add(label(hit));
      return { ...e, license: hit.license };
    }
    return e;
  });
  const stale = overrides
    .filter((o) => !used.has(label(o)))
    .map((o) => ({
      rule: "license.stale-override",
      package: label(o),
      detail: "override not needed or package not locked",
    }));
  return { entries: merged, stale };
}

function npmLicenses(
  root: string,
  locked: readonly Locked[],
): { name: string; version: string; license: string | undefined }[] {
  const store = join(root, "node_modules/.pnpm");
  const dirs = existsSync(store) ? readdirSync(store) : [];
  return locked.map((l) => {
    const prefix = `${l.name.replace("/", "+")}@${l.version}`;
    const dir = dirs.find((d) => d === prefix || d.startsWith(`${prefix}_`));
    let license: string | undefined;
    if (dir) {
      const pkg = JSON.parse(readFileSync(join(store, dir, "node_modules", l.name, "package.json"), "utf8")) as {
        license?: string | { type?: string };
      };
      license = typeof pkg.license === "string" ? pkg.license : pkg.license?.type;
    }
    return { name: l.name, version: l.version, license };
  });
}

export function scan(opts: ScanOptions): { locked: Locked[]; findings: DepFinding[] } {
  const { root } = opts;
  const findings: DepFinding[] = [];
  const locked: Locked[] = [];
  const cargo = parseCargoLock(readFileSync(join(root, "compiler/Cargo.lock"), "utf8"));
  const npm = parsePnpmLock(readFileSync(join(root, "pnpm-lock.yaml"), "utf8"));
  const goSumPath = join(root, "backend/go.sum");
  const go = parseGoMod(
    readFileSync(join(root, "backend/go.mod"), "utf8"),
    existsSync(goSumPath) ? readFileSync(goSumPath, "utf8") : "",
  );
  for (const part of [cargo, npm, go]) {
    locked.push(...part.locked);
    findings.push(...part.findings);
  }
  findings.push(...matchAdvisories(locked, loadAdvisories(opts.advisoriesPath)));
  if (opts.licenses) {
    findings.push(...checkLicenses(cargoLicenses(root)));
    const overridePath = join(root, "supply-chain/license-overrides.json");
    const overrides = existsSync(overridePath)
      ? (JSON.parse(readFileSync(overridePath, "utf8")) as { overrides: Override[] }).overrides
      : [];
    const merged = applyOverrides(npmLicenses(root, npm.locked), overrides);
    findings.push(...checkLicenses(merged.entries));
    // Overrides are platform-dependent; a package installed here must not need one.
    findings.push(...merged.stale.filter((f) => !npm.locked.some((l) => `${l.name}@${l.version}` === f.package)));
  }
  return { locked, findings };
}
