import assert from "node:assert/strict";
import { test } from "node:test";
import { type Batch, NO_NODE, type NodeRec, nodeHash, type Op, Replica, sumAdd } from "@vsf/observer";

const facts = (tag: string, name: string | null = null) => ({
  tag,
  role: null,
  name,
  text: null,
  flags: 1,
  rels: [] as [],
});

function rec(id: number, parent: number, tag: string, name: string | null = null): NodeRec {
  return { id, parent, ...facts(tag, name), scope: null };
}

function batch(epoch: number, seq: number, kind: "full" | "delta", ops: Op[], tree: NodeRec[]): Batch {
  const sum = tree.reduce((s, r) => sumAdd(s, nodeHash(r)), 0);
  return { epoch, seq, kind, ops, sum, count: tree.length, truncated: false };
}

type AddOp = Extract<Op, { op: "add" }>;

const add = (id: number, parent: number, after: number, tag: string, name: string | null = null): AddOp => ({
  op: "add",
  id,
  parent,
  after,
  ...facts(tag, name),
});

function base(): { r: Replica; tree: NodeRec[] } {
  const tree = [rec(1, NO_NODE, "html"), rec(2, 1, "body"), rec(3, 2, "p", "one")];
  const r = new Replica();
  const res = r.apply(
    batch(
      1,
      0,
      "full",
      [add(1, NO_NODE, NO_NODE, "html"), add(2, 1, NO_NODE, "body"), add(3, 2, NO_NODE, "p", "one")],
      tree,
    ),
  );
  assert.equal(res.status, "ok");
  return { r, tree };
}

test("a full batch builds the tree and the order of siblings follows the after pointers", () => {
  const { r } = base();
  const tree = [
    rec(1, NO_NODE, "html"),
    rec(2, 1, "body"),
    rec(3, 2, "p", "one"),
    rec(4, 2, "p", "two"),
    rec(5, 2, "p", "zero"),
  ];
  const ops = [add(4, 2, 3, "p", "two"), add(5, 2, NO_NODE, "p", "zero")];
  assert.equal(r.apply(batch(1, 1, "delta", ops, tree)).status, "ok");
  const names = r.canonical()[0]?.[7][0]?.[7].map((n) => n[2]);
  assert.deepEqual(names, ["zero", "one", "two"]);
  assert.equal(r.seq, 1);
});

test("a skipped sequence number is a gap, changes nothing and a later replay repairs it", () => {
  const { r } = base();
  const t1 = [rec(1, NO_NODE, "html"), rec(2, 1, "body"), rec(3, 2, "p", "one"), rec(4, 2, "p", "two")];
  const t2 = [...t1, rec(5, 2, "p", "three")];
  const b1 = batch(1, 1, "delta", [add(4, 2, 3, "p", "two")], t1);
  const b2 = batch(1, 2, "delta", [add(5, 2, 4, "p", "three")], t2);
  const before = JSON.stringify(r.canonical());
  assert.deepEqual(r.apply(b2), { status: "gap", expected: 1, got: 2 });
  assert.equal(r.state, "gap");
  assert.equal(JSON.stringify(r.canonical()), before);
  assert.equal(r.apply(b1).status, "ok");
  assert.equal(r.apply(b2).status, "ok");
  assert.equal(r.state, "synced");
  assert.equal(r.size, 5);
});

test("a batch that fails midway is rolled back completely", () => {
  const { r } = base();
  const before = JSON.stringify(r.canonical());
  const bad = batch(1, 1, "delta", [add(4, 2, 3, "p", "two"), { op: "remove", id: 99 }], []);
  const res = r.apply(bad);
  assert.equal(res.status, "invalid");
  assert.equal(JSON.stringify(r.canonical()), before);
  assert.equal(r.size, 3);
  assert.equal(r.seq, 0);
  assert.equal(r.state, "diverged");
});

test("a digest mismatch is divergence even when every op applied cleanly", () => {
  const { r } = base();
  const wrong = batch(1, 1, "delta", [add(4, 2, 3, "p", "two")], [rec(1, NO_NODE, "html")]);
  assert.equal(r.apply(wrong).status, "diverged");
  assert.equal(r.size, 3);
  assert.equal(r.state, "diverged");
});

test("deltas need their own epoch and a newer full batch replaces everything", () => {
  const { r } = base();
  const t = [rec(1, NO_NODE, "html"), rec(2, 1, "body"), rec(3, 2, "p", "one"), rec(4, 2, "p", "two")];
  assert.deepEqual(r.apply(batch(2, 1, "delta", [add(4, 2, 3, "p", "two")], t)), {
    status: "epoch_mismatch",
    expected: 1,
    got: 2,
  });
  const fresh = [rec(7, NO_NODE, "html")];
  assert.equal(r.apply(batch(2, 0, "full", [add(7, NO_NODE, NO_NODE, "html")], fresh)).status, "ok");
  assert.equal(r.epoch, 2);
  assert.equal(r.size, 1);
  assert.equal(r.apply(batch(2, 0, "full", [add(7, NO_NODE, NO_NODE, "html")], fresh)).status, "epoch_mismatch");
  assert.equal(new Replica().apply(batch(1, 1, "delta", [], [])).status, "invalid");
});

test("removing a subtree drops every descendant and set rewrites only mutable fields", () => {
  const { r } = base();
  const t = [rec(1, NO_NODE, "html"), rec(2, 1, "body")];
  assert.equal(r.apply(batch(1, 1, "delta", [{ op: "remove", id: 3 }], t)).status, "ok");
  assert.equal(r.size, 2);
  const t2 = [rec(1, NO_NODE, "html"), { ...rec(2, 1, "body"), name: "main" }];
  const set: Op = { op: "set", id: 2, role: null, name: "main", text: null, flags: 1, rels: [] };
  assert.equal(r.apply(batch(1, 2, "delta", [set], t2)).status, "ok");
  assert.equal(r.nodes.get(2)?.name, "main");
  assert.equal(r.nodes.get(2)?.tag, "body");
});

test("canonical form is independent of node ids and relations point at positions", () => {
  const a = new Replica();
  const b = new Replica();
  const mk = (ids: number[]) => {
    const [h, p, q] = ids as [number, number, number];
    const tree: NodeRec[] = [
      rec(h, NO_NODE, "html"),
      { ...rec(p, h, "p", "x"), rels: [["labelledby", q]] },
      rec(q, h, "span", "y"),
    ];
    const ops: Op[] = [
      add(h, NO_NODE, NO_NODE, "html"),
      { ...add(p, h, NO_NODE, "p", "x"), rels: [["labelledby", q]] },
      add(q, h, p, "span", "y"),
    ];
    return batch(1, 0, "full", ops, tree);
  };
  assert.equal(a.apply(mk([1, 2, 3])).status, "ok");
  assert.equal(b.apply(mk([40, 11, 27])).status, "ok");
  assert.deepEqual(a.canonical(), b.canonical());
  assert.deepEqual(a.canonical()[0]?.[7][0]?.[5], [["labelledby", 2]]);
});

test("the pack snapshot keeps structure, drops relations and masks to the pack flag bits", () => {
  const { r } = base();
  const snap = r.toSnapshot();
  assert.equal(snap.nodes.length, 3);
  assert.deepEqual(
    snap.nodes.map((n) => n.parent),
    [0xffffffff, 0, 1],
  );
  assert.ok(snap.nodes.every((n) => n.flags <= 0b1111));
});
