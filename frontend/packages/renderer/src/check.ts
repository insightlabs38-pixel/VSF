import { CONTROL_CONTRAST, MIN_TARGET, TEXT_CONTRAST } from "./compile.ts";
import { INTERACTIVE, type PComponent, type PresentationBody, ROLES, type SemanticView } from "./model.ts";

export interface Problem {
  code: string;
  at: string;
  detail?: string;
}

/** Measured values the constraints are held to; supplied by the renderer's tokens. */
export interface Measured {
  targetPx: number;
  controlContrast: number;
  textContrast: number;
}

function documentOrder(body: PresentationBody): string[] {
  const byId = new Map(body.components.map((c) => [c.id, c]));
  const out: string[] = [];
  const seen = new Set<string>();
  const walk = (id: string): void => {
    if (seen.has(id)) return;
    seen.add(id);
    out.push(id);
    for (const k of byId.get(id)?.children ?? []) walk(k);
  };
  for (const r of body.regions) for (const id of r.components) walk(id);
  return out;
}

/** The dispatchable source operations a presentation exposes, independent of how they are laid out. */
export function operationsOf(
  sem: SemanticView,
  body: PresentationBody,
): { action: string; entity: string; effect: string }[] {
  const out = new Map<string, { action: string; entity: string; effect: string }>();
  for (const id of documentOrder(body)) {
    const c = body.components.find((x) => x.id === id) as PComponent;
    const a = c.binding?.action;
    if (!a) continue;
    const def = sem.actions.find((x) => x.id === a);
    out.set(a, { action: a, entity: def?.entity ?? "", effect: def?.effect.declared ?? "unknown" });
  }
  return [...out.values()].sort((x, y) => x.action.localeCompare(y.action));
}

