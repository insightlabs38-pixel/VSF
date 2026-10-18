import type { Replica } from "./model.ts";
import { type MissingSignalOut, missingSignal, type RuntimeCaps, type ScopeStats } from "./scope.ts";

export interface CapabilityEntryOut {
  id: string;
  state: "available" | "degraded" | "unavailable" | "unknown";
  constraints?: string[];
  reason?: string;
}

/** What this runtime can and cannot observe, shaped as EnvironmentCapabilityManifest.observation entries. */
export function captureCapabilities(caps: RuntimeCaps, win: Window | null): CapabilityEntryOut[] {
  const hasNavigation = !!(win as (Window & { navigation?: unknown }) | null)?.navigation;
  return [
    { id: "observe.dom.snapshot", state: "available" },
    { id: "observe.dom.mutations", state: "available", constraints: ["batched", "epoch and sequence numbered"] },
    { id: "observe.shadow.open", state: "available" },
    caps.openOrClosedShadowRoot
      ? { id: "observe.shadow.closed", state: "available", constraints: ["through the extension API only"] }
      : {
          id: "observe.shadow.closed",
          state: "unavailable",
          reason: "page-context scripts cannot see closed shadow roots",
        },
    { id: "observe.shadow.late_attach", state: "degraded", reason: "attachShadow after capture needs rescan()" },
    { id: "observe.frames.same_origin", state: "available" },
    {
      id: "observe.frames.cross_origin",
      state: "unavailable",
      reason: "same-origin policy: frames appear as opaque placeholders",
    },
    { id: "observe.layout.geometry", state: "available", constraints: ["on demand per node"] },
    {
      id: "observe.layout.visibility",
      state: "degraded",
      reason: "stylesheet-only changes are seen at the next probe",
    },
    { id: "observe.a11y.computed_names", state: "degraded", reason: "simplified accessible-name computation" },
    {
      id: "observe.a11y.tree",
      state: "unavailable",
      reason: "the browser accessibility tree is not exposed to the page",
    },
    { id: "observe.env.locale", state: "available", constraints: ["only what the document declares"] },
    { id: "observe.env.viewport", state: "available" },
    { id: "observe.env.flags", state: "degraded", reason: "only flags exposed through configured probes" },
    hasNavigation
      ? { id: "observe.navigation.spa", state: "available" }
      : { id: "observe.navigation.spa", state: "degraded", reason: "URL changes are noticed at the next mutation" },
    { id: "observe.network", state: "unavailable", reason: "no traffic capture in this runtime" },
    { id: "observe.canvas", state: "unavailable", reason: "canvas pixels are not interpreted" },
  ];
}

const BASELINE: MissingSignalOut[] = [
  { signal: "accessibility_tree", reason: "not_supported" },
  { signal: "network", reason: "not_supported" },
  { signal: "protocol_stream", reason: "not_supported" },
  { signal: "layout", reason: "not_attempted" },
];

/** Gaps to attach to the observation: the runtime baseline plus every placeholder actually present. */
export function missingSignals(replica: Replica, stats: ScopeStats, caps: RuntimeCaps): MissingSignalOut[] {
  const seen = new Map<string, MissingSignalOut>();
  const add = (m: MissingSignalOut): void => {
    seen.set(`${m.signal}/${m.reason}`, m);
  };
  for (const m of BASELINE) add(m);
  for (const rec of replica.nodes.values()) {
    const m = rec.scope ? missingSignal(rec.scope) : null;
    if (m) add(m);
  }
  if (stats.possiblyClosed > 0 && !caps.openOrClosedShadowRoot)
    add({ signal: "closed_shadow_root", reason: "unknown" });
  return [...seen.values()].sort((a, b) => (a.signal + a.reason).localeCompare(b.signal + b.reason));
}
