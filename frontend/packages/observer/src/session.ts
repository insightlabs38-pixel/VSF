import {
  type CaptureOptions,
  type DescribeCtx,
  describeElement,
  isLeaf,
  NAME_FROM_CONTENT,
  probeGeometry,
  SKIP_TAGS,
} from "./capture.ts";
import { type FlagProbe, observeEnvironment } from "./environment.ts";
import {
  type Batch,
  type EnvironmentObservation,
  FLAG_DISABLED,
  FLAG_OPAQUE,
  FLAG_VISIBLE,
  NO_NODE,
  type NodeFacts,
  type NodeRec,
  nodeHash,
  type Op,
  type ScopeKind,
  type ScopeMeta,
  sumAdd,
  sumSub,
} from "./model.ts";
import {
  childScopes,
  DEFAULT_SCOPE_LIMITS,
  type ObservationPolicy,
  type RuntimeCaps,
  type ScopeLimits,
  type ScopeStats,
} from "./scope.ts";
import type { Diagnostics } from "./sensitive.ts";

export interface Limits extends ScopeLimits {
  maxNodes: number;
  maxTextChars: number;
  ringBatches: number;
}

export const DEFAULT_LIMITS: Limits = {
  ...DEFAULT_SCOPE_LIMITS,
  maxNodes: 100_000,
  maxTextChars: 256,
  ringBatches: 64,
};

export interface SessionConfig {
  doc: Document;
  policy: ObservationPolicy;
  sink: (batch: Batch) => void;
  /** Allocates the epoch for each new baseline; the collector owns the counter across reloads. */
  nextEpoch: () => number;
  caps?: RuntimeCaps;
  limits?: Partial<Limits>;
  flagProbes?: readonly FlagProbe[];
  diagnostics?: Diagnostics;
  /** Quiet period before queued mutations are published; null publishes only on flush(). */
  batchMs?: number | null;
}

export class ObservationBlocked extends Error {
  readonly code: "origin_not_granted" | "no_document";
  constructor(code: ObservationBlocked["code"]) {
    super(code);
    this.name = "ObservationBlocked";
    this.code = code;
  }
}

export interface SessionStats {
  described: number;
  scannedSiblings: number;
  batches: number;
  ops: number;
  fullSyncs: number;
  subtreeSyncs: number;
  replays: number;
  scopes: ScopeStats;
}

export type ResyncRequest = { kind: "full" } | { kind: "subtree"; id: number } | { kind: "replay"; from: number };

interface Live {
  id: number;
  node: Node | null;
  rec: NodeRec;
  hash: number;
  kids: number[];
  deps: number[];
  sdepth: number;
  origin: string | null;
}

const APPEND = -2;
const VISIBILITY_ATTRS = new Set(["hidden", "style", "class", "open", "aria-hidden"]);
const RELEVANT_ATTRS = new Set([
  ...VISIBILITY_ATTRS,
  "role",
  "aria-label",
  "aria-labelledby",
  "aria-describedby",
  "aria-disabled",
  "disabled",
  "type",
  "title",
  "alt",
  "placeholder",
  "for",
  "id",
  "lang",
  "dir",
  "contenteditable",
  "tabindex",
  "href",
  "scope",
  "multiple",
  "size",
  "sandbox",
  "src",
  "name",
  "autocomplete",
  "data-vsf-scope",
]);

const pathOf = (win: Window | null): string => (win ? `${win.location.origin}${win.location.pathname}` : "");

function sameRels(a: NodeFacts["rels"], b: NodeFacts["rels"]): boolean {
  return a.length === b.length && a.every((x, i) => x[0] === b[i]?.[0] && x[1] === b[i]?.[1]);
}

function sameFacts(a: NodeFacts, b: NodeFacts): boolean {
  return a.role === b.role && a.name === b.name && a.text === b.text && a.flags === b.flags && sameRels(a.rels, b.rels);
}

/** Indices (into `seq`) of one longest strictly increasing subsequence. */
function lis(seq: number[]): Set<number> {
  const tails: number[] = [];
  const prev = new Array<number>(seq.length).fill(-1);
  for (let i = 0; i < seq.length; i++) {
    const v = seq[i] as number;
    let lo = 0;
    let hi = tails.length;
    while (lo < hi) {
      const mid = (lo + hi) >> 1;
      if ((seq[tails[mid] as number] as number) < v) lo = mid + 1;
      else hi = mid;
    }
    if (lo > 0) prev[i] = tails[lo - 1] as number;
    tails[lo] = i;
  }
  const keep = new Set<number>();
  for (let i = tails[tails.length - 1] ?? -1; i >= 0; i = prev[i] as number) keep.add(i);
  return keep;
}

