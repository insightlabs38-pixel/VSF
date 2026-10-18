import assert from "node:assert/strict";
import { test } from "node:test";
import { parse } from "@vsf/schema";
import { type Ctx, Obj } from "../src/rt.ts";

const doc = (text: string) => Obj.of(parse(new TextEncoder().encode(text)), "");
const old: Ctx = { version: [1, 0, 0] };
const next: Ctx = { version: [1, 1, 0] };

test("since gates fields by the declared document version", () => {
  const read = (text: string, ctx: Ctx, required: boolean) => {
    const o = doc(text);
    o.req("a");
    return o.since("b", [1, 1, 0], required, ctx);
  };
  assert.throws(() => read('{"a":1,"b":2}', old, true), { code: "ir.field_from_future" });
  assert.equal(read('{"a":1}', old, true), undefined);
  assert.equal(read('{"a":1,"b":2}', next, true), 2);
  assert.throws(() => read('{"a":1}', next, true), { code: "ir.missing_field" });
  assert.equal(read('{"a":1}', next, false), undefined);
});
