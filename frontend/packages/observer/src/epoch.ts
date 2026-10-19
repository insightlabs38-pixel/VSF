const KEY = "vsf.epoch";

/** Strictly increasing per tab and origin, and never below the wall clock so lost storage cannot reuse an epoch. */
export function persistentEpochs(win: Window, now: () => number = Date.now): () => number {
  let memory = 0;
  return () => {
    try {
      const prev = Number(win.sessionStorage.getItem(KEY));
      const next = Math.max((Number.isSafeInteger(prev) && prev >= 0 ? prev : 0) + 1, now());
      win.sessionStorage.setItem(KEY, String(next));
      return next;
    } catch {
      memory = Math.max(memory + 1, now());
      return memory;
    }
  };
}
