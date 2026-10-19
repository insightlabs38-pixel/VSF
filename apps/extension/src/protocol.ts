export type Command =
  | { type: "status" }
  | { type: "grant"; origin: string }
  | { type: "revoke"; origin: string }
  | { type: "capture"; tabId: number }
  | { type: "observe"; tabId: number };

export type DenyReason =
  | "sender_not_allowed"
  | "bad_request"
  | "origin_not_granted"
  | "origin_unobservable"
  | "origin_changed"
  | "inject_failed"
  | "pack_invalid";

export type Reply = { ok: true; [key: string]: unknown } | { ok: false; error: DenyReason | string; detail?: string };

/** An origin is scheme://host[:port] with http(s) only; paths, wildcards and credentials are refused. */
export function normalizeOrigin(value: unknown): string | null {
  if (typeof value !== "string" || value.length > 300) return null;
  let url: URL;
  try {
    url = new URL(value);
  } catch {
    return null;
  }
  if (url.protocol !== "http:" && url.protocol !== "https:") return null;
  if (url.username || url.password || url.origin !== value) return null;
  // URL parsers disagree on characters such as "*"; only plain DNS labels, IPv4 and bracketed IPv6 pass.
  if (!/^(?:[a-z0-9](?:[a-z0-9-]*[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]*[a-z0-9])?)*|\[[0-9a-f:.]+\])$/.test(url.hostname))
    return null;
  return url.origin;
}

export function parseCommand(raw: unknown): Command | null {
  if (typeof raw !== "object" || raw === null) return null;
  const m = raw as Record<string, unknown>;
  switch (m.type) {
    case "status":
      return { type: "status" };
    case "grant":
    case "revoke": {
      const origin = normalizeOrigin(m.origin);
      return origin === null ? null : { type: m.type, origin };
    }
    case "capture":
    case "observe":
      return typeof m.tabId === "number" && Number.isSafeInteger(m.tabId) && m.tabId >= 0
        ? { type: m.type, tabId: m.tabId }
        : null;
    default:
      return null;
  }
}

/** Only the packaged panel page may issue commands; content scripts and web pages never can. */
export function isCommandSender(
  sender: { id?: string; url?: string; tab?: unknown; frameId?: number },
  selfId: string,
  panelUrl: string,
): boolean {
  return (
    sender.id === selfId &&
    sender.url !== undefined &&
    (sender.url === panelUrl || sender.url.startsWith(`${panelUrl}?`)) &&
    sender.frameId === 0
  );
}