export class Session {
  readonly stats: SessionStats = {
    described: 0,
    scannedSiblings: 0,
    batches: 0,
    ops: 0,
    fullSyncs: 0,
    subtreeSyncs: 0,
    replays: 0,
    scopes: { possiblyClosed: 0 },
  };
  private readonly cfg: SessionConfig;
  private readonly limits: Limits;
  private readonly caps: RuntimeCaps;
  private readonly ctx: DescribeCtx;
  private readonly ids = new WeakMap<Node, number>();
  private readonly live = new Map<number, Live>();
  private readonly dependents = new Map<number, Set<number>>();
  private readonly frameGen = new WeakMap<Element, number>();
  private readonly frameListeners = new Map<Element, () => void>();
  private readonly ring: Batch[] = [];
  private nextId = 1;
  private sum = 0;
  private truncated = false;
  private epochNo = -1;
  private seqNo = 0;
  private mo: MutationObserver | null = null;
  private timer: ReturnType<typeof setTimeout> | null = null;
  private pending: MutationRecord[] = [];
  private envDirty = false;
  private env: EnvironmentObservation | null = null;
  private url = "";
  private stopped = false;
  private scopeCount = 0;
  private readonly onEnvChange: () => void;

  private constructor(cfg: SessionConfig) {
    this.cfg = cfg;
    this.limits = { ...DEFAULT_LIMITS, ...cfg.limits };
    this.caps = cfg.caps ?? {};
    const opts: CaptureOptions = { maxTextChars: this.limits.maxTextChars };
    this.ctx = { opts, idOf: (el) => this.idOf(el) };
    this.onEnvChange = () => {
      this.envDirty = true;
      this.schedule();
    };
  }

  static start(cfg: SessionConfig): Session {
    const win = cfg.doc.defaultView;
    if (!win || !cfg.doc.documentElement) throw new ObservationBlocked("no_document");
    if (!cfg.policy.allowOrigin(win.location.origin)) throw new ObservationBlocked("origin_not_granted");
    const s = new Session(cfg);
    s.mo = new win.MutationObserver((records) => {
      s.pending.push(...records);
      s.schedule();
    });
    win.addEventListener("resize", s.onEnvChange);
    const nav = (win as Window & { navigation?: EventTarget }).navigation;
    nav?.addEventListener("navigatesuccess", s.onEnvChange);
    s.fullSync();
    return s;
  }

  get epoch(): number {
    return this.epochNo;
  }

  get seq(): number {
    return this.seqNo;
  }

  get size(): number {
    return this.live.size;
  }

  stop(): void {
    this.stopped = true;
    this.mo?.disconnect();
    if (this.timer !== null) clearTimeout(this.timer);
    const win = this.cfg.doc.defaultView;
    win?.removeEventListener("resize", this.onEnvChange);
    (win as (Window & { navigation?: EventTarget }) | null)?.navigation?.removeEventListener(
      "navigatesuccess",
      this.onEnvChange,
    );
    for (const [el, fn] of this.frameListeners) el.removeEventListener("load", fn);
    this.frameListeners.clear();
  }

  private diag(code: string, fields: Record<string, number | boolean> = {}): void {
    this.cfg.diagnostics?.emit(code, fields);
  }

  private idOf(node: Node): number {
    let id = this.ids.get(node);
    if (id === undefined) {
      id = this.nextId++;
      this.ids.set(node, id);
    }
    return id;
  }

  private liveOf(node: Node): Live | undefined {
    const id = this.ids.get(node);
    return id === undefined ? undefined : this.live.get(id);
  }

  private schedule(): void {
    if (this.stopped || this.cfg.batchMs === null || this.timer !== null) return;
    this.timer = setTimeout(() => {
      this.timer = null;
      this.flush();
    }, this.cfg.batchMs ?? 16);
  }

  private observeRoot(root: Node): void {
    this.mo?.observe(root, { childList: true, subtree: true, attributes: true, characterData: true });
  }

