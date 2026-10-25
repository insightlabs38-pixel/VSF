import { existsSync, readFileSync } from "node:fs";
import type { Annotation, Effect } from "./annotation.ts";
import { ROOT } from "./manifest.ts";
import { norm } from "./match.ts";

export type Step =
  | { do: "fill"; target: string; value: string }
  | { do: "choose"; target: string; value: string }
  | { do: "toggle"; target: string; checked: boolean }
  | { do: "click"; target: string; item?: string }
  | { do: "upload"; target: string; value: string; text: string }
  | { do: "press"; key: string };

export type Expect =
  | { kind: "text"; contains: string }
  | { kind: "absent"; contains: string }
  | { kind: "fragment"; contains: string }
  | { kind: "value"; target: string; value: string }
  | { kind: "checked"; target: string; checked: boolean }
  | { kind: "rows"; target: string; count: number };

export interface Operation {
  name: string;
  effect: Effect;
  steps: Step[];
  expect: Expect[];
  /** The operation must change nothing the expectations describe. */
  negative?: boolean;
}

export interface Operations {
  schema: "vsf-corpus-operations-v1";
  source: string;
  page: string;
  dossierSha256: string;
  annotator: { kind: "separate-agent" | "human"; id: string; sawRecognizerOutput: false; basis: string[] };
  operations: Operation[];
}

const DO = ["fill", "choose", "toggle", "click", "upload", "press"];
const KINDS = ["text", "absent", "fragment", "value", "checked", "rows"];

export const targetsOf = (op: Operation): string[] => [
  ...op.steps.flatMap((s) => ("target" in s ? [s.target] : [])),
  ...op.expect.flatMap((e) => ("target" in e ? [e.target] : [])),
];

export function annotatedNames(a: Annotation): Set<string> {
  return new Set([...a.fields, ...a.actions, ...a.collections, ...a.composites].map((n) => norm(n.name)));
}

/** Shape and reference checks only; whether an operation is true of the application is decided by the reference driver. */
export function validateOperations(o: unknown, a?: Annotation): string[] {
  const out: string[] = [];
  const x = o as Partial<Operations>;
  if (x?.schema !== "vsf-corpus-operations-v1") return ["schema"];
  if (!x.source || !x.page) out.push("source/page");
  if (!/^[0-9a-f]{64}$/.test(x.dossierSha256 ?? "")) out.push("dossierSha256");
  if (x.annotator?.sawRecognizerOutput !== false || !x.annotator.basis?.length) out.push("annotator");
  if (!Array.isArray(x.operations)) return [...out, "operations missing"];
  const names = a ? annotatedNames(a) : null;
  for (const op of x.operations) {
    if (!op.name || !Array.isArray(op.steps) || op.steps.length === 0) out.push(`operation ${op.name}: steps`);
    if (!Array.isArray(op.expect) || op.expect.length === 0) out.push(`operation ${op.name}: expect`);
    for (const s of op.steps ?? []) if (!DO.includes(s.do)) out.push(`operation ${op.name}: step ${String(s.do)}`);
    for (const e of op.expect ?? [])
      if (!KINDS.includes(e.kind)) out.push(`operation ${op.name}: expect ${String(e.kind)}`);
    if (names)
      for (const t of targetsOf(op)) if (!names.has(norm(t))) out.push(`operation ${op.name}: unknown target ${t}`);
  }
  return out;
}

const FROZEN = `${ROOT}fixtures/corpus/operations.frozen.json`;
const TRIAGE = `${ROOT}fixtures/corpus/operations.triage.json`;

export type Triage = Record<
  string,
  { class: "reference_limit" | "information_limit" | "annotation_defect"; reason: string }
>;

/** Keys `source/page/operation` of every operation written in the first annotation round. */
export const loadFrozen = (): Set<string> =>
  new Set(
    (
      JSON.parse(readFileSync(FROZEN, "utf8")) as { operations: { source: string; page: string; operation: string }[] }
    ).operations.map((o) => `${o.source}/${o.page}/${o.operation}`),
  );

/** Reviewed explanations for operations that are not the recognizer's failure; the counts are never reduced by them. */
export const loadTriage = (): Triage =>
  existsSync(TRIAGE) ? (JSON.parse(readFileSync(TRIAGE, "utf8")) as { entries: Triage }).entries : {};
