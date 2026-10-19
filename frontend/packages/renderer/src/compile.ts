import {
  type CompileOptions,
  EFFECT_ORDER,
  INTERACTIVE,
  ident,
  type PComponent,
  type PConstraint,
  type PresentationBody,
  type SemanticView,
  type ViewSpec,
} from "./model.ts";

/** Hard floors the renderer's tokens must meet; constraints carry them so a different renderer is held to the same bar. */
export const MIN_TARGET = 24;
export const TEXT_CONTRAST = 7;
export const CONTROL_CONTRAST = 4;

function fieldKind(valueType: string): PComponent["kind"] {
  if (valueType === "choice") return "choice";
  if (valueType === "boolean") return "checkbox";
  if (valueType === "binary") return "text";
  return "text_input";
}

const ROLE_OF: Record<string, string> = {
  text_input: "textbox",
  choice: "combobox",
  checkbox: "checkbox",
  text: "text",
};

function viewComponents(sem: SemanticView, view: ViewSpec, out: PComponent[], cons: PConstraint[]): string[] {
  const ent = sem.entities.find((e) => e.id === view.entity);
  if (!ent) throw new Error(`unknown entity ${view.entity}`);
  const e = ident(ent.id);
  const actions = sem.actions
    .filter((a) => a.entity === ent.id)
    .sort((a, b) => EFFECT_ORDER[a.effect.declared] - EFFECT_ORDER[b.effect.declared] || a.id.localeCompare(b.id));
  const title = ent.label ?? ent.id;
  const heading: PComponent = {
    id: `hd_${e}`,
    kind: "heading",
    binding: { entity: ent.id },
    children: [],
    accessibility: { role: "heading", name_source: "explicit", name: title },
  };
  const actionComps: PComponent[] = actions.map((a) => ({
    id: `act_${e}_${ident(a.id)}`,
    kind: "button",
    binding: { entity: ent.id, action: a.id },
    children: [],
    accessibility: { role: "button", name_source: "explicit", name: a.label },
  }));
  const preserved: PComponent[] = (view.preserved ?? []).map((p) => ({
    id: `surf_${ident(p.id)}`,
    kind: "preserved_surface",
    children: [],
    accessibility: { role: "application", name_source: "explicit", name: p.name },
  }));
  const root = view.layout === "form" ? `form_${e}` : `tbl_${e}`;
  const fieldComps: PComponent[] = ent.fields.map((f) => {
    const kind = view.layout === "grid" ? "cell" : fieldKind(f.value_type);
    return {
      id: `${view.layout === "grid" ? "col" : "fld"}_${e}_${ident(f.id)}`,
      kind,
      binding: { entity: ent.id, field: f.id },
      children: [],
      accessibility: {
        role: kind === "cell" ? "columnheader" : (ROLE_OF[kind] ?? "textbox"),
        name_source: "field_label",
      },
    };
  });
  if (view.layout === "form") {
    const group: PComponent | undefined = actionComps.length
      ? {
          id: `grp_${e}_actions`,
          kind: "group",
          children: actionComps.map((c) => c.id),
          accessibility: { role: "group", name_source: "explicit", name: `${title} actions` },
        }
      : undefined;
    const form: PComponent = {
      id: root,
      kind: "form",
      binding: { entity: ent.id },
      children: [
        heading.id,
        ...fieldComps.map((c) => c.id),
        ...(group ? [group.id] : []),
        ...preserved.map((p) => p.id),
      ],
      accessibility: { role: "form", name_source: "explicit", name: title },
    };
    out.push(form, heading, ...fieldComps, ...(group ? [group] : []), ...actionComps, ...preserved);
    if (actionComps.length > 1) {
      cons.push({ id: `keep_${e}_actions`, kind: "keep_together", subjects: actionComps.map((c) => c.id) });
      cons.push({ id: `order_${e}_actions`, kind: "ordering", subjects: actionComps.map((c) => c.id) });
    }
  } else {
    const table: PComponent = {
      id: root,
      kind: "table",
      binding: { entity: ent.id },
      children: [...fieldComps.map((c) => c.id), ...actionComps.map((c) => c.id)],
      accessibility: { role: "grid", name_source: "explicit", name: title },
    };
    out.push(table, heading, ...fieldComps, ...actionComps, ...preserved);
    if (actionComps.length > 1) {
      cons.push({ id: `order_${e}_actions`, kind: "ordering", subjects: actionComps.map((c) => c.id) });
    }
  }
  return [heading.id, root, ...preserved.map((p) => p.id)];
}

/** Compiles the semantic view into PresentationIR: every field and operation of a rendered entity is bound, with hard constraints. */
export function compilePresentation(sem: SemanticView, opts: CompileOptions): PresentationBody {
  const components: PComponent[] = [];
  const constraints: PConstraint[] = [];
  const regions: PresentationBody["regions"] = [];
  for (const view of opts.views) {
    const ids = viewComponents(sem, view, components, constraints);
    regions.push({ id: `view_${ident(view.entity)}`, components: ids });
  }
  if (opts.navigation && opts.views.length > 0) {
    const links: PComponent[] = opts.views.map((v) => {
      const ent = sem.entities.find((e) => e.id === v.entity);
      return {
        id: `lnk_${ident(v.entity)}`,
        kind: "link",
        binding: { entity: v.entity },
        children: [],
        accessibility: { role: "link", name_source: "explicit", name: ent?.label ?? v.entity },
      };
    });
    components.push(
      {
        id: "nav",
        kind: "list",
        children: links.map((l) => l.id),
        accessibility: { role: "navigation", name_source: "explicit", name: "Views" },
      },
      ...links,
    );
    regions.unshift({ id: "nav", components: ["nav"] });
  }
  const interactive = components.filter((c) => INTERACTIVE.has(c.kind));
  const controls = interactive.filter((c) => c.kind === "button" || c.kind === "link").map((c) => c.id);
  const text = interactive.filter((c) => c.kind !== "button" && c.kind !== "link").map((c) => c.id);
  if (interactive.length)
    constraints.push({
      id: "target",
      kind: "min_target_size",
      subjects: interactive.map((c) => c.id),
      value: MIN_TARGET,
    });
  if (controls.length)
    constraints.push({ id: "contrast_controls", kind: "min_contrast", subjects: controls, value: CONTROL_CONTRAST });
  if (text.length)
    constraints.push({ id: "contrast_text", kind: "min_contrast", subjects: text, value: TEXT_CONTRAST });
  return {
    presentation_version: opts.version ?? "pv1",
    semantic: opts.semanticDigest,
    locale: opts.locale,
    direction: opts.direction,
    theme: opts.theme,
    constraints,
    components,
    regions,
  };
}
