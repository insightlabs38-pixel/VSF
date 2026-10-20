import assert from "node:assert/strict";
import { test } from "node:test";
import { parsePrefs } from "../src/prefs.ts";

test("display preferences are accepted only from a closed vocabulary", () => {
  assert.deepEqual(parsePrefs({}), {});
  assert.deepEqual(parsePrefs({ scheme: "dark", density: "compact", accent: "mint", nav: "side" }), {
    scheme: "dark",
    density: "compact",
    accent: "mint",
    nav: "side",
  });
  assert.deepEqual(
    parsePrefs({ layout: { order: ["shell_nav", "shell_main"], collapsed: ["shell_rail"], sizes: { shell_nav: 24 } } }),
    {
      layout: { order: ["shell_nav", "shell_main"], collapsed: ["shell_rail"], sizes: { shell_nav: 24 } },
    },
  );
});

test("anything outside that vocabulary is refused as a whole", () => {
  const bad: unknown[] = [
    null,
    [],
    "dark",
    { scheme: "purple" },
    { scheme: 1 },
    { density: "tiny" },
    { unknown: true },
    { token: "abc" },
    { layout: [] },
    { layout: { order: "a" } },
    { layout: { order: ["Has Space"] } },
    { layout: { order: Array.from({ length: 65 }, (_, i) => `r${i}`) } },
    { layout: { sizes: { a: -1 } } },
    { layout: { sizes: { a: Number.NaN } } },
    { layout: { sizes: { a: 1001 } } },
    { layout: { sizes: { "bad id": 3 } } },
    { layout: { other: [] } },
    JSON.parse('{"__proto__":{"scheme":"dark"}}'),
  ];
  for (const b of bad) assert.equal(parsePrefs(b), null, JSON.stringify(b));
});

test("parsing returns a copy that holds only what was accepted", () => {
  const input = { scheme: "light" as const };
  const out = parsePrefs(input);
  assert.deepEqual(out, input);
  assert.notEqual(out, input);
});
