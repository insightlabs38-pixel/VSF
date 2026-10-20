import { type KV, Lock } from "./kv.ts";

export interface KeyStore {
  get(): Promise<CryptoKey | undefined>;
  put(key: CryptoKey): Promise<void>;
}

export class VaultError extends Error {
  readonly code: "tampered" | "unreadable";
  constructor(code: VaultError["code"]) {
    super(`vault.${code}`);
    this.name = "VaultError";
    this.code = code;
  }
}

const b64 = (bytes: Uint8Array): string => {
  let s = "";
  for (let i = 0; i < bytes.length; i += 0x8000) s += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
  return btoa(s);
};
const unb64 = (s: string): Uint8Array<ArrayBuffer> => Uint8Array.from(atob(s), (c) => c.charCodeAt(0));

interface Sealed {
  v: 1;
  iv: string;
  ct: string;
}

/** AES-GCM sealing with a key the page, the storage area and the exported profile never see in the clear. */
export class Vault {
  private readonly lock = new Lock();
  private key: Promise<CryptoKey> | undefined;
  private readonly keys: KeyStore;
  private readonly kv: KV;
  private readonly prefix: string;

  constructor(keys: KeyStore, kv: KV, prefix = "vault:") {
    this.keys = keys;
    this.kv = kv;
    this.prefix = prefix;
  }

  private masterKey(): Promise<CryptoKey> {
    this.key ??= this.lock.run(async () => {
      const have = await this.keys.get();
      if (have) return have;
      const made = await crypto.subtle.generateKey({ name: "AES-GCM", length: 256 }, false, ["encrypt", "decrypt"]);
      await this.keys.put(made);
      return made;
    });
    return this.key;
  }

  private aad = (name: string): Uint8Array<ArrayBuffer> => new TextEncoder().encode(`vsf.vault.v1:${name}`);

  async seal(name: string, value: unknown): Promise<void> {
    const iv = crypto.getRandomValues(new Uint8Array(12));
    const plain = new TextEncoder().encode(JSON.stringify(value));
    const ct = await crypto.subtle.encrypt(
      { name: "AES-GCM", iv, additionalData: this.aad(name) },
      await this.masterKey(),
      plain,
    );
    const record: Sealed = { v: 1, iv: b64(iv), ct: b64(new Uint8Array(ct)) };
    await this.kv.set(this.prefix + name, record);
  }

  async open<T>(name: string): Promise<T | undefined> {
    const record = (await this.kv.get(this.prefix + name)) as Sealed | undefined;
    if (record === undefined) return undefined;
    if (record.v !== 1 || typeof record.iv !== "string" || typeof record.ct !== "string")
      throw new VaultError("unreadable");
    try {
      const plain = await crypto.subtle.decrypt(
        { name: "AES-GCM", iv: unb64(record.iv), additionalData: this.aad(name) },
        await this.masterKey(),
        unb64(record.ct),
      );
      return JSON.parse(new TextDecoder().decode(plain)) as T;
    } catch {
      throw new VaultError("tampered");
    }
  }

  async remove(name: string): Promise<void> {
    await this.kv.remove(this.prefix + name);
  }
}

/** The master key lives in IndexedDB as a non-extractable CryptoKey: it can be used but never read out. */
export function indexedDbKeyStore(name = "vsf-vault"): KeyStore {
  const open = (): Promise<IDBDatabase> =>
    new Promise((resolve, reject) => {
      const req = indexedDB.open(name, 1);
      req.onupgradeneeded = () => req.result.createObjectStore("keys");
      req.onsuccess = () => resolve(req.result);
      req.onerror = () => reject(req.error);
    });
  const run = async <T>(mode: IDBTransactionMode, fn: (s: IDBObjectStore) => IDBRequest<T>): Promise<T> => {
    const db = await open();
    try {
      return await new Promise<T>((resolve, reject) => {
        const req = fn(db.transaction("keys", mode).objectStore("keys"));
        req.onsuccess = () => resolve(req.result);
        req.onerror = () => reject(req.error);
      });
    } finally {
      db.close();
    }
  };
  return {
    get: async () => (await run("readonly", (s) => s.get("master"))) as CryptoKey | undefined,
    put: async (key) => void (await run("readwrite", (s) => s.put(key, "master"))),
  };
}

export function memoryKeyStore(): KeyStore {
  let k: CryptoKey | undefined;
  return {
    get: async () => k,
    put: async (key) => {
      k = key;
    },
  };
}
