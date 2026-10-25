export type TransportKind = "websocket" | "sse" | "webrtc" | "http_poll";

export const TRANSPORT_KINDS: readonly TransportKind[] = ["websocket", "sse", "webrtc", "http_poll"];

export type Need = "secure_context" | "network_peer" | "user_permission" | "same_origin_credentials";

export interface Backpressure {
  /** credit: the sender waits for credit; drop_oldest: old data is lost and counted; block: writers wait; none: unbounded (refused by validation). */
  mode: "credit" | "drop_oldest" | "block" | "none";
  maxBufferedBytes: number | null;
}

export interface ReconnectPolicy {
  supported: boolean;
  /** offset: the source can resume after a sequence number; none: anything missed is lost and must be reported. */
  resume: "offset" | "none";
  maxAttempts: number;
  backoffMs: [number, number];
}

export interface TransportDeclaration {
  kind: TransportKind;
  /** What the source application supports, from evidence; not what this browser supports. */
  available: boolean;
  reason?: string;
  needs: Need[];
  backpressure: Backpressure;
  reconnect: ReconnectPolicy;
  ordered: boolean;
  bidirectional: boolean;
}

export interface TransportManifest {
  schema: "vsf-transport-capabilities-v1";
  source: string;
  transports: TransportDeclaration[];
  /** Preference order; the first usable entry wins. */
  prefer: TransportKind[];
  /** When nothing is usable the source's own interface is kept; this is never silently a different transport. */
  whenNoneUsable: "retain_source_ui";
}

export interface Problem {
  path: string;
  message: string;
}

const MAX_BUFFER = 64 << 20;

/** Shape and safety checks only; whether a source really offers a transport is decided by evidence elsewhere. */
export function validateManifest(x: unknown): Problem[] {
  const out: Problem[] = [];
  const m = x as Partial<TransportManifest> | null;
  if (!m || typeof m !== "object" || m.schema !== "vsf-transport-capabilities-v1")
    return [{ path: "", message: "schema" }];
  if (typeof m.source !== "string" || m.source === "") out.push({ path: "source", message: "missing" });
  if (m.whenNoneUsable !== "retain_source_ui")
    out.push({ path: "whenNoneUsable", message: "must keep the source interface" });
  if (!Array.isArray(m.transports) || m.transports.length === 0)
    return [...out, { path: "transports", message: "missing" }];
  const seen = new Set<string>();
  m.transports.forEach((t, i) => {
    const at = `transports/${i}`;
    if (!TRANSPORT_KINDS.includes(t.kind)) out.push({ path: `${at}/kind`, message: "unknown kind" });
    if (seen.has(t.kind)) out.push({ path: `${at}/kind`, message: "declared twice" });
    seen.add(t.kind);
    if (typeof t.available !== "boolean") out.push({ path: `${at}/available`, message: "boolean required" });
    if (t.available === false && !t.reason)
      out.push({ path: `${at}/reason`, message: "an unavailable transport says why" });
    const bp = t.backpressure;
    if (!bp || !["credit", "drop_oldest", "block", "none"].includes(bp.mode))
      out.push({ path: `${at}/backpressure`, message: "mode required" });
    else if (bp.mode === "none") out.push({ path: `${at}/backpressure`, message: "unbounded buffering is refused" });
    else if (
      bp.maxBufferedBytes === null ||
      !Number.isInteger(bp.maxBufferedBytes) ||
      bp.maxBufferedBytes <= 0 ||
      bp.maxBufferedBytes > MAX_BUFFER
    )
      out.push({ path: `${at}/backpressure/maxBufferedBytes`, message: `1..${MAX_BUFFER} required` });
    const rc = t.reconnect;
    if (!rc || typeof rc.supported !== "boolean" || !["offset", "none"].includes(rc.resume))
      out.push({ path: `${at}/reconnect`, message: "policy required" });
    else {
      if (rc.supported && (!Number.isInteger(rc.maxAttempts) || rc.maxAttempts < 1 || rc.maxAttempts > 20))
        out.push({ path: `${at}/reconnect/maxAttempts`, message: "1..20" });
      if (rc.supported && !(rc.backoffMs?.[0] > 0 && rc.backoffMs[1] >= rc.backoffMs[0]))
        out.push({ path: `${at}/reconnect/backoffMs`, message: "0 < min <= max" });
      if (!rc.supported && rc.resume === "offset")
        out.push({ path: `${at}/reconnect`, message: "resume needs reconnect" });
    }
    if (t.kind === "sse" && t.bidirectional)
      out.push({ path: `${at}/bidirectional`, message: "server-sent events are one-way" });
    if (!Array.isArray(t.needs)) out.push({ path: `${at}/needs`, message: "list required" });
  });
  for (const k of m.prefer ?? []) if (!seen.has(k)) out.push({ path: "prefer", message: `${k} is not declared` });
  if (!Array.isArray(m.prefer) || m.prefer.length === 0) out.push({ path: "prefer", message: "missing" });
  return out;
}