  private scopeFacts(tag: string, flags = 0): NodeFacts {
    return { tag, role: null, name: null, text: null, flags, rels: [] };
  }

  private setDeps(live: Live, deps: Element[]): void {
    for (const old of live.deps) this.dependents.get(old)?.delete(live.id);
    live.deps = deps.map((d) => this.idOf(d));
    for (const d of live.deps) {
      let set = this.dependents.get(d);
      if (!set) {
        set = new Set();
        this.dependents.set(d, set);
      }
      set.add(live.id);
    }
  }

  private lastScopeKid(parent: Live): number {
    let last = NO_NODE;
    for (const k of parent.kids) {
      const l = this.live.get(k);
      if (l?.node && l.node.nodeType === 1) break;
      last = k;
    }
    return last;
  }

  private addLive(
    ops: Op[],
    id: number,
    parent: Live | null,
    afterIn: number,
    facts: NodeFacts,
    scope: ScopeMeta | null,
    node: Node | null,
    deps: Element[],
    sdepth: number,
    origin: string | null,
  ): Live {
    const rec: NodeRec = { id, parent: parent ? parent.id : NO_NODE, ...facts, scope };
    const live: Live = { id, node, rec, hash: nodeHash(rec), kids: [], deps: [], sdepth, origin };
    let after = afterIn;
    if (parent) {
      const last = parent.kids[parent.kids.length - 1] ?? NO_NODE;
      if (after === APPEND) after = last;
      if (after === last) parent.kids.push(id);
      else parent.kids.splice(after === NO_NODE ? 0 : parent.kids.indexOf(after) + 1, 0, id);
    } else after = NO_NODE;
    this.live.set(id, live);
    this.sum = sumAdd(this.sum, live.hash);
    ops.push({ op: "add", id, parent: rec.parent, after, ...facts, ...(scope ? { scope } : {}) });
    this.setDeps(live, deps);
    return live;
  }

  private removeLive(ops: Op[], id: number): void {
    const root = this.live.get(id);
    if (!root) return;
    const parent = this.live.get(root.rec.parent);
    if (parent) parent.kids.splice(parent.kids.indexOf(id), 1);
    const stack = [root];
    while (stack.length > 0) {
      const cur = stack.pop() as Live;
      this.live.delete(cur.id);
      this.sum = sumSub(this.sum, cur.hash);
      for (const d of cur.deps) this.dependents.get(d)?.delete(cur.id);
      this.dependents.delete(cur.id);
      const fn = cur.node && cur.node.nodeType === 1 ? this.frameListeners.get(cur.node as Element) : undefined;
      if (fn) {
        (cur.node as Element).removeEventListener("load", fn);
        this.frameListeners.delete(cur.node as Element);
      }
      for (const k of cur.kids) {
        const child = this.live.get(k);
        if (child) stack.push(child);
      }
    }
    ops.push({ op: "remove", id });
  }

  private updateFacts(ops: Op[], live: Live, d: { facts: NodeFacts; deps: Element[] }): boolean {
    const f = d.facts;
    if (sameFacts(live.rec, f)) {
      this.setDeps(live, d.deps);
      return false;
    }
    this.sum = sumSub(this.sum, live.hash);
    live.rec = { ...live.rec, role: f.role, name: f.name, text: f.text, flags: f.flags, rels: f.rels };
    live.hash = nodeHash(live.rec);
    this.sum = sumAdd(this.sum, live.hash);
    this.setDeps(live, d.deps);
    ops.push({ op: "set", id: live.id, role: f.role, name: f.name, text: f.text, flags: f.flags, rels: f.rels });
    return true;
  }

  private modeledChildren(parent: Node): Element[] {
    if (parent.nodeType === 9) {
      const root = (parent as Document).documentElement;
      return root ? [root] : [];
    }
    const kids = (parent as Element | ShadowRoot).children;
    const out: Element[] = [];
    for (let i = 0; i < kids.length; i++) {
      const k = kids[i] as Element;
      if (!SKIP_TAGS.has(k.tagName)) out.push(k);
    }
    return out;
  }

  private generation(el: Element, _kind: ScopeKind): number {
    return this.frameGen.get(el) ?? 1;
  }

