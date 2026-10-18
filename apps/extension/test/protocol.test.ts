import assert from "node:assert/strict";
import { test } from "node:test";
import { isCommandSender, normalizeOrigin, parseCommand } from "../src/protocol.ts";

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
