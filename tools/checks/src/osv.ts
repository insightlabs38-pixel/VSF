import { createHash } from "node:crypto";
import type { Ecosystem, Locked } from "./depreview.ts";

export const OSV_URL = "https://api.osv.dev/v1/querybatch";
const ECOSYSTEM: Record<Ecosystem, string> = { cargo: "crates.io", npm: "npm", go: "Go" };

export interface Query {
  package: { name: string; ecosystem: string };
  version: string;
}

export interface Ignore {
  id: string;
  reason: string;
  expires: string;
}

export interface VulnFinding {
  package: string;
  ids: string[];
}

export type Outcome =
  | { status: "clean"; queried: number; digest: string; findings: [] }
  | { status: "findings"; queried: number; digest: string; findings: VulnFinding[] }
  | { status: "unavailable"; queried: number; digest: string; findings: []; reason: string };

export interface Options {
  fetchImpl?: typeof fetch;
  batchSize?: number;
  attempts?: number;
  timeoutMs?: number;
  backoffMs?: number;
  now?: Date;
  ignores?: readonly Ignore[];
  sleep?: (ms: number) => Promise<void>;
}

/** Exactly what is asked of the database, in a fixed order: the same lockfiles always produce the same request. */
export function buildQueries(locked: readonly Locked[]): Query[] {
  const seen = new Set<string>();
  const out: Query[] = [];
  for (const l of locked) {
    const q: Query = { package: { name: l.name, ecosystem: ECOSYSTEM[l.ecosystem] }, version: l.version };
    const key = `${q.package.ecosystem}\0${q.package.name}\0${q.version}`;
    if (!seen.has(key)) {
      seen.add(key);
      out.push(q);
    }
  }
  const key = (q: Query): string => `${q.package.ecosystem}\0${q.package.name}\0${q.version}`;
  return out.sort((a, b) => (key(a) < key(b) ? -1 : key(a) > key(b) ? 1 : 0));
}

export const requestDigest = (queries: readonly Query[]): string =>
  `sha256:${createHash("sha256").update(JSON.stringify(queries)).digest("hex")}`;

class Unavailable extends Error {}

async function post(
  batch: readonly Query[],
  {
    fetchImpl = fetch,
    attempts = 3,
    timeoutMs = 30_000,
    backoffMs = 1_000,
    sleep = (ms) => new Promise((r) => setTimeout(r, ms)),
  }: Options,
): Promise<{ vulns?: { id: string }[] }[]> {
  let last = "no attempt was made";
  for (let attempt = 1; attempt <= attempts; attempt++) {
    try {
      const res = await fetchImpl(OSV_URL, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ queries: batch }),
        signal: AbortSignal.timeout(timeoutMs),
      });
      if (res.status >= 500 || res.status === 429) {
        last = `HTTP ${res.status}`;
      } else if (!res.ok) {
        throw new Unavailable(`HTTP ${res.status}`);
      } else {
        const body = (await res.json()) as { results?: unknown };
        if (!Array.isArray(body.results) || body.results.length !== batch.length)
          throw new Unavailable("response does not answer every query");
        return body.results as { vulns?: { id: string }[] }[];
      }
    } catch (e) {
      if (e instanceof Unavailable) throw e;
      last = (e as Error).name === "TimeoutError" ? "timeout" : (e as Error).message;
    }
    if (attempt < attempts) await sleep(backoffMs * 2 ** (attempt - 1));
  }
  throw new Unavailable(`gave up after ${attempts} attempts: ${last}`);
}

/** Findings minus reviewed, unexpired ignores; an ignore past its date stops protecting anything. */
export function applyIgnores(
  raw: VulnFinding[],
  ignores: readonly Ignore[],
  now: Date,
): { findings: VulnFinding[]; expired: Ignore[] } {
  const live = new Set(ignores.filter((i) => new Date(`${i.expires}T23:59:59Z`) >= now).map((i) => i.id));
  const expired = ignores.filter((i) => !live.has(i.id));
  const findings = raw
    .map((f) => ({ package: f.package, ids: f.ids.filter((id) => !live.has(id)) }))
    .filter((f) => f.ids.length > 0);
  return { findings, expired };
}

/** Never reports `clean` unless every query was answered; any gap is `unavailable`. */
export async function scanVulnerabilities(locked: readonly Locked[], opts: Options = {}): Promise<Outcome> {
  const queries = buildQueries(locked);
  const digest = requestDigest(queries);
  if (queries.length === 0)
    return { status: "unavailable", queried: 0, digest, findings: [], reason: "nothing was queried" };
  const size = opts.batchSize ?? 500;
  const raw: VulnFinding[] = [];
  try {
    for (let i = 0; i < queries.length; i += size) {
      const batch = queries.slice(i, i + size);
      const results = await post(batch, opts);
      results.forEach((r, k) => {
        const ids = (r.vulns ?? []).map((v) => v.id).sort();
        if (ids.length > 0) {
          const q = batch[k] as Query;
          raw.push({ package: `${q.package.ecosystem}:${q.package.name}@${q.version}`, ids });
        }
      });
    }
  } catch (e) {
    return { status: "unavailable", queried: queries.length, digest, findings: [], reason: (e as Error).message };
  }
  const { findings } = applyIgnores(raw, opts.ignores ?? [], opts.now ?? new Date());
  return findings.length === 0
    ? { status: "clean", queried: queries.length, digest, findings: [] }
    : { status: "findings", queried: queries.length, digest, findings };
}
