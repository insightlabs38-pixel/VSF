export const ENVELOPE_VERSION = 1;

export type Kind = "observation" | "compiled" | "render";

/** Who produced the envelope, in terms the receiver checks independently: tab, frame, origin and transform epoch. */
export interface Source {
  tab: number;
  frame: number;
  origin: string;
  epoch: string;
}

export interface Envelope {
  v: typeof ENVELOPE_VERSION;
  kind: Kind;
  id: string;
  parent: string | null;
  seq: number;
  source: Source;
  body: Record<string, unknown>;
}

export type EnvelopeError =
  | "envelope.malformed"
  | "envelope.version"
  | "envelope.kind"
  | "envelope.too_large"
  | "envelope.id"
  | "envelope.source"
  | "envelope.body"
  | "envelope.source_mismatch"
  | "envelope.stale_epoch"
  | "envelope.replay"
  | "envelope.parent"
  | "envelope.stage_order"
  | "envelope.digest";

export type Checked = { ok: true; envelope: Envelope } | { ok: false; code: EnvelopeError };

/** Serialized size limits per stage; the observation carries a base64 pack, the others carry summaries and bundles. */
export const MAX_BYTES: Record<Kind, number> = { observation: 4 << 20, compiled: 16 << 10, render: 1 << 20 };

const ID = /^[a-z0-9-]{8,64}$/;
const HEX64 = /^[0-9a-f]{64}$/;
const APP = /^[a-z][a-z0-9-]{1,39}$/;
const KINDS: readonly Kind[] = ["observation", "compiled", "render"];
const PARENT_KIND: Record<Kind, Kind | null> = { observation: null, compiled: "observation", render: "compiled" };

const isObj = (v: unknown): v is Record<string, unknown> => typeof v === "object" && v !== null && !Array.isArray(v);
const isInt = (v: unknown, lo: number, hi: number): v is number =>
  typeof v === "number" && Number.isSafeInteger(v) && v >= lo && v <= hi;
const onlyKeys = (o: Record<string, unknown>, keys: string[]): boolean =>
  Object.keys(o).every((k) => keys.includes(k)) && keys.every((k) => k in o || OPTIONAL.has(k));
const OPTIONAL = new Set(["reason", "bundle"]);

/** Deterministic JSON: sorted keys, no whitespace; both the producer and the receiver of a digest use this. */
export function canonicalJson(value: unknown): string {
  if (Array.isArray(value)) return `[${value.map(canonicalJson).join(",")}]`;
  if (isObj(value))
    return `{${Object.keys(value)
      .sort()
      .map((k) => `${JSON.stringify(k)}:${canonicalJson(value[k])}`)
      .join(",")}}`;
  return JSON.stringify(value);
}

export async function sha256Hex(data: Uint8Array | string): Promise<string> {
  const bytes = typeof data === "string" ? new TextEncoder().encode(data) : data;
  const digest = await crypto.subtle.digest("SHA-256", bytes as BufferSource);
  return [...new Uint8Array(digest)].map((b) => b.toString(16).padStart(2, "0")).join("");
}

function bodyOk(kind: Kind, body: unknown): boolean {
  if (!isObj(body)) return false;
  switch (kind) {
    case "observation":
      return (
        onlyKeys(body, ["pack", "digest", "bytes", "nodes", "truncated"]) &&
        typeof body.pack === "string" &&
        /^[A-Za-z0-9+/]*={0,2}$/.test(body.pack) &&
        typeof body.digest === "string" &&
        HEX64.test(body.digest) &&
        isInt(body.bytes, 0, 3 << 20) &&
        isInt(body.nodes, 0, 200_000) &&
        typeof body.truncated === "boolean"
      );
    case "compiled": {
      if (!onlyKeys(body, ["pack_digest", "node_count", "classes", "app", "match"])) return false;
      const classes = body.classes;
      return (
        typeof body.pack_digest === "string" &&
        HEX64.test(body.pack_digest) &&
        isInt(body.node_count, 0, 200_000) &&
        isObj(classes) &&
        Object.keys(classes).length <= 32 &&
        Object.values(classes).every((n) => isInt(n, 0, 200_000)) &&
        (body.app === null || (typeof body.app === "string" && APP.test(body.app))) &&
        (body.match === "matched" || body.match === "no_bundle" || body.match === "mismatch")
      );
    }
    case "render": {
      if (!onlyKeys(body, ["app", "bundle_digest", "state", "reason", "bundle"])) return false;
      const state = body.state;
      if (state !== "ready" && state !== "source_ui") return false;
      if (body.reason !== undefined && !(typeof body.reason === "string" && body.reason.length <= 120)) return false;
      if (state === "ready" && !isObj(body.bundle)) return false;
      if (state === "source_ui" && body.bundle !== undefined) return false;
      return (
        typeof body.app === "string" &&
        APP.test(body.app) &&
        typeof body.bundle_digest === "string" &&
        HEX64.test(body.bundle_digest)
      );
    }
  }
}

