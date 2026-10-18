import {
  FLAG_EDITABLE,
  FLAG_INTERACTIVE,
  FLAG_MASK,
  FLAG_REDACTED,
  FLAG_VISIBLE,
  NONE,
  type Snapshot,
} from "./pack.ts";

export const NO_NODE = -1;
export const FLAG_DISABLED = 16;
export const FLAG_OPAQUE = 32;
export const FLAG_RTL = 64;
export const FLAG_ACCOUNT = 128;
export const FLAG_CREDENTIAL = 256;
export { FLAG_EDITABLE, FLAG_INTERACTIVE, FLAG_REDACTED, FLAG_VISIBLE };

export type ScopeKind = "document" | "frame" | "shadow";

export type OpaqueReason =
  | "cross_origin_frame"
  | "sandboxed_frame"
  | "not_loaded"
  | "blocked_by_policy"
  | "closed_shadow_root"
  | "canvas_content"
  | "native_widget"
  | "limit_exceeded";

export interface ScopeMeta {
  kind: ScopeKind;
  origin: string | null;
  generation: number;
  mode?: "open" | "closed";
  opaque?: OpaqueReason;
}

export type RelKind = "labelledby" | "describedby" | "label";
export type Rel = readonly [kind: RelKind, target: number];

export interface NodeFacts {
  tag: string;
  role: string | null;
  name: string | null;
  text: string | null;
  flags: number;
  rels: Rel[];
}

export interface NodeRec extends NodeFacts {
  id: number;
  parent: number;
  scope: ScopeMeta | null;
}

export type Observed<T> = { state: "observed"; value: T } | { state: "unknown" };

export interface Viewport {
  width: number;
  height: number;
  dpr: number;
  bucket: "xs" | "sm" | "md" | "lg" | "xl";
}

export interface EnvironmentObservation {
  locale: Observed<string>;
  viewport: Observed<Viewport>;
  flags: Record<string, Observed<string>>;
}

export type Op =
  | ({ op: "add"; id: number; parent: number; after: number; scope?: ScopeMeta } & NodeFacts)
  | { op: "remove"; id: number }
  | ({ op: "set"; id: number } & Omit<NodeFacts, "tag">)
  | { op: "env"; env: EnvironmentObservation }
  | { op: "nav"; url: string };

export interface Batch {
  epoch: number;
  seq: number;
  kind: "full" | "delta";
  ops: Op[];
  /** Order-independent digest and node count of the whole tree after this batch. */
  sum: number;
  count: number;
  truncated: boolean;
}

const MOD = 2 ** 52;

function fnv(text: string, seed: number): number {
  let h = seed >>> 0;
  for (let i = 0; i < text.length; i++) {
    h ^= text.charCodeAt(i);
    h = Math.imul(h, 16777619) >>> 0;
  }
  return h;
}

/** 52-bit hash so sums of many nodes stay exact in a double. */
export function nodeHash(r: NodeRec): number {
  const key = `${r.id}|${r.parent}|${r.tag}|${r.role}|${r.name}|${r.text}|${r.flags}|${r.rels.map((x) => x.join(":")).join(",")}|${r.scope ? JSON.stringify(r.scope) : ""}`;
  const hi = fnv(key, 2166136261) & 0xfffff;
  return hi * 2 ** 32 + fnv(key, 0x9e3779b1);
}

export const sumAdd = (sum: number, h: number): number => (sum + h) % MOD;
export const sumSub = (sum: number, h: number): number => (sum - h + MOD) % MOD;

export type CanonNode = [
  tag: string,
  role: string | null,
  name: string | null,
  text: string | null,
  flags: number,
  rels: [string, number][],
  scope: ScopeMeta | null,
  children: CanonNode[],
];

export type ApplyResult =
  | { status: "ok" }
  | { status: "gap"; expected: number; got: number }
  | { status: "epoch_mismatch"; expected: number; got: number }
  | { status: "invalid"; reason: string }
  | { status: "diverged" };