export interface Environment {
  /** Which transports this runtime has at all. */
  has: Record<TransportKind, boolean>;
  secureContext: boolean;
  /** Policy of the page or the deployment, e.g. a content-security-policy that refuses sockets. */
  allowed: Record<TransportKind, boolean>;
  permissions: readonly string[];
  /** A peer reachable for peer-to-peer transports. */
  networkPeer: boolean;
}

export type Attempt =
  | "chosen"
  | "declared_unavailable"
  | "missing_in_environment"
  | "blocked_by_policy"
  | "needs_secure_context"
  | "needs_permission"
  | "needs_network_peer"
  | "not_preferred";

export interface Negotiation {
  chosen: TransportKind | null;
  tried: { kind: TransportKind; outcome: Attempt }[];
  /** native: the first preference worked; fallback: a later one did; source_ui: nothing did and the source interface stays. */
  mode: "native" | "fallback" | "source_ui";
}

export function usable(d: TransportDeclaration, env: Environment): Attempt {
  if (!d.available) return "declared_unavailable";
  if (!env.has[d.kind]) return "missing_in_environment";
  if (!env.allowed[d.kind]) return "blocked_by_policy";
  if (d.needs.includes("secure_context") && !env.secureContext) return "needs_secure_context";
  if (d.needs.includes("user_permission") && !env.permissions.includes(d.kind)) return "needs_permission";
  if (d.needs.includes("network_peer") && !env.networkPeer) return "needs_network_peer";
  return "chosen";
}

/** Picks the first preferred transport the source offers and this environment can run, recording why the others were not used. */
export function negotiate(m: TransportManifest, env: Environment): Negotiation {
  const problems = validateManifest(m);
  if (problems.length > 0) return { chosen: null, tried: [], mode: "source_ui" };
  const tried: Negotiation["tried"] = [];
  let chosen: TransportKind | null = null;
  for (const kind of m.prefer) {
    const d = m.transports.find((t) => t.kind === kind) as TransportDeclaration;
    const outcome = chosen === null ? usable(d, env) : "not_preferred";
    if (outcome === "chosen") chosen = kind;
    tried.push({ kind, outcome });
  }
  return { chosen, tried, mode: chosen === null ? "source_ui" : chosen === m.prefer[0] ? "native" : "fallback" };
}

/** What a browser-like global offers; pass `globalThis` or a test double. */
export function detectEnvironment(
  g: {
    WebSocket?: unknown;
    EventSource?: unknown;
    RTCPeerConnection?: unknown;
    isSecureContext?: boolean;
    fetch?: unknown;
  },
  policy: Partial<Record<TransportKind, boolean>> = {},
  permissions: readonly string[] = [],
  networkPeer = true,
): Environment {
  return {
    has: {
      websocket: typeof g.WebSocket === "function",
      sse: typeof g.EventSource === "function",
      webrtc: typeof g.RTCPeerConnection === "function",
      http_poll: typeof g.fetch === "function",
    },
    secureContext: g.isSecureContext === true,
    allowed: { websocket: true, sse: true, webrtc: true, http_poll: true, ...policy },
    permissions,
    networkPeer,
  };
}

