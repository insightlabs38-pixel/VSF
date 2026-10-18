import { createHash } from "node:crypto";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";

interface SourceEntry {
  id: string;
  family: string;
  family_code: string;
  layer: string;
  launch_priority: string;
  name: string;
}

interface SourceFamily {
  code: string;
  name: string;
  layer: string;
  priority: string;
  description: string;
  count: number;
}

interface SourceCatalog {
  version: string;
  status: string;
  count: number;
  family_count: number;
  families: SourceFamily[];
  entries: SourceEntry[];
}

export const MATURITY_LEVELS = ["candidate", "specified", "implemented", "conformant"] as const;

export const SOURCE_JSON = "docs/deep/plan/09_Reference_Catalog/universal_interface_primitive_catalog.json";
export const SOURCE_CSV = "docs/deep/plan/09_Reference_Catalog/universal_interface_primitive_catalog.csv";
export const SOURCE_GAPS = "docs/deep/plan/09_Reference_Catalog/vsf_preimplementation_gap_register.md";
export const REGISTRY_PATH = "registry/candidates/catalog.v0.json";
export const GAPS_PATH = "registry/gaps/gap-register.v0.json";

const sha256 = (b: Buffer | string): string => createHash("sha256").update(b).digest("hex");

export function buildRegistry(root: string): { registry: string; gaps: string } {
  const raw = readFileSync(join(root, SOURCE_JSON));
  const src = JSON.parse(raw.toString("utf8")) as SourceCatalog;
  const registry = {
    schema: "vsf-candidate-registry-v0",
    note: "Imported inventory of candidate capabilities. Registration is not implementation or conformance evidence.",
    source: { file: SOURCE_JSON, sha256: sha256(raw), version: src.version, status: src.status },
    maturity_levels: MATURITY_LEVELS,
    counts: { entries: src.entries.length, families: src.families.length },
    families: src.families.map((f) => ({
      code: f.code,
      name: f.name,
      layer: f.layer,
      priority: f.priority,
      count: f.count,
    })),
    entries: src.entries.map((e) => ({
      id: e.id,
      family_code: e.family_code,
      family: e.family,
      layer: e.layer,
      launch_priority: e.launch_priority,
      name: e.name,
      maturity: "candidate",
    })),
  };
  const md = readFileSync(join(root, SOURCE_GAPS), "utf8");
  const rows = md
    .split("\n")
    .filter((l) => l.startsWith("| ") && !l.startsWith("| Area"))
    .map((l) =>
      l
        .split("|")
        .slice(1, -1)
        .map((c) => c.trim()),
    )
    .filter((c) => c.length === 5 && /^P[012]$/.test(c[1] ?? ""));
  const gaps = {
    schema: "vsf-gap-register-v0",
    source: { file: SOURCE_GAPS, sha256: sha256(md) },
    gaps: rows.map(([area, priority, catalog_assessment, decision, minimum_verification], i) => ({
      id: `GAP-${String(i + 1).padStart(2, "0")}`,
      area,
      priority,
      catalog_assessment,
      decision,
      minimum_verification,
      status: "open",
    })),
  };
  return { registry: `${JSON.stringify(registry, null, 1)}\n`, gaps: `${JSON.stringify(gaps, null, 1)}\n` };
}

export function writeRegistry(root: string): void {
  const { registry, gaps } = buildRegistry(root);
  for (const [rel, body] of [
    [REGISTRY_PATH, registry],
    [GAPS_PATH, gaps],
  ] as const) {
    mkdirSync(dirname(join(root, rel)), { recursive: true });
    writeFileSync(join(root, rel), body);
  }
}

export function validateRegistry(root: string): string[] {
  const problems: string[] = [];
  const reg = JSON.parse(readFileSync(join(root, REGISTRY_PATH), "utf8")) as ReturnType<typeof JSON.parse>;
  const src = JSON.parse(readFileSync(join(root, SOURCE_JSON), "utf8")) as SourceCatalog;
  const built = buildRegistry(root);
  if (readFileSync(join(root, REGISTRY_PATH), "utf8") !== built.registry)
    problems.push("registry differs from deterministic import of the source catalog");
  if (readFileSync(join(root, GAPS_PATH), "utf8") !== built.gaps)
    problems.push("gap register differs from deterministic import");
  if (reg.entries.length !== 924 || src.count !== 924)
    problems.push(`expected 924 entries, found ${reg.entries.length}`);
  if (reg.families.length !== 29) problems.push(`expected 29 families, found ${reg.families.length}`);
  const ids = new Set<string>();
  const perFamily = new Map<string, number>();
  for (const e of reg.entries as { id: string; family_code: string; maturity: string }[]) {
    if (ids.has(e.id)) problems.push(`duplicate id ${e.id}`);
    ids.add(e.id);
    if (!e.id.startsWith(`${e.family_code}-`)) problems.push(`${e.id} does not belong to family ${e.family_code}`);
    if (e.maturity !== "candidate") problems.push(`${e.id} claims maturity ${e.maturity} without conformance evidence`);
    perFamily.set(e.family_code, (perFamily.get(e.family_code) ?? 0) + 1);
  }
  for (const f of reg.families as { code: string; count: number }[]) {
    if (perFamily.get(f.code) !== f.count)
      problems.push(`family ${f.code} count ${f.count} != ${perFamily.get(f.code)}`);
  }
  const csvIds = readFileSync(join(root, SOURCE_CSV), "utf8")
    .split("\n")
    .slice(1)
    .filter(Boolean)
    .map((l) => l.split(",")[0]);
  if (csvIds.length !== ids.size || csvIds.some((id) => !ids.has(id ?? "")))
    problems.push("CSV and JSON catalogs disagree");
  const gaps = JSON.parse(readFileSync(join(root, GAPS_PATH), "utf8")) as { gaps: { id: string; status: string }[] };
  if (gaps.gaps.length !== 41) problems.push(`expected 41 gaps, found ${gaps.gaps.length}`);
  if (gaps.gaps.some((g) => g.status !== "open")) problems.push("gap register claims a closed gap without evidence");
  return problems;
}
