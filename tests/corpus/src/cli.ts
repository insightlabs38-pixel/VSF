import { createHash } from "node:crypto";
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { validateAnnotation } from "./annotation.ts";
import { clusters, summarize } from "./evaluate.ts";
import { runOperations, summarizeOperations } from "./execute.ts";
import { fetchSource } from "./fetch.ts";
import { loadManifest, PARTITIONS, type Partition, ROOT, type Source, validateManifest } from "./manifest.ts";
import { validateOperations } from "./operations.ts";
import { ANNOTATIONS, annotationPath, captureSources, dossierPath, evaluateSources, operationsPath } from "./run.ts";

const [, , cmd = "", ...args] = process.argv;
const flag = (n: string): string | undefined => {
  const i = args.indexOf(`--${n}`);
  return i >= 0 ? args[i + 1] : undefined;
};
const m = loadManifest();
const partitions = (flag("partition") ?? "dev,validation").split(",") as Partition[];
const only = flag("only")?.split(",");
const pick = (): Source[] =>
  m.sources.filter((s) => partitions.includes(s.partition) && (!only || only.some((o) => o.split("/")[0] === s.id)));
function guardHeldout(): void {
  if (partitions.includes("heldout") && !args.includes("--aggregate-only") && !args.includes("--unseal"))
    throw new Error("held-out detail is sealed: pass --aggregate-only, or --unseal once the B11 changes are final");
}
const sha = (s: string | Buffer): string => createHash("sha256").update(s).digest("hex");