/** A bounded buffer between a producer and a consumer that states what it did with every item. */
export class Pipe<T extends { bytes: number }> {
  private items: T[] = [];
  private held = 0;
  dropped = 0;
  private readonly bp: Backpressure;
  private readonly credit: number;

  constructor(bp: Backpressure, credit = 0) {
    this.bp = bp;
    this.credit = credit;
    if (bp.mode === "none" || bp.maxBufferedBytes === null) throw new Error("unbounded buffering is refused");
  }

  get buffered(): number {
    return this.held;
  }

  /** accepted: kept; dropped_oldest: kept after discarding older items (counted); refused: the caller must wait. */
  push(item: T): "accepted" | "dropped_oldest" | "refused" {
    const max = this.bp.maxBufferedBytes as number;
    if (item.bytes > max) return "refused";
    if (this.held + item.bytes <= max) {
      this.items.push(item);
      this.held += item.bytes;
      return "accepted";
    }
    if (this.bp.mode === "drop_oldest") {
      while (this.held + item.bytes > max && this.items.length > 0) {
        this.held -= (this.items.shift() as T).bytes;
        this.dropped++;
      }
      this.items.push(item);
      this.held += item.bytes;
      return "dropped_oldest";
    }
    return "refused";
  }

  take(): T | undefined {
    const item = this.items.shift();
    if (item) this.held -= item.bytes;
    return item;
  }

  get window(): number {
    return this.bp.mode === "credit"
      ? Math.max(0, (this.bp.maxBufferedBytes as number) - this.held) + this.credit
      : Infinity;
  }
}

export type Sent = "queued" | "sent" | "acked" | "unknown" | "discarded";

export interface Outbound {
  seq: number;
  data: string;
  /** True only when the source is known to treat a second delivery as a no-op. */
  repeatable: boolean;
  state: Sent;
}

/** Outgoing messages with at-most-once semantics unless a message is declared repeatable. */
export class OutboundQueue {
  private all = new Map<number, Outbound>();
  private next = 1;

  enqueue(data: string, repeatable = false): Outbound {
    const m: Outbound = { seq: this.next++, data, repeatable, state: "queued" };
    this.all.set(m.seq, m);
    return m;
  }

  markSent(seq: number): void {
    const m = this.all.get(seq);
    if (m?.state === "queued") m.state = "sent";
  }

  ack(seq: number): void {
    const m = this.all.get(seq);
    if (m && (m.state === "sent" || m.state === "unknown")) m.state = "acked";
  }

  /** The connection dropped: sent-but-unacknowledged messages are unknown, queued ones were never sent. */
  disconnected(): void {
    for (const m of this.all.values()) {
      if (m.state === "sent") m.state = "unknown";
      else if (m.state === "queued") m.state = "discarded";
    }
  }

  /** What may be sent again after reconnecting: only messages declared repeatable, never an unknown write. */
  resendable(): Outbound[] {
    return [...this.all.values()].filter((m) => m.state === "unknown" && m.repeatable);
  }

  unknown(): Outbound[] {
    return [...this.all.values()].filter((m) => m.state === "unknown" && !m.repeatable);
  }

  stateOf(seq: number): Sent | undefined {
    return this.all.get(seq)?.state;
  }
}

export interface Resume {
  attempt: number;
  delayMs: number;
  /** offset: ask the source for everything after this sequence; none: report that output was lost. */
  mode: "offset" | "none";
  after: number;
}

/** Plans reconnect attempts from the declaration; backoff grows, is bounded, and the attempts are finite. */
export function planReconnect(
  policy: ReconnectPolicy,
  lastSeq: number,
  attempt: number,
  jitter: () => number = Math.random,
): Resume | null {
  if (!policy.supported || attempt >= policy.maxAttempts) return null;
  const [min, max] = policy.backoffMs;
  const base = Math.min(max, min * 2 ** attempt);
  return {
    attempt: attempt + 1,
    delayMs: Math.round(base * (0.5 + jitter() * 0.5)),
    mode: policy.resume,
    after: lastSeq,
  };
}
