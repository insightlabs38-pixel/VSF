export type Command =
  | { type: "status" }
  | { type: "grant"; origin: string }
  | { type: "revoke"; origin: string }
  | { type: "capture"; tabId: number }
  | { type: "observe"; tabId: number }
  | { type: "transform"; tabId: number }
  | { type: "restore"; tabId: number }
  | { type: "connect"; url: string }
  | { type: "pair_begin"; url: string }
  | { type: "pair_finish"; code: string }
  | { type: "unpair" }
  | { type: "ping" }
  | { type: "disconnect" };

export type DenyReason =
  | "sender_not_allowed"
  | "bad_request"
  | "origin_not_granted"
  | "origin_unobservable"
  | "origin_changed"
  | "inject_failed"
  | "render_failed"
  | "permission_not_held"
  | "unpaired"
  | "pairing_failed"
  | "service_refused"
  | "service_unreachable"
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

/** The local service is a loopback IPv4 http origin with an explicit port; nothing else is ever forwarded to. */
export function normalizeService(value: unknown): string | null {
  if (typeof value !== "string" || value.length > 40) return null;
  const m = /^http:\/\/127\.0\.0\.1:(\d{1,5})$/.exec(value);
  if (m === null) return null;
  const port = Number(m[1]);
  return port >= 1 && port <= 65535 && String(port) === m[1] ? value : null;
}

export interface Dispatch {
  type: "dispatch";
  requestId: string;
  key: string;
  epoch: string;
  seq: number;
  action: string;
  entity: string;
  rowKey?: string;
  values: Record<string, string | number | boolean>;
}

export type ShellMessage = Dispatch | { type: "reconnect"; epoch: string };

const NAME = /^[a-z][a-z0-9_]{0,63}$/;
const ID = /^[a-z0-9-]{8,64}$/;

/** What the page-side shell may say: operation requests and a reconnect request, closed and bounded. */
export function parseShellMessage(raw: unknown): ShellMessage | null {
  if (typeof raw !== "object" || raw === null) return null;
  const m = raw as Record<string, unknown>;
  if (m.type === "reconnect")
    return typeof m.epoch === "string" && ID.test(m.epoch) ? { type: "reconnect", epoch: m.epoch } : null;
  if (m.type !== "dispatch") return null;
  const values = m.values;
  if (
    typeof m.requestId !== "string" ||
    !ID.test(m.requestId) ||
    typeof m.key !== "string" ||
    !/^[0-9a-f]{48}$/.test(m.key) ||
    typeof m.epoch !== "string" ||
    !ID.test(m.epoch) ||
    typeof m.seq !== "number" ||
    !Number.isSafeInteger(m.seq) ||
    m.seq < 1 ||
    typeof m.action !== "string" ||
    !NAME.test(m.action) ||
    typeof m.entity !== "string" ||
    !NAME.test(m.entity) ||
    (m.rowKey !== undefined && (typeof m.rowKey !== "string" || m.rowKey.length > 128)) ||
    typeof values !== "object" ||
    values === null ||
    Array.isArray(values)
  )
    return null;
  const entries = Object.entries(values as Record<string, unknown>);
  if (entries.length > 32 || JSON.stringify(values).length > 4096) return null;
  for (const [k, v] of entries) if (!NAME.test(k) || !["string", "number", "boolean"].includes(typeof v)) return null;
  return {
    type: "dispatch",
    requestId: m.requestId,
    key: m.key,
    epoch: m.epoch,
    seq: m.seq,
    action: m.action,
    entity: m.entity,
    ...(m.rowKey === undefined ? {} : { rowKey: m.rowKey as string }),
    values: values as Dispatch["values"],
  };
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
    case "connect": {
      const url = normalizeService(m.url);
      return url === null ? null : { type: "connect", url };
    }
    case "pair_begin": {
      const url = normalizeService(m.url);
      return url === null ? null : { type: "pair_begin", url };
    }
    case "pair_finish":
      return typeof m.code === "string" && /^[0-9A-Za-z -]{10,16}$/.test(m.code)
        ? { type: "pair_finish", code: m.code }
        : null;
    case "unpair":
      return { type: "unpair" };
    case "ping":
      return { type: "ping" };
    case "disconnect":
      return { type: "disconnect" };
    case "capture":
    case "observe":
    case "transform":
    case "restore":
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
