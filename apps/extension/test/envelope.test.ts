import assert from "node:assert/strict";
import { test } from "node:test";
import { MAX_ENVELOPE_CHARS, makeEnvelope, PROTOCOL, parseEnvelope } from "../src/envelope.ts";

const S = "11111111-1111-4111-8111-111111111111";
const I = "22222222-2222-4222-8222-222222222222";
const ok = (kind: string, body: object) => ({ proto: PROTOCOL, kind, id: I, session: S, seq: 3, body });

test("every kind round-trips with its own body", () => {
  const cases: [string, object][] = [
    ["hello", {}],
    ["refresh", {}],
    ["heartbeat", { at: 5 }],
    ["restore", { reason: "escape" }],
    ["dispatch", { action: "act_save", entity: "t_orders", rowKey: "k_1001" }],
    ["prefs", { prefs: { density: "compact" } }],
  ];
  for (const [kind, body] of cases) {
    const r = parseEnvelope(ok(kind, body));
    assert.equal(r.ok, true, kind);
  }
  const made = makeEnvelope("restore", S, 1, { reason: "user" }, I);
  assert.deepEqual(parseEnvelope(made), { ok: true, env: made });
});

test("shape errors are classified, not guessed at", () => {
  const e = (raw: unknown) => {
    const r = parseEnvelope(raw);
    return r.ok ? "ok" : r.error;
  };
  assert.equal(e(null), "bad_envelope");
  assert.equal(e([]), "bad_envelope");
  assert.equal(e("x"), "bad_envelope");
  assert.equal(e({ ...ok("hello", {}), extra: 1 }), "bad_envelope");
  assert.equal(e({ proto: PROTOCOL }), "bad_envelope");
  assert.equal(e({ ...ok("hello", {}), proto: "vsf-ext-v2" }), "unsupported_version");
  assert.equal(e(ok("shell", {})), "unknown_kind");
  assert.equal(e({ ...ok("hello", {}), id: "not-a-uuid" }), "bad_id");
  assert.equal(e({ ...ok("hello", {}), session: "../../x" }), "bad_session");
  assert.equal(e({ ...ok("hello", {}), seq: -1 }), "bad_seq");
  assert.equal(e({ ...ok("hello", {}), seq: 1.5 }), "bad_seq");
  assert.equal(e({ ...ok("hello", {}), seq: Number.MAX_SAFE_INTEGER + 1 }), "bad_seq");
  assert.equal(e({ ...ok("hello", {}), body: [] }), "bad_body");
  assert.equal(e(Object.assign(Object.create({ inherited: 1 }), ok("hello", {}))), "bad_envelope");
});

test("bodies are exact: no extra keys, no wrong types, no unbounded text", () => {
  const e = (kind: string, body: object) => {
    const r = parseEnvelope(ok(kind, body));
    return r.ok ? "ok" : r.error;
  };
  assert.equal(e("hello", { x: 1 }), "bad_body");
  assert.equal(e("heartbeat", { at: "now" }), "bad_body");
  assert.equal(e("heartbeat", { at: Number.POSITIVE_INFINITY }), "bad_body");
  assert.equal(e("restore", { reason: "because" }), "bad_body");
  assert.equal(e("dispatch", { action: "A B", entity: "e" }), "bad_body");
  assert.equal(e("dispatch", { action: "a", entity: "e", rowKey: "bad key!" }), "bad_body");
  assert.equal(e("dispatch", { action: "a", entity: "e", extra: 1 }), "bad_body");
  assert.equal(e("prefs", { prefs: { scheme: "plaid" } }), "bad_body");
  assert.equal(e("prefs", {}), "bad_body");
  assert.equal(
    e("dispatch", { action: "a".repeat(64), entity: "e".repeat(64), rowKey: "r".repeat(MAX_ENVELOPE_CHARS.dispatch) }),
    "too_large",
  );
});

test("a body that cannot be serialized is refused instead of throwing", () => {
  const cyclic: Record<string, unknown> = {};
  cyclic.self = cyclic;
  assert.doesNotThrow(() => parseEnvelope({ ...ok("hello", {}), body: cyclic }));
  const r = parseEnvelope({ ...ok("hello", {}), body: cyclic });
  assert.equal(r.ok, false);
});
