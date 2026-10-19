import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import { observedTrafficEvidence, shapeOfJson, templatizePath, validateProtocolEvidence } from "@vsf/observer";
import { Ajv } from "ajv";

const dir = new URL("../../../schemas/protocol-evidence/", import.meta.url);
const schema = JSON.parse(readFileSync(new URL("v1.schema.json", dir), "utf8"));
const ajv = new Ajv({ allErrors: true, strict: false });
const structural = ajv.compile(schema);
const PLACEHOLDERS: Record<string, string> = {
  "{{AWS_KEY}}": ["AKIA", "IOSFODNN7EXAMPLE"].join(""),
  "{{USERINFO}}": ["user", ":pw", "@"].join(""),
};
const expand = (text: string): string => Object.entries(PLACEHOLDERS).reduce((s, [k, v]) => s.replaceAll(k, v), text);
const load = (sub: string) =>
  readdirSync(fileURLToPath(new URL(`fixtures/${sub}/`, dir)))
    .sort()
    .map((f) => ({
      name: f.replace(/\.json$/, ""),
      json: JSON.parse(expand(readFileSync(new URL(`fixtures/${sub}/${f}`, dir), "utf8"))),
    }));

test("every valid fixture passes the schema and the promotion rules", () => {
  const all = load("valid");
  assert.ok(all.length >= 5);
  for (const f of all) {
    assert.equal(structural(f.json), true, `${f.name}: ${JSON.stringify(structural.errors)}`);
    assert.deepEqual(validateProtocolEvidence(f.json), [], f.name);
  }
});

test("every invalid fixture is rejected with exactly the expected reason", () => {
  const all = load("invalid");
  assert.ok(all.length >= 19);
  for (const f of all) {
    const got = [...new Set(validateProtocolEvidence(f.json.doc).map((p) => p.code))].sort();
    assert.deepEqual(got, [...f.json.expect].sort(), f.name);
  }
});

test("the schema file and the hand-written structural checks agree on every fixture", () => {
  const verdict = (doc: unknown) => {
    const codes = validateProtocolEvidence(doc).map((p) => p.code);
    return codes.every((c) => c.startsWith("schema."));
  };
  for (const f of load("valid")) assert.equal(structural(f.json), verdict(f.json), f.name);
  for (const f of load("invalid")) {
    const semanticOnly = f.json.expect.every((c: string) => !c.startsWith("schema."));
    if (semanticOnly) assert.equal(structural(f.json.doc), true, `${f.name} should be structurally valid`);
    else assert.equal(structural(f.json.doc), false, `${f.name} should fail the schema`);
  }
});

test("undocumented traffic is never supported by default, whatever else is claimed", () => {
  const ev = observedTrafficEvidence({
    sourceAppId: "acme:crm-eu1",
    origin: "https://crm.example.com",
    method: "GET",
    url: "https://crm.example.com/v1/orders/48151623?token=abc123&page=2",
    responseJson: { id: "o-1", total: 12.5, "alice@example.com": 1 },
    principalRef: `prin_${"ab".repeat(16)}`,
    scope: "observe.protocol",
    grantedAt: 1,
    recordedAt: 2,
    idHex: "0123456789abcdef0123",
  });
  assert.equal(ev.support, "unsupported");
  assert.equal(ev.stability, "undocumented");
  assert.equal(ev.endpoint.path_template, "/v1/orders/{id}");
  assert.deepEqual(validateProtocolEvidence(ev), []);
  assert.ok(!JSON.stringify(ev).includes("abc123"));
  assert.ok(!JSON.stringify(ev).includes("alice@example.com"));
  assert.deepEqual(
    ev.shape.response.map((f) => f.name),
    ["id", "total"],
  );
  assert.ok(
    validateProtocolEvidence({ ...ev, support: "supported" }).some((p) => p.code === "evidence.undocumented_supported"),
  );
  assert.ok(validateProtocolEvidence({ ...ev, support: "candidate" }).length === 0);
});

test("the factory refuses a request that carries credentials or crosses origins", () => {
  const base = {
    sourceAppId: "acme:crm-eu1",
    origin: "https://crm.example.com",
    method: "GET" as const,
    principalRef: `prin_${"ab".repeat(16)}`,
    scope: "observe.protocol",
    grantedAt: 1,
    recordedAt: 2,
    idHex: "0123456789abcdef0123",
  };
  assert.throws(() =>
    observedTrafficEvidence({ ...base, url: `${["https://u", ":p@"].join("")}crm.example.com/v1/x` }),
  );
  assert.throws(() => observedTrafficEvidence({ ...base, url: "https://other.example.com/v1/x" }));
});

test("path templating replaces identifiers and nothing else", () => {
  assert.equal(templatizePath("/v1/orders/48151623/items/7"), "/v1/orders/{id}/items/{id}");
  assert.equal(templatizePath("/v1/orders/123e4567-e89b-12d3-a456-426614174000"), "/v1/orders/{id}");
  assert.equal(templatizePath("/v1/orders/{order_id}/lines"), "/v1/orders/{order_id}/lines");
  assert.equal(templatizePath("/v1/ping"), "/v1/ping");
  assert.deepEqual(shapeOfJson([1, 2]), []);
  assert.deepEqual(
    shapeOfJson({ b: null, a: [1], "bad key": 1, ok_1: true }).map((f) => `${f.name}:${f.type}`),
    ["a:array", "b:null", "ok_1:boolean"],
  );
});

test("anything that is not an evidence object is invalid, never silently accepted", () => {
  for (const v of [undefined, null, 0, "x", [], true]) {
    assert.ok(validateProtocolEvidence(v).length > 0, String(v));
  }
});
