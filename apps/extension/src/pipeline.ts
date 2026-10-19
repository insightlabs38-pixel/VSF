import { type AppBundle, BUNDLES, bundleDigest, bundleFor, mismatch } from "./bundles.ts";
import { ENVELOPE_VERSION, type Envelope, sha256Hex } from "./envelope.ts";

export class PipelineError extends Error {
  readonly code: string;
  constructor(code: string) {
    super(code);
    this.name = "PipelineError";
    this.code = code;
  }
}

export interface Deps {
  /** The packaged compiler: validates a pack and returns its canonical summary. */
  analyze(pack: Uint8Array): Record<string, unknown>;
  newId(): string;
  bundles?: readonly AppBundle[];
}

export function decodePack(b64: string): Uint8Array {
  const bin = atob(b64);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

export interface Stages {
  compiled: Envelope;
  render: Envelope;
  bundle: AppBundle | undefined;
}

/** Observation to compiled to render: each stage is a new envelope whose parent is the one before. */
export async function runStages(observation: Envelope, pathname: string, deps: Deps): Promise<Stages> {
  const body = observation.body as { pack: string; digest: string; bytes: number };
  const pack = decodePack(body.pack);
  if (pack.length !== body.bytes || (await sha256Hex(pack)) !== body.digest) throw new PipelineError("envelope.digest");
  const summary = deps.analyze(pack);
  if (summary.digest !== body.digest) throw new PipelineError("envelope.digest");
  const classes = (summary.classes ?? {}) as Record<string, number>;
  const bundles = deps.bundles ?? BUNDLES;
  const bundle = bundleFor(pathname, bundles);
  const why = bundle === undefined ? "no application bundle for this page" : mismatch(bundle, classes);
  const matched = bundle !== undefined && why === null;
  const compiled: Envelope = {
    v: ENVELOPE_VERSION,
    kind: "compiled",
    id: deps.newId(),
    parent: observation.id,
    seq: observation.seq + 1,
    source: observation.source,
    body: {
      pack_digest: body.digest,
      node_count: summary.node_count as number,
      classes,
      app: bundle?.id ?? null,
      match: bundle === undefined ? "no_bundle" : matched ? "matched" : "mismatch",
    },
  };
  const digest = bundle === undefined ? await sha256Hex("") : await bundleDigest(bundle);
  const render: Envelope = {
    v: ENVELOPE_VERSION,
    kind: "render",
    id: deps.newId(),
    parent: compiled.id,
    seq: compiled.seq + 1,
    source: observation.source,
    body: matched
      ? { app: bundle.id, bundle_digest: digest, state: "ready", bundle: bundle as unknown as Record<string, unknown> }
      : { app: bundle?.id ?? "none", bundle_digest: digest, state: "source_ui", reason: (why ?? "").slice(0, 120) },
  };
  return { compiled, render, bundle };
}
