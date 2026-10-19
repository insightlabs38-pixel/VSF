export type EffectClass =
  | "read_only"
  | "idempotent_write"
  | "conditional_write"
  | "non_idempotent_write"
  | "destructive"
  | "indeterminate";

export type Certainty = "verified" | "inferred" | "unknown" | "conflicted";
export type ValueType =
  | "text"
  | "integer"
  | "decimal"
  | "boolean"
  | "instant"
  | "date"
  | "choice"
  | "reference"
  | "binary"
  | "unknown";

/** The slice of SemanticIR a presentation depends on, as plain decoded JSON. */
export interface SemanticView {
  entities: {
    id: string;
    kind: string;
    label?: string;
    certainty: Certainty;
    fields: { id: string; name: string; value_type: ValueType; nullable: string; certainty: Certainty }[];
  }[];
  actions: {
    id: string;
    entity?: string;
    label: string;
    effect: { declared: EffectClass };
    source_confirmation: string;
    certainty: Certainty;
  }[];
}

export type ComponentKind =
  | "text"
  | "heading"
  | "button"
  | "link"
  | "text_input"
  | "choice"
  | "checkbox"
  | "table"
  | "row"
  | "cell"
  | "form"
  | "list"
  | "group"
  | "preserved_surface";

export type ConstraintKind = "min_contrast" | "min_target_size" | "keep_together" | "ordering";
export type NameSource = "field_label" | "explicit" | "content";

export interface PComponent {
  id: string;
  kind: ComponentKind;
  binding?: { entity?: string; field?: string; action?: string };
  children: string[];
  accessibility: { role: string; name_source: NameSource; name?: string; shortcut?: string };
}

export interface PConstraint {
  id: string;
  kind: ConstraintKind;
  subjects: string[];
  value?: number;
}

export interface PresentationBody {
  presentation_version: string;
  semantic: string;
  locale: string;
  direction: "ltr" | "rtl" | "auto";
  theme: {
    scheme: "light" | "dark" | "system";
    accent: "none" | "mint" | "blue";
    density: "compact" | "comfortable" | "spacious";
  };
  constraints: PConstraint[];
  components: PComponent[];
  regions: { id: string; components: string[] }[];
}

export type Layout = "form" | "grid";

export interface ViewSpec {
  entity: string;
  layout: Layout;
  preserved?: { id: string; name: string }[];
}

export interface CompileOptions {
  semanticDigest: string;
  locale: string;
  direction: PresentationBody["direction"];
  theme: PresentationBody["theme"];
  views: ViewSpec[];
  navigation: boolean;
  version?: string;
}

/** Severity order used for the hard ordering constraint: safe operations first, destructive last. */
export const EFFECT_ORDER: Record<EffectClass, number> = {
  read_only: 0,
  idempotent_write: 1,
  conditional_write: 2,
  non_idempotent_write: 3,
  indeterminate: 4,
  destructive: 5,
};

export const INTERACTIVE: ReadonlySet<ComponentKind> = new Set(["button", "link", "text_input", "choice", "checkbox"]);

export const ROLES: Record<ComponentKind, readonly string[]> = {
  text: ["text", "paragraph"],
  heading: ["heading"],
  button: ["button"],
  link: ["link"],
  text_input: ["textbox", "searchbox"],
  choice: ["combobox", "listbox"],
  checkbox: ["checkbox", "switch"],
  table: ["grid", "table"],
  row: ["row"],
  cell: ["gridcell", "columnheader", "cell"],
  form: ["form"],
  list: ["list", "navigation"],
  group: ["group", "region"],
  preserved_surface: ["application", "group", "region", "document"],
};

export function ident(s: string): string {
  const out = s
    .toLowerCase()
    .replace(/[^a-z0-9_-]+/g, "_")
    .replace(/^[^a-z]+/, "x");
  if (out.length > 64) throw new Error(`identifier too long: ${s}`);
  return out;
}
