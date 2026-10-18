const KEY = "vsf.epoch";

/** Per-tab, per-origin counter that survives reloads; storage that is unavailable falls back to memory. */
export function persistentEpochs(win: Window): () => number {
  let memory = 0;
  return () => {
    try {
      const prev = Number(win.sessionStorage.getItem(KEY));
      const next = (Number.isSafeInteger(prev) && prev >= 0 ? prev : 0) + 1;
      win.sessionStorage.setItem(KEY, String(next));
      return next;
    } catch {
      return ++memory;
    }
  };
}