  private attachScopes(
    ops: Op[],
    live: Live,
    el: Element,
    stack: { el: Element; parent: Live; after: number }[],
  ): void {
    const scopes = childScopes(
      el,
      live.origin,
      this.caps,
      this.cfg.policy,
      (k, kind) => this.generation(k, kind),
      this.stats.scopes,
    );
    for (const s of scopes) {
      const tooDeep = live.sdepth + 1 > this.limits.maxScopeDepth || this.scopeCount >= this.limits.maxScopes;
      if (s.access === "accessible" && !tooDeep) {
        const tag = s.meta.kind === "shadow" ? "#shadow-root" : "#document";
        this.scopeCount++;
        const sl = this.addLive(
          ops,
          this.idOf(s.root),
          live,
          APPEND,
          this.scopeFacts(tag),
          s.meta,
          s.root,
          [],
          live.sdepth + 1,
          s.meta.origin,
        );
        this.observeRoot(s.root);
        const kids = this.modeledChildren(s.root);
        for (let i = kids.length - 1; i >= 0; i--) stack.push({ el: kids[i] as Element, parent: sl, after: APPEND });
      } else {
        const meta: ScopeMeta = tooDeep || !s.meta.opaque ? { ...s.meta, opaque: "limit_exceeded" } : s.meta;
        this.addLive(
          ops,
          this.nextId++,
          live,
          APPEND,
          this.scopeFacts("#opaque", FLAG_OPAQUE),
          meta,
          null,
          [],
          live.sdepth + 1,
          meta.origin,
        );
      }
      if (el.tagName === "IFRAME" || el.tagName === "FRAME") this.watchFrame(el);
    }
  }

  private watchFrame(el: Element): void {
    if (this.frameListeners.has(el)) return;
    const fn = (): void => this.frameLoaded(el);
    this.frameListeners.set(el, fn);
    el.addEventListener("load", fn);
  }

  private frameLoaded(el: Element): void {
    if (this.stopped) return;
    const live = this.liveOf(el);
    if (!live) return;
    this.frameGen.set(el, (this.frameGen.get(el) ?? 1) + 1);
    const ops: Op[] = [];
    if (this.flushInto(ops)) return;
    for (const k of [...live.kids]) {
      const child = this.live.get(k);
      if (child && !(child.node?.nodeType === 1)) this.removeLive(ops, k);
    }
    const stack: { el: Element; parent: Live; after: number }[] = [];
    this.attachScopes(ops, live, el, stack);
    this.drain(ops, stack);
    this.diag("scope.frame_reloaded", { generation: this.frameGen.get(el) ?? 1 });
    this.publish(ops);
  }

  private drain(ops: Op[], stack: { el: Element; parent: Live; after: number }[]): void {
    while (stack.length > 0) {
      const item = stack.pop() as { el: Element; parent: Live; after: number };
      const el = item.el;
      if (SKIP_TAGS.has(el.tagName)) continue;
      const existing = this.liveOf(el);
      if (existing) this.removeLive(ops, existing.id);
      if (this.live.size >= this.limits.maxNodes) {
        this.truncated = true;
        continue;
      }
      const d = describeElement(el, this.ctx);
      this.stats.described++;
      const live = this.addLive(
        ops,
        this.idOf(el),
        item.parent,
        item.after,
        d.facts,
        null,
        el,
        d.deps,
        item.parent.sdepth,
        item.parent.origin,
      );
      this.attachScopes(ops, live, el, stack);
      if (isLeaf(el)) continue;
      const kids = el.children;
      for (let i = kids.length - 1; i >= 0; i--) {
        const k = kids[i] as Element;
        if (!SKIP_TAGS.has(k.tagName)) stack.push({ el: k, parent: live, after: APPEND });
      }
    }
  }

  private makeBatch(kind: "full" | "delta", ops: Op[]): Batch {
    return {
      epoch: this.epochNo,
      seq: this.seqNo,
      kind,
      ops,
      sum: this.sum,
      count: this.live.size,
      truncated: this.truncated,
    };
  }

  private publish(ops: Op[]): void {
    if (ops.length === 0 || this.stopped) return;
    this.seqNo++;
    const batch = this.makeBatch("delta", ops);
    this.ring.push(batch);
    if (this.ring.length > this.limits.ringBatches) this.ring.shift();
    this.stats.batches++;
    this.stats.ops += ops.length;
    this.cfg.sink(batch);
  }

