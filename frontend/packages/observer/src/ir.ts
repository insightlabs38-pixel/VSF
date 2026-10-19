import { missingSignals } from "./advertise.ts";
import { FLAG_REDACTED, type Replica } from "./model.ts";
import type { RuntimeCaps, ScopeStats } from "./scope.ts";

export interface HostInfo {
  sourceAppId: string;
  variantResolution: "verified" | "inferred" | "unknown" | "conflicted";
  sourceVariantId?: string;
  runtimeId: string;
  principalRef: string;
  capturedAt: number;
  staleAfterMs: number;
  clockSkewMs?: number;
  /** `sha256:<hex>` of the VSFS pack bytes and of the redaction pattern set, computed by the host. */
  packDigest: string;
  packBytes: number;
  patternsDigest: string;
}

/** The ObservationIR body for the replica's current state, always a full observation of its epoch. */
export function observationBody(
  replica: Replica,
  stats: ScopeStats,
  caps: RuntimeCaps,
  host: HostInfo,
): Record<string, unknown> {
  let frames = 0;
  let documents = 0;
  let redacted = 0;
  let origin = "";
  for (const n of replica.nodes.values()) {
    if ((n.flags & FLAG_REDACTED) !== 0) redacted++;
    if (n.scope?.kind === "frame") frames++;
    if (n.scope && n.scope.kind !== "shadow" && !n.scope.opaque) documents++;
    if (n.parent === -1 && n.scope) origin = n.scope.origin ?? "";
  }
  const freshness: Record<string, unknown> = { stale_after_ms: host.staleAfterMs };
  if (host.clockSkewMs !== undefined) freshness.clock_skew_ms = host.clockSkewMs;
  const body: Record<string, unknown> = {
    source_app_id: host.sourceAppId,
    variant_resolution: host.variantResolution,
    origin,
    freshness,
    runtime_id: host.runtimeId,
    principal_ref: host.principalRef,
    redactions: { redacted_nodes: redacted, patterns: host.patternsDigest },
    observation_epoch: replica.epoch,
    mode: "full",
    captured_at: host.capturedAt,
    scope: { frames, documents, full_document: !replica.truncated },
    payload: { format: "vsfs_pack_v1", digest: host.packDigest, bytes: host.packBytes, nodes: replica.size },
    missing_signals: missingSignals(replica, stats, caps),
  };
  if (host.sourceVariantId !== undefined) body.source_variant_id = host.sourceVariantId;
  return body;
}

export type Freshness = "fresh" | "stale" | "unsynced";

/** Evidence is usable only while the replica equals the page and the observation is younger than its window. */
export function freshnessOf(replica: Replica, capturedAt: number, now: number, staleAfterMs: number): Freshness {
  if (replica.state !== "synced") return "unsynced";
  return now - capturedAt > staleAfterMs ? "stale" : "fresh";
}