/** Consumer-side mirror of the observed tree; a batch applies completely or not at all. */
export class Replica {
  epoch = -1;
  seq = -1;
  state: "empty" | "synced" | "gap" | "diverged" = "empty";
  truncated = false;
  env: EnvironmentObservation | null = null;
  url: string | null = null;
  readonly nodes = new Map<number, NodeRec>();
  readonly children = new Map<number, number[]>();
  readonly roots: number[] = [];
  private sum = 0;

  get size(): number {
    return this.nodes.size;
  }

  private kids(parent: number): number[] {
    if (parent === NO_NODE) return this.roots;
    let list = this.children.get(parent);
    if (!list) {
      list = [];
      this.children.set(parent, list);
    }
    return list;
  }

  private removeTree(id: number, undo: (() => void)[]): void {
    const stack = [id];
    const gone: NodeRec[] = [];
    while (stack.length > 0) {
      const cur = stack.pop() as number;
      const rec = this.nodes.get(cur);
      if (!rec) continue;
      gone.push(rec);
      for (const c of this.children.get(cur) ?? []) stack.push(c);
    }
    const root = this.nodes.get(id);
    if (!root) return;
    const siblings = this.kids(root.parent);
    const at = siblings.indexOf(id);
    siblings.splice(at, 1);
    const saved = new Map<number, number[] | undefined>();
    for (const rec of gone) {
      saved.set(rec.id, this.children.get(rec.id));
      this.children.delete(rec.id);
      this.nodes.delete(rec.id);
      this.sum = sumSub(this.sum, nodeHash(rec));
    }
    undo.push(() => {
      for (const rec of gone) {
        this.nodes.set(rec.id, rec);
        const k = saved.get(rec.id);
        if (k) this.children.set(rec.id, k);
        this.sum = sumAdd(this.sum, nodeHash(rec));
      }
      this.kids(root.parent).splice(at, 0, id);
    });
  }

  private applyOp(op: Op, undo: (() => void)[]): string | null {
    switch (op.op) {
      case "add": {
        if (this.nodes.has(op.id)) return `duplicate node ${op.id}`;
        if (op.parent !== NO_NODE && !this.nodes.has(op.parent)) return `unknown parent ${op.parent}`;
        const siblings = this.kids(op.parent);
        let at = 0;
        if (op.after !== NO_NODE) {
          const i = siblings.indexOf(op.after);
          if (i < 0) return `unknown sibling ${op.after}`;
          at = i + 1;
        }
        const rec: NodeRec = {
          id: op.id,
          parent: op.parent,
          tag: op.tag,
          role: op.role,
          name: op.name,
          text: op.text,
          flags: op.flags,
          rels: op.rels,
          scope: op.scope ?? null,
        };
        this.nodes.set(op.id, rec);
        siblings.splice(at, 0, op.id);
        this.sum = sumAdd(this.sum, nodeHash(rec));
        undo.push(() => {
          this.sum = sumSub(this.sum, nodeHash(rec));
          this.nodes.delete(op.id);
          this.children.delete(op.id);
          this.kids(op.parent).splice(at, 1);
        });
        return null;
      }
      case "remove": {
        if (!this.nodes.has(op.id)) return `unknown node ${op.id}`;
        this.removeTree(op.id, undo);
        return null;
      }
      case "set": {
        const rec = this.nodes.get(op.id);
        if (!rec) return `unknown node ${op.id}`;
        const prev = { ...rec };
        this.sum = sumSub(this.sum, nodeHash(rec));
        rec.role = op.role;
        rec.name = op.name;
        rec.text = op.text;
        rec.flags = op.flags;
        rec.rels = op.rels;
        this.sum = sumAdd(this.sum, nodeHash(rec));
        undo.push(() => {
          this.sum = sumSub(this.sum, nodeHash(rec));
          Object.assign(rec, prev);
          this.sum = sumAdd(this.sum, nodeHash(rec));
        });
        return null;
      }
      case "env": {
        const prev = this.env;
        this.env = op.env;
        undo.push(() => {
          this.env = prev;
        });
        return null;
      }
      case "nav": {
        const prev = this.url;
        this.url = op.url;
        undo.push(() => {
          this.url = prev;
        });
        return null;
      }
    }
  }

