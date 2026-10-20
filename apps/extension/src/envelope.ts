export const PROTOCOL = "vsf-ext-v1";

import { type Prefs, parsePrefs } from "./prefs.ts";

export type PageKind = "hello" | "dispatch" | "refresh" | "restore" | "heartbeat" | "prefs";

export const RESTORE_REASONS = ["user", "crash", "downgrade", "orphaned", "escape", "removed"] as const;
export type RestoreReason = (typeof RESTORE_REASONS)[number];

export interface DispatchBody {
  action: string;
  entity: string;
  rowKey?: string;
}

export interface BodyOf {
  hello: Record<string, never>;
  refresh: Record<string, never>;
  heartbeat: { at: number };
  restore: { reason: RestoreReason };
  dispatch: DispatchBody;
  prefs: { prefs: Prefs };
}

export interface Envelope<K extends PageKind = PageKind> {
  proto: typeof PROTOCOL;
  kind: K;
  /** Unique per request; a repeated id is answered from the record, never executed twice. */
  id: string;
  /** Names the page session the sender belongs to; only that session's tab and document may use it. */
  session: string;
  /** Strictly increasing per session, so a replayed or reordered message is recognizable. */
  seq: number;
  body: BodyOf[K];
}

export type EnvelopeError =
  | "bad_envelope"
  | "unsupported_version"
  | "unknown_kind"
  | "too_large"
  | "bad_id"
  | "bad_session"
  | "bad_seq"
  | "bad_body";

/** Upper bounds on the serialized size of each kind; the page is untrusted input for the worker. */
export const MAX_ENVELOPE_CHARS: Record<PageKind, number> = {
  hello: 512,
  refresh: 512,
  restore: 512,
  heartbeat: 384,
  dispatch: 2048,
  prefs: 4096,
};

const ID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
const IDENT = /^[a-z][a-z0-9_-]{0,63}$/;
const ROW_KEY = /^[A-Za-z0-9_.:-]{1,128}$/;
const KINDS = new Set<string>(["hello", "dispatch", "refresh", "restore", "heartbeat", "prefs"]);
const KEYS = new Set(["proto", "kind", "id", "session", "seq", "body"]);

export type Parsed = { ok: true; env: Envelope } | { ok: false; error: EnvelopeError };

const fail = (error: EnvelopeError): Parsed => ({ ok: false, error });

function isPlain(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v) && Object.getPrototypeOf(v) === Object.prototype;
}

function bodyOk(kind: PageKind, body: Record<string, unknown>): boolean {
  const keys = Object.keys(body);
  switch (kind) {
    case "hello":
    case "refresh":
      return keys.length === 0;
    case "heartbeat":
      return keys.length === 1 && typeof body.at === "number" && Number.isFinite(body.at);
    case "restore":
      return keys.length === 1 && (RESTORE_REASONS as readonly unknown[]).includes(body.reason);
    case "prefs":
      return keys.length === 1 && parsePrefs(body.prefs) !== null;
    case "dispatch": {
      if (keys.some((k) => k !== "action" && k !== "entity" && k !== "rowKey")) return false;
      if (typeof body.action !== "string" || !IDENT.test(body.action)) return false;
      if (typeof body.entity !== "string" || !IDENT.test(body.entity)) return false;
      return body.rowKey === undefined || (typeof body.rowKey === "string" && ROW_KEY.test(body.rowKey));
    }
  }
}

export function parseEnvelope(raw: unknown): Parsed {
  if (!isPlain(raw)) return fail("bad_envelope");
  const keys = Object.keys(raw);
  if (keys.length !== KEYS.size || keys.some((k) => !KEYS.has(k))) return fail("bad_envelope");
  if (raw.proto !== PROTOCOL) return fail("unsupported_version");
  if (typeof raw.kind !== "string" || !KINDS.has(raw.kind)) return fail("unknown_kind");
  const kind = raw.kind as PageKind;
  if (typeof raw.id !== "string" || !ID.test(raw.id)) return fail("bad_id");
  if (typeof raw.session !== "string" || !ID.test(raw.session)) return fail("bad_session");
  if (typeof raw.seq !== "number" || !Number.isSafeInteger(raw.seq) || raw.seq < 0) return fail("bad_seq");
  if (!isPlain(raw.body)) return fail("bad_body");
  let size: number;
  try {
    size = JSON.stringify(raw).length;
  } catch {
    return fail("bad_envelope");
  }
  if (size > MAX_ENVELOPE_CHARS[kind]) return fail("too_large");
  if (!bodyOk(kind, raw.body)) return fail("bad_body");
  return { ok: true, env: raw as unknown as Envelope };
}

export function makeEnvelope<K extends PageKind>(
  kind: K,
  session: string,
  seq: number,
  body: BodyOf[K],
  id: string = crypto.randomUUID(),
): Envelope<K> {
  return { proto: PROTOCOL, kind, id, session, seq, body };
}
