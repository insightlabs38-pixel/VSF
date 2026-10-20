import type { AnnotatedField, Annotation, Effect, FieldKind } from "./annotation.ts";

export interface RecUnit {
  type: "field" | "action" | "collection";
  node: number;
  kind?: string;
  binding?: { state: string; text?: string };
  name?: string;
  verbs?: string[];
  usable?: boolean;
  columns?: (string | null)[];
  label?: string | null;
  rows?: number;
}
export interface Recognition {
  units: RecUnit[];
}
export interface SnapNode {
  id: number;
  parent: number;
  tag: string;
  role: string | null;
}

export function norm(s: string | null | undefined): string {
  return (s ?? "")
    .normalize("NFKC")
    .toLowerCase()
    .replace(/[*:]+/g, " ")
    .replace(/\(required\)|\(optional\)/g, " ")
    .replace(/[^\p{L}\p{N}]+/gu, " ")
    .trim();
}

const VERB_EFFECT: Record<string, Effect> = {
  commit: "state_change",
  create: "state_change",
  destroy: "destructive",
  navigate: "navigation",
  dismiss: "navigation",
  query: "read",
  export: "read",
};

export const effectsOf = (verbs: string[] | undefined): Effect[] => [
  ...new Set((verbs ?? []).map((v) => VERB_EFFECT[v] ?? "unknown")),
];

export type Agreement = "exact" | "coarser" | "unclassified" | "contradict";

export function fieldAgreement(annotated: FieldKind, recognized: string | undefined): Agreement {
  if (annotated === recognized) return "exact";
  if ((annotated === "date" || annotated === "choice") && recognized === "text") return "coarser";
  return "contradict";
}

export function actionAgreement(annotated: Effect, verbs: string[] | undefined): Agreement {
  const e = effectsOf(verbs);
  if (e.length === 0) return annotated === "unknown" ? "exact" : "unclassified";
  if (e.includes(annotated)) return "exact";
  return annotated === "unknown" ? "unclassified" : "contradict";
}

function jaccard(a: string[], b: string[]): number {
  const x = new Set(a),
    y = new Set(b);
  if (x.size === 0 && y.size === 0) return 1;
  let inter = 0;
  for (const v of x) if (y.has(v)) inter++;
  return inter / (x.size + y.size - inter);
}

export interface Tally {
  annotated: number;
  found: number;
  exact: number;
  coarser: number;
  unclassified: number;
  contradict: number;
}
const tally = (): Tally => ({ annotated: 0, found: 0, exact: 0, coarser: 0, unclassified: 0, contradict: 0 });

export interface PageResult {
  source: string;
  page: string;
  fields: Tally;
  actions: Tally;
  collections: Tally & { columnsExact: number; rowsExact: number; rowsChecked: number };
  composites: { annotated: number; recognized: number; byKind: Record<string, number> };
  workflows: { annotated: number; supported: number; unsupported: string[] };
  recognizedOnPage: { fields: number; actions: number; collections: number };
  unannotated: { fields: number; actions: number; collections: number };
  unsupportedDeclared: number;
  /** Recognized, usable claims that the annotation contradicts: wrong kind, wrong effect. */
  falseClaims: number;
}