  apply(batch: Batch): ApplyResult {
    if (batch.kind === "full") {
      if (batch.seq !== 0) return { status: "invalid", reason: "full batch must have seq 0" };
      if (batch.epoch <= this.epoch) return { status: "epoch_mismatch", expected: this.epoch + 1, got: batch.epoch };
      this.reset();
    } else {
      if (this.state === "empty") return { status: "invalid", reason: "delta before any full batch" };
      if (batch.epoch !== this.epoch) return { status: "epoch_mismatch", expected: this.epoch, got: batch.epoch };
      if (batch.seq !== this.seq + 1) {
        this.state = "gap";
        return { status: "gap", expected: this.seq + 1, got: batch.seq };
      }
    }
    const undo: (() => void)[] = [];
    const before = { url: this.url, env: this.env };
    for (const op of batch.ops) {
      const problem = this.applyOp(op, undo);
      if (problem !== null) {
        if (batch.kind === "delta") for (const u of undo.reverse()) u();
        this.state = batch.kind === "delta" ? "diverged" : "empty";
        return { status: "invalid", reason: problem };
      }
    }
    if (this.sum !== batch.sum || this.nodes.size !== batch.count) {
      if (batch.kind === "delta") {
        for (const u of undo.reverse()) u();
        this.url = before.url;
        this.env = before.env;
      }
      this.state = "diverged";
      return { status: "diverged" };
    }
    this.epoch = batch.epoch;
    this.seq = batch.seq;
    this.truncated = batch.truncated;
    this.state = "synced";
    return { status: "ok" };
  }

  private reset(): void {
    this.nodes.clear();
    this.children.clear();
    this.roots.length = 0;
    this.sum = 0;
    this.env = null;
    this.url = null;
  }

  /** Id-free structure: relations point at preorder positions so independent captures compare equal. */
  canonical(): CanonNode[] {
    const order = new Map<number, number>();
    const walk = (ids: number[]): void => {
      for (const id of ids) {
        order.set(id, order.size);
        walk(this.children.get(id) ?? []);
      }
    };
    walk(this.roots);
    const build = (id: number): CanonNode => {
      const r = this.nodes.get(id) as NodeRec;
      return [
        r.tag,
        r.role,
        r.name,
        r.text,
        r.flags,
        r.rels.map(([k, t]): [string, number] => [k, order.get(t) ?? -1]),
        r.scope,
        (this.children.get(id) ?? []).map(build),
      ];
    };
    return this.roots.map(build);
  }

  /** The accessible tree as a VSFS pack snapshot; opaque placeholders are kept as tagged nodes. */
  toSnapshot(): Snapshot {
    const strings: string[] = [];
    const index = new Map<string, number>();
    const intern = (s: string | null): number => {
      if (s === null) return NONE;
      let i = index.get(s);
      if (i === undefined) {
        i = strings.length;
        strings.push(s);
        index.set(s, i);
      }
      return i;
    };
    const nodes: Snapshot["nodes"] = [];
    const walk = (ids: number[], parent: number): void => {
      for (const id of ids) {
        const r = this.nodes.get(id) as NodeRec;
        nodes.push({
          parent,
          tag: intern(r.tag),
          role: intern(r.role),
          name: intern(r.name),
          text: intern(r.text),
          flags: r.flags & FLAG_MASK,
        });
        walk(this.children.get(id) ?? [], nodes.length - 1);
      }
    };
    walk(this.roots, NONE);
    return { strings, nodes };
  }
}
