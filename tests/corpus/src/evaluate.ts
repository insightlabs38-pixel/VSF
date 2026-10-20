import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { existsSync, readFileSync, writeFileSync } from "node:fs";
import type { Annotation } from "./annotation.ts";
import type { Capture } from "./capture.ts";
import { ROOT } from "./manifest.ts";
import { aggregate, matchPage, type PageResult, type Recognition, type SnapNode } from "./match.ts";

export const RECOGNIZER = `${ROOT}compiler/target/release/vsf-recognizer`;

export interface Timed {
  result: Recognition;
  wallMs: number;
  rssKb: number;
}

/** Runs the production recognizer binary on a capture and measures it. */
export function recognize(capture: Capture, tmp: string): Timed {
  if (!existsSync(RECOGNIZER))
    throw new Error(`build the recognizer first: cargo build --release -p vsf-recognizer (${RECOGNIZER})`);
  const file = `${tmp}/${capture.source}__${capture.page}.json`;
  writeFileSync(file, JSON.stringify(capture.snapshot));
  const t0 = performance.now();
  const probe =
    "import resource,subprocess,sys;p=subprocess.run(sys.argv[1:],capture_output=True);sys.stdout.buffer.write(p.stdout);sys.stderr.write(p.stderr.decode()+'\\n'+str(resource.getrusage(resource.RUSAGE_CHILDREN).ru_maxrss));sys.exit(p.returncode)";
  const r = spawnSync("python3", ["-I", "-c", probe, RECOGNIZER, "recognize", file], {
    encoding: "utf8",
    maxBuffer: 256 << 20,
  });
  const wallMs = performance.now() - t0;
  if (r.status !== 0)
    throw new Error(
      `recognizer failed on ${capture.source}/${capture.page}: ${String(r.stderr ?? r.error).slice(0, 200)}`,
    );
  const rssKb = Number(r.stderr.trim().split("\n").pop()) || 0;
  return { result: JSON.parse(r.stdout) as Recognition, wallMs: Math.round(wallMs * 10) / 10, rssKb };
}

export interface Row {
  source: string;
  page: string;
  partition: string;
  result: PageResult;
  nodes: number;
  recognizeMs: number;
  rssKb: number;
  captureMs: number;
  blocked: number;
  mirrored: number;
}

export function evaluateOne(a: Annotation, c: Capture, t: Timed, partition: string): Row {
  const nodes = c.snapshot.nodes as unknown as SnapNode[];
  return {
    source: c.source,
    page: c.page,
    partition,
    result: matchPage(a, t.result),
    nodes: nodes.length,
    recognizeMs: t.wallMs,
    rssKb: t.rssKb,
    captureMs: c.captureMs,
    blocked: c.blocked.length,
    mirrored: c.mirrored.length,
  };
}

export const ratio = (n: number, d: number): number | null => (d === 0 ? null : Math.round((n / d) * 1000) / 1000);

export function summarize(rows: Row[]) {
  const g = aggregate(rows.map((r) => r.result));
  const t = (x: { annotated: number; found: number; exact: number; coarser: number }) => ({
    annotated: x.annotated,
    found: x.found,
    recall: ratio(x.found, x.annotated),
    semanticExact: ratio(x.exact, x.found),
    semanticExactOrCoarser: ratio(x.exact + x.coarser, x.found),
  });
  const times = rows.map((r) => r.recognizeMs).sort((a, b) => a - b);
  return {
    pages: g.pages,
    structural: { fields: t(g.fields), actions: t(g.actions), collections: t(g.collections) },
    falseClaims: g.falseClaims,
    contradictions: {
      fields: g.fields.contradict,
      actions: g.actions.contradict,
      collections: g.collections.contradict,
    },
    composites: {
      annotated: g.composites.annotated,
      recognized: g.composites.recognized,
      recall: ratio(g.composites.recognized, g.composites.annotated),
      byKind: g.composites.byKind,
    },
    workflows: {
      annotated: g.workflows.annotated,
      supported: g.workflows.supported,
      coverage: ratio(g.workflows.supported, g.workflows.annotated),
    },
    unannotatedRecognized: g.unannotated,
    recognizedOnPage: g.recognizedOnPage,
    declaredUnsupported: g.unsupportedDeclared,
    executableOperationCorrectness: {
      measured: false,
      reason:
        "needs derived contracts executed against disposable source state; measured in B11-T05/T06 for supported workflows",
    },
    latency: {
      recognizeMsP50: times[Math.floor(times.length / 2)] ?? null,
      recognizeMsMax: times.at(-1) ?? null,
      nodesMax: Math.max(0, ...rows.map((r) => r.nodes)),
      rssKbMax: Math.max(0, ...rows.map((r) => r.rssKb)),
    },
  };
}

export const sha256 = (s: string): string => createHash("sha256").update(s).digest("hex");
export const readJson = <T>(p: string): T => JSON.parse(readFileSync(p, "utf8")) as T;
