import assert from "node:assert/strict";
import { cpSync, mkdtempSync, readdirSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { test } from "node:test";
import { generate } from "../src/cli.ts";
import { acceptedVersions, familyDigests } from "../src/digest.ts";
import { loadModel } from "../src/idl.ts";

const root = resolve(import.meta.dirname, "../../..");
const schemas = join(root, "schemas/ir");

// biome-ignore lint/suspicious/noExplicitAny: mutations edit untyped IDL JSON
type Json = any;

function variant(edit: (docs: Map<string, Json>) => void): string {
  const dir = mkdtempSync(join(tmpdir(), "idl-gen-"));
  cpSync(join(schemas, "released/1.0.0"), dir, { recursive: true });
  const docs = new Map<string, Json>(readdirSync(dir).map((f) => [f, JSON.parse(readFileSync(join(dir, f), "utf8"))]));
  edit(docs);
  for (const [f, d] of docs) writeFileSync(join(dir, f), JSON.stringify(d));
  return dir;
}

const digestOf = (dir: string) => Object.fromEntries(familyDigests(loadModel(dir)).map((d) => [d.family, d.digest]));

test("generation is deterministic and matches the committed bindings", () => {
  const a = generate(schemas);
  const b = generate(schemas);
  assert.deepEqual(a, b);
  for (const [rel, body] of Object.entries(a)) assert.equal(readFileSync(join(root, rel), "utf8"), body, rel);
});

test("prose edits never change a schema digest", () => {
  const edited = variant((docs) => {
    docs.get("common.idl.json").types.Label.doc = "Reworded.";
  });
  assert.deepEqual(digestOf(edited), digestOf(join(schemas, "released/1.0.0")));
});

test("a structural edit changes exactly the families whose closure contains it", () => {
  const edited = variant((docs) => {
    docs.get("environment.idl.json").types.Limits.fields.max_nodes.maxItems = 7;
  });
  const before = digestOf(join(schemas, "released/1.0.0"));
  const after = digestOf(edited);
  const changed = Object.keys(before).filter((f) => before[f] !== after[f]);
  assert.deepEqual(changed, ["environment"]);
});

test("a 1.1 schema with a since field generates version gating in every language", () => {
  const dir = variant((docs) => {
    for (const d of docs.values()) d.ir_version = "1.1.0";
    docs.get("environment.idl.json").types.Attestation.fields.note = { type: "Label", optional: true, since: "1.1.0" };
  });
  const out = generate(dir);
  assert.match(out["compiler/crates/vsf-ir/src/generated.rs"] as string, /o\.since\("note", \(1, 1, 0\), false/);
  assert.match(out["frontend/packages/ir/src/generated.ts"] as string, /\.since\("note", \[1, 1, 0\], false/);
  assert.match(out["backend/ir/generated.go"] as string, /\.Since\("note", rt\.Version\{1, 1, 0\}, false/);
  assert.notDeepEqual(digestOf(dir), digestOf(join(schemas, "released/1.0.0")));
});

test("every released snapshot and the working schema stay readable, each under its own digest", () => {
  const model = loadModel(schemas);
  const accepted = acceptedVersions(schemas, model, loadModel);
  const released = digestOf(join(schemas, "released/1.0.0"));
  const old = accepted.filter((a) => a.version.join(".") === "1.0.0");
  assert.equal(old.length, Object.keys(released).filter((f) => released[f]).length);
  for (const a of old) assert.equal(a.digest, released[a.family], a.family);
  const now = accepted.filter((a) => a.version.join(".") === model.irVersion.join("."));
  assert.ok(now.length > 0);
  assert.ok(now.every((a) => digestOf(schemas)[a.family] === a.digest));
  if (model.irVersion.join(".") !== "1.0.0") {
    for (const a of now) assert.notEqual(a.digest, released[a.family], `${a.family} must differ once the schema moves`);
  }
});

test("prose never changes a digest, but a field that is itself named doc does", () => {
  const base = digestOf(variant(() => {}));
  const reword = digestOf(
    variant((docs) => {
      const t = docs.get("operation.idl.json").types.OperationContract;
      t.doc = "Reworded prose.";
    }),
  );
  assert.deepEqual(reword, base);
  const field = digestOf(
    variant((docs) => {
      docs.get("operation.idl.json").types.OperationContract.fields.doc = { type: "Label", optional: true };
    }),
  );
  assert.notEqual(field.operation, base.operation);
  const retyped = digestOf(
    variant((docs) => {
      docs.get("operation.idl.json").types.OperationContract.fields.doc = { type: "LongText", optional: true };
    }),
  );
  assert.notEqual(retyped.operation, field.operation);
});
