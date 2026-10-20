import type { Envelope, EnvelopeError } from "./envelope.ts";

export type Phase = "assembling" | "rendered";

export type ActionState = "available" | "unknown" | "conflicted" | "unusable";

export interface OpRecord {
  id: string;
  kind: string;
  /** `sent` means the effect may have happened: a restarted worker reports it and never sends it again. */
  state: "sent" | "done" | "unknown";
  reply?: unknown;
  at: number;
}

export const SESSION_VERSION = 1;

export interface SessionRecord {
  /** Records from another version are never interpreted; the page they belong to is restored instead. */
  v: number;
  id: string;
  tabId: number;
  origin: string;
  documentId: string | undefined;
  epoch: number;
  semanticDigest: string;
  shapeDigest: string;
  phase: Phase;
  lastSeq: number;
  createdAt: number;
  seenAt: number;
  actions: Record<string, { state: ActionState; reason: string | null }>;
  ops: OpRecord[];
  /** The companion's binding for this document, or why there is none; the page works the same without it. */
  bridge?: { id: string; expiresAt: number } | { error: string };
}

export const MAX_OPS = 64;

export interface SenderLike {
  id?: string | undefined;
  url?: string | undefined;
  origin?: string | undefined;
  tab?: { id?: number | undefined } | undefined;
  frameId?: number | undefined;
  documentId?: string | undefined;
}

export type SourceError =
  | "sender_not_allowed"
  | "session_unknown"
  | "tab_mismatch"
  | "frame_mismatch"
  | "origin_mismatch"
  | "document_mismatch";

/** A page message is trusted only if it comes from this extension's script in the same top-level document the session was built on. */
export function checkSource(
  sender: SenderLike,
  selfId: string,
  session: SessionRecord | undefined,
): { ok: true } | { ok: false; error: SourceError } {
  if (sender.id !== selfId || sender.tab?.id === undefined) return { ok: false, error: "sender_not_allowed" };
  if (session === undefined) return { ok: false, error: "session_unknown" };
  if (sender.tab.id !== session.tabId) return { ok: false, error: "tab_mismatch" };
  if (sender.frameId !== 0) return { ok: false, error: "frame_mismatch" };
  let origin: string | undefined = sender.origin;
  if (origin === undefined && sender.url !== undefined) {
    try {
      origin = new URL(sender.url).origin;
    } catch {
      origin = undefined;
    }
  }
  if (origin !== session.origin) return { ok: false, error: "origin_mismatch" };
  if (session.documentId !== undefined && sender.documentId !== session.documentId)
    return { ok: false, error: "document_mismatch" };
  return { ok: true };
}

export type Admission =
  | { kind: "new" }
  | { kind: "duplicate"; op: OpRecord }
  | { kind: "refused"; error: EnvelopeError | "stale_seq" };

/** Same id returns the recorded outcome; an older sequence number with a new id is a replay. */
export function admit(session: SessionRecord, env: Envelope): Admission {
  const prior = session.ops.find((o) => o.id === env.id);
  if (prior) return { kind: "duplicate", op: prior };
  if (env.seq <= session.lastSeq) return { kind: "refused", error: "stale_seq" };
  return { kind: "new" };
}

export function noteOp(session: SessionRecord, op: OpRecord): void {
  const at = session.ops.findIndex((o) => o.id === op.id);
  if (at >= 0) session.ops[at] = op;
  else session.ops.push(op);
  if (session.ops.length > MAX_OPS) session.ops.splice(0, session.ops.length - MAX_OPS);
}
