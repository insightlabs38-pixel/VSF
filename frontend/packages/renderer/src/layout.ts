import type { Arrange, PCustomization, PRegion, PresentationBody } from "./model.ts";

export type Place = "top" | "start" | "center" | "end" | "bottom";

/** A body uses the region layout engine only when some region says how it is arranged. */
export const isLayout = (body: PresentationBody): boolean =>
  body.regions.some((r) => r.role || r.arrange || r.parent || r.dock || r.size || r.responsive?.length);

const byOrder = (all: PRegion[]) => (a: PRegion, b: PRegion) =>
  (a.order ?? all.indexOf(a)) - (b.order ?? all.indexOf(b)) || all.indexOf(a) - all.indexOf(b);

const RANK: Record<Place, number> = { top: 0, start: 1, center: 2, end: 3, bottom: 4 };

export function childrenOf(body: PresentationBody, parent: string | undefined): PRegion[] {
  const list = body.regions.filter((r) => r.parent === parent);
  const cmp = byOrder(body.regions);
  return parent === undefined ? list.sort((a, b) => RANK[placeOf(a)] - RANK[placeOf(b)] || cmp(a, b)) : list.sort(cmp);
}

/** Regions in reading (DOM) order: top, start, centre, end, bottom; siblings by `order`, then declaration; parents first. */
export function renderOrder(body: PresentationBody): PRegion[] {
  const out: PRegion[] = [];
  const walk = (parent: string | undefined, depth: number): void => {
    if (depth > body.regions.length) return;
    for (const r of childrenOf(body, parent)) {
      out.push(r);
      walk(r.id, depth + 1);
    }
  };
  walk(undefined, 0);
  return out;
}

/** Where a top-level region sits around the main area. */
export function placeOf(r: PRegion): Place {
  const edge = r.dock?.edge;
  if (edge === "top" || r.role === "header") return "top";
  if (edge === "bottom" || r.role === "footer") return "bottom";
  if (edge === "start" || (!edge && r.role === "navigation")) return "start";
  if (edge === "end" || (!edge && r.role === "aside")) return "end";
  return "center";
}

/** The arrangement in force at a width in ch: the tightest breakpoint the width is below wins. */
export function effectiveArrange(r: PRegion, widthCh: number): { arrange: Arrange; collapse: boolean } {
  let arrange: Arrange = r.arrange ?? "stack";
  let collapse = false;
  for (const rule of [...(r.responsive ?? [])].sort((a, b) => b.below - a.below)) {
    if (widthCh < rule.below) {
      arrange = rule.arrange;
      collapse = rule.collapse;
    }
  }
  return { arrange, collapse };
}

export const hideable = (r: PRegion): boolean =>
  r.dock?.collapsible === true || (r.responsive ?? []).some((x) => x.collapse);

/** What a person has changed; only the parts the presentation allows ever take effect. */
export interface LayoutPrefs {
  order?: string[];
  collapsed?: string[];
  sizes?: Record<string, number>;
}

export interface Effective {
  order: PRegion[];
  collapsed: Set<string>;
  sizes: Map<string, number>;
}

/** Applies preferences within the customization the presentation grants; pinned regions can never be hidden. */
export function effectiveLayout(body: PresentationBody, prefs: LayoutPrefs | undefined): Effective {
  const grant: PCustomization = body.customization ?? {
    reorder: false,
    resize: false,
    collapse: false,
    density: false,
    scheme: false,
  };
  const sizes = new Map<string, number>();
  const collapsed = new Set<string>();
  let order = renderOrder(body);
  if (prefs?.order && grant.reorder) {
    const rank = new Map(prefs.order.map((id, i) => [id, i]));
    const siblings = (r: PRegion): string => r.parent ?? `@${placeOf(r)}`;
    const groups = new Map<string, PRegion[]>();
    for (const r of order) groups.set(siblings(r), [...(groups.get(siblings(r)) ?? []), r]);
    const sorted = new Map<string, PRegion[]>();
    for (const [k, rs] of groups) {
      sorted.set(
        k,
        [...rs].sort(
          (a, b) =>
            (rank.get(a.id) ?? Number.MAX_SAFE_INTEGER) - (rank.get(b.id) ?? Number.MAX_SAFE_INTEGER) ||
            rs.indexOf(a) - rs.indexOf(b),
        ),
      );
    }
    const walk = (key: string): PRegion[] => (sorted.get(key) ?? []).flatMap((r) => [r, ...walk(r.id)]);
    order = (["top", "start", "center", "end", "bottom"] as Place[]).flatMap((p) => walk(`@${p}`));
  }
  for (const r of body.regions) {
    if (prefs?.collapsed?.includes(r.id) && grant.collapse && hideable(r)) collapsed.add(r.id);
    const want = prefs?.sizes?.[r.id];
    if (want !== undefined && grant.resize && r.size && (r.dock?.resizable ?? true)) {
      sizes.set(r.id, Math.min(r.size.max ?? Number.POSITIVE_INFINITY, Math.max(r.size.min, want)));
    }
  }
  return { order, collapsed, sizes };
}
