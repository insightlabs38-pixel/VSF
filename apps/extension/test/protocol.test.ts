import assert from "node:assert/strict";
import { test } from "node:test";
import {
  isCommandSender,
  normalizeOrigin,
  normalizeService,
  parseCommand,
  parseShellMessage,
} from "../src/protocol.ts";

test("origins must be exact http(s) origins", () => {
  assert.equal(normalizeOrigin("https://example.test"), "https://example.test");
  assert.equal(normalizeOrigin("http://127.0.0.1:8080"), "http://127.0.0.1:8080");
  for (const bad of [
    "https://example.test/",
    "https://example.test/path",
    "https://*.example.test",
    "ftp://example.test",
    "javascript:alert(1)",
    "chrome-extension://abc",
    ["https://user", ":", "pw", "@example.test"].join(""),
    "https://example.test?x=1",
    "",
    "example.test",
    `https://${"a".repeat(400)}.test`,
    42,
    null,
    undefined,
    {},
  ]) {
    assert.equal(normalizeOrigin(bad), null, String(bad));
  }
});

test("commands are validated strictly", () => {
  assert.deepEqual(parseCommand({ type: "status", extra: 1 }), { type: "status" });
  assert.deepEqual(parseCommand({ type: "capture", tabId: 7 }), { type: "capture", tabId: 7 });
  assert.deepEqual(parseCommand({ type: "observe", tabId: 7 }), { type: "observe", tabId: 7 });
  assert.deepEqual(parseCommand({ type: "grant", origin: "https://a.test" }), {
    type: "grant",
    origin: "https://a.test",
  });
  for (const bad of [
    null,
    undefined,
    1,
    "x",
    [],
    {},
    { type: "capture" },
    { type: "capture", tabId: -1 },
    { type: "capture", tabId: 1.2 },
    { type: "capture", tabId: "1" },
    { type: "observe" },
    { type: "observe", tabId: -1 },
    { type: "observe", tabId: "1" },
    { type: "grant", origin: "https://a.test/x" },
    { type: "revoke" },
    { type: "exec", code: "1" },
  ]) {
    assert.equal(parseCommand(bad), null, JSON.stringify(bad));
  }
});

test("only the packaged panel in the top frame may issue commands", () => {
  const panel = "chrome-extension://abc/panel.html";
  assert.equal(isCommandSender({ id: "abc", url: panel, frameId: 0 }, "abc", panel), true);
  assert.equal(isCommandSender({ id: "abc", url: `${panel}?x=1`, frameId: 0 }, "abc", panel), true);
  assert.equal(isCommandSender({ id: "abc", url: panel, frameId: 3 }, "abc", panel), false);
  assert.equal(isCommandSender({ id: "other", url: panel, frameId: 0 }, "abc", panel), false);
  assert.equal(isCommandSender({ id: "abc", url: "https://evil.test/", frameId: 0 }, "abc", panel), false);
  assert.equal(isCommandSender({ id: "abc", url: `${panel}.evil.html`, frameId: 0 }, "abc", panel), false);
  assert.equal(isCommandSender({ id: "abc", frameId: 0 }, "abc", panel), false);
  assert.equal(isCommandSender({ url: panel, frameId: 0 }, "abc", panel), false);
});

test("the local service must be a loopback http origin with a port", () => {
  assert.equal(normalizeService("http://127.0.0.1:8080"), "http://127.0.0.1:8080");
  for (const bad of [
    "http://127.0.0.1",
    "http://127.0.0.1:0",
    "http://127.0.0.1:99999",
    "http://127.0.0.1:080",
    "https://127.0.0.1:8080",
    "http://localhost:8080",
    "http://127.0.0.1.evil.test:8080",
    "http://127.0.0.1:8080/",
    "http://user@127.0.0.1:8080",
    "http://0x7f.1:8080",
    "http://[::1]:8080",
    42,
    null,
  ])
    assert.equal(normalizeService(bad), null, String(bad));
  assert.deepEqual(parseCommand({ type: "connect", url: "http://127.0.0.1:8080" }), {
    type: "connect",
    url: "http://127.0.0.1:8080",
  });
  assert.equal(parseCommand({ type: "connect", url: "http://evil.test:8080" }), null);
  assert.deepEqual(parseCommand({ type: "disconnect" }), { type: "disconnect" });
});

test("shell messages are closed and bounded", () => {
  const good = {
    type: "dispatch",
    requestId: "req-00000001",
    key: "ab".repeat(24),
    epoch: "epoch-00000001",
    seq: 1,
    action: "set_stage",
    entity: "order",
    values: { stage: "won", n: 2, flag: true },
  };
  assert.deepEqual(parseShellMessage(good), good);
  assert.deepEqual(parseShellMessage({ type: "reconnect", epoch: "epoch-00000001" }), {
    type: "reconnect",
    epoch: "epoch-00000001",
  });
  const bad: Record<string, unknown>[] = [
    { ...good, type: "capture" },
    { ...good, requestId: "X" },
    { ...good, key: "short" },
    { ...good, seq: 0 },
    { ...good, action: "Set Stage" },
    { ...good, rowKey: 5 },
    { ...good, values: [] },
    { ...good, values: { "bad key": 1 } },
    { ...good, values: { nested: { a: 1 } } },
    { ...good, values: { big: "x".repeat(5000) } },
    { ...good, values: Object.fromEntries(Array.from({ length: 40 }, (_, i) => [`k${i}`, i])) },
    { type: "reconnect" },
  ];
  for (const b of bad) assert.equal(parseShellMessage(b), null, JSON.stringify(b).slice(0, 60));
  assert.equal(parseShellMessage(null), null);
});