async function main(): Promise<number> {
  for (const p of partitions) if (!PARTITIONS.includes(p)) throw new Error(`unknown partition ${p}`);
  switch (cmd) {
    case "validate": {
      const probs = validateManifest(m);
      for (const p of probs) console.error(`${p.id}: ${p.message}`);
      for (const s of m.sources)
        for (const p of s.pages) {
          const f = annotationPath(s.id, p.id);
          if (existsSync(f)) {
            const ann = JSON.parse(readFileSync(f, "utf8")) as { dossierSha256: string; source: string; page: string };
            const errs = validateAnnotation(ann);
            if (ann.source !== s.id || ann.page !== p.id) errs.push("source/page do not match the file");
            const d = dossierPath(s.id, p.id);
            if (existsSync(d) && sha(readFileSync(d)) !== ann.dossierSha256)
              errs.push("dossierSha256 does not match the current dossier");
            for (const e of errs) {
              console.error(`annotation ${s.id}/${p.id}: ${e}`);
              probs.push({ id: "annotation", message: e });
            }
          }
        }
      for (const s of m.sources)
        for (const p of s.pages) {
          const f = operationsPath(s.id, p.id);
          if (!existsSync(f)) continue;
          const ops = JSON.parse(readFileSync(f, "utf8")) as { dossierSha256: string };
          const ap = annotationPath(s.id, p.id);
          const errs = validateOperations(ops, existsSync(ap) ? JSON.parse(readFileSync(ap, "utf8")) : undefined);
          const d = dossierPath(s.id, p.id);
          if (existsSync(d) && sha(readFileSync(d)) !== ops.dossierSha256)
            errs.push("dossierSha256 does not match the current dossier");
          for (const e of errs) {
            const defect = e.includes(": unknown target ");
            console.error(`operations ${s.id}/${p.id}: ${e}${defect ? " (annotation defect, scored as such)" : ""}`);
            if (!defect) probs.push({ id: "operations", message: e });
          }
        }
      if (probs.length === 0)
        console.log(
          `corpus: ok (${m.sources.length} sources, ${m.sources.reduce((n, s) => n + s.pages.length, 0)} pages)`,
        );
      return probs.length === 0 ? 0 : 1;
    }
    case "fetch":
      for (const s of pick()) console.log(`${s.id}: ${fetchSource(s)}`);
      return 0;
    case "capture": {
      const caps = await captureSources(pick(), only, args.includes("--refresh-dossiers"));
      for (const c of caps)
        console.log(
          `${c.source}/${c.page} nodes=${(c.snapshot.nodes as unknown[]).length} ms=${c.captureMs} blocked=${c.blocked.length} mirrored=${c.mirrored.length} sha=${c.snapshotSha256.slice(0, 12)}`,
        );
      const indexFile = `${ROOT}fixtures/corpus/captures.index.json`;
      const prior: Record<string, { structureSha256: string }> = existsSync(indexFile)
        ? (JSON.parse(readFileSync(indexFile, "utf8")) as { pages: Record<string, { structureSha256: string }> }).pages
        : {};
      if (args.includes("--verify")) {
        const drift = caps
          .filter((c) => prior[`${c.source}/${c.page}`]?.structureSha256 !== c.structureSha256)
          .map((c) => `${c.source}/${c.page}`);
        for (const d of drift) console.error(`capture differs from the recorded digest: ${d}`);
        console.log(`verified ${caps.length - drift.length}/${caps.length} captures reproduce their recorded digest`);
        return drift.length === 0 ? 0 : 4;
      }
      if (args.includes("--index")) {
        const pages = { ...prior };
        for (const c of caps)
          pages[`${c.source}/${c.page}`] = {
            structureSha256: c.structureSha256,
            nodes: (c.snapshot.nodes as unknown[]).length,
            blocked: c.blocked.length,
            mirrored: c.mirrored.length,
          } as never;
        const sorted = Object.fromEntries(Object.entries(pages).sort(([a], [b]) => (a < b ? -1 : 1)));
        writeFileSync(
          indexFile,
          `${JSON.stringify({ schema: "vsf-corpus-captures-v1", environment: m.environment, pages: sorted }, null, 1)}\n`,
        );
      }
      return 0;
    }
    case "provenance": {
      const rows = m.sources.map(
        (s) =>
          `| ${s.id} | ${s.upstream.replace("https://github.com/", "")} | ${s.fetch.kind === "git" ? s.fetch.rev.slice(0, 12) : s.fetch.kind === "release" ? `release ${s.fetch.sha256.slice(0, 12)}` : `pip ${s.fetch.requirements[0]?.split(" ")[0]}`} | ${s.license} | ${s.framework} | ${s.family} | ${s.partition} | ${s.pages.length} |`,
      );
      const note = m.sources.flatMap((s) => (s.dropped ?? []).map((d) => `- ${s.id}/${d.page}: dropped, ${d.reason}`));
      console.log(
        [
          "| source | upstream | pinned | license | framework | family | partition | pages |",
          "|---|---|---|---|---|---|---|---|",
          ...rows,
          "",
          ...note,
        ].join("\n"),
      );
      return 0;
    }
    case "dossier-sha": {
      for (const s of pick())
        for (const p of s.pages) {
          const f = dossierPath(s.id, p.id);
          if (existsSync(f)) console.log(`${s.id}/${p.id} ${sha(readFileSync(f))}`);
        }
      return 0;
    }
    case "evaluate": {
      guardHeldout();
      const { rows, missing } = evaluateSources(pick(), partitions);
      const out = {
        schema: "vsf-corpus-result-v1",
        partitions,
        pages: rows.length,
        missing,
        summary: summarize(rows),
        clusters: clusters(
          rows,
          (id) => m.sources.find((x) => x.id === id)?.family ?? id,
          !args.includes("--aggregate-only"),
        ),
        perSource: Object.fromEntries(
          [...new Set(rows.map((r) => r.source))].map((id) => [id, summarize(rows.filter((r) => r.source === id))]),
        ),
        rows: args.includes("--aggregate-only") ? undefined : rows,
      };
      const to = flag("out");
      if (to) {
        mkdirSync(to.replace(/\/[^/]*$/, ""), { recursive: true });
        writeFileSync(to, `${JSON.stringify(out, null, 1)}\n`);
      } else console.log(JSON.stringify(out.summary, null, 1));
      return missing.length === 0 ? 0 : 3;
    }
    case "reference":
    case "operations": {
      if (cmd === "operations") guardHeldout();
      const rows = await runOperations({
        sources: pick(),
        only,
        recognized: cmd === "operations",
        jobs: Number(flag("jobs") ?? 1),
      });
      const aggregateOnly = args.includes("--aggregate-only");
      const out = {
        schema: "vsf-corpus-operations-result-v1",
        partitions,
        recognizer: process.env.VSF_RECOGNIZER ?? "current build",
        summary: summarizeOperations(rows),
        perSource: aggregateOnly
          ? undefined
          : Object.fromEntries(
              [...new Set(rows.map((r) => r.source))].map((id) => [
                id,
                summarizeOperations(rows.filter((r) => r.source === id)),
              ]),
            ),
        rows: aggregateOnly ? undefined : rows,
      };
      const to = flag("out");
      if (to) {
        mkdirSync(to.replace(/\/[^/]*$/, ""), { recursive: true });
        writeFileSync(to, `${JSON.stringify(out, null, 1)}\n`);
      } else console.log(JSON.stringify(aggregateOnly ? out.summary : out, null, 1));
      return 0;
    }
    case "seal": {
      const items = m.sources
        .filter((s) => s.partition === "heldout")
        .flatMap((s) =>
          s.pages.map((p) => {
            const f = annotationPath(s.id, p.id);
            return { source: s.id, page: p.id, annotationSha256: existsSync(f) ? sha(readFileSync(f)) : null };
          }),
        );
      const seal = {
        schema: "vsf-corpus-seal-v1",
        rule: "held-out annotations are committed once, before any recognition change, and are not read for development; the aggregate baseline is measured before any B11 change",
        commitments: items,
      };
      writeFileSync(`${ROOT}fixtures/corpus/heldout.seal.json`, `${JSON.stringify(seal, null, 1)}\n`);
      console.log(`sealed ${items.length} held-out pages`);
      return 0;
    }
    default:
      console.error(
        "usage: corpus <validate|fetch|capture|dossier-sha|evaluate|reference|operations|seal> [--unseal] [--partition dev,validation,heldout] [--only source/page,...] [--out file] [--aggregate-only]",
      );
      return 64;
  }
}
process.exit(await main());

export { ANNOTATIONS };
