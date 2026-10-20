import { Lock } from "./kv.ts";
import type { Vault } from "./vault.ts";

export const PAIR_ITERATIONS = 100_000;
const CRED = "bridge";
const SKEW_S = 30;
const REQUEST_TIMEOUT_MS = 8000;

export interface Stored {
  endpoint: string;
  clientId: string;
  refresh: string;
  refreshExpires: number;
  access: string;
  accessExpires: number;
}

export type LinkError =
  | "bad_endpoint"
  | "bad_code"
  | "not_paired"
  | "pair_denied"
  | "bridge_unreachable"
  | "bridge_protocol"
  | "server_not_authentic"
  | "rate_limited"
  | "refresh_denied";

export class LinkFailure extends Error {
  readonly code: LinkError;
  constructor(code: LinkError) {
    super(code);
    this.name = "LinkFailure";
    this.code = code;
  }
}

export interface LinkDeps {
  vault: Vault;
  /** This extension's own origin, which the companion checks and the proof is bound to. */
  origin: string;
  fetch: typeof fetch;
  now(): number;
}

const enc = new TextEncoder();
const b64u = (bytes: Uint8Array): string =>
  btoa(String.fromCharCode(...bytes))
    .replaceAll("+", "-")
    .replaceAll("/", "_")
    .replaceAll("=", "");
const unb64u = (s: string): Uint8Array<ArrayBuffer> =>
  Uint8Array.from(atob(s.replaceAll("-", "+").replaceAll("_", "/")), (c) => c.charCodeAt(0));

/** Only a loopback address and an unprivileged port: the bridge is never a remote service. */
export function parseEndpoint(raw: unknown): string | null {
  if (typeof raw !== "string") return null;
  const m = /^127\.0\.0\.1:(\d{4,5})$/.exec(raw);
  const port = Number(m?.[1]);
  return m && port >= 1024 && port <= 65535 ? raw : null;
}

export function normalizeCode(code: string): string {
  return [...code.toUpperCase()]
    .filter((c) => c !== "-" && c !== " ")
    .map((c) => (c === "O" ? "0" : c === "I" || c === "L" ? "1" : c))
    .join("");
}

const CODE = /^[0-9A-HJKMNP-TV-Z]{16}$/;

async function codeKey(code: string, salt: Uint8Array<ArrayBuffer>): Promise<CryptoKey> {
  const base = await crypto.subtle.importKey("raw", enc.encode(code), "PBKDF2", false, ["deriveBits"]);
  const bits = await crypto.subtle.deriveBits(
    { name: "PBKDF2", hash: "SHA-256", salt, iterations: PAIR_ITERATIONS },
    base,
    256,
  );
  return crypto.subtle.importKey("raw", bits, { name: "HMAC", hash: "SHA-256" }, false, ["sign", "verify"]);
}

function framed(label: string, parts: Uint8Array[]): Uint8Array<ArrayBuffer> {
  const chunks: Uint8Array[] = [enc.encode(label)];
  for (const p of parts) chunks.push(new Uint8Array([0]), p);
  const out = new Uint8Array(chunks.reduce((n, c) => n + c.length, 0));
  let at = 0;
  for (const c of chunks) {
    out.set(c, at);
    at += c.length;
  }
  return out;
}

/** The extension's side of pairing, refresh and authorized calls; secrets exist only sealed or in flight. */
export class BridgeLink {
  private readonly lock = new Lock();
  private readonly d: LinkDeps;

  constructor(deps: LinkDeps) {
    this.d = deps;
  }