  /** Starts a new epoch with a complete baseline of everything currently observable. */
  fullSync(): void {
    this.stats.fullSyncs++;
    this.live.clear();
    this.dependents.clear();
    this.ring.length = 0;
    this.sum = 0;
    this.truncated = false;
    this.scopeCount = 0;
    this.pending = [];
    this.mo?.takeRecords();
    for (const [el, fn] of this.frameListeners) el.removeEventListener("load", fn);
    this.frameListeners.clear();
    this.epochNo = this.cfg.nextEpoch();
    this.seqNo = 0;
    const doc = this.cfg.doc;
    const win = doc.defaultView as Window;
    const ops: Op[] = [];
    const meta: ScopeMeta = { kind: "document", origin: win.location.origin, generation: 1 };
    const root = this.addLive(
      ops,
      this.idOf(doc),
      null,
      NO_NODE,
      this.scopeFacts("#document"),
      meta,
      doc,
      [],
      0,
      meta.origin,
    );
    this.observeRoot(doc);
    this.env = observeEnvironment(doc, this.cfg.flagProbes);
    this.url = pathOf(win);
    ops.push({ op: "env", env: this.env }, { op: "nav", url: this.url });
    const stack = this.modeledChildren(doc)
      .reverse()
      .map((el) => ({ el, parent: root, after: APPEND }));
    this.drain(ops, stack);
    const batch = this.makeBatch("full", ops);
    this.stats.batches++;
    this.stats.ops += ops.length;
    this.cfg.sink(batch);
  }

  private reconcile(parent: Live, ops: Op[]): void {
    const node = parent.node;
    if (!node) return;
    const desired = isLeaf(node as Element) ? [] : this.modeledChildren(node);
    const current: Live[] = [];
    for (const k of parent.kids) {
      const l = this.live.get(k);
      if (l?.node && l.node.nodeType === 1) current.push(l);
    }
    this.stats.scannedSiblings += desired.length + current.length;
    const at = new Map<Node, number>();
    current.forEach((l, i) => {
      at.set(l.node as Node, i);
    });
    const seq: number[] = [];
    const seqEls: Element[] = [];
    for (const el of desired) {
      const i = at.get(el);
      if (i !== undefined) {
        seq.push(i);
        seqEls.push(el);
      }
    }
    const keepIdx = lis(seq);
    const kept = new Set<Node>();
    seqEls.forEach((el, i) => {
      if (keepIdx.has(i)) kept.add(el);
    });
    for (const l of current) if (!kept.has(l.node as Node)) this.removeLive(ops, l.id);
    let after = this.lastScopeKid(parent);
    for (const el of desired) {
      if (kept.has(el)) {
        after = (this.liveOf(el) as Live).id;
        continue;
      }
      const stack = [{ el, parent, after }];
      this.drain(ops, stack);
      const added = this.liveOf(el);
      if (added) after = added.id;
    }
  }

  private previousTracked(el: Element, parent: Live): number | "unknown" {
    for (let p = el.previousElementSibling; p; p = p.previousElementSibling) {
      this.stats.scannedSiblings++;
      if (SKIP_TAGS.has(p.tagName)) continue;
      const l = this.liveOf(p);
      return l && l.rec.parent === parent.id ? l.id : "unknown";
    }
    return this.lastScopeKid(parent);
  }

  private flushInto(ops: Op[]): boolean {
    if (this.stopped) return false;
    const before = this.epochNo;
    if (this.timer !== null) {
      clearTimeout(this.timer);
      this.timer = null;
    }
    const records = [...this.pending, ...(this.mo?.takeRecords() ?? [])];
    this.pending = [];
    if (records.length > 0) this.process(records, ops);
    if (this.epochNo !== before) return true;
    if (this.envDirty || records.length > 0) this.refreshEnv(ops);
    return false;
  }

  /** Publishes everything queued so far as one batch (or nothing when nothing observable changed). */
  flush(): void {
    const ops: Op[] = [];
    if (this.flushInto(ops)) return;
    this.publish(ops);
  }

