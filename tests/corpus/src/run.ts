import { existsSync, mkdirSync, mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { createServer } from "node:net";
import { tmpdir } from "node:os";
import { chromium } from "@playwright/test";
import type { Annotation } from "./annotation.ts";
import { type Capture, capturePage } from "./capture.ts";
import { ensurePackage, parseAlias } from "./cdn.ts";
import { dossierOf } from "./dossier.ts";
import { evaluateOne, type Row, readJson, recognize } from "./evaluate.ts";
import { dirOf, fetchSource } from "./fetch.ts";
import { CACHE, type Page, type Partition, ROOT, type Source } from "./manifest.ts";
import { type Running, serveProcess, serveStatic } from "./serve.ts";

export const ANNOTATIONS = `${ROOT}fixtures/corpus/annotations/`;
export const capturePath = (s: string, p: string): string => `${CACHE}captures/${s}/${p}.json`;
export const dossierPath = (s: string, p: string): string => `${ROOT}fixtures/corpus/dossiers/${s}/${p}.md`;
export const annotationPath = (s: string, p: string): string => `${ANNOTATIONS}${s}/${p}.json`;

function freePort(): Promise<number> {
  return new Promise((resolve) => {
    const s = createServer();
    s.listen(0, "127.0.0.1", () => {
      const port = (s.address() as { port: number }).port;
      s.close(() => resolve(port));
    });
  });
}

export async function start(s: Source): Promise<Running> {
  const dir = fetchSource(s);
  if (s.serve?.kind === "static") {
    const mounts: Record<string, string> = {};
    for (const [prefix, spec] of Object.entries(s.mounts ?? {})) {
      const ref = parseAlias(spec);
      if (!ref) throw new Error(`${s.id}: bad mount ${spec}`);
      mounts[prefix] = `${ensurePackage(ref)}${ref.path}`;
    }
    return serveStatic(`${dir}${s.serve.root}`, mounts);
  }
  if (s.serve?.kind === "process") {
    const work = mkdtempSync(`${tmpdir()}/corpus-${s.id}-`);
    return serveProcess(s, work, await freePort());
  }
  throw new Error(`${s.id}: nothing to serve`);
}

/** Captures every page of the given sources in one browser and stores captures under the cache and writes a dossier only when none is frozen yet. */
export async function captureSources(sources: Source[], only?: string[], refreshDossiers = false): Promise<Capture[]> {
  const browser = await chromium.launch({ channel: "chromium" });
  const out: Capture[] = [];
  try {
    for (const s of sources) {
      const running = await start(s);
      try {
        for (const p of s.pages) {
          if (only && !only.some((o) => o === s.id || o === `${s.id}/${p.id}`)) continue;
          const c = await capturePage(browser, running.origin, s, p);
          mkdirSync(`${CACHE}captures/${s.id}`, { recursive: true });
          mkdirSync(`${ROOT}fixtures/corpus/dossiers/${s.id}`, { recursive: true });
          writeFileSync(capturePath(s.id, p.id), JSON.stringify(c));
          if (refreshDossiers || !existsSync(dossierPath(s.id, p.id)))
            writeFileSync(dossierPath(s.id, p.id), dossierOf(s, p, c).text);
          out.push(c);
        }
      } finally {
        await running.stop();
      }
    }
  } finally {
    await browser.close();
  }
  return out;
}

export function evaluateSources(sources: Source[], partitions: Partition[]): { rows: Row[]; missing: string[] } {
  const tmp = mkdtempSync(`${tmpdir()}/corpus-eval-`);
  const rows: Row[] = [];
  const missing: string[] = [];
  for (const s of sources) {
    if (!partitions.includes(s.partition)) continue;
    for (const p of s.pages) {
      const ap = annotationPath(s.id, p.id);
      const cp = capturePath(s.id, p.id);
      if (!existsSync(ap) || !existsSync(cp)) {
        missing.push(`${s.id}/${p.id}`);
        continue;
      }
      const a = readJson<Annotation>(ap);
      const c = readJson<Capture>(cp);
      rows.push(evaluateOne(a, c, recognize(c, tmp), s.partition));
    }
  }
  return { rows, missing };
}

export const pageOf = (s: Source, id: string): Page => {
  const p = s.pages.find((x) => x.id === id);
  if (!p) throw new Error(`${s.id}/${id}`);
  return p;
};
export { dirOf, readFileSync };