  private async post(
    endpoint: string,
    path: string,
    body: unknown,
    access?: string,
  ): Promise<{ status: number; json: Record<string, unknown> }> {
    let res: Response;
    try {
      res = await this.d.fetch(`http://${endpoint}${path}`, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          "X-VSF-Bridge": "1",
          ...(access ? { Authorization: `Bearer ${access}` } : {}),
        },
        body: JSON.stringify(body),
        credentials: "omit",
        redirect: "error",
        signal: AbortSignal.timeout(REQUEST_TIMEOUT_MS),
      });
    } catch {
      throw new LinkFailure("bridge_unreachable");
    }
    let json: Record<string, unknown> = {};
    try {
      json = (await res.json()) as Record<string, unknown>;
    } catch {
      if (res.status === 200) throw new LinkFailure("bridge_protocol");
    }
    return { status: res.status, json };
  }

  async pair(endpointRaw: string, codeRaw: string, name: string): Promise<{ clientId: string }> {
    const endpoint = parseEndpoint(endpointRaw);
    if (endpoint === null) throw new LinkFailure("bad_endpoint");
    const code = normalizeCode(codeRaw);
    if (!CODE.test(code)) throw new LinkFailure("bad_code");
    const nonceC = crypto.getRandomValues(new Uint8Array(24));
    const begin = await this.post(endpoint, "/v1/pair/begin", { nonce: b64u(nonceC) });
    if (begin.status === 429) throw new LinkFailure("rate_limited");
    if (begin.status !== 200) throw new LinkFailure("pair_denied");
    const { offer_id: offerId, salt, nonce_s: ns, iterations } = begin.json as Record<string, string | number>;
    if (
      iterations !== PAIR_ITERATIONS ||
      typeof offerId !== "string" ||
      typeof salt !== "string" ||
      typeof ns !== "string"
    )
      throw new LinkFailure("bridge_protocol");
    const nonceS = unb64u(ns);
    const key = await codeKey(code, unb64u(salt));
    const origin = this.d.origin;
    const proof = new Uint8Array(
      await crypto.subtle.sign("HMAC", key, framed("vsf-pair-c", [nonceC, nonceS, enc.encode(origin)])),
    );
    const fin = await this.post(endpoint, "/v1/pair/finish", { offer_id: offerId, proof: b64u(proof), name });
    if (fin.status === 429) throw new LinkFailure("rate_limited");
    if (fin.status !== 200) throw new LinkFailure("pair_denied");
    const c = fin.json as Record<string, string | number>;
    if (
      typeof c.client_id !== "string" ||
      typeof c.refresh !== "string" ||
      typeof c.access !== "string" ||
      typeof c.proof !== "string"
    )
      throw new LinkFailure("bridge_protocol");
    const authentic = await crypto.subtle.verify(
      "HMAC",
      key,
      unb64u(c.proof),
      framed("vsf-pair-s", [nonceC, nonceS, enc.encode(c.client_id)]),
    );
    if (!authentic) throw new LinkFailure("server_not_authentic");
    await this.d.vault.seal(CRED, {
      endpoint,
      clientId: c.client_id,
      refresh: c.refresh,
      refreshExpires: Number(c.refresh_expires),
      access: c.access,
      accessExpires: Number(c.access_expires),
    } satisfies Stored);
    return { clientId: c.client_id };
  }

  private load(): Promise<Stored | undefined> {
    return this.d.vault.open<Stored>(CRED);
  }

  /** A usable access token, renewed first when it is about to lapse; concurrent callers share one renewal. */
  accessToken(): Promise<{ access: string; endpoint: string } | null> {
    return this.lock.run(async () => {
      const s = await this.load();
      if (!s) return null;
      const now = this.d.now() / 1000;
      if (s.accessExpires - SKEW_S > now) return { access: s.access, endpoint: s.endpoint };
      if (s.refreshExpires <= now) {
        await this.d.vault.remove(CRED);
        throw new LinkFailure("refresh_denied");
      }
      const r = await this.post(s.endpoint, "/v1/token/refresh", { client_id: s.clientId, refresh: s.refresh });
      if (r.status !== 200) {
        if (r.status === 401 || r.status === 403) await this.d.vault.remove(CRED);
        throw new LinkFailure(r.status === 429 ? "rate_limited" : "refresh_denied");
      }
      const c = r.json as Record<string, string | number>;
      const next: Stored = {
        endpoint: s.endpoint,
        clientId: s.clientId,
        refresh: String(c.refresh),
        refreshExpires: Number(c.refresh_expires),
        access: String(c.access),
        accessExpires: Number(c.access_expires),
      };
      await this.d.vault.seal(CRED, next);
      return { access: next.access, endpoint: next.endpoint };
    });
  }

  /** Ends the pairing on the companion, then forgets the credentials here either way. */
  async unpair(): Promise<{ companionNotified: boolean }> {
    const s = await this.load().catch(() => undefined);
    let notified = false;
    if (s) {
      try {
        const live = await this.accessToken();
        if (live) notified = (await this.post(live.endpoint, "/v1/pair/revoke", {}, live.access)).status === 200;
      } catch {
        notified = false;
      }
    }
    await this.d.vault.remove(CRED);
    return { companionNotified: notified };
  }

  async status(): Promise<{ paired: boolean; endpoint?: string; clientId?: string; accessValid?: boolean }> {
    let s: Stored | undefined;
    try {
      s = await this.load();
    } catch {
      return { paired: false };
    }
    if (!s) return { paired: false };
    return {
      paired: true,
      endpoint: s.endpoint,
      clientId: s.clientId,
      accessValid: s.accessExpires > this.d.now() / 1000,
    };
  }

  /** An authenticated POST to the companion; one renewal is attempted if the token was refused. */
  async call(path: string, body: unknown): Promise<{ status: number; json: Record<string, unknown> }> {
    const live = await this.accessToken();
    if (!live) throw new LinkFailure("not_paired");
    return this.post(live.endpoint, path, body, live.access);
  }
}