  private refreshEnv(ops: Op[]): void {
    if (this.envDirty) {
      this.envDirty = false;
      const next = observeEnvironment(this.cfg.doc, this.cfg.flagProbes);
      if (JSON.stringify(next) !== JSON.stringify(this.env)) {
        this.env = next;
        ops.push({ op: "env", env: next });
      }
    }
    const url = pathOf(this.cfg.doc.defaultView);
    if (url !== this.url) {
      this.url = url;
      ops.push({ op: "nav", url });
    }
  }

  private process(records: MutationRecord[], ops: Op[]): void {
    const doc = this.cfg.doc;
    const structural = new Map<Node, MutationRecord[]>();
    const reconcileNow = new Set<Node>();
    const textDirty = new Set<Element>();
    const attrDirty = new Map<Element, Set<string>>();
    for (const r of records) {
      if (r.type === "childList") {
        if (r.target === doc) {
          this.diag("epoch.document_replaced");
          this.fullSync();
          return;
        }
        const list = structural.get(r.target) ?? [];
        structural.set(r.target, list);
        list.push(r);
      } else if (r.type === "attributes") {
        const el = r.target as Element;
        const name = r.attributeName ?? "";
        if (
          el.tagName === "META" ||
          (el === doc.documentElement && (name === "lang" || name.startsWith("data-flag-")))
        ) {
          this.envDirty = true;
        }
        if (!RELEVANT_ATTRS.has(name) && !name.startsWith("aria-")) continue;
        const set = attrDirty.get(el) ?? new Set<string>();
        attrDirty.set(el, set);
        set.add(name);
      } else if (r.type === "characterData") {
        const p = r.target.parentNode;
        if (p && p.nodeType === 1) textDirty.add(p as Element);
      }
    }
    for (const [parentNode, recs] of structural) {
      const parent = this.liveOf(parentNode);
      if (!parent) continue;
      for (const r of recs) {
        for (const n of r.removedNodes) {
          if (n.nodeType === 3) {
            if (parentNode.nodeType === 1) textDirty.add(parentNode as Element);
            continue;
          }
          const l = n.nodeType === 1 ? this.liveOf(n) : undefined;
          if (!l || l.rec.parent !== parent.id) continue;
          if (n.parentNode === parentNode) reconcileNow.add(parentNode);
          else this.removeLive(ops, l.id);
        }
      }
    }
    for (const [parentNode, recs] of structural) {
      const parent = this.liveOf(parentNode);
      if (!parent) continue;
      let simple = !reconcileNow.has(parentNode);
      const candidates: Element[] = [];
      if (simple) {
        for (const r of recs) {
          for (const n of r.addedNodes) {
            if (n.nodeType === 3) {
              if (parentNode.nodeType === 1) textDirty.add(parentNode as Element);
              continue;
            }
            if (n.nodeType !== 1 || SKIP_TAGS.has((n as Element).tagName) || n.parentNode !== parentNode) continue;
            const known = this.liveOf(n);
            if (known) {
              if (known.rec.parent !== parent.id) simple = false;
              continue;
            }
            if (!candidates.includes(n as Element)) candidates.push(n as Element);
          }
        }
      }
      if (!simple || this.live.size >= this.limits.maxNodes || isLeaf(parentNode as Element)) {
        this.reconcile(parent, ops);
        continue;
      }
      if (candidates.length > 1) {
        candidates.sort((a, b) => (a.compareDocumentPosition(b) & Node.DOCUMENT_POSITION_FOLLOWING ? -1 : 1));
      }
      for (const el of candidates) {
        const after = this.previousTracked(el, parent);
        if (after === "unknown") {
          this.reconcile(parent, ops);
          break;
        }
        this.drain(ops, [{ el, parent, after }]);
      }
    }
    const seen = new Set<number>();
    const redo = (el: Element): void => {
      const live = this.liveOf(el);
      if (!live || seen.has(live.id)) return;
      seen.add(live.id);
      const before = live.rec.flags;
      const d = describeElement(el, this.ctx);
      this.stats.described++;
      this.updateFacts(ops, live, d);
      const changed = before ^ live.rec.flags;
      if (changed & (FLAG_VISIBLE | FLAG_DISABLED)) this.refreshSubtree(live, ops, seen);
      for (const dep of this.dependents.get(live.id) ?? []) {
        const dl = this.live.get(dep);
        if (dl?.node && dl.node.nodeType === 1) redo(dl.node as Element);
      }
    };
    for (const [el, names] of attrDirty) {
      const live = this.liveOf(el);
      if (!live) continue;
      if ([...names].every((n) => n === "class" || n === "style")) {
        const visible = (el as Element & { checkVisibility?: () => boolean }).checkVisibility?.() ?? true;
        if (visible === ((live.rec.flags & FLAG_VISIBLE) !== 0)) continue;
      }
      redo(el);
    }
    for (const el of textDirty) {
      redo(el);
      let depth = 0;
      for (let p = el.parentElement; p && depth < 4; p = p.parentElement, depth++) {
        const l = this.liveOf(p);
        if (l?.rec.role && NAME_FROM_CONTENT.has(l.rec.role)) redo(p);
      }
    }
  }