export function matchPage(a: Annotation, rec: Recognition): PageResult {
  const units = rec.units;
  const used = new Set<number>();
  const r: PageResult = {
    source: a.source,
    page: a.page,
    fields: tally(),
    actions: tally(),
    collections: { ...tally(), columnsExact: 0, rowsExact: 0, rowsChecked: 0 },
    composites: { annotated: a.composites.length, recognized: 0, byKind: {} },
    workflows: { annotated: a.workflows.length, supported: 0, unsupported: [] },
    recognizedOnPage: { fields: 0, actions: 0, collections: 0 },
    unannotated: { fields: 0, actions: 0, collections: 0 },
    unsupportedDeclared: a.unsupported.length,
    falseClaims: 0,
  };
  for (const u of units)
    r.recognizedOnPage[u.type === "field" ? "fields" : u.type === "action" ? "actions" : "collections"]++;
  const status = new Map<string, Agreement | "missing">();

  const take = (type: RecUnit["type"], pred: (u: RecUnit) => boolean): RecUnit | undefined => {
    const u = units.find((x) => x.type === type && !used.has(x.node) && pred(x));
    if (u) used.add(u.node);
    return u;
  };
  for (const f of a.fields as AnnotatedField[]) {
    r.fields.annotated++;
    const n = norm(f.name);
    const u = take("field", (x) => x.binding?.state === "bound" && norm(x.binding.text) === n);
    if (!u) {
      status.set(`f:${n}`, "missing");
      continue;
    }
    r.fields.found++;
    const ag = fieldAgreement(f.kind, u.kind);
    r.fields[ag]++;
    status.set(`f:${n}`, ag);
    if (ag === "contradict" && u.usable) r.falseClaims++;
  }
  for (const act of a.actions) {
    r.actions.annotated++;
    const n = norm(act.name);
    const u = take("action", (x) => norm(x.name) === n);
    if (!u) {
      status.set(`a:${n}`, "missing");
      continue;
    }
    r.actions.found++;
    const ag = actionAgreement(act.effect, u.verbs);
    r.actions[ag]++;
    status.set(`a:${n}`, ag);
    if (ag === "contradict" && u.usable) r.falseClaims++;
  }
  for (const c of a.collections) {
    r.collections.annotated++;
    const cols = c.columns.map(norm).filter(Boolean);
    const u = take(
      "collection",
      (x) => norm(x.label) === norm(c.name) || jaccard((x.columns ?? []).map(norm).filter(Boolean), cols) >= 0.5,
    );
    if (!u) {
      status.set(`c:${norm(c.name)}`, "missing");
      continue;
    }
    r.collections.found++;
    const rc = (u.columns ?? []).map(norm).filter(Boolean);
    const same = jaccard(rc, cols) === 1;
    if (same) r.collections.columnsExact++;
    if (c.rows !== null) {
      r.collections.rowsChecked++;
      if (u.rows === c.rows) r.collections.rowsExact++;
    }
    const ok = same && (c.rows === null || u.rows === c.rows);
    r.collections[ok ? "exact" : "contradict"]++;
    status.set(`c:${norm(c.name)}`, ok ? "exact" : "contradict");
    if (!ok) r.falseClaims++;
  }
  for (const k of ["fields", "actions", "collections"] as const) {
    const type = k === "fields" ? "field" : k === "actions" ? "action" : "collection";
    r.unannotated[k] = units.filter((x) => x.type === type && !used.has(x.node)).length;
  }
  for (const c of a.composites) r.composites.byKind[c.kind] = (r.composites.byKind[c.kind] ?? 0) + 1;
  for (const w of a.workflows) {
    const ok = w.needs.every((need) => {
      const n = norm(need);
      const hit = [`f:${n}`, `a:${n}`, `c:${n}`].map((k) => status.get(k)).find((v) => v !== undefined);
      return hit === "exact" || hit === "coarser";
    });
    if (ok) r.workflows.supported++;
    else r.workflows.unsupported.push(w.name);
  }
  return r;
}

export interface Aggregate {
  pages: number;
  fields: Tally;
  actions: Tally;
  collections: Tally;
  composites: { annotated: number; recognized: number; byKind: Record<string, number> };
  workflows: { annotated: number; supported: number };
  recognizedOnPage: { fields: number; actions: number; collections: number };
  unannotated: { fields: number; actions: number; collections: number };
  falseClaims: number;
  unsupportedDeclared: number;
}

const add = (a: Tally, b: Tally): void => {
  for (const k of Object.keys(a) as (keyof Tally)[]) a[k] += b[k];
};

export function aggregate(rs: PageResult[]): Aggregate {
  const g: Aggregate = {
    pages: rs.length,
    fields: tally(),
    actions: tally(),
    collections: tally(),
    composites: { annotated: 0, recognized: 0, byKind: {} },
    workflows: { annotated: 0, supported: 0 },
    recognizedOnPage: { fields: 0, actions: 0, collections: 0 },
    unannotated: { fields: 0, actions: 0, collections: 0 },
    falseClaims: 0,
    unsupportedDeclared: 0,
  };
  for (const r of rs) {
    add(g.fields, r.fields);
    add(g.actions, r.actions);
    add(g.collections, r.collections);
    g.composites.annotated += r.composites.annotated;
    g.composites.recognized += r.composites.recognized;
    for (const [k, v] of Object.entries(r.composites.byKind))
      g.composites.byKind[k] = (g.composites.byKind[k] ?? 0) + v;
    g.workflows.annotated += r.workflows.annotated;
    g.workflows.supported += r.workflows.supported;
    for (const k of ["fields", "actions", "collections"] as const) {
      g.recognizedOnPage[k] += r.recognizedOnPage[k];
      g.unannotated[k] += r.unannotated[k];
    }
    g.falseClaims += r.falseClaims;
    g.unsupportedDeclared += r.unsupportedDeclared;
  }
  return g;
}
