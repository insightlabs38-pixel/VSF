import assert from "node:assert/strict";
import { test } from "node:test";
import { memoryKV } from "../src/kv.ts";
import { BridgeLink, LinkFailure, normalizeCode, PAIR_ITERATIONS, parseEndpoint } from "../src/link.ts";
import { memoryKeyStore, Vault } from "../src/vault.ts";

const ORIGIN = "chrome-extension://abcdefghijklmnopabcdefghijklmnop";

function link(fetchImpl: typeof fetch, now = () => 1_000_000) {
  const kv = memoryKV();
  const vault = new Vault(memoryKeyStore(), kv);
  return { link: new BridgeLink({ vault, origin: ORIGIN, fetch: fetchImpl, now }), vault, kv };
}

const json = (status: number, body: unknown): Response =>
  new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });

test("only loopback endpoints with unprivileged ports are accepted", () => {
  assert.equal(parseEndpoint("127.0.0.1:43211"), "127.0.0.1:43211");
  for (const bad of [
    "localhost:4000",
    "127.0.0.1:80",
    "127.0.0.1:70000",
    "127.0.0.1",
    "0.0.0.0:4000",
    "10.0.0.1:4000",
    "127.0.0.1:4000/x",
    "http://127.0.0.1:4000",
    4000,
    null,
  ])
    assert.equal(parseEndpoint(bad), null, String(bad));
});

test("codes normalize the way the companion does", () => {
  assert.equal(normalizeCode("abcd-0o1i-lxyz-2345"), "ABCD00111XYZ2345");
});

test("malformed endpoints and codes never reach the network", async () => {
  let calls = 0;
  const { link: l } = link((async () => {
    calls++;
    return json(200, {});
  }) as typeof fetch);
  await assert.rejects(() => l.pair("example.com:4000", "AAAA-AAAA-AAAA-AAAA", "x"), { code: "bad_endpoint" });
  await assert.rejects(() => l.pair("127.0.0.1:4000", "short", "x"), { code: "bad_code" });
  await assert.rejects(() => l.pair("127.0.0.1:4000", "UUUU-UUUU-UUUU-UUUU", "x"), { code: "bad_code" });
  assert.equal(calls, 0);
});

test("an unreachable companion is reported as such", async () => {
  const { link: l } = link((async () => {
    throw new TypeError("fetch failed");
  }) as typeof fetch);
  await assert.rejects(() => l.pair("127.0.0.1:4000", "0123-4567-89AB-CDEF", "x"), { code: "bridge_unreachable" });
});

test("a companion that does not prove knowledge of the code is not trusted and stores nothing", async () => {
  const { link: l, kv } = link((async (url: string) => {
    if (String(url).endsWith("/begin"))
      return json(200, {
        offer_id: "o",
        salt: "AAAAAAAAAAAAAAAAAAAAAA",
        nonce_s: "AAAAAAAAAAAAAAAAAAAAAA",
        iterations: PAIR_ITERATIONS,
        expires_at: 1,
      });
    return json(200, {
      client_id: "cli_x",
      refresh: "r",
      refresh_expires: 9,
      access: "a",
      access_expires: 9,
      proof: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
    });
  }) as typeof fetch);
  await assert.rejects(() => l.pair("127.0.0.1:4000", "0123-4567-89AB-CDEF", "x"), { code: "server_not_authentic" });
  assert.equal(await kv.get("vault:bridge"), undefined);
});

test("a companion asking for fewer key-stretching rounds is refused", async () => {
  const { link: l } = link((async () =>
    json(200, {
      offer_id: "o",
      salt: "AAAAAAAAAAAAAAAAAAAAAA",
      nonce_s: "AAAAAAAAAAAAAAAAAAAAAA",
      iterations: 1,
      expires_at: 1,
    })) as typeof fetch);
  await assert.rejects(() => l.pair("127.0.0.1:4000", "0123-4567-89AB-CDEF", "x"), { code: "bridge_protocol" });
});

