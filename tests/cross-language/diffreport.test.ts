import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import { Ajv } from "ajv";

const root = new URL("../../", import.meta.url);
const path = (rel: string): string => fileURLToPath(new URL(rel, root));

interface Change {
  path: string;
  class: string;
  review: boolean;
}
interface Report {
  class: string | null;
  review_required: boolean;
  summary: Record<string, number>;
  changes: Change[];
  selection?: { mode: string; reason: string | null; tests: string[] };
}

const schema = JSON.parse(readFileSync(path("schemas/versioning/diff-report.v1.schema.json"), "utf8"));
const dir = path("fixtures/versions/diff");
const reports = readdirSync(dir).map((f) => ({ f, v: JSON.parse(readFileSync(`${dir}/${f}`, "utf8")) as Report }));

test("every committed diff report satisfies the published schema", () => {
  const validate = new Ajv({ allErrors: true, strict: false }).compile(schema);
  assert.ok(reports.length >= 3);
  for (const { f, v } of reports) assert.ok(validate(v), `${f}: ${JSON.stringify(validate.errors)}`);
});

test("a consumer can summarize and gate a report without reading prose", () => {
  for (const { f, v } of reports) {
    const counted: Record<string, number> = {};
    for (const c of v.changes) counted[c.class] = (counted[c.class] ?? 0) + 1;
    for (const [k, n] of Object.entries(v.summary)) assert.equal(counted[k] ?? 0, n, `${f}: ${k}`);
    assert.equal(
      v.review_required,
      v.changes.some((c) => c.review),
      f,
    );
    const paths = v.changes.map((c) => c.path);
    assert.deepEqual(paths, [...paths].sort(), `${f}: changes are not in path order`);
  }
});

test("a report the schema should refuse is refused", () => {
  const validate = new Ajv({ allErrors: true, strict: false }).compile(schema);
  const base = structuredClone(reports[0]?.v) as unknown as Record<string, unknown>;
  assert.equal(validate({ ...base, class: "mostly-fine" }), false);
  assert.equal(validate({ ...base, extra: true }), false);
  assert.equal(validate({ ...base, from: "not-a-digest" }), false);
  const { summary: _drop, ...noSummary } = base;
  assert.equal(validate(noSummary), false);
});
