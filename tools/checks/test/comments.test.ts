import assert from "node:assert/strict";
import { test } from "node:test";
import { lintComments } from "../src/comments.ts";

test("single-line comments pass", () => {
  assert.deepEqual(lintComments("a.ts", "// one\nconst a = 1;\n// two\n"), []);
  assert.deepEqual(lintComments("a.rs", "/// doc\nfn f() {}\n"), []);
  assert.deepEqual(lintComments("a.sh", "#!/usr/bin/env bash\n# one\nset -e\n"), []);
});

test("consecutive line comments are rejected", () => {
  const v = lintComments("a.go", "// a\n// b\npackage x\n");
  assert.equal(v.length, 1);
  assert.equal(v[0]?.line, 1);
  assert.equal(lintComments("a.sh", "# a\n# b\n").length, 1);
  assert.equal(lintComments("a.rs", "/// a\n/// b\nfn f() {}\n").length, 1);
});

test("multi-line block comments are rejected, one-line blocks pass", () => {
  assert.equal(lintComments("a.ts", "/*\n * x\n */\nconst a = 1;\n").length, 1);
  assert.deepEqual(lintComments("a.ts", "/* x */ const a = 1;\n"), []);
});

test("shebang followed by one comment is allowed", () => {
  assert.deepEqual(lintComments("a.sh", "#!/bin/sh\n# only\n"), []);
});
