import type { OpaqueReason, ScopeKind, ScopeMeta } from "./model.ts";
import { ownUi } from "./own.ts";

const own = ownUi();

/** Runtime-specific abilities; absent members mean the runtime cannot do it and must say so. */
export interface RuntimeCaps {
  openOrClosedShadowRoot?: (el: Element) => ShadowRoot | null;
}

export interface ObservationPolicy {
  allowOrigin(origin: string): boolean;
}

export interface ScopeLimits {
  maxScopeDepth: number;
  maxScopes: number;
}

export const DEFAULT_SCOPE_LIMITS: ScopeLimits = { maxScopeDepth: 8, maxScopes: 256 };

export type ChildScope =
  | { access: "accessible"; root: Document | ShadowRoot; meta: ScopeMeta }
  | { access: "opaque"; meta: ScopeMeta };

const FRAME_TAGS = new Set(["IFRAME", "FRAME"]);

function originOf(url: string | null, base: string): string | null {
  if (url === null || url.trim() === "") return null;
  try {
    const u = new URL(url, base);
    return u.protocol === "http:" || u.protocol === "https:" ? u.origin : null;
  } catch {
    return null;
  }
}

function frameDocument(el: Element): Document | null {
  try {
    return (el as HTMLIFrameElement).contentDocument;
  } catch {
    return null;
  }
}

/** Embedded documents without their own network origin (about:blank, srcdoc) run as their parent. */
function documentOrigin(doc: Document, parentOrigin: string | null): string | null {
  const href = doc.location?.href ?? "";
  if (href === "about:blank" || href === "about:srcdoc") return parentOrigin;
  return originOf(href, href);
}

function opaque(kind: ScopeKind, reason: OpaqueReason, origin: string | null, generation: number): ChildScope {
  return { access: "opaque", meta: { kind, origin, generation, opaque: reason } };
}

function frameScope(
  el: Element,
  parentOrigin: string | null,
  policy: ObservationPolicy,
  generation: number,
): ChildScope {
  const doc = frameDocument(el);
  if (doc?.documentElement) {
    const origin = documentOrigin(doc, parentOrigin);
    if (origin === null || !policy.allowOrigin(origin)) return opaque("frame", "blocked_by_policy", origin, generation);
    return { access: "accessible", root: doc, meta: { kind: "frame", origin, generation } };
  }
  const declared = originOf(el.getAttribute("src"), el.ownerDocument.baseURI);
  const sandbox = el.getAttribute("sandbox");
  if (sandbox !== null && !sandbox.split(/\s+/).includes("allow-same-origin")) {
    return opaque("frame", "sandboxed_frame", declared, generation);
  }
  if (declared !== null && declared !== parentOrigin)
    return opaque("frame", "cross_origin_frame", declared, generation);
  return opaque("frame", "not_loaded", declared, generation);
}

export interface ScopeStats {
  /** Custom elements with no visible shadow root that the runtime cannot rule out as closed. */
  possiblyClosed: number;
}

/** Scopes hanging off one element: its shadow root and/or its embedded document, in that order. */
export function childScopes(
  el: Element,
  parentOrigin: string | null,
  caps: RuntimeCaps,
  policy: ObservationPolicy,
  generations: (key: Element, kind: ScopeKind) => number,
  stats?: ScopeStats,
): ChildScope[] {
  const out: ChildScope[] = [];
  const tag = el.tagName;
  const root = caps.openOrClosedShadowRoot ? caps.openOrClosedShadowRoot(el) : el.shadowRoot;
  if (root) {
    const meta: ScopeMeta = {
      kind: "shadow",
      origin: parentOrigin,
      generation: generations(el, "shadow"),
      mode: root.mode,
    };
    out.push({ access: "accessible", root, meta });
  } else if (stats && !caps.openOrClosedShadowRoot && tag.includes("-")) {
    stats.possiblyClosed++;
  }
  if (FRAME_TAGS.has(tag)) out.push(frameScope(el, parentOrigin, policy, generations(el, "frame")));
  else if (tag === "OBJECT" || tag === "EMBED") out.push(opaque("frame", "native_widget", null, 0));
  else if (tag === "CANVAS") out.push(opaque("document", "canvas_content", parentOrigin, 0));
  return out;
}

export interface ScopeEntry {
  index: number;
  parent: number;
  depth: number;
  kind: ScopeKind;
  hostTag: string | null;
  origin: string | null;
  access: "accessible" | "opaque";
  reason: OpaqueReason | null;
  mode: "open" | "closed" | null;
}

/** Walks every reachable scope once; inaccessible ones appear as placeholders and are never entered. */
export function enumerateScopes(
  top: Document,
  caps: RuntimeCaps,
  policy: ObservationPolicy,
  limits: ScopeLimits = DEFAULT_SCOPE_LIMITS,
  stats?: ScopeStats,
): ScopeEntry[] {
  const topOrigin = originOf(top.location?.href ?? null, top.location?.href ?? "");
  const out: ScopeEntry[] = [
    {
      index: 0,
      parent: -1,
      depth: 0,
      kind: "document",
      hostTag: null,
      origin: topOrigin,
      access: "accessible",
      reason: null,
      mode: null,
    },
  ];
  const queue: { root: Document | ShadowRoot; index: number; origin: string | null }[] = [
    { root: top, index: 0, origin: topOrigin },
  ];
  const generation = (): number => 1;
  while (queue.length > 0) {
    const cur = queue.shift() as (typeof queue)[number];
    const walker = (cur.root.ownerDocument ?? (cur.root as Document)).createTreeWalker(cur.root, 1);
    for (let n = walker.nextNode() as Element | null; n; n = walker.nextNode() as Element | null) {
      if (own.has(n)) continue;
      for (const s of childScopes(n, cur.origin, caps, policy, generation, stats)) {
        const depth = out[cur.index]?.depth ?? 0;
        const entry: ScopeEntry = {
          index: out.length,
          parent: cur.index,
          depth: depth + 1,
          kind: s.meta.kind,
          hostTag: n.tagName.toLowerCase(),
          origin: s.meta.origin,
          access: s.access,
          reason: s.access === "opaque" ? (s.meta.opaque ?? null) : null,
          mode: s.meta.mode ?? null,
        };
        if (out.length >= limits.maxScopes || entry.depth > limits.maxScopeDepth) {
          entry.access = "opaque";
          entry.reason = "limit_exceeded";
          out.push(entry);
          continue;
        }
        out.push(entry);
        if (s.access === "accessible") queue.push({ root: s.root, index: entry.index, origin: s.meta.origin });
      }
    }
  }
  return out;
}

export interface MissingSignalOut {
  signal: string;
  reason: "not_permitted" | "not_supported" | "not_attempted" | "failed" | "unknown";
}

/** Maps a placeholder to the ObservationIR missing signal that must accompany the observation. */
export function missingSignal(meta: ScopeMeta): MissingSignalOut | null {
  const reason = meta.opaque;
  if (reason === undefined) return null;
  if (reason === "not_loaded") return { signal: "cross_origin_frame", reason: "unknown" };
  if (reason === "closed_shadow_root" || reason === "canvas_content" || reason === "native_widget") {
    return { signal: reason, reason: "not_supported" };
  }
  if (reason === "limit_exceeded") {
    return { signal: meta.kind === "shadow" ? "closed_shadow_root" : "cross_origin_frame", reason: "not_attempted" };
  }
  return { signal: "cross_origin_frame", reason: "not_permitted" };
}
