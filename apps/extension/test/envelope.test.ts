import assert from "node:assert/strict";
import { test } from "node:test";
import { Chain, canonicalJson, ENVELOPE_VERSION, type Envelope, MAX_BYTES, parseEnvelope } from "../src/envelope.ts";

const HEX = "ab".repeat(32);
const source = { tab: 7, frame: 0, origin: "http://127.0.0.1:9", epoch: "epoch-00000001" };
const expect = { ...source };

const observation = (over: Partial<Envelope> = {}): Envelope => ({
  v: ENVELOPE_VERSION,
  kind: "observation",
  id: "obs-00000001",
  parent: null,
  seq: 1,
  source,
  body: { pack: "AAAA", digest: HEX, bytes: 3, nodes: 4, truncated: false },
  ...over,
});
const compiled = (over: Partial<Envelope> = {}): Envelope => ({
  v: ENVELOPE_VERSION,
  kind: "compiled",
  id: "cmp-00000001",
  parent: "obs-00000001",
  seq: 2,
  source,
  body: { pack_digest: HEX, node_count: 4, classes: { button: 1 }, app: "forms-order", match: "matched" },
  ...over,
});
const render = (over: Partial<Envelope> = {}): Envelope => ({
  v: ENVELOPE_VERSION,
  kind: "render",
  id: "rnd-00000001",
  parent: "cmp-00000001",
  seq: 3,
  source,
  body: { app: "forms-order", bundle_digest: HEX, state: "ready", bundle: { id: "forms-order" } },
  ...over,
});

const code = (r: ReturnType<typeof parseEnvelope>): string => (r.ok ? "ok" : r.code);

test("well-formed envelopes of every stage are accepted", () => {
  for (const e of [observation(), compiled(), render()]) assert.equal(code(parseEnvelope(e)), "ok", e.kind);
  const degraded = render({ body: { app: "none", bundle_digest: HEX, state: "source_ui", reason: "no bundle" } });
  assert.equal(code(parseEnvelope(degraded)), "ok");
});

test("shape, version, kind and size are checked before anything else", () => {
  const cases: [string, unknown, string][] = [
    ["not an object", "x", "envelope.malformed"],
    ["array", [], "envelope.malformed"],
    ["extra key", { ...observation(), extra: 1 }, "envelope.malformed"],
    ["missing key", { ...observation(), seq: undefined }, "envelope.malformed"],
    ["future version", { ...observation(), v: 2 }, "envelope.version"],
    ["unknown kind", { ...observation(), kind: "dispatch" }, "envelope.kind"],
    ["bad id", observation({ id: "UPPER_CASE!" }), "envelope.id"],
    ["observation with parent", observation({ parent: "obs-00000009" }), "envelope.parent"],
    ["compiled without parent", compiled({ parent: null }), "envelope.parent"],
    ["zero seq", observation({ seq: 0 }), "envelope.malformed"],
    ["fractional seq", observation({ seq: 1.5 }), "envelope.malformed"],
    ["negative tab", observation({ source: { ...source, tab: -1 } }), "envelope.source"],
    ["source extra key", observation({ source: { ...source, extra: 1 } as never }), "envelope.source"],
    ["long origin", observation({ source: { ...source, origin: `http://${"a".repeat(400)}` } }), "envelope.source"],
    ["bad epoch", observation({ source: { ...source, epoch: "no" } }), "envelope.source"],
  ];
  for (const [name, input, want] of cases) assert.equal(code(parseEnvelope(input)), want, name);
});

