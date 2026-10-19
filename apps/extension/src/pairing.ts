export interface Credentials {
  url: string;
  pairingId: string;
  access: string;
  accessExpires: number;
  refresh: string;
  refreshExpires: number;
}

export interface PendingPair {
  url: string;
  pairId: string;
  nonceC: string;
  nonceS: string;
  expires: number;
}

export interface State {
  pending?: PendingPair;
  creds?: Credentials;
}

export interface Store {
  get(): Promise<State>;
  set(state: State): Promise<void>;
}

export type Result<T = object> = ({ ok: true } & T) | { ok: false; error: string };

const HEADERS = { "Content-Type": "application/json", "X-VSF-Bridge": "1" };

/** Letters people misread are not in the alphabet; case, dashes and spaces are typing noise. */
export function normalizeCode(code: string): string {
  return code.toUpperCase().replace(/[-\s]/g, "");
}

/** HMAC-SHA256 keyed by the code over the exchange; the Go service computes the identical value. */
export async function proofHex(
  code: string,
  pairId: string,
  peer: string,
  nonceC: string,
  nonceS: string,
): Promise<string> {
  const enc = new TextEncoder();
  const key = await crypto.subtle.importKey(
    "raw",
    enc.encode(normalizeCode(code)),
    { name: "HMAC", hash: "SHA-256" },
    false,
    ["sign"],
  );
  const message = ["vsf-pair-v1", pairId, peer, nonceC, nonceS].map((p) => `${p}\0`).join("");
  const mac = await crypto.subtle.sign("HMAC", key, enc.encode(message));
  return [...new Uint8Array(mac)].map((b) => b.toString(16).padStart(2, "0")).join("");
}

export interface Options {
  fetch: typeof fetch;
  store: Store;
  /** The origin the browser sends for this extension, which the service matches against its allow-list. */
  peer: string;
  now(): number;
}

interface Wire {
  pair_id?: string;
  nonce_s?: string;
  expires_at?: string;
  pairing_id?: string;
  access?: string;
  access_expires_at?: string;
  refresh?: string;
  refresh_expires_at?: string;
  error?: string;
}

const hex = (n: number): string =>
  [...crypto.getRandomValues(new Uint8Array(n))].map((b) => b.toString(16).padStart(2, "0")).join("");

export class PairingClient {
  private readonly o: Options;

  constructor(options: Options) {
    this.o = options;
  }

  private async call(
    url: string,
    path: string,
    body: object,
    bearer?: string,
  ): Promise<{ status: number; json: Wire }> {
    const res = await this.o.fetch(`${url}${path}`, {
      method: "POST",
      headers: bearer === undefined ? HEADERS : { ...HEADERS, Authorization: `Bearer ${bearer}` },
      body: JSON.stringify(body),
      credentials: "omit",
      cache: "no-store",
    });
    return { status: res.status, json: (await res.json().catch(() => ({}))) as Wire };
  }

  /** Step one: ask the companion to pair. Nothing is granted yet; the operator has to approve it there. */
  async begin(url: string): Promise<Result<{ pairId: string; expires: number }>> {
    try {
      const nonceC = hex(16);
      const { status, json } = await this.call(url, "/v1/pair/begin", { nonce_c: nonceC });
      if (status !== 200 || !json.pair_id || !json.nonce_s || !json.expires_at)
        return { ok: false, error: json.error ?? `http_${status}` };
      const expires = Date.parse(json.expires_at);
      await this.o.store.set({
        ...(await this.o.store.get()),
        pending: { url, pairId: json.pair_id, nonceC, nonceS: json.nonce_s, expires },
      });
      return { ok: true, pairId: json.pair_id, expires };
    } catch {
      return { ok: false, error: "unreachable" };
    }
  }

  /** Step two: prove the code the operator read out. The code itself never leaves the extension. */
  async finish(code: string): Promise<Result<{ pairingId: string }>> {
    const state = await this.o.store.get();
    const p = state.pending;
    if (p === undefined || this.o.now() >= p.expires) return { ok: false, error: "no_pending_pairing" };
    try {
      const proof = await proofHex(code, p.pairId, this.o.peer, p.nonceC, p.nonceS);
      const { status, json } = await this.call(p.url, "/v1/pair/finish", { pair_id: p.pairId, proof });
      if (status !== 200 || !json.access || !json.refresh) {
        if (status === 404 || status === 403 || status === 429) {
          const { pending: _burned, ...rest } = state;
          await this.o.store.set(rest);
        }
        return { ok: false, error: json.error ?? `http_${status}` };
      }
      const creds = this.fromWire(p.url, json);
      await this.o.store.set({ creds });
      return { ok: true, pairingId: creds.pairingId };
    } catch {
      return { ok: false, error: "unreachable" };
    }
  }

  private fromWire(url: string, j: Wire): Credentials {
    return {
      url,
      pairingId: j.pairing_id as string,
      access: j.access as string,
      accessExpires: Date.parse(j.access_expires_at as string),
      refresh: j.refresh as string,
      refreshExpires: Date.parse(j.refresh_expires_at as string),
    };
  }

  async credentials(): Promise<Credentials | undefined> {
    return (await this.o.store.get()).creds;
  }

  /** Rotates the refresh token; a refused renewal ends the pairing locally so no stale credential lingers. */
  async renew(): Promise<boolean> {
    const state = await this.o.store.get();
    const c = state.creds;
    if (c === undefined || this.o.now() >= c.refreshExpires) {
      await this.o.store.set({});
      return false;
    }
    try {
      const { status, json } = await this.call(c.url, "/v1/pair/renew", { refresh: c.refresh });
      if (status === 200 && json.access && json.refresh) {
        await this.o.store.set({ creds: this.fromWire(c.url, json) });
        return true;
      }
      if (status === 401 || status === 403) await this.o.store.set({});
    } catch {
      // The companion was unreachable; keep what we have and let the caller report it.
    }
    return false;
  }

  /** Ends the pairing on the companion when it can be reached, and always forgets it here. */
  async unpair(): Promise<void> {
    const c = (await this.o.store.get()).creds;
    await this.o.store.set({});
    if (c !== undefined) await this.call(c.url, "/v1/pair/revoke", {}, c.access).catch(() => undefined);
  }
}
