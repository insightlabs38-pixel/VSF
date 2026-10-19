export type OpState = "started" | "done" | "unknown";

export interface Op {
  state: OpState;
  tab: number;
  action: string;
  at: number;
  /** Present once the local service answered; an unknown outcome never has one. */
  result?: { ok: boolean; message?: string };
  reason?: string;
}

export interface Store {
  get(): Promise<Record<string, Op>>;
  set(ops: Record<string, Op>): Promise<void>;
}

export const REQUEST_ID = /^[a-z0-9-]{8,64}$/;
const KEEP = 200;

export type Begin = { fresh: true } | { fresh: false; op: Op };

/** A durable record of every forwarded operation, written first; a repeated request id is answered from it. */
export class Ledger {
  private readonly store: Store;
  private readonly now: () => number;

  constructor(store: Store, now: () => number = Date.now) {
    this.store = store;
    this.now = now;
  }

  async begin(id: string, tab: number, action: string): Promise<Begin> {
    const ops = await this.store.get();
    const seen = ops[id];
    if (seen !== undefined) return { fresh: false, op: seen };
    ops[id] = { state: "started", tab, action, at: this.now() };
    const ids = Object.keys(ops);
    // Finished entries go first when the ledger is full; entries whose outcome is open are never dropped.
    for (const old of ids) {
      if (Object.keys(ops).length <= KEEP) break;
      if (ops[old]?.state === "done") delete ops[old];
    }
    await this.store.set(ops);
    return { fresh: true };
  }

  async finish(id: string, result: { ok: boolean; message?: string }): Promise<Op | undefined> {
    const ops = await this.store.get();
    const op = ops[id];
    if (op === undefined || op.state !== "started") return op;
    ops[id] = { ...op, state: "done", result };
    await this.store.set(ops);
    return ops[id];
  }

  /** The request left, but no answer arrived: the source may or may not have acted, so it is never sent again. */
  async lose(id: string, reason: string): Promise<Op | undefined> {
    const ops = await this.store.get();
    const op = ops[id];
    if (op === undefined || op.state !== "started") return op;
    ops[id] = { ...op, state: "unknown", reason };
    await this.store.set(ops);
    return ops[id];
  }

  /** Run once per worker start: whatever was still in flight when the previous worker died is now unknown. */
  async reconcile(): Promise<string[]> {
    const ops = await this.store.get();
    const lost: string[] = [];
    for (const [id, op] of Object.entries(ops))
      if (op.state === "started") {
        ops[id] = { ...op, state: "unknown", reason: "worker_restarted" };
        lost.push(id);
      }
    if (lost.length > 0) await this.store.set(ops);
    return lost;
  }

  async all(): Promise<Record<string, Op>> {
    return this.store.get();
  }
}
