/** Memory-only credential for one transformed tab; a worker restart loses every one of them by design. */
export interface TabSession {
  key: string;
  epoch: string;
  seq: number;
  origin: string;
}

export type SessionError =
  | "stale_session"
  | "sender_not_allowed"
  | "origin_changed"
  | "replay"
  | "bad_request"
  | "origin_not_granted";

export interface ShellSender {
  id?: string | undefined;
  url?: string | undefined;
  origin?: string | undefined;
  tab?: { id?: number | undefined } | undefined;
  frameId?: number | undefined;
}

export interface Authenticated {
  tab: number;
  session: TabSession;
}

/** Constant-time comparison for equal-length secrets; unequal lengths fail without leaking where they differ. */
export function sameSecret(a: string, b: string): boolean {
  if (a.length !== b.length) return false;
  let diff = 0;
  for (let i = 0; i < a.length; i++) diff |= a.charCodeAt(i) ^ b.charCodeAt(i);
  return diff === 0;
}

export function newKey(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(24));
  return [...bytes].map((b) => b.toString(16).padStart(2, "0")).join("");
}

/** A content-script message is accepted from this extension's top frame, on the session's origin, with its key and epoch. */
export function authenticate(
  msg: { key?: unknown; epoch?: unknown; seq?: unknown },
  sender: ShellSender,
  selfId: string,
  sessions: ReadonlyMap<number, TabSession>,
  grantedOrigins: readonly string[],
): { ok: true; auth: Authenticated } | { ok: false; error: SessionError } {
  if (sender.id !== selfId || sender.tab?.id === undefined || sender.frameId !== 0)
    return { ok: false, error: "sender_not_allowed" };
  const tab = sender.tab.id;
  const session = sessions.get(tab);
  if (
    session === undefined ||
    typeof msg.key !== "string" ||
    typeof msg.epoch !== "string" ||
    !sameSecret(msg.key, session.key) ||
    msg.epoch !== session.epoch
  )
    return { ok: false, error: "stale_session" };
  const origin = sender.origin ?? (sender.url ? new URL(sender.url).origin : undefined);
  if (origin !== session.origin) return { ok: false, error: "origin_changed" };
  if (!grantedOrigins.includes(session.origin)) return { ok: false, error: "origin_not_granted" };
  if (typeof msg.seq !== "number" || !Number.isSafeInteger(msg.seq) || msg.seq <= session.seq)
    return { ok: false, error: "replay" };
  return { ok: true, auth: { tab, session } };
}
