import { type Companion, startCompanion } from "../support/companion.ts";
import { expect, test } from "../support/harness.ts";

type Env = Parameters<Parameters<typeof test>[2]>[0]["env"];

async function pair(env: Env, c: Companion, code?: string) {
  const begin = (await env.send({ type: "pair_begin", url: c.url })) as { ok: boolean; pairId?: string };
  expect(begin.ok).toBe(true);
  const request = await c.next("pair_request");
  expect(request).toMatchObject({ peer: `chrome-extension://${env.extId}`, pair_id: begin.pairId });
  c.say(`approve ${begin.pairId}`);
  const issued = (await c.next("code")) as { code: string };
  return env.send({ type: "pair_finish", code: code ?? issued.code });
}

test("the operator approves, the extension proves the code, and the companion then answers it", async ({ env }) => {
  const c = await startCompanion(`chrome-extension://${env.extId}`);
  try {
    expect(await env.send({ type: "ping" })).toMatchObject({ ok: false, error: "unpaired" });
    expect(await pair(env, c)).toMatchObject({ ok: true });
    expect(await env.send({ type: "ping" })).toMatchObject({ ok: true });
    const status = (await env.send({ type: "status" })) as { paired: string | null; service: string };
    expect(status.paired).toMatch(/^[0-9a-f]{32}$/);
    expect(status.service).toBe(c.url);
    expect(JSON.stringify(status)).not.toMatch(/vsf_[ar]_/);
    c.say("list");
    const listed = (await c.next("pairings")) as { pairings: { peer: string; revoked: boolean }[] };
    expect(listed.pairings).toMatchObject([{ peer: `chrome-extension://${env.extId}`, revoked: false }]);
  } finally {
    await c.stop();
  }
});

test("nothing is granted until the operator approves, and a wrong or reused code gets nowhere", async ({ env }) => {
  const c = await startCompanion(`chrome-extension://${env.extId}`);
  try {
    const begin = (await env.send({ type: "pair_begin", url: c.url })) as { pairId: string };
    expect(await env.send({ type: "pair_finish", code: "AAAAA-AAAAA" })).toMatchObject({ ok: false });
    expect(await env.send({ type: "ping" })).toMatchObject({ ok: false, error: "unpaired" });
    c.say(`approve ${begin.pairId}`);
    const issued = (await c.next("code")) as { code: string };
    const wrong = issued.code.startsWith("A") ? "BBBBB-BBBBB" : "AAAAA-AAAAA";
    for (let i = 0; i < 3; i++)
      expect(await env.send({ type: "pair_finish", code: wrong })).toMatchObject({ ok: false });
    expect(await env.send({ type: "pair_finish", code: issued.code })).toMatchObject({ ok: false });
    expect(await env.send({ type: "ping" })).toMatchObject({ ok: false, error: "unpaired" });
  } finally {
    await c.stop();
  }
});

test("a denied request never becomes a pairing", async ({ env }) => {
  const c = await startCompanion(`chrome-extension://${env.extId}`);
  try {
    const begin = (await env.send({ type: "pair_begin", url: c.url })) as { pairId: string };
    await c.next("pair_request");
    c.say(`deny ${begin.pairId}`);
    await c.next("denied");
    expect(await env.send({ type: "pair_finish", code: "AAAAA-AAAAA" })).toMatchObject({ ok: false });
    expect(await env.send({ type: "ping" })).toMatchObject({ ok: false, error: "unpaired" });
  } finally {
    await c.stop();
  }
});

test("a peer that is not the extension cannot ask to pair, whatever it claims", async ({ env }) => {
  const c = await startCompanion(`chrome-extension://${env.extId}`);
  try {
    const nonce = "00112233445566778899aabbccddeeff";
    for (const origin of ["https://evil.test", "chrome-extension://bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "null"]) {
      const res = await fetch(`${c.url}/v1/pair/begin`, {
        method: "POST",
        headers: { Origin: origin, "Content-Type": "application/json", "X-VSF-Bridge": "1" },
        body: JSON.stringify({ nonce_c: nonce }),
      });
      expect(res.status, origin).toBe(403);
    }
    const page = await env.context.newPage();
    await page.goto(`${env.a}/forms/`);
    const fromPage = await page.evaluate(async (url) => {
      try {
        const r = await fetch(`${url}/v1/pair/begin`, {
          method: "POST",
          headers: { "Content-Type": "application/json", "X-VSF-Bridge": "1" },
          body: JSON.stringify({ nonce_c: "00112233445566778899aabbccddeeff" }),
        });
        return r.status;
      } catch (e) {
        return `blocked:${(e as Error).name}`;
      }
    }, c.url);
    expect(String(fromPage)).toMatch(/blocked/);
    await new Promise((r) => setTimeout(r, 400));
    expect(c.events.filter((e) => e.event === "pair_request")).toEqual([]);
  } finally {
    await c.stop();
  }
});

test("an expired credential is renewed by the extension without asking the operator again", async ({ env }) => {
  const c = await startCompanion(`chrome-extension://${env.extId}`, ["-access-ttl", "1500ms"]);
  try {
    expect(await pair(env, c)).toMatchObject({ ok: true });
    expect(await env.send({ type: "ping" })).toMatchObject({ ok: true });
    await new Promise((r) => setTimeout(r, 2000));
    expect(await env.send({ type: "ping" })).toMatchObject({ ok: true });
    expect(c.events.filter((e) => e.event === "pair_request")).toHaveLength(1);
  } finally {
    await c.stop();
  }
});

test("revoking at the companion, or unpairing in the extension, ends access at once", async ({ env }) => {
  const c = await startCompanion(`chrome-extension://${env.extId}`);
  try {
    expect(await pair(env, c)).toMatchObject({ ok: true });
    const id = ((await env.send({ type: "status" })) as { paired: string }).paired;
    c.say(`revoke ${id}`);
    await c.next("revoked");
    expect(await env.send({ type: "ping" })).toMatchObject({ ok: false });
    expect(((await env.send({ type: "status" })) as { paired: string | null }).paired).toBeNull();

    expect(await pair(env, c)).toMatchObject({ ok: true });
    const second = ((await env.send({ type: "status" })) as { paired: string }).paired;
    expect(await env.send({ type: "unpair" })).toMatchObject({ ok: true });
    c.say("list");
    const listed = (await c.next("pairings")) as { pairings: { pairing_id: string; revoked: boolean }[] };
    expect(listed.pairings.find((p) => p.pairing_id === second)?.revoked).toBe(true);
    expect(await env.send({ type: "ping" })).toMatchObject({ ok: false, error: "unpaired" });
  } finally {
    await c.stop();
  }
});
