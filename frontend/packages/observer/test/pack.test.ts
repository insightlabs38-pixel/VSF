import assert from "node:assert/strict";
import { test } from "node:test";
import {
  analyzePack,
  Builder,
  decodePack,
  encodePack,
  generatePackBytes,
  NONE,
  type PackError,
  summaryJson,
} from "../src/index.ts";

function sample() {
  const b = new Builder();
  const root = b.node(NONE, "html", null, null, null, 1);
  const form = b.node(root, "form", null, "Order", null, 1);
  b.node(form, "input", "textbox", "Name", null, 0b111);
  b.node(form, "button", null, null, "Submit é", 0b11);
  return encodePack(b.snapshot);
}

test("roundtrip is exact and canonical", () => {
  const bytes = sample();
  assert.deepEqual(encodePack(decodePack(bytes)), bytes);
});

test("empty snapshot is valid and empty input is not", () => {
  const bytes = encodePack({ strings: [], nodes: [] });
  assert.equal(bytes.length, 28);
  assert.deepEqual(decodePack(bytes), { strings: [], nodes: [] });
  assert.throws(() => decodePack(new Uint8Array()), { code: "pack.empty" });
});

function code(bytes: Uint8Array): string {
  try {
    decodePack(bytes);
  } catch (e) {
    return (e as PackError).code;
  }
  return "ok";
}

test("rejects every malformed shape with the same codes as Rust", () => {
  const base = sample();
  const mutate = (f: (b: Uint8Array) => void): Uint8Array => {
    const copy = base.slice();
    f(copy);
    return copy;
  };
  assert.equal(
    code(
      mutate((b) => {
        b[0] = 88;
      }),
    ),
    "pack.bad_magic",
  );
  assert.equal(
    code(
      mutate((b) => {
        b[4] = 9;
      }),
    ),
    "pack.bad_version",
  );
  assert.equal(
    code(
      mutate((b) => {
        b[6] = 1;
      }),
    ),
    "pack.bad_flags",
  );
  assert.equal(
    code(
      mutate((b) => {
        b[20] = 1;
      }),
    ),
    "pack.reserved_nonzero",
  );
  assert.equal(code(base.slice(0, 23)), "pack.truncated");
  assert.equal(code(base.slice(0, base.length - 1)), "pack.truncated");
  assert.equal(code(Uint8Array.from([...base, 0])), "pack.length_mismatch");
  assert.equal(code(mutate((b) => void new DataView(b.buffer).setUint32(16, 0xffffffff, true))), "pack.too_large");
  const nodesAt = base.length - 4 * 24;
  const setAt = (at: number, v: number): Uint8Array =>
    mutate((b) => void new DataView(b.buffer).setUint32(at, v, true));
  assert.equal(code(setAt(nodesAt + 24, 1)), "pack.parent_invalid");
  assert.equal(code(setAt(nodesAt + 28, 999)), "pack.string_index_out_of_range");
  assert.equal(code(setAt(nodesAt + 4, NONE)), "pack.string_index_out_of_range");
  assert.equal(code(setAt(nodesAt + 20, 16)), "pack.node_flags_invalid");
  assert.equal(code(setAt(28, 99)), "pack.offsets_invalid");
});

test("rejects invalid UTF-8 and non-canonical string tables", () => {
  const base = sample();
  const stringCount = new DataView(base.buffer).getUint32(8, true);
  const bad = base.slice();
  bad[24 + 4 * (stringCount + 1)] = 0xff;
  assert.equal(code(bad), "pack.invalid_utf8");
  const reordered = decodePack(base);
  [reordered.strings[0], reordered.strings[1]] = [reordered.strings[1] as string, reordered.strings[0] as string];
  for (const n of reordered.nodes) {
    for (const f of ["tag", "role", "name", "text"] as const) {
      if (n[f] === 0) n[f] = 1;
      else if (n[f] === 1) n[f] = 0;
    }
  }
  assert.equal(code(encodePack(reordered)), "pack.not_canonical");
  const unused = decodePack(base);
  unused.strings.push("unused");
  assert.equal(code(encodePack(unused)), "pack.not_canonical");
});

test("never throws anything but PackError on truncations and bit flips", () => {
  const base = sample();
  for (let cut = 0; cut < base.length; cut++) code(base.slice(0, cut));
  for (let i = 0; i < base.length; i++) {
    for (let bit = 0; bit < 8; bit++) {
      const b = base.slice();
      b[i] = (b[i] ?? 0) ^ (1 << bit);
      code(b);
    }
  }
});

test("analysis summary has the canonical key order", async () => {
  const sum = await analyzePack(sample());
  assert.deepEqual(sum.classes, { button: 1, form: 1, other: 1, textbox: 1 });
  assert.equal(sum.max_depth, 2);
  assert.match(
    summaryJson(sum),
    /^\{"classes":\{"button":1,"form":1,"other":1,"textbox":1\},"digest":"[0-9a-f]{64}","max_depth":2,/,
  );
});

test("generator is deterministic and scales", () => {
  assert.deepEqual(generatePackBytes(7, 1000), generatePackBytes(7, 1000));
  assert.notDeepEqual(generatePackBytes(7, 1000), generatePackBytes(8, 1000));
  for (const n of [0, 1, 2, 1000, 10000]) assert.equal(decodePack(generatePackBytes(3, n)).nodes.length, n);
});
