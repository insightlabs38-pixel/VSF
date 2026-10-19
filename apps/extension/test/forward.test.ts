import assert from "node:assert/strict";
import { test } from "node:test";
import { forward } from "../src/forward.ts";

const body = {
  requestId: "req-00000001",
  action: "set_stage",
  entity: "order",
  values: { stage: "won" },
  origin: "http://127.0.0.1:9",
};

const reply = (status: number, json: unknown): typeof fetch =>
  (async () => new Response(JSON.stringify(json), { status })) as typeof fetch;

test("the request is one JSON post with the request id and no ambient credentials", async () => {
  let seen: { url: string; init: RequestInit } | undefined;
  const f = (async (url: string, init: RequestInit) => {
    seen = { url, init };
    return new Response(JSON.stringify({ ok: true, message: "done" }), { status: 200 });
  }) as unknown as typeof fetch;
  assert.deepEqual(await forward("http://127.0.0.1:5", body, f), { kind: "answered", ok: true, message: "done" });
  assert.equal(seen?.url, "http://127.0.0.1:5/v1/operate");
  assert.equal(seen?.init.method, "POST");
  assert.equal(seen?.init.credentials, "omit");
  const h = seen?.init.headers as Record<string, string>;
  assert.deepEqual(
    [h["X-VSF-Request"], h["X-VSF-Bridge"], h["Content-Type"]],
    [body.requestId, "1", "application/json"],
  );
  assert.deepEqual(JSON.parse(String(seen?.init.body)), body);
});

test("a refusal is a definite answer", async () => {
  assert.deepEqual(await forward("http://127.0.0.1:5", body, reply(403, { error: "bridge.unauthorized" })), {
    kind: "answered",
    ok: false,
    message: "bridge.unauthorized",
  });
  assert.deepEqual(await forward("http://127.0.0.1:5", body, reply(200, { ok: false })), {
    kind: "answered",
    ok: false,
  });
});

test("a server error, a dropped connection and a timeout all leave the outcome open", async () => {
  assert.deepEqual(await forward("http://127.0.0.1:5", body, reply(502, {})), { kind: "lost", reason: "server_error" });
  const down = (async () => {
    throw new TypeError("fetch failed");
  }) as unknown as typeof fetch;
  assert.deepEqual(await forward("http://127.0.0.1:5", body, down), { kind: "lost", reason: "network" });
  const hang = ((_u: string, init: RequestInit) =>
    new Promise((_r, reject) =>
      init.signal?.addEventListener("abort", () => reject(Object.assign(new Error("aborted"), { name: "AbortError" }))),
    )) as unknown as typeof fetch;
  assert.deepEqual(await forward("http://127.0.0.1:5", body, hang, 20), { kind: "lost", reason: "timeout" });
});

test("a malformed success body is a refusal, not a success", async () => {
  const garbage = (async () => new Response("<html>", { status: 200 })) as unknown as typeof fetch;
  assert.deepEqual(await forward("http://127.0.0.1:5", body, garbage), { kind: "answered", ok: false });
});
