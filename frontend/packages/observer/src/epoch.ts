const KEY = "vsf.epoch";

/** The page can write this storage, so a stored value may lead the clock by at most a day before it is ignored. */
export const MAX_LEAD_MS = 24 * 3600 * 1000;

/** Strictly increasing per tab and origin, never below the wall clock, and immune to a hostile stored value. */
export function persistentEpochs(win: Window, now: () => number = Date.now): () => number {
  let memory = 0;
  return () => {
    const clock = now();
    memory = Math.max(memory + 1, clock);
    try {
      const raw = win.sessionStorage.getItem(KEY);
      const prev = raw !== null && /^\d{1,16}$/.test(raw) ? Number(raw) : 0;
      const trusted = Number.isSafeInteger(prev) && prev <= clock + MAX_LEAD_MS ? prev : 0;
      memory = Math.max(memory, trusted + 1);
      win.sessionStorage.setItem(KEY, String(memory));
    } catch {
      /* storage unavailable: the in-memory high-water mark still increases */
    }
    return memory;
  };
}
