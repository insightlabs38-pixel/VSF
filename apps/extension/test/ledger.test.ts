import assert from "node:assert/strict";
import { test } from "node:test";
import { Ledger, type Op, type Store } from "../src/ledger.ts";

function memory(failSetsAfter = Number.POSITIVE_INFINITY): Store & { data: Record<string, Op>; sets: number } {
  const s = {
    data: {} as Record<string, Op>,
    sets: 0,
    async get() {
      return structuredClone(s.data);
    },
    async set(ops: Record<string, Op>) {
      if (s.sets >= failSetsAfter) throw new Error("storage failure");
      s.sets++;
      s.data = structuredClone(ops);
    },
  };
  return s;
}

const id = (n: number): string => `req-${String(n).padStart(8, "0")}`;

test("a new request id is fresh once and answered from the record afterwards", async () => {
  const l = new Ledger(memory(), () => 5);
  assert.deepEqual(await l.begin(id(1), 3, "set_stage"), { fresh: true });
  const again = await l.begin(id(1), 3, "set_stage");
  assert.deepEqual(again, { fresh: false, op: { state: "started", tab: 3, action: "set_stage", at: 5 } });
  assert.equal((await l.finish(id(1), { ok: true, message: "done" }))?.state, "done");
  const replay = await l.begin(id(1), 3, "set_stage");
  assert.ok(!replay.fresh && replay.op.state === "done" && replay.op.result?.message === "done");
});

test("an operation that lost its answer is unknown and stays unknown", async () => {
  const l = new Ledger(memory());
  await l.begin(id(1), 1, "a");
  assert.equal((await l.lose(id(1), "timeout"))?.state, "unknown");
  assert.equal((await l.finish(id(1), { ok: true }))?.state, "unknown");
  const again = await l.begin(id(1), 1, "a");
  assert.ok(!again.fresh && again.op.state === "unknown" && again.op.reason === "timeout");
});

test("a late answer cannot rewrite an outcome that was already settled", async () => {
  const l = new Ledger(memory());
  await l.begin(id(1), 1, "a");
  await l.finish(id(1), { ok: false, message: "refused" });
  assert.equal((await l.lose(id(1), "late"))?.state, "done");
  assert.equal((await l.finish(id(1), { ok: true }))?.result?.ok, false);
  assert.equal(await l.finish(id(9), { ok: true }), undefined);
});

test("a worker restart turns every in-flight request into unknown and touches nothing else", async () => {
  const m = memory();
  const l = new Ledger(m);
  await l.begin(id(1), 1, "a");
  await l.begin(id(2), 1, "b");
  await l.finish(id(2), { ok: true });
  await l.begin(id(3), 1, "c");
  await l.lose(id(3), "network");
  assert.deepEqual(await l.reconcile(), [id(1)]);
  assert.deepEqual(Object.fromEntries(Object.entries(m.data).map(([k, v]) => [k, v.state])), {
    [id(1)]: "unknown",
    [id(2)]: "done",
    [id(3)]: "unknown",
  });
  assert.equal(m.data[id(1)]?.reason, "worker_restarted");
  assert.deepEqual(await l.reconcile(), []);
});

test("if the record cannot be written the request is refused before it could be forwarded", async () => {
  const m = memory(0);
  const l = new Ledger(m);
  await assert.rejects(l.begin(id(1), 1, "a"), /storage failure/);
  assert.deepEqual(m.data, {});
});

test("full ledgers drop finished entries first and never an open one", async () => {
  const m = memory();
  const l = new Ledger(m);
  for (let i = 0; i < 200; i++) {
    await l.begin(id(i), 1, "a");
    if (i % 2 === 0) await l.finish(id(i), { ok: true });
  }
  for (let i = 200; i < 260; i++) await l.begin(id(i), 1, "a");
  const states = Object.values(m.data).map((o) => o.state);
  assert.ok(Object.keys(m.data).length <= 260);
  assert.equal(states.filter((s) => s === "started").length, 100 + 60);
  assert.ok(!(id(0) in m.data), "oldest finished entry evicted");
  assert.ok(id(1) in m.data, "open entry kept");
});
