import assert from "node:assert/strict";
import { test } from "node:test";
import { authenticate, newKey, sameSecret, type TabSession } from "../src/session.ts";

const ORIGIN = "http://127.0.0.1:9";
const live = (): TabSession => ({ key: "k".repeat(48), epoch: "epoch-00000001", seq: 4, origin: ORIGIN });
const sessions = (s: TabSession | null = live()): Map<number, TabSession> => new Map(s ? [[7, s]] : []);
const sender = { id: "self", tab: { id: 7 }, frameId: 0, origin: ORIGIN };
const msg = { key: "k".repeat(48), epoch: "epoch-00000001", seq: 5 };
const check = (m: object, s: object, map = sessions(), granted = [ORIGIN]) =>
  authenticate(m, s as never, "self", map, granted);

test("the shell of a live session is authenticated", () => {
  const r = check(msg, sender);
  assert.ok(r.ok && r.auth.tab === 7 && r.auth.session.origin === ORIGIN);
});

test("only this extension's top frame in a tab with a session may speak", () => {
  for (const [name, s] of [
    ["foreign extension", { ...sender, id: "other" }],
    ["no tab", { id: "self", frameId: 0, origin: ORIGIN }],
    ["subframe", { ...sender, frameId: 3 }],
  ] as const)
    assert.deepEqual(check(msg, s), { ok: false, error: "sender_not_allowed" }, name);
  assert.deepEqual(check(msg, { ...sender, tab: { id: 8 } }), { ok: false, error: "stale_session" });
});

test("a wrong, missing or old key or epoch is a stale session", () => {
  for (const m of [
    { ...msg, key: "x".repeat(48) },
    { ...msg, key: undefined },
    { ...msg, key: "k".repeat(47) },
    { ...msg, epoch: "epoch-00000002" },
    { ...msg, epoch: 5 },
  ])
    assert.deepEqual(check(m, sender), { ok: false, error: "stale_session" }, JSON.stringify(m));
  assert.deepEqual(check(msg, sender, sessions(null)), { ok: false, error: "stale_session" });
});

test("a tab that moved to another origin or lost its grant is refused", () => {
  assert.deepEqual(check(msg, { ...sender, origin: "http://evil.test" }), { ok: false, error: "origin_changed" });
  assert.deepEqual(check(msg, { id: "self", tab: { id: 7 }, frameId: 0, url: "http://evil.test/x" }), {
    ok: false,
    error: "origin_changed",
  });
  assert.deepEqual(check(msg, sender, sessions(), []), { ok: false, error: "origin_not_granted" });
});

test("sequence numbers only go forward", () => {
  for (const seq of [4, 3, 0, -1, 1.5, "5", undefined])
    assert.deepEqual(check({ ...msg, seq }, sender), { ok: false, error: "replay" }, String(seq));
});

test("keys are random, fixed length and compared without early exit", () => {
  const a = newKey();
  assert.match(a, /^[0-9a-f]{48}$/);
  assert.notEqual(a, newKey());
  assert.ok(sameSecret(a, a));
  assert.ok(!sameSecret(a, `${a.slice(0, -1)}${a.endsWith("0") ? "1" : "0"}`));
  assert.ok(!sameSecret(a, a.slice(1)));
});
