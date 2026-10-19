import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import { normalizeCode, PairingClient, proofHex, type State } from "../src/pairing.ts";

const PEER = "chrome-extension://lgnekpogndcjggibkbaekekanfonjhie";
const URL_ = "http://127.0.0.1:9";

const vector = JSON.parse(
  readFileSync(new URL("../../../fixtures/pairing/proof-vector.json", import.meta.url), "utf8"),
) as {
  cases: {
    name: string;
    code: string;
    pair_id: string;
    peer: string;
    nonce_c: string;
    nonce_s: string;
    proof: string;
  }[];
};

test("the proof matches the vectors the Go service is tested against", async () => {
  for (const c of vector.cases)
    assert.equal(await proofHex(c.code, c.pair_id, c.peer, c.nonce_c, c.nonce_s), c.proof, c.name);
  assert.equal(normalizeCode("7k2m9-q4 xvd"), "7K2M9Q4XVD");
});

interface Call {
  path: string;
  body: Record<string, unknown>;
  auth: string | undefined;
}

function rig(replies: ((c: Call) => { status: number; json: unknown } | "throw")[]) {
  const calls: Call[] = [];
  let state: State = {};
  let t = 1_000_000;
  const f = (async (url: string, init: RequestInit) => {
    const call: Call = {
      path: new globalThis.URL(url).pathname,
      body: JSON.parse(String(init.body)),
      auth: (init.headers as Record<string, string>).Authorization,
    };
    calls.push(call);
    const r = (replies.shift() ?? (() => ({ status: 500, json: {} })))(call);
    if (r === "throw") throw new TypeError("fetch failed");
    return new Response(JSON.stringify(r.json), { status: r.status });
  }) as unknown as typeof fetch;
  const client = new PairingClient({
    fetch: f,
    peer: PEER,
    now: () => t,
    store: {
      get: async () => structuredClone(state),
      set: async (s) => {
        state = structuredClone(s);
      },
    },
  });
  return { client, calls, state: () => state, advance: (ms: number) => (t += ms) };
}

const credsJson = (n: number) => ({
  pairing_id: "pair-1",
  access: `vsf_a_${String(n).repeat(64)}`,
  access_expires_at: new Date(1_000_000 + 900_000).toISOString(),
  refresh: `vsf_r_${String(n).repeat(64)}`,
  refresh_expires_at: new Date(1_000_000 + 86_400_000).toISOString(),
});
const begun = {
  status: 200,
  json: { pair_id: "p".repeat(32), nonce_s: "ab".repeat(16), expires_at: new Date(1_000_000 + 120_000).toISOString() },
};

test("begin sends a fresh nonce and keeps only what finishing needs", async () => {
  const r = rig([() => begun]);
  const out = await r.client.begin(URL_);
  assert.deepEqual(out, { ok: true, pairId: "p".repeat(32), expires: 1_000_000 + 120_000 });
  assert.match(String(r.calls[0]?.body.nonce_c), /^[0-9a-f]{32}$/);
  assert.equal(r.calls[0]?.path, "/v1/pair/begin");
  assert.equal(r.calls[0]?.auth, undefined);
  assert.equal(r.state().pending?.nonceS, "ab".repeat(16));
  assert.equal(r.state().creds, undefined);
});

test("finish proves the code without sending it and stores credentials", async () => {
  let sent: Call | undefined;
  const r = rig([
    () => begun,
    (c) => {
      sent = c;
      return { status: 200, json: credsJson(1) };
    },
  ]);
  await r.client.begin(URL_);
  const out = await r.client.finish("7k2m9-q4xvd");
  assert.deepEqual(out, { ok: true, pairingId: "pair-1" });
  assert.equal(JSON.stringify(sent?.body).includes("7K2M9"), false);
  assert.match(String(sent?.body.proof), /^[0-9a-f]{64}$/);
  assert.equal(sent?.body.pair_id, "p".repeat(32));
  assert.equal(r.state().pending, undefined);
  assert.equal(r.state().creds?.access, `vsf_a_${"1".repeat(64)}`);
});

test("finish needs a live pending pairing and drops one the service burned", async () => {
  const none = rig([]);
  assert.deepEqual(await none.client.finish("AAAAAAAAAA"), { ok: false, error: "no_pending_pairing" });
  const late = rig([() => begun]);
  await late.client.begin(URL_);
  late.advance(121_000);
  assert.deepEqual(await late.client.finish("AAAAAAAAAA"), { ok: false, error: "no_pending_pairing" });
  const burned = rig([() => begun, () => ({ status: 403, json: { error: "bridge.pairing.attempts_exceeded" } })]);
  await burned.client.begin(URL_);
  assert.deepEqual(await burned.client.finish("AAAAAAAAAA"), { ok: false, error: "bridge.pairing.attempts_exceeded" });
  assert.equal(burned.state().pending, undefined);
});

test("an unreachable or refusing companion never leaves partial credentials", async () => {
  assert.deepEqual(await rig(["throw" as never]).client.begin(URL_), { ok: false, error: "unreachable" });
  const refused = rig([() => ({ status: 403, json: { error: "bridge.pairing.peer_not_allowed" } })]);
  assert.deepEqual(await refused.client.begin(URL_), { ok: false, error: "bridge.pairing.peer_not_allowed" });
  assert.deepEqual(refused.state(), {});
  const half = rig([() => begun, "throw" as never]);
  await half.client.begin(URL_);
  assert.deepEqual(await half.client.finish("AAAAAAAAAA"), { ok: false, error: "unreachable" });
  assert.equal(half.state().creds, undefined);
});

async function pairedRig(extra: Parameters<typeof rig>[0]) {
  const r = rig([() => begun, () => ({ status: 200, json: credsJson(1) }), ...extra]);
  await r.client.begin(URL_);
  await r.client.finish("AAAAAAAAAA");
  return r;
}

test("renewal replaces the credentials and a refusal ends the pairing locally", async () => {
  const r = await pairedRig([() => ({ status: 200, json: credsJson(2) })]);
  assert.equal(await r.client.renew(), true);
  assert.equal(r.calls.at(-1)?.body.refresh, `vsf_r_${"1".repeat(64)}`);
  assert.equal((await r.client.credentials())?.access, `vsf_a_${"2".repeat(64)}`);
  const refused = await pairedRig([() => ({ status: 401, json: { error: "bridge.auth.refresh_reused" } })]);
  assert.equal(await refused.client.renew(), false);
  assert.deepEqual(refused.state(), {});
});

test("a down companion keeps credentials and an expired refresh token is dropped without a call", async () => {
  const down = await pairedRig(["throw" as never]);
  assert.equal(await down.client.renew(), false);
  assert.ok((await down.client.credentials()) !== undefined);
  const old = await pairedRig([]);
  old.advance(86_400_001);
  const before = old.calls.length;
  assert.equal(await old.client.renew(), false);
  assert.equal(old.calls.length, before);
  assert.deepEqual(old.state(), {});
});

test("unpairing forgets locally even if the companion cannot be reached, and revokes when it can", async () => {
  const ok = await pairedRig([() => ({ status: 200, json: { ok: true } })]);
  await ok.client.unpair();
  assert.deepEqual(ok.state(), {});
  assert.equal(ok.calls.at(-1)?.path, "/v1/pair/revoke");
  assert.equal(ok.calls.at(-1)?.auth, `Bearer vsf_a_${"1".repeat(64)}`);
  const down = await pairedRig(["throw" as never]);
  await down.client.unpair();
  assert.deepEqual(down.state(), {});
});