export function checkPresentation(sem: SemanticView, body: PresentationBody, measured?: Measured): Problem[] {
  const p: Problem[] = [];
  const add = (code: string, at: string, detail?: string): void => {
    p.push(detail === undefined ? { code, at } : { code, at, detail });
  };
  const ids = new Set<string>();
  for (const c of body.components) {
    if (ids.has(c.id)) add("component.duplicate_id", c.id);
    ids.add(c.id);
  }
  const byId = new Map(body.components.map((c) => [c.id, c]));
  const parentOf = new Map<string, string>();
  for (const c of body.components) {
    for (const k of c.children) {
      if (!byId.has(k)) add("component.unknown_child", c.id, k);
      else if (parentOf.has(k)) add("component.multiple_parents", k);
      else parentOf.set(k, c.id);
    }
  }
  const inRegion = new Set<string>();
  for (const r of body.regions) {
    for (const k of r.components) {
      if (!byId.has(k)) add("region.unknown_component", r.id, k);
      inRegion.add(k);
    }
  }
  for (const c of body.components) {
    if (!inRegion.has(c.id) && !parentOf.has(c.id)) add("component.orphan", c.id);
  }
  for (const c of body.components) {
    let cur = parentOf.get(c.id);
    for (let n = 0; cur !== undefined && n <= body.components.length; n++) {
      if (cur === c.id) {
        add("component.cycle", c.id);
        break;
      }
      cur = parentOf.get(cur);
    }
  }
  const rendered = new Set<string>();
  const boundFields = new Set<string>();
  const boundActions = new Set<string>();
  for (const c of body.components) {
    const b = c.binding;
    if (b?.entity) {
      const ent = sem.entities.find((e) => e.id === b.entity);
      if (!ent) add("binding.unknown_entity", c.id, b.entity);
      else {
        if (c.kind === "form" || c.kind === "table") rendered.add(ent.id);
        if (b.field) {
          if (!ent.fields.some((f) => f.id === b.field)) add("binding.unknown_field", c.id, b.field);
          boundFields.add(`${ent.id}.${b.field}`);
        }
      }
    }
    if (b?.action) {
      const def = sem.actions.find((a) => a.id === b.action);
      if (!def) add("binding.unknown_action", c.id, b.action);
      else {
        if (def.entity && b.entity !== def.entity) add("binding.action_entity_mismatch", c.id, b.action);
        boundActions.add(def.id);
      }
      if (c.kind !== "button" && c.kind !== "link") add("binding.action_on_non_control", c.id);
    }
    if (c.accessibility.name_source === "field_label" && !b?.field) add("a11y.label_without_field", c.id);
    if (c.accessibility.name_source === "explicit" && !c.accessibility.name?.trim()) add("a11y.missing_name", c.id);
    if (INTERACTIVE.has(c.kind) && c.accessibility.name_source === "content")
      add("a11y.interactive_named_by_content", c.id);
    if (!ROLES[c.kind].includes(c.accessibility.role))
      add("a11y.role_mismatch", c.id, `${c.kind}:${c.accessibility.role}`);
  }
  for (const e of sem.entities) {
    if (!rendered.has(e.id)) continue;
    for (const f of e.fields)
      if (!boundFields.has(`${e.id}.${f.id}`)) add("operation.field_dropped", `${e.id}.${f.id}`);
    for (const a of sem.actions)
      if (a.entity === e.id && !boundActions.has(a.id)) add("operation.action_dropped", a.id);
  }
  const order = documentOrder(body);
  const index = new Map(order.map((id, i) => [id, i]));
  const covered = { target: new Set<string>(), controls: new Set<string>(), text: new Set<string>() };
  const cids = new Set<string>();
  for (const k of body.constraints) {
    if (cids.has(k.id)) add("constraint.duplicate_id", k.id);
    cids.add(k.id);
    for (const s of k.subjects) if (!byId.has(s)) add("constraint.unknown_subject", k.id, s);
    const subs = k.subjects.filter((s) => byId.has(s));
    if (k.kind === "ordering") {
      const at = subs.map((s) => index.get(s) ?? -1);
      if (at.some((x, i) => i > 0 && x < (at[i - 1] as number))) add("constraint.ordering_violated", k.id);
    }
    if (k.kind === "keep_together") {
      const parents = new Set(subs.map((s) => parentOf.get(s) ?? ""));
      const at = subs.map((s) => index.get(s) ?? -1).sort((x, y) => x - y);
      const adjacent = at.every((x, i) => i === 0 || x === (at[i - 1] as number) + 1);
      if (parents.size !== 1 || !adjacent) add("constraint.keep_together_violated", k.id);
    }
    if (k.kind === "min_target_size") {
      if ((k.value ?? 0) < MIN_TARGET) add("constraint.target_below_floor", k.id, String(k.value ?? 0));
      if (measured && measured.targetPx < (k.value ?? 0))
        add("constraint.target_not_met", k.id, `${measured.targetPx}<${k.value}`);
      for (const s of subs) covered.target.add(s);
    }
    if (k.kind === "min_contrast") {
      const floor = Math.min(
        ...subs.map((s) =>
          byId.get(s)?.kind === "button" || byId.get(s)?.kind === "link" ? CONTROL_CONTRAST : TEXT_CONTRAST,
        ),
      );
      if (subs.length && (k.value ?? 0) < floor) add("constraint.contrast_below_floor", k.id, String(k.value ?? 0));
      if (measured) {
        const need = k.value ?? 0;
        const have = subs.every((s) => byId.get(s)?.kind === "button" || byId.get(s)?.kind === "link")
          ? measured.controlContrast
          : measured.textContrast;
        if (have < need) add("constraint.contrast_not_met", k.id, `${have}<${need}`);
      }
      for (const s of subs) covered.text.add(s);
    }
  }
  for (const c of body.components) {
    if (!INTERACTIVE.has(c.kind)) continue;
    if (!covered.target.has(c.id)) add("constraint.target_uncovered", c.id);
    if (!covered.text.has(c.id)) add("constraint.contrast_uncovered", c.id);
  }
  return p;
}
