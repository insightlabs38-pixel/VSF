import assert from "node:assert/strict";
import { test } from "node:test";
import { checkIdentity, checkMessage } from "../src/commitmsg.ts";

const ids = (text: string): string[] => checkMessage(text).map((r) => r.id);

test("accepts a concise technical message of nine lines", () => {
  assert.deepEqual(ids("Add canonical JSON profile\n\nUTF-16 key order; integers only"), []);
  assert.deepEqual(ids(Array.from({ length: 9 }, (_, i) => `line ${i}`).join("\n")), []);
});

test("rejects ten lines and empty messages", () => {
  assert.deepEqual(ids(Array.from({ length: 10 }, (_, i) => `line ${i}`).join("\n")), ["message.too-long"]);
  assert.deepEqual(ids("  \n"), ["message.empty"]);
});

test("rejects attribution, signatures and links", () => {
  const bad: [string, string][] = [
    ["x\n\nCo-Authored-By: someone", "attribution.co-author"],
    ["x\n\nSigned-off-by: someone", "attribution.signature"],
    ["Fix bug (Claude Code)", "attribution.assistant"],
    ["Fix bug by AI", "attribution.ai"],
    ["Generated with a tool", "attribution.generated"],
    ["See https://example.test/x", "attribution.link"],
  ];
  for (const [text, id] of bad) assert.ok(ids(text).includes(id), `${text} -> ${id}`);
});

test("identity must be the repository owner", () => {
  assert.deepEqual(checkIdentity("insightlabs38-pixel", "insightlabs38@gmail.com"), []);
  assert.equal(checkIdentity("Claude", "noreply@anthropic.com").length, 1);
});
