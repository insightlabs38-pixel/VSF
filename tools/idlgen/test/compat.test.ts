import assert from "node:assert/strict";
import { cpSync, mkdtempSync, readdirSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { test } from "node:test";
import { checkAgainstReleased, compare, latestReleased } from "../src/compat.ts";
import { loadModel } from "../src/idl.ts";

const schemas = resolve(import.meta.dirname, "../../../schemas/ir");
const released = join(schemas, "released/1.0.0");

// biome-ignore lint/suspicious/noExplicitAny: mutations edit untyped IDL JSON
type Json = any;
type Def = Json;
type Types = Record<string, Json>;
type Docs = Map<string, { types: Types; [k: string]: Json }>;

/** Applies `mutate` to a copy of the released IDL and compares the copy against the original. */
function change(version: string, mutate: (types: Types, docs: Docs) => void) {
  const dir = mkdtempSync(join(tmpdir(), "idl-test-"));
  cpSync(released, dir, { recursive: true });
  const docs: Docs = new Map();
  for (const f of readdirSync(dir)) docs.set(f, JSON.parse(readFileSync(join(dir, f), "utf8")));
  const all: Types = {};
  for (const d of docs.values()) Object.assign(all, d.types);
  mutate(all, docs);
  for (const [f, d] of docs) {
    d.ir_version = version;
    writeFileSync(join(dir, f), JSON.stringify(d));
  }
  const v = compare(loadModel(released), loadModel(dir));
  return { codes: v.findings.map((f) => f.code), errors: v.errors.map((e) => e.code), v };
}

/** Drops a type and every field that referenced it, so the remaining model stays well formed. */
const remove = (docs: Docs, name: string) => {
  for (const d of docs.values()) {
    delete d.types[name];
    for (const t of Object.values(d.types))
      for (const [k, f] of Object.entries<Def>(t.fields ?? {})) if (f.type === name) delete t.fields[k];
  }
};

const replace = (def: Def, next: Def) => {
  for (const k of Object.keys(def)) delete def[k];
  Object.assign(def, next);
};

const breaking: [string, (t: Types, d: Docs) => void, string][] = [
  [
    "removing an enum value",
    (t) => (t.Certainty.values = ["verified", "inferred", "unknown"]),
    "idl.enum_value_removed",
  ],
  [
    "renaming a field",
    (t) => {
      t.Limits.fields.max_node_count = t.Limits.fields.max_nodes;
      delete t.Limits.fields.max_nodes;
    },
    "idl.field_removed",
  ],
  ["changing a field type", (t) => (t.Limits.fields.max_nodes.type = "Label"), "idl.field_type_changed"],
  [
    "making an optional field required",
    (t) => delete t.Attestation.fields.digest.optional,
    "idl.field_became_required",
  ],
  [
    "adding a required field without since",
    (t) => (t.Attestation.fields.extra = { type: "Label" }),
    "idl.required_field_added",
  ],
  ["lowering a maximum length", (t) => (t.Label.maxLen = 100), "idl.length_narrowed"],
  ["changing a pattern", (t) => (t.Digest.pattern = "^sha512:[0-9a-f]{128}$"), "idl.pattern_changed"],
  ["narrowing an integer range", (t) => (t.Revision.max = 1000), "idl.int_range_narrowed"],
  ["removing a union variant", (t) => delete t.ClaimValue.variants.money, "idl.variant_removed"],
  ["retargeting a union variant", (t) => (t.ClaimValue.variants.text = "ClaimInt"), "idl.variant_retargeted"],
  ["renaming a union tag", (t) => (t.ClaimValue.tag = "type"), "idl.tag_changed"],
  ["removing a type", (_t, d) => remove(d, "Unit"), "idl.type_removed"],
  ["changing a type kind", (t) => replace(t.Count, { kind: "string", maxLen: 10 }), "idl.kind_changed"],
  ["changing text handling", (t) => (t.Label.text = "none"), "idl.text_mode_changed"],
  [
    "tightening a list bound",
    (t) => (t.Unit.fields.symbol = { type: "list<Label>", maxItems: 3 }),
    "idl.field_type_changed",
  ],
];

for (const [name, mutate, code] of breaking) {
  test(`rejected without a major bump: ${name}`, () => {
    for (const version of ["1.0.0", "1.1.0", "1.0.1"]) {
      const r = change(version, mutate);
      assert.ok(r.codes.includes(code), `${code} in ${r.codes}`);
      assert.ok(r.errors.includes("idl.breaking_without_major"), `${version}: ${r.errors}`);
    }
  });
  test(`accepted with an explicit major bump: ${name}`, () => {
    const r = change("2.0.0", mutate);
    assert.ok(r.codes.includes(code));
    assert.deepEqual(r.errors, []);
  });
}

test("narrowing a list bound is breaking", () => {
  const dir = mkdtempSync(join(tmpdir(), "idl-test-"));
  cpSync(released, dir, { recursive: true });
  const file = join(dir, "semantic.idl.json");
  const doc = JSON.parse(readFileSync(file, "utf8"));
  const lists = Object.values(doc.types as Types)
    .flatMap((t) => (t.kind === "struct" ? Object.values(t.fields as Types) : []))
    .filter((f) => f.maxItems);
  assert.ok(lists.length > 0);
  (lists[0] as Def).maxItems -= 1;
  writeFileSync(file, JSON.stringify(doc));
  const v = compare(loadModel(released), loadModel(dir));
  assert.ok(v.findings.some((f) => f.code === "idl.items_narrowed"));
  assert.ok(v.errors.some((e) => e.code === "idl.breaking_without_major"));
});

const additive: [string, (t: Types, d: Docs) => void, string, boolean][] = [
  [
    "adding an optional field with since",
    (t) => (t.Attestation.fields.note = { type: "Label", optional: true, since: "1.1.0" }),
    "idl.field_added",
    true,
  ],
  [
    "adding a required-from-1.1 field",
    (t) => (t.Attestation.fields.note = { type: "Label", since: "1.1.0" }),
    "idl.field_added",
    true,
  ],
  ["adding an enum value", (t) => t.Certainty.values.push("partial"), "idl.enum_value_added", false],
  [
    "adding a union variant",
    (t) => {
      t.ClaimValue.variants.label = "ClaimText";
    },
    "idl.variant_added",
    false,
  ],
  [
    "adding a type",
    (_t, d) => ((d.get("common.idl.json")?.types as Types).Extra = { kind: "int", min: 0, max: 5 }),
    "idl.type_added",
    false,
  ],
  ["widening a maximum length", (t) => (t.Label.maxLen = 400), "idl.length_widened", false],
  ["widening an integer range", (t) => (t.Revision.max = 4294967296), "idl.int_range_widened", false],
  [
    "making a required field optional",
    (t) => (t.Limits.fields.max_nodes.optional = true),
    "idl.field_became_optional",
    false,
  ],
];

for (const [name, mutate, code, needsSince] of additive) {
  test(`accepted with a minor bump: ${name}`, () => {
    const r = change("1.1.0", mutate);
    assert.ok(r.codes.includes(code), `${code} in ${r.codes}`);
    assert.deepEqual(r.errors, []);
  });
  // A since newer than ir_version is already an invalid model, so those cases cannot run unbumped.
  if (!needsSince) {
    test(`rejected without a version bump: ${name}`, () => {
      const r = change("1.0.0", mutate);
      assert.ok(r.errors.includes("idl.change_without_version_bump"), `${r.errors}`);
    });
  }
}

test("a field with a future since is not a valid model at the old version", () => {
  assert.throws(
    () => change("1.0.0", (t) => (t.Attestation.fields.note = { type: "Label", optional: true, since: "1.1.0" })),
    /since is newer than ir_version/,
  );
});

test("a field added in a minor release must declare the version that introduced it", () => {
  const r = change("1.1.0", (t) => (t.Attestation.fields.note = { type: "Label", optional: true }));
  assert.deepEqual(r.errors, ["idl.added_field_without_since"]);
  const late = change("1.2.0", (t) => (t.Attestation.fields.note = { type: "Label", optional: true, since: "1.0.0" }));
  assert.deepEqual(late.errors, ["idl.added_field_without_since"]);
});

test("changing when a field appears is breaking", () => {
  const dir = mkdtempSync(join(tmpdir(), "idl-test-"));
  const base = mkdtempSync(join(tmpdir(), "idl-test-"));
  for (const [target, since] of [
    [base, "1.1.0"],
    [dir, "1.2.0"],
  ] as const) {
    cpSync(released, target, { recursive: true });
    for (const f of readdirSync(target)) {
      const doc = JSON.parse(readFileSync(join(target, f), "utf8"));
      doc.ir_version = "1.2.0";
      if (f.startsWith("environment")) doc.types.Attestation.fields.note = { type: "Label", optional: true, since };
      writeFileSync(join(target, f), JSON.stringify(doc));
    }
  }
  const v = compare(loadModel(base), loadModel(dir));
  assert.deepEqual(
    v.findings.map((f) => f.code),
    ["idl.since_changed"],
  );
});

test("a version regression is rejected", () => {
  assert.deepEqual(change("0.9.0", () => {}).errors, ["idl.version_regressed"]);
});

test("documentation-only edits are neither breaking nor additive", () => {
  const r = change("1.0.0", (t) => (t.Label.doc = "Edited prose."));
  assert.deepEqual(r.codes, []);
  assert.deepEqual(r.errors, []);
});

test("the working schemas are compatible with the latest released snapshot", () => {
  const v = checkAgainstReleased(schemas);
  assert.equal(latestReleased(schemas)?.version.join("."), "1.2.0");
  assert.deepEqual(v.errors, []);
  assert.equal(v.baseline, "1.2.0");
});

test("every released snapshot is additively compatible with the one before it", () => {
  const v = compare(loadModel(released), loadModel(join(schemas, "released/1.1.0")));
  assert.deepEqual(v.errors, []);
  assert.ok(v.findings.length > 0);
});

test("1.2.0 is additively compatible with 1.1.0", () => {
  const v = compare(loadModel(join(schemas, "released/1.1.0")), loadModel(join(schemas, "released/1.2.0")));
  assert.deepEqual(v.errors, []);
  assert.ok(v.findings.some((f) => f.code === "idl.field_added"));
});