  private refreshSubtree(root: Live, ops: Op[], seen: Set<number>): void {
    const stack = [...root.kids];
    while (stack.length > 0) {
      const l = this.live.get(stack.pop() as number);
      if (!l) continue;
      stack.push(...l.kids);
      if (l.node?.nodeType !== 1 || seen.has(l.id)) continue;
      seen.add(l.id);
      const d = describeElement(l.node as Element, this.ctx);
      this.stats.described++;
      this.updateFacts(ops, l, d);
    }
  }

  /** Re-checks custom elements for shadow roots attached after capture, which the DOM never announces. */
  rescan(): void {
    const ops: Op[] = [];
    if (this.flushInto(ops)) return;
    for (const live of [...this.live.values()]) {
      const el = live.node;
      if (el?.nodeType !== 1 || !(el as Element).tagName.includes("-")) continue;
      const hasScope = live.kids.some((k) => {
        const kid = this.live.get(k);
        return kid && kid.node?.nodeType !== 1;
      });
      if (hasScope) continue;
      const stack: { el: Element; parent: Live; after: number }[] = [];
      const before = ops.length;
      this.attachScopes(ops, live, el as Element, stack);
      this.drain(ops, stack);
      if (ops.length > before) this.diag("scope.late_shadow_found");
    }
    this.publish(ops);
  }

  /** Answers a consumer that lost batches: replay if still buffered, else a bounded rebuild. */
  resync(req: ResyncRequest): void {
    if (this.stopped) return;
    if (req.kind === "replay") {
      const first = this.ring[0];
      if (first && req.from >= first.seq && req.from <= this.seqNo) {
        this.stats.replays++;
        for (const b of this.ring) if (b.seq >= req.from) this.cfg.sink(b);
        return;
      }
      this.diag("resync.replay_unavailable", { from: req.from });
      this.fullSync();
      return;
    }
    if (req.kind === "full") {
      this.fullSync();
      return;
    }
    const live = this.live.get(req.id);
    if (!live?.node) {
      this.fullSync();
      return;
    }
    this.stats.subtreeSyncs++;
    const ops: Op[] = [];
    if (this.flushInto(ops)) return;
    for (const k of [...live.kids]) this.removeLive(ops, k);
    const stack: { el: Element; parent: Live; after: number }[] = [];
    if (live.node.nodeType === 1) {
      const d = describeElement(live.node as Element, this.ctx);
      this.stats.described++;
      this.updateFacts(ops, live, d);
      this.attachScopes(ops, live, live.node as Element, stack);
      if (!isLeaf(live.node as Element)) {
        for (const k of this.modeledChildren(live.node).reverse()) stack.push({ el: k, parent: live, after: APPEND });
      }
    } else {
      for (const k of this.modeledChildren(live.node).reverse()) stack.push({ el: k, parent: live, after: APPEND });
    }
    this.drain(ops, stack);
    this.publish(ops);
  }

  /** Layout is read only for the nodes asked about, and the stamp tells the consumer how old the numbers are. */
  probe(ids: number[]): {
    epoch: number;
    seq: number;
    items: { id: number; rect: { x: number; y: number; width: number; height: number } | null }[];
  } {
    this.flush();
    const items = ids.map((id) => {
      const n = this.live.get(id)?.node;
      return { id, rect: n && n.nodeType === 1 ? probeGeometry(n as Element) : null };
    });
    return { epoch: this.epochNo, seq: this.seqNo, items };
  }

  /** Ids for tests and tools to address live nodes by element. */
  idForElement(el: Element): number | null {
    const l = this.liveOf(el);
    return l ? l.id : null;
  }
}