test("bodies are validated per stage with closed shapes", () => {
  const obs = (body: Record<string, unknown>) => observation({ body: { ...observation().body, ...body } });
  assert.equal(code(parseEnvelope(obs({ pack: "not base64!" }))), "envelope.body");
  assert.equal(code(parseEnvelope(obs({ digest: "short" }))), "envelope.body");
  assert.equal(code(parseEnvelope(obs({ nodes: 1e9 }))), "envelope.body");
  assert.equal(code(parseEnvelope(obs({ extra: true }))), "envelope.body");
  const cmp = (body: Record<string, unknown>) => compiled({ body: { ...compiled().body, ...body } });
  assert.equal(code(parseEnvelope(cmp({ match: "maybe" }))), "envelope.body");
  assert.equal(code(parseEnvelope(cmp({ classes: { button: -1 } }))), "envelope.body");
  assert.equal(code(parseEnvelope(cmp({ app: "Bad App" }))), "envelope.body");
  const rnd = (body: Record<string, unknown>) => render({ body: { ...render().body, ...body } });
  assert.equal(code(parseEnvelope(rnd({ state: "ready", bundle: undefined }))), "envelope.body");
  assert.equal(code(parseEnvelope(rnd({ state: "source_ui" }))), "envelope.body");
  assert.equal(code(parseEnvelope(rnd({ state: "hidden" }))), "envelope.body");
  assert.equal(code(parseEnvelope(rnd({ reason: "x".repeat(121) }))), "envelope.body");
});

test("each stage has its own byte bound", () => {
  const big = (kind: "observation" | "compiled" | "render"): Envelope => {
    const e = { observation, compiled, render }[kind]();
    return { ...e, body: { ...e.body, pad: "x".repeat(MAX_BYTES[kind]) } };
  };
  for (const kind of ["observation", "compiled", "render"] as const)
    assert.equal(code(parseEnvelope(big(kind))), "envelope.too_large", kind);
  assert.ok(MAX_BYTES.compiled < MAX_BYTES.render && MAX_BYTES.render < MAX_BYTES.observation);
});

test("a stream accepts the stages in order exactly once", () => {
  const c = new Chain();
  for (const e of [observation(), compiled(), render()]) assert.equal(code(c.accept(e, expect)), "ok", e.kind);
});

test("replays, reordering and parent forgery are refused", () => {
  const c = new Chain();
  assert.equal(code(c.accept(observation(), expect)), "ok");
  assert.equal(code(c.accept(observation(), expect)), "envelope.replay");
  assert.equal(code(c.accept(observation({ id: "obs-00000002", seq: 1 }), expect)), "envelope.replay");
  assert.equal(code(c.accept(compiled({ parent: "obs-00000099" }), expect)), "envelope.parent");
  assert.equal(code(c.accept(render({ parent: "obs-00000001" }), expect)), "envelope.stage_order");
  assert.equal(code(c.accept(compiled(), expect)), "ok");
  assert.equal(code(c.accept(compiled({ id: "cmp-00000002", seq: 3 }), expect)), "envelope.stage_order");
  assert.equal(code(c.accept(render(), expect)), "ok");
  assert.equal(code(c.accept(render({ id: "rnd-00000002", seq: 4 }), expect)), "envelope.stage_order");
});

test("source identity must match tab, frame, origin and epoch", () => {
  const stale = new Chain();
  assert.equal(
    code(stale.accept(observation({ source: { ...source, epoch: "epoch-00000002" } }), expect)),
    "envelope.stale_epoch",
  );
  for (const [name, s] of [
    ["tab", { ...source, tab: 8 }],
    ["frame", { ...source, frame: 1 }],
    ["origin", { ...source, origin: "http://evil.test" }],
  ] as const)
    assert.equal(code(new Chain().accept(observation({ source: s }), expect)), "envelope.source_mismatch", name);
});

test("an ended session accepts nothing more, even a fresh sequence number", () => {
  const c = new Chain();
  assert.equal(code(c.accept(observation(), expect)), "ok");
  c.end(expect.tab, expect.epoch);
  assert.equal(code(c.accept(observation({ id: "obs-00000003", seq: 50 }), expect)), "envelope.stale_epoch");
});

test("a receiver told about the compiled stage out of band can accept the render stage alone", () => {
  const c = new Chain();
  assert.equal(code(c.accept(render(), expect)), "envelope.parent");
  c.seed(expect.tab, expect.epoch, "cmp-00000001", "compiled", 2);
  assert.equal(code(c.accept(render(), expect)), "ok");
});

test("canonical JSON is key-order independent", () => {
  assert.equal(canonicalJson({ b: 1, a: [{ d: 2, c: 3 }] }), canonicalJson({ a: [{ c: 3, d: 2 }], b: 1 }));
  assert.equal(canonicalJson({ a: 1, b: 2 }), '{"a":1,"b":2}');
});
