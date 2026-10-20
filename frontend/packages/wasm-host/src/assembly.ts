import type { Fabric } from "./index.ts";

export interface AssemblyParams {
  origin: string;
  variantId: string;
  observedAt: number;
  epoch: number;
}

export interface AssemblyAction {
  state: "available" | "unknown" | "conflicted" | "unusable";
  reason: string | null;
  sources: { node: number; row?: string }[];
}

export interface Assembly {
  schema: "vsf-assembly-v1";
  observation: string;
  semantic: { schema: string; schema_digest: string; body: Record<string, unknown> };
  semantic_digest: string;
  views: { entity: string; layout: "grid" | "form" }[];
  rows: Record<string, { key: string; values: Record<string, string> }[]>;
  actions: Record<string, AssemblyAction>;
  withheld: { node: number; reason: string }[];
  limits: { nodes: number; truncated: boolean };
}

export class AssemblyRejected extends Error {
  readonly code = "assembly.malformed";
}

/** The frame the compiler reads: big-endian header length, canonical header JSON, then the snapshot bytes. */
export function assemblyFrame(params: AssemblyParams, snapshot: Uint8Array): Uint8Array {
  const header = new TextEncoder().encode(
    JSON.stringify({
      epoch: params.epoch,
      observed_at: params.observedAt,
      origin: params.origin,
      variant_id: params.variantId,
    }),
  );
  const out = new Uint8Array(4 + header.length + snapshot.length);
  new DataView(out.buffer).setUint32(0, header.length, false);
  out.set(header, 4);
  out.set(snapshot, 4 + header.length);
  return out;
}

const STATES = new Set(["available", "unknown", "conflicted", "unusable"]);

export function parseAssembly(bytes: Uint8Array): Assembly {
  const doc = JSON.parse(new TextDecoder().decode(bytes)) as Assembly;
  const ok =
    doc?.schema === "vsf-assembly-v1" &&
    typeof doc.semantic_digest === "string" &&
    /^sha256:[0-9a-f]{64}$/.test(doc.semantic_digest) &&
    Array.isArray(doc.views) &&
    typeof doc.semantic === "object" &&
    Array.isArray((doc.semantic.body as { entities?: unknown }).entities) &&
    Object.values(doc.actions ?? {}).every((a) => STATES.has(a.state));
  if (!ok) throw new AssemblyRejected("assembly document does not match vsf-assembly-v1");
  return doc;
}

export function assemble(fabric: Fabric, params: AssemblyParams, snapshot: Uint8Array): Assembly {
  return parseAssembly(fabric.assemble(assemblyFrame(params, snapshot)));
}
