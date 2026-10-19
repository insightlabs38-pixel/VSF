export interface Stats {
  /** Every mutation of the rendered tree goes through here, so this is the DOM write count. */
  writes: number;
  created: number;
  removed: number;
  moved: number;
}

export const newStats = (): Stats => ({ writes: 0, created: 0, removed: 0, moved: 0 });

export class Dom {
  readonly doc: Document;
  readonly stats: Stats;

  constructor(doc: Document, stats: Stats = newStats()) {
    this.doc = doc;
    this.stats = stats;
  }

  el<K extends keyof HTMLElementTagNameMap>(tag: K, cls?: string): HTMLElementTagNameMap[K] {
    const e = this.doc.createElement(tag);
    if (cls) e.className = cls;
    this.stats.created++;
    return e;
  }

  attr(e: Element, name: string, v: string | null): void {
    if (e.getAttribute(name) === v) return;
    this.stats.writes++;
    if (v === null) e.removeAttribute(name);
    else e.setAttribute(name, v);
  }

  text(e: Node, v: string): void {
    if (e.textContent === v) return;
    this.stats.writes++;
    e.textContent = v;
  }

  /** Sets `value` only when it differs, so caret and selection survive no-op updates. */
  value(e: HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement, v: string): void {
    if (e.value === v) return;
    this.stats.writes++;
    const focused = e.getRootNode() instanceof ShadowRoot && (e.getRootNode() as ShadowRoot).activeElement === e;
    if (focused && e instanceof HTMLInputElement && e.type === "text") {
      const [s, t] = [e.selectionStart, e.selectionEnd];
      const was = e.value.length;
      e.value = v;
      if (s !== null && t !== null) {
        const delta = v.length - was;
        const clamp = (n: number): number => Math.max(0, Math.min(v.length, n));
        e.setSelectionRange(clamp(s >= was ? s + delta : s), clamp(t >= was ? t + delta : t));
      }
      return;
    }
    e.value = v;
  }

  checked(e: HTMLInputElement, v: boolean): void {
    if (e.checked === v) return;
    this.stats.writes++;
    e.checked = v;
  }

  prop<T extends object, K extends keyof T>(o: T, k: K, v: T[K]): void {
    if (o[k] === v) return;
    this.stats.writes++;
    o[k] = v;
  }

  append(parent: Node, child: Node, before: Node | null = null): void {
    this.stats.writes++;
    parent.insertBefore(child, before);
  }

  remove(n: ChildNode): void {
    this.stats.writes++;
    this.stats.removed++;
    n.remove();
  }
}

/** Indices of a longest strictly increasing subsequence of `seq`, ignoring negatives. */
function lis(seq: number[]): Set<number> {
  const tails: number[] = [];
  const prev: number[] = new Array(seq.length).fill(-1);
  for (let i = 0; i < seq.length; i++) {
    const v = seq[i] as number;
    if (v < 0) continue;
    let lo = 0;
    let hi = tails.length;
    while (lo < hi) {
      const mid = (lo + hi) >> 1;
      if ((seq[tails[mid] as number] as number) < v) lo = mid + 1;
      else hi = mid;
    }
    prev[i] = lo > 0 ? (tails[lo - 1] as number) : -1;
    tails[lo] = i;
  }
  const keep = new Set<number>();
  let k = tails.length ? (tails[tails.length - 1] as number) : -1;
  while (k >= 0) {
    keep.add(k);
    k = prev[k] as number;
  }
  return keep;
}

/** Places `wanted` in order under `parent` (after `from`), moving as few existing nodes as possible. */
export function placeChildren(dom: Dom, parent: Node, wanted: Node[], from: Node | null = null): void {
  const pos = new Map<Node, number>();
  let i = 0;
  for (let n = from ? from.nextSibling : parent.firstChild; n; n = n.nextSibling) pos.set(n, i++);
  const seq = wanted.map((n) => pos.get(n) ?? -1);
  const keep = lis(seq);
  let anchor: Node | null = null;
  for (let j = wanted.length - 1; j >= 0; j--) {
    const n = wanted[j] as Node;
    if (!keep.has(j)) {
      if (n.parentNode === parent) dom.stats.moved++;
      dom.append(parent, n, anchor);
    }
    anchor = n;
  }
}
