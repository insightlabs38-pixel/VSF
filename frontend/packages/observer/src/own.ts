const KEY = Symbol.for("vsf.ownUi");

/** Elements the extension itself created; held in the isolated world, so the page cannot add its own content to the set. */
export function ownUi(): WeakSet<Element> {
  const g = globalThis as unknown as Record<symbol, WeakSet<Element> | undefined>;
  let set = g[KEY];
  if (!set) {
    set = new WeakSet();
    g[KEY] = set;
  }
  return set;
}
