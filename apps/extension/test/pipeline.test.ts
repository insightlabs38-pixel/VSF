import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { test } from "node:test";
import { BUNDLES, bundleDigest, bundleFor, mismatch } from "../src/bundles.ts";
import { ENVELOPE_VERSION, type Envelope } from "../src/envelope.ts";
import { type Deps, PipelineError, runStages } from "../src/pipeline.ts";

const source = { tab: 3, frame: 0, origin: "http://127.0.0.1:9", epoch: "epoch-00000001" };
const hex = (b: Uint8Array): string => createHash("sha256").update(b).digest("hex");

function observed(pack: Uint8Array, over: Record<string, unknown> = {}): Envelope {
  return {
    v: ENVELOPE_VERSION,
    kind: "observation",
    id: "obs-00000001",
    parent: null,
    seq: 1,
    source,
    body: {
      pack: Buffer.from(pack).toString("base64"),
      digest: hex(pack),
      bytes: pack.length,
      nodes: 9,
      truncated: false,
      ...over,
    },
  };
}

let n = 0;
const deps = (classes: Record<string, number>): Deps => ({
  analyze: (pack) => ({ digest: hex(pack), node_count: 9, classes }),
  newId: () => `id-${String(++n).padStart(8, "0")}`,
});

const formsClasses = { textbox: 3, button: 1, form: 1, heading: 1, other: 4 };
const pack = Uint8Array.from([1, 2, 3, 4]);

test("a page that matches its bundle is compiled and rendered from that bundle", async () => {
  const s = await runStages(observed(pack), "/forms/", deps(formsClasses));
  assert.equal(s.compiled.parent, "obs-00000001");
  assert.equal(s.render.parent, s.compiled.id);
  assert.deepEqual([s.compiled.seq, s.render.seq], [2, 3]);
  assert.deepEqual(s.compiled.source, source);
  assert.equal((s.compiled.body as { match: string }).match, "matched");
  const body = s.render.body as { state: string; app: string; bundle_digest: string };
  assert.deepEqual([body.state, body.app], ["ready", "forms-order"]);
  assert.equal(body.bundle_digest, await bundleDigest(s.bundle as never));
});

test("a page that no longer looks like its bundle keeps the source UI and says why", async () => {
  const s = await runStages(observed(pack), "/forms/", deps({ button: 1 }));
  assert.equal((s.compiled.body as { match: string }).match, "mismatch");
  const body = s.render.body as { state: string; reason: string; bundle?: unknown };
  assert.equal(body.state, "source_ui");
  assert.match(body.reason, /expected at least 3 textbox, saw 0/);
  assert.equal(body.bundle, undefined);
});

test("a page without a bundle is left alone", async () => {
  const s = await runStages(observed(pack), "/somewhere-else/", deps(formsClasses));
  assert.equal((s.compiled.body as { app: unknown }).app, null);
  assert.equal((s.render.body as { state: string }).state, "source_ui");
});

test("a pack that does not match its declared digest or size is refused", async () => {
  await assert.rejects(runStages(observed(pack, { digest: "00".repeat(32) }), "/forms/", deps(formsClasses)), (e) => {
    assert.ok(e instanceof PipelineError);
    return e.code === "envelope.digest";
  });
  await assert.rejects(runStages(observed(pack, { bytes: 99 }), "/forms/", deps(formsClasses)), /envelope.digest/);
  const lying: Deps = {
    ...deps(formsClasses),
    analyze: () => ({ digest: "00".repeat(32), node_count: 1, classes: {} }),
  };
  await assert.rejects(runStages(observed(pack), "/forms/", lying), /envelope.digest/);
});

test("bundles match by exact path or prefix and carry only data", async () => {
  assert.equal(bundleFor("/forms/")?.id, "forms-order");
  assert.equal(bundleFor("/ui/orders/o1")?.id, "orders-stage");
  assert.equal(bundleFor("/ui/other/o1"), undefined);
  assert.equal(mismatch(BUNDLES[0] as never, formsClasses), null);
  for (const b of BUNDLES) {
    const text = JSON.stringify(b);
    assert.doesNotMatch(text, /function|=>|eval|<script|javascript:/i, b.id);
    assert.match(await bundleDigest(b), /^[0-9a-f]{64}$/);
  }
});

test("a bundle's digest changes with any edit and not with key order", async () => {
  const b = BUNDLES[0] as never as { title: string };
  const before = await bundleDigest(BUNDLES[0] as never);
  const edited = { ...(BUNDLES[0] as object), title: "Changed" } as never;
  assert.notEqual(await bundleDigest(edited), before);
  const reordered = Object.fromEntries(Object.entries(BUNDLES[0] as object).reverse()) as never;
  assert.equal(await bundleDigest(reordered), before);
  void b;
});
