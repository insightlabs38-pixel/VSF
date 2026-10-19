import { CONTROL_CONTRAST, MIN_TARGET, TEXT_CONTRAST } from "./compile.ts";
import { hideable, renderOrder } from "./layout.ts";
import { INTERACTIVE, type PComponent, type PresentationBody, ROLES, type SemanticView } from "./model.ts";

export const MAX_COMPONENTS = 10_000;
export const MAX_DEPTH = 64;

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

export function documentOrder(body: PresentationBody): string[] {
  const byId = new Map(body.components.map((c) => [c.id, c]));
  const out: string[] = [];
  const seen = new Set<string>();
  const walk = (id: string): void => {
    if (seen.has(id)) return;
    seen.add(id);
    out.push(id);
    for (const k of byId.get(id)?.children ?? []) walk(k);
  };
  for (const r of renderOrder(body)) for (const id of r.components) walk(id);
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

const IDENT = /^[a-z][a-z0-9_-]{0,63}$/;

/** Mirrors the Rust layout rules, so a presentation the validator would refuse is never drawn. */
function checkRegions(
  sem: SemanticView,
  body: PresentationBody,
  byId: Map<string, PComponent>,
  add: (code: string, at: string, detail?: string) => void,
): void {
  const ids = new Set<string>();
  for (const r of body.regions) {
    if (!IDENT.test(r.id)) add("region.bad_id", r.id);
    if (ids.has(r.id)) add("region.duplicate_id", r.id);
    ids.add(r.id);
  }
  const kids = new Map<string, number>();
  const mains = new Map<string, number>();
  for (const r of body.regions) {
    if (r.parent !== undefined) {
      if (r.parent === r.id) add("region.cycle", r.id);
      else if (!ids.has(r.parent)) add("region.unknown_parent", r.id, r.parent);
      else kids.set(r.parent, (kids.get(r.parent) ?? 0) + 1);
    }
    if (r.role === "main") mains.set(r.parent ?? "", (mains.get(r.parent ?? "") ?? 0) + 1);
    const sz = r.size;
    if (
      sz &&
      ((sz.max !== undefined && sz.max < sz.min) ||
        (sz.basis !== undefined && (sz.basis < sz.min || (sz.max !== undefined && sz.basis > sz.max))))
    )
      add("region.size", r.id);
    if (r.dock && !(r.role && ["navigation", "aside", "toolbar", "header", "footer"].includes(r.role)))
      add("region.dock_role", r.id);
    let last = Number.POSITIVE_INFINITY;
    for (const rs of r.responsive ?? []) {
      if (rs.below >= last) add("region.responsive_order", r.id);
      last = rs.below;
    }
  }
  for (const r of body.regions) {
    const seen = new Set<string>([r.id]);
    for (let cur = r.parent; cur !== undefined; cur = body.regions.find((x) => x.id === cur)?.parent) {
      if (seen.has(cur)) {
        add("region.cycle", r.id);
        break;
      }
      seen.add(cur);
    }
  }
  for (const [parent, n] of mains) if (n > 1) add("region.main_count", parent || "root");
  const needsTwo = (a: string | undefined): boolean => a === "split" || a === "tabs";
  for (const r of body.regions) {
    const parts = kids.get(r.id) ?? 0;
    if ((needsTwo(r.arrange) || (r.responsive ?? []).some((x) => needsTwo(x.arrange))) && parts < 2)
      add("region.arrange_empty", r.id);
    if (!hideable(r)) continue;
    const stack = [...r.components];
    const seen = new Set<string>();
    while (stack.length > 0) {
      const id = stack.pop() as string;
      if (seen.has(id)) continue;
      seen.add(id);
      const c = byId.get(id);
      if (!c) continue;
      stack.push(...c.children);
      const action = c.binding?.action;
      if (!action) continue;
      const effect = sem.actions.find((a) => a.id === action)?.effect.declared;
      if (effect === undefined || (effect !== "read_only" && effect !== "idempotent_write"))
        add("region.hideable_holds_risky_action", r.id, action);
    }
  }
}

export function checkPresentation(sem: SemanticView, body: PresentationBody, measured?: Measured): Problem[] {
  const p: Problem[] = [];
  const add = (code: string, at: string, detail?: string): void => {
    p.push(detail === undefined ? { code, at } : { code, at, detail });
  };
  if (body.components.length > MAX_COMPONENTS) {
    add("body.too_large", "components", String(body.components.length));
    return p;
  }
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
  const depthOf = (id: string): number => {
    let d = 0;
    for (let cur = parentOf.get(id); cur !== undefined && d <= MAX_DEPTH + 1; cur = parentOf.get(cur)) d++;
    return d;
  };
  for (const c of body.components) if (depthOf(c.id) > MAX_DEPTH) add("component.too_deep", c.id);
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
  checkRegions(sem, body, byId, add);
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
