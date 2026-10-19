export type Value = string | number | boolean | null;
export type Listener = (v: Value) => void;

/** Flat key/value state with per-key subscriptions: a write wakes only the listeners of that key. */
export class Store {
  private vals = new Map<string, Value>();
  private subs = new Map<string, Set<Listener>>();
  private sets = new Map<string, RowSet>();
  private count = 0;

  get(key: string): Value {
    return this.vals.get(key) ?? null;
  }

  set(key: string, v: Value): boolean {
    if (this.vals.get(key) === v || (v === null && !this.vals.has(key))) return false;
    this.vals.set(key, v);
    for (const f of [...(this.subs.get(key) ?? [])]) f(v);
    return true;
  }

  subscribe(key: string, fn: Listener): () => void {
    const s = this.subs.get(key) ?? new Set<Listener>();
    this.subs.set(key, s);
    s.add(fn);
    this.count++;
    return () => {
      if (s.delete(fn)) this.count--;
      if (s.size === 0 && this.subs.get(key) === s) this.subs.delete(key);
    };
  }

  rows(entity: string): RowSet {
    const have = this.sets.get(entity);
    if (have) return have;
    const made = new RowSet(
      () => this.count++,
      () => this.count--,
    );
    this.sets.set(entity, made);
    return made;
  }

  /** Live listeners, used to prove that removed views leave nothing behind. */
  subscriptionCount(): number {
    return this.count;
  }
}

export type Row = Record<string, Value>;

/** Keyed rows. Cell writes reach only that cell's listeners; structure changes reach the list listeners. */
export class RowSet {
  private data = new Map<string, Row>();
  private keys: string[] = [];
  private cells = new Map<string, Set<Listener>>();
  private lists = new Set<() => void>();

  private up: () => void;
  private down: () => void;

  constructor(up: () => void, down: () => void) {
    this.up = up;
    this.down = down;
  }

  get size(): number {
    return this.keys.length;
  }

  order(): readonly string[] {
    return this.keys;
  }

  row(key: string): Row | undefined {
    return this.data.get(key);
  }

  cell(key: string, field: string): Value {
    return this.data.get(key)?.[field] ?? null;
  }

  replace(rows: { key: string; values: Row }[]): void {
    this.data = new Map(rows.map((r) => [r.key, { ...r.values }]));
    this.keys = rows.map((r) => r.key);
    this.emitList();
    for (const [id, subs] of this.cells) {
      const [k, f] = split(id);
      for (const fn of [...subs]) fn(this.cell(k, f));
    }
  }

  setCell(key: string, field: string, v: Value): boolean {
    const row = this.data.get(key);
    if (!row || row[field] === v) return false;
    row[field] = v;
    for (const fn of [...(this.cells.get(`${key}\u0000${field}`) ?? [])]) fn(v);
    return true;
  }

  insert(key: string, values: Row, at = this.keys.length): void {
    if (this.data.has(key)) throw new Error(`duplicate row ${key}`);
    this.data.set(key, { ...values });
    this.keys.splice(Math.min(at, this.keys.length), 0, key);
    this.emitList();
  }

  remove(key: string): void {
    if (!this.data.delete(key)) return;
    this.keys = this.keys.filter((k) => k !== key);
    this.emitList();
  }

  sortBy(field: string, dir: 1 | -1 = 1): void {
    const cmp = (a: Value, b: Value): number =>
      typeof a === "number" && typeof b === "number"
        ? a - b
        : String(a ?? "").localeCompare(String(b ?? ""), "en", { numeric: true });
    this.keys = [...this.keys].sort(
      (a, b) => dir * cmp(this.cell(a, field), this.cell(b, field)) || a.localeCompare(b),
    );
    this.emitList();
  }

  onList(fn: () => void): () => void {
    this.lists.add(fn);
    this.up();
    return () => {
      if (this.lists.delete(fn)) this.down();
    };
  }

  onCell(key: string, field: string, fn: Listener): () => void {
    const id = `${key}\u0000${field}`;
    const s = this.cells.get(id) ?? new Set<Listener>();
    this.cells.set(id, s);
    s.add(fn);
    this.up();
    return () => {
      if (s.delete(fn)) this.down();
      if (s.size === 0 && this.cells.get(id) === s) this.cells.delete(id);
    };
  }

  private emitList(): void {
    for (const f of [...this.lists]) f();
  }
}

function split(id: string): [string, string] {
  const i = id.indexOf("\u0000");
  return [id.slice(0, i), id.slice(i + 1)];
}
