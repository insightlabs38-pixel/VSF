import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

export const ROOT = fileURLToPath(new URL("../../../", import.meta.url));
export const CACHE = `${ROOT}.corpus-cache/`;
export const MANIFEST_PATH = fileURLToPath(new URL("../corpus.json", import.meta.url));

export type Partition = "dev" | "validation" | "heldout";
export const PARTITIONS: Partition[] = ["dev", "validation", "heldout"];

export interface UploadStep {
  selector: string;
  files: { name: string; text: string; mimeType?: string }[];
}

export interface PageStep {
  /** A user-level action reaching a state worth annotating, e.g. opening a dialog. */
  click?: string;
  fill?: { selector: string; value: string };
  press?: string;
  upload?: UploadStep;
  waitMs?: number;
}

export interface Page {
  id: string;
  path: string;
  /** What a person does on this page, in the application's own terms. */
  workflows: string[];
  steps?: PageStep[];
  /** Why this state, when it is not the page as loaded. */
  state?: string;
}

export interface GroundTruth {
  /** Where the expected behaviour comes from, independent of VSF output. */
  basis: string[];
  method: string;
  annotator: string;
}

export interface GitSource {
  kind: "git";
  url: string;
  rev: string;
  sparse?: string[];
}

export interface ReleaseSource {
  kind: "release";
  url: string;
  sha256: string;
}

export interface PipSource {
  kind: "pip";
  /** Exact requirement lines, each pinned with == and at least one --hash=sha256 value. */
  requirements: string[];
}

export interface Source {
  id: string;
  name: string;
  upstream: string;
  fetch: GitSource | ReleaseSource | PipSource;
  license: string;
  licenseNote: string;
  framework: string;
  /** Applications sharing a template, component library or code base share a family and always share a partition. */
  family: string;
  partition: Partition;
  privacy: string;
  /** Directory served as the site root, relative to the fetched tree; absent for a process. */
  serve?:
    | { kind: "static"; root: string }
    | { kind: "process"; command: string[]; port: number; health: string; setup?: string[][] };
  groundTruth: GroundTruth;
  pages: Page[];
  /** Exact third-party URLs the application loads, mapped to the pinned npm file that serves them, as `pkg@x.y.z/path`. */
  aliases?: Record<string, string>;
  /** Paths the application expects but does not ship (built output), served from a pinned npm package as `pkg@x.y.z/subdir`. */
  mounts?: Record<string, string>;
  /** Pages considered and not kept, with why. */
  dropped?: { page: string; reason: string }[];
}

export interface Manifest {
  schema: "vsf-corpus-v1";
  observedAt: string;
  environment: string;
  sources: Source[];
}

export function loadManifest(path = MANIFEST_PATH): Manifest {
  return JSON.parse(readFileSync(path, "utf8")) as Manifest;
}

const SHA1 = /^[0-9a-f]{40}$/;
const SHA256 = /^[0-9a-f]{64}$/;

export interface Problem {
  id: string;
  message: string;
}

/** The rules that keep the corpus independent: pins, provenance and no template shared across partitions. */
export function validateManifest(m: Manifest): Problem[] {
  const out: Problem[] = [];
  const add = (id: string, message: string): void => void out.push({ id, message });
  if (m.schema !== "vsf-corpus-v1") add("manifest.schema", String(m.schema));
  const ids = new Set<string>();
  const familyPartition = new Map<string, Partition>();
  for (const s of m.sources) {
    if (ids.has(s.id)) add("source.duplicate", s.id);
    ids.add(s.id);
    if (!PARTITIONS.includes(s.partition)) add("source.partition", `${s.id}: ${s.partition}`);
    if (s.fetch.kind === "git" && !SHA1.test(s.fetch.rev)) add("source.unpinned", `${s.id}: ${s.fetch.rev}`);
    if (s.fetch.kind === "release" && !SHA256.test(s.fetch.sha256))
      add("source.unpinned", `${s.id}: release without sha256`);
    if (s.fetch.kind === "pip")
      for (const r of s.fetch.requirements)
        if (!/^[A-Za-z0-9_.-]+==[0-9][^\s]*(?: --hash=sha256:[0-9a-f]{64})+$/.test(r))
          add("source.unpinned", `${s.id}: requirement not pinned with a hash: ${r}`);
    for (const k of ["upstream", "license", "licenseNote", "framework", "family", "privacy"] as const)
      if (!s[k]) add("source.provenance", `${s.id}: missing ${k}`);
    if (!s.groundTruth?.basis?.length || !s.groundTruth.method || !s.groundTruth.annotator)
      add("source.ground-truth", `${s.id}: ground truth needs basis, method and annotator`);
    if (s.pages.length === 0) add("source.pages", `${s.id}: no pages`);
    const prior = familyPartition.get(s.family);
    if (prior && prior !== s.partition)
      add("source.contamination", `${s.id}: family ${s.family} is in ${prior} and ${s.partition}`);
    familyPartition.set(s.family, s.partition);
    const pageIds = new Set<string>();
    for (const p of s.pages) {
      if (pageIds.has(p.id)) add("page.duplicate", `${s.id}/${p.id}`);
      pageIds.add(p.id);
      if (!p.path.startsWith("/") || p.workflows.length === 0) add("page.shape", `${s.id}/${p.id}`);
    }
  }
  return out;
}

export const pagesOf = (m: Manifest, partitions: Partition[]): { source: Source; page: Page }[] =>
  m.sources
    .filter((s) => partitions.includes(s.partition))
    .flatMap((source) => source.pages.map((page) => ({ source, page })));
