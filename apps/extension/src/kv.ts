export interface KV {
  get(key: string): Promise<unknown>;
  set(key: string, value: unknown): Promise<void>;
  remove(key: string): Promise<void>;
}

/** Serializes read-modify-write cycles: the worker handles messages concurrently and storage has no transactions. */
export class Lock {
  private tail: Promise<unknown> = Promise.resolve();

  run<T>(fn: () => Promise<T>): Promise<T> {
    const next = this.tail.then(fn, fn);
    this.tail = next.catch(() => undefined);
    return next;
  }
}

export function chromeKV(area: chrome.storage.StorageArea): KV {
  return {
    async get(key) {
      return (await area.get(key))[key];
    },
    async set(key, value) {
      await area.set({ [key]: value });
    },
    async remove(key) {
      await area.remove(key);
    },
  };
}

export function memoryKV(): KV & { dump(): Record<string, unknown> } {
  const m = new Map<string, unknown>();
  return {
    async get(key) {
      const v = m.get(key);
      return v === undefined ? undefined : structuredClone(v);
    },
    async set(key, value) {
      m.set(key, structuredClone(value));
    },
    async remove(key) {
      m.delete(key);
    },
    dump: () => Object.fromEntries(m),
  };
}