test("denials and rate limits map to stable codes", async () => {
  for (const [status, code] of [
    [403, "pair_denied"],
    [429, "rate_limited"],
  ] as const) {
    const { link: l } = link((async () => json(status, { error: "x" })) as typeof fetch);
    await assert.rejects(() => l.pair("127.0.0.1:4000", "0123-4567-89AB-CDEF", "x"), { code });
  }
});

async function seeded(
  clock: { t: number },
  handler: (url: string, body: Record<string, string>, auth?: string) => Response,
) {
  const calls: { url: string; auth?: string }[] = [];
  const f = (async (url: string, init: RequestInit) => {
    const auth = (init.headers as Record<string, string>).Authorization;
    calls.push(auth === undefined ? { url } : { url, auth });
    return handler(url, JSON.parse(String(init.body)), auth);
  }) as typeof fetch;
  const made = link(f, () => clock.t * 1000);
  await made.vault.seal("bridge", {
    endpoint: "127.0.0.1:4000",
    clientId: "cli_1",
    refresh: "R1",
    refreshExpires: 10_000,
    access: "A1",
    accessExpires: 100,
  });
  return { ...made, calls };
}

test("a valid access token is used as is; an expiring one is renewed once for concurrent callers", async () => {
  const clock = { t: 50 };
  const { link: l, calls } = await seeded(clock, (url) =>
    url.endsWith("/refresh")
      ? json(200, { client_id: "cli_1", refresh: "R2", refresh_expires: 10_000, access: "A2", access_expires: 5_000 })
      : json(200, { ok: true }),
  );
  assert.deepEqual(await l.accessToken(), { access: "A1", endpoint: "127.0.0.1:4000" });
  assert.equal(calls.length, 0);
  clock.t = 90;
  const [a, b, c] = await Promise.all([l.accessToken(), l.accessToken(), l.accessToken()]);
  assert.equal(calls.filter((x) => x.url.endsWith("/refresh")).length, 1);
  assert.deepEqual([a?.access, b?.access, c?.access], ["A2", "A2", "A2"]);
});

test("a refused renewal removes the credentials so the user must pair again", async () => {
  const clock = { t: 200 };
  const { link: l, vault } = await seeded(clock, () => json(401, { error: "pair.refresh_reused" }));
  await assert.rejects(() => l.accessToken(), { code: "refresh_denied" });
  assert.equal(await vault.open("bridge"), undefined);
  assert.deepEqual(await l.status(), { paired: false });
  assert.equal(await l.accessToken(), null);
});

test("an expired refresh secret is dropped without contacting the companion", async () => {
  const clock = { t: 20_000 };
  const { link: l, calls, vault } = await seeded(clock, () => json(200, {}));
  await assert.rejects(() => l.accessToken(), { code: "refresh_denied" });
  assert.equal(calls.length, 0);
  assert.equal(await vault.open("bridge"), undefined);
});

test("unpairing tells the companion and forgets locally even when it cannot", async () => {
  const clock = { t: 50 };
  const first = await seeded(clock, (url, _b, auth) =>
    url.endsWith("/revoke") && auth === "Bearer A1" ? json(200, { ok: true }) : json(500, {}),
  );
  assert.deepEqual(await first.link.unpair(), { companionNotified: true });
  assert.equal(await first.vault.open("bridge"), undefined);

  const second = await seeded(clock, () => {
    throw new TypeError("down");
  });
  assert.deepEqual(await second.link.unpair(), { companionNotified: false });
  assert.equal(await second.vault.open("bridge"), undefined);
});

test("calls need a pairing and carry the bearer credential and the CSRF header", async () => {
  const { link: none } = link((async () => json(200, {})) as typeof fetch);
  await assert.rejects(
    () => none.call("/v1/ping", {}),
    (e: unknown) => e instanceof LinkFailure && e.code === "not_paired",
  );
  const clock = { t: 50 };
  const { link: l, calls } = await seeded(clock, () => json(200, { ok: true }));
  assert.deepEqual((await l.call("/v1/x", { a: 1 })).json, { ok: true });
  assert.deepEqual(calls[0], { url: "http://127.0.0.1:4000/v1/x", auth: "Bearer A1" });
});
