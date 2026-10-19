import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import { validateProtocolEvidence } from "@vsf/observer";
import { Ajv } from "ajv";

const dir = new URL("../../../fixtures/recognition/protocol/", import.meta.url);
const schema = JSON.parse(
  readFileSync(new URL("../../../schemas/protocol-evidence/v1.schema.json", import.meta.url), "utf8"),
);
const structural = new Ajv({ allErrors: true, strict: false }).compile(schema);

test("recognition protocol fixtures are valid protocol evidence", () => {
  const files = readdirSync(fileURLToPath(dir)).filter((f) => f.endsWith(".json"));
  assert.ok(files.length >= 5);
  for (const f of files) {
    const json = JSON.parse(readFileSync(new URL(f, dir), "utf8"));
    assert.equal(structural(json), true, `${f}: ${JSON.stringify(structural.errors)}`);
    assert.deepEqual(validateProtocolEvidence(json), [], f);
  }
});
