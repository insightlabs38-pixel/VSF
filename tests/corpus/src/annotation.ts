export type Effect = "read" | "state_change" | "destructive" | "navigation" | "unknown";
export type FieldKind = "text" | "choice" | "toggle" | "range" | "file" | "date" | "other";
export type CompositeKind =
  | "dialog"
  | "menu"
  | "tabs"
  | "pagination"
  | "bulk_selection"
  | "file_input"
  | "date_picker"
  | "tree"
  | "combobox_popup"
  | "virtual_list"
  | "sortable_columns"
  | "filter_bar"
  | "editable_cell"
  | "drag_drop"
  | "other";

export interface AnnotatedField {
  name: string;
  kind: FieldKind;
  required?: boolean;
}
export interface AnnotatedAction {
  name: string;
  effect: Effect;
}
export interface AnnotatedCollection {
  name: string;
  columns: string[];
  rows: number | null;
  virtualized?: boolean;
}
export interface AnnotatedComposite {
  kind: CompositeKind;
  name: string;
}
export interface AnnotatedWorkflow {
  name: string;
  needs: string[];
  effect: Effect;
}
export interface Annotation {
  schema: "vsf-corpus-annotation-v1";
  source: string;
  page: string;
  dossierSha256: string;
  annotator: {
    kind: "separate-agent" | "human" | "upstream-test";
    id: string;
    sawRecognizerOutput: false;
    basis: string[];
  };
  fields: AnnotatedField[];
  actions: AnnotatedAction[];
  collections: AnnotatedCollection[];
  composites: AnnotatedComposite[];
  workflows: AnnotatedWorkflow[];
  unsupported: { what: string; reason: string }[];
}

const EFFECTS: Effect[] = ["read", "state_change", "destructive", "navigation", "unknown"];
const KINDS: FieldKind[] = ["text", "choice", "toggle", "range", "file", "date", "other"];
const COMPOSITES: CompositeKind[] = [
  "dialog",
  "menu",
  "tabs",
  "pagination",
  "bulk_selection",
  "file_input",
  "date_picker",
  "tree",
  "combobox_popup",
  "virtual_list",
  "sortable_columns",
  "filter_bar",
  "editable_cell",
  "drag_drop",
  "other",
];

export function validateAnnotation(a: unknown): string[] {
  const out: string[] = [];
  const x = a as Partial<Annotation>;
  if (x?.schema !== "vsf-corpus-annotation-v1") return ["schema"];
  if (!x.source || !x.page) out.push("source/page");
  if (!/^[0-9a-f]{64}$/.test(x.dossierSha256 ?? "")) out.push("dossierSha256");
  if (x.annotator?.sawRecognizerOutput !== false || !x.annotator.basis?.length) out.push("annotator");
  for (const f of x.fields ?? []) if (!f.name || !KINDS.includes(f.kind)) out.push(`field ${f.name}`);
  for (const f of x.actions ?? []) if (!f.name || !EFFECTS.includes(f.effect)) out.push(`action ${f.name}`);
  for (const c of x.collections ?? []) if (!c.name || !Array.isArray(c.columns)) out.push(`collection ${c.name}`);
  for (const c of x.composites ?? []) if (!c.name || !COMPOSITES.includes(c.kind)) out.push(`composite ${c.name}`);
  for (const w of x.workflows ?? [])
    if (!w.name || !Array.isArray(w.needs) || !EFFECTS.includes(w.effect)) out.push(`workflow ${w.name}`);
  const names = new Set(
    [...(x.fields ?? []), ...(x.actions ?? []), ...(x.collections ?? []), ...(x.composites ?? [])].map((n) =>
      n.name
        .normalize("NFKC")
        .toLowerCase()
        .replace(/[^\p{L}\p{N}]+/gu, " ")
        .trim(),
    ),
  );
  for (const w of x.workflows ?? [])
    for (const n of w.needs ?? [])
      if (
        !names.has(
          n
            .normalize("NFKC")
            .toLowerCase()
            .replace(/[^\p{L}\p{N}]+/gu, " ")
            .trim(),
        )
      )
        out.push(`workflow ${w.name} needs unknown ${n}`);
  for (const k of ["fields", "actions", "collections", "composites", "workflows", "unsupported"] as const)
    if (!Array.isArray(x[k])) out.push(`${k} missing`);
  return out;
}