/** Shape, version, size and body checks; whether the sender may speak for this source is {@link Chain}'s job. */
export function parseEnvelope(raw: unknown): Checked {
  if (!isObj(raw)) return { ok: false, code: "envelope.malformed" };
  if (!onlyKeys(raw, ["v", "kind", "id", "parent", "seq", "source", "body"]))
    return { ok: false, code: "envelope.malformed" };
  if (raw.v !== ENVELOPE_VERSION) return { ok: false, code: "envelope.version" };
  const kind = raw.kind;
  if (!KINDS.includes(kind as Kind)) return { ok: false, code: "envelope.kind" };
  if (JSON.stringify(raw).length > MAX_BYTES[kind as Kind]) return { ok: false, code: "envelope.too_large" };
  if (typeof raw.id !== "string" || !ID.test(raw.id)) return { ok: false, code: "envelope.id" };
  const wantsParent = PARENT_KIND[kind as Kind] !== null;
  if (wantsParent ? typeof raw.parent !== "string" || !ID.test(raw.parent) : raw.parent !== null)
    return { ok: false, code: "envelope.parent" };
  if (!isInt(raw.seq, 1, Number.MAX_SAFE_INTEGER)) return { ok: false, code: "envelope.malformed" };
  const s = raw.source;
  if (
    !isObj(s) ||
    !onlyKeys(s, ["tab", "frame", "origin", "epoch"]) ||
    !isInt(s.tab, 0, 2 ** 31) ||
    !isInt(s.frame, 0, 2 ** 31) ||
    typeof s.origin !== "string" ||
    s.origin.length > 300 ||
    typeof s.epoch !== "string" ||
    !ID.test(s.epoch)
  )
    return { ok: false, code: "envelope.source" };
  if (!bodyOk(kind as Kind, raw.body)) return { ok: false, code: "envelope.body" };
  return { ok: true, envelope: raw as unknown as Envelope };
}

export interface Expected {
  tab: number;
  frame: number;
  origin: string;
  epoch: string;
}

interface Stream {
  seq: number;
  kinds: Map<string, Kind>;
  used: Set<string>;
}

const KEEP = 64;

/** Per-stream receiver state: ordering, replay and parent linkage across the stages of one transform. */
export class Chain {
  private streams = new Map<string, Stream>();
  private ended = new Set<string>();

  accept(raw: unknown, expect: Expected): Checked {
    const parsed = parseEnvelope(raw);
    if (!parsed.ok) return parsed;
    const e = parsed.envelope;
    if (e.source.epoch !== expect.epoch || this.ended.has(expect.epoch))
      return { ok: false, code: "envelope.stale_epoch" };
    if (e.source.tab !== expect.tab || e.source.frame !== expect.frame || e.source.origin !== expect.origin)
      return { ok: false, code: "envelope.source_mismatch" };
    const key = `${expect.tab}:${expect.epoch}`;
    const stream = this.streams.get(key) ?? { seq: 0, kinds: new Map(), used: new Set() };
    if (e.seq <= stream.seq) return { ok: false, code: "envelope.replay" };
    const want = PARENT_KIND[e.kind];
    if (want !== null) {
      const parentKind = e.parent === null ? undefined : stream.kinds.get(e.parent);
      if (parentKind === undefined) return { ok: false, code: "envelope.parent" };
      if (parentKind !== want || (e.parent !== null && stream.used.has(e.parent)))
        return { ok: false, code: "envelope.stage_order" };
      stream.used.add(e.parent as string);
    }
    stream.seq = e.seq;
    stream.kinds.set(e.id, e.kind);
    while (stream.kinds.size > KEEP) stream.kinds.delete(stream.kinds.keys().next().value as string);
    this.streams.set(key, stream);
    return parsed;
  }

  /** Registers an envelope the receiver was told about out of band, so a child of it can be accepted. */
  seed(tab: number, epoch: string, id: string, kind: Kind, seq: number): void {
    const key = `${tab}:${epoch}`;
    const stream = this.streams.get(key) ?? { seq: 0, kinds: new Map(), used: new Set() };
    stream.kinds.set(id, kind);
    stream.seq = Math.max(stream.seq, seq);
    this.streams.set(key, stream);
  }

  /** Ends a transform session; envelopes for that epoch are stale from then on. */
  end(tab: number, epoch: string): void {
    this.streams.delete(`${tab}:${epoch}`);
    this.ended.add(epoch);
  }
}
