import assert from "node:assert/strict";
import { test } from "node:test";
import type { Locked } from "../src/depreview.ts";
import { applyIgnores, buildQueries, requestDigest, scanVulnerabilities } from "../src/osv.ts";

const locked: Locked[] = [
  { ecosystem: "cargo", name: "serde", version: "1.0.200" },
  { ecosystem: "npm", name: "left-pad", version: "1.3.0" },
  { ecosystem: "go", name: "golang.org/x/text", version: "0.3.6" },
  { ecosystem: "cargo", name: "serde", version: "1.0.200" },
];

const reply =
  (results: unknown, status = 200) =>
  async () =>
    new Response(JSON.stringify({ results }), { status, headers: { "content-type": "application/json" } });
const fast = { sleep: async () => {}, backoffMs: 0 };

test("the request is deterministic, deduplicated and ecosystem-mapped", () => {
  const q = buildQueries(locked);
  assert.equal(q.length, 3);
  assert.deepEqual(
    q.map((x) => x.package.ecosystem),
    ["Go", "crates.io", "npm"],
  );
  assert.equal(requestDigest(q), requestDigest(buildQueries([...locked].reverse())));
});

test("answers for every query and no vulnerabilities is clean", async () => {
  const r = await scanVulnerabilities(locked, { ...fast, fetchImpl: reply([{}, {}, {}]) });
  assert.equal(r.status, "clean");
  assert.equal(r.queried, 3);
});

test("a reported vulnerability is a finding naming the package", async () => {
  const r = await scanVulnerabilities(locked, {
    ...fast,
    fetchImpl: reply([{ vulns: [{ id: "GO-1" }] }, {}, { vulns: [{ id: "GHSA-b" }, { id: "GHSA-a" }] }]),
  });
  assert.equal(r.status, "findings");
  assert.deepEqual(
    r.findings.map((f) => [f.package, f.ids]),
    [
      ["Go:golang.org/x/text@0.3.6", ["GO-1"]],
      ["npm:left-pad@1.3.0", ["GHSA-a", "GHSA-b"]],
    ],
  );
});

test("network failure, timeout, server errors and malformed answers are unavailable, never clean", async () => {
  const boom = async () => {
    throw new Error("offline");
  };
  for (const fetchImpl of [boom, reply([], 503), reply([{}, {}]), reply("nope"), reply([], 400)]) {
    const r = await scanVulnerabilities(locked, { ...fast, fetchImpl: fetchImpl as typeof fetch });
    assert.equal(r.status, "unavailable");
  }
  const none = await scanVulnerabilities([], fast);
  assert.equal(none.status, "unavailable");
});

test("retries are bounded and a transient failure then success is clean", async () => {
  let calls = 0;
  const flaky = async () => {
    calls++;
    return calls < 3 ? new Response("", { status: 502 }) : new Response(JSON.stringify({ results: [{}, {}, {}] }));
  };
  const ok = await scanVulnerabilities(locked, { ...fast, fetchImpl: flaky as typeof fetch });
  assert.equal(ok.status, "clean");
  assert.equal(calls, 3);
  calls = 0;
  const dead = async () => {
    calls++;
    return new Response("", { status: 500 });
  };
  const bad = await scanVulnerabilities(locked, { ...fast, attempts: 4, fetchImpl: dead as typeof fetch });
  assert.equal(bad.status, "unavailable");
  assert.equal(calls, 4);
});

test("large lockfiles are sent in bounded batches and one failed batch makes the whole scan unavailable", async () => {
  const many: Locked[] = Array.from({ length: 1201 }, (_, i) => ({
    ecosystem: "npm",
    name: `p${i}`,
    version: "1.0.0",
  }));
  const sizes: number[] = [];
  const echo = async (_u: unknown, init?: RequestInit) => {
    const { queries } = JSON.parse(String(init?.body)) as { queries: unknown[] };
    sizes.push(queries.length);
    return new Response(JSON.stringify({ results: queries.map(() => ({})) }));
  };
  const r = await scanVulnerabilities(many, { ...fast, fetchImpl: echo as typeof fetch });
  assert.equal(r.status, "clean");
  assert.deepEqual(sizes, [500, 500, 201]);
  let n = 0;
  const secondFails = async (u: unknown, init?: RequestInit) =>
    ++n === 2 ? new Response("", { status: 500 }) : echo(u, init);
  const partial = await scanVulnerabilities(many, { ...fast, attempts: 1, fetchImpl: secondFails as typeof fetch });
  assert.equal(partial.status, "unavailable");
});

test("ignores need an unexpired date; an expired one protects nothing", () => {
  const raw = [{ package: "npm:x@1", ids: ["A", "B"] }];
  const now = new Date("2026-06-01T00:00:00Z");
  const live = applyIgnores(raw, [{ id: "A", reason: "reviewed", expires: "2026-12-31" }], now);
  assert.deepEqual(live.findings, [{ package: "npm:x@1", ids: ["B"] }]);
  const stale = applyIgnores(raw, [{ id: "A", reason: "reviewed", expires: "2026-01-01" }], now);
  assert.deepEqual(stale.findings, raw);
  assert.equal(stale.expired.length, 1);
});
