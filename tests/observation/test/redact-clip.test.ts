import assert from "node:assert/strict";
import { test } from "node:test";
import { redactThenClip } from "@vsf/observer";

const token = ["gh", "p_", "a".repeat(36)].join("");

test("a secret that straddles the clip point leaves no head behind", () => {
  for (let pad = 0; pad < 40; pad++) {
    const text = `${"x".repeat(pad)}${token}${" tail".repeat(20)}`;
    const out = redactThenClip(text, 40);
    assert.ok(!out.includes("ghp_"), `pad ${pad}: ${out}`);
    assert.ok(!/a{6}/.test(out), `pad ${pad}: ${out}`);
    assert.ok(out.length <= 40);
  }
});

test("ordinary text is clipped on a character boundary and kept", () => {
  assert.equal(redactThenClip("hello world", 5), "hello");
  assert.equal(redactThenClip("short", 50), "short");
  const pair = "😀".repeat(10);
  const out = redactThenClip(pair, 5);
  assert.ok(!/[\ud800-\udbff]$/.test(out));
});
