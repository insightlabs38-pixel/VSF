import assert from "node:assert/strict";
import { test } from "node:test";
import { memoryKV } from "../src/kv.ts";
import { memoryKeyStore, Vault, VaultError } from "../src/vault.ts";

const PLANTED = "refresh-secret-value-0123456789";

test("sealed values round-trip and the stored record holds no plaintext", async () => {
  const kv = memoryKV();
  const vault = new Vault(memoryKeyStore(), kv);
  await vault.seal("bridge", { refresh: PLANTED, n: 7 });
  assert.deepEqual(await vault.open("bridge"), { refresh: PLANTED, n: 7 });
  assert.ok(!JSON.stringify(kv.dump()).includes(PLANTED));
  assert.equal(await vault.open("absent"), undefined);
  await vault.remove("bridge");
  assert.equal(await vault.open("bridge"), undefined);
  assert.ok(!("vault:bridge" in kv.dump()), "the record itself must be gone, not blanked");
});

test("the master key is non-extractable", async () => {
  const keys = memoryKeyStore();
  const vault = new Vault(keys, memoryKV());
  await vault.seal("x", 1);
  const key = await keys.get();
  assert.ok(key);
  assert.equal(key.extractable, false);
  await assert.rejects(() => crypto.subtle.exportKey("raw", key));
});

test("tampering, swapping records between names and a different key are all detected", async () => {
  const kv = memoryKV();
  const keys = memoryKeyStore();
  const vault = new Vault(keys, kv);
  await vault.seal("a", { v: PLANTED });
  await vault.seal("b", { v: "other" });

  const a = (await kv.get("vault:a")) as { ct: string };
  const flipped = a.ct.startsWith("A") ? `B${a.ct.slice(1)}` : `A${a.ct.slice(1)}`;
  await kv.set("vault:a", { ...a, ct: flipped });
  await assert.rejects(
    () => vault.open("a"),
    (e: unknown) => e instanceof VaultError && e.code === "tampered",
  );

  await vault.seal("a", { v: PLANTED });
  await kv.set("vault:b", await kv.get("vault:a"));
  await assert.rejects(
    () => vault.open("b"),
    (e: unknown) => e instanceof VaultError && e.code === "tampered",
  );

  const stranger = new Vault(memoryKeyStore(), kv);
  await assert.rejects(
    () => stranger.open("a"),
    (e: unknown) => e instanceof VaultError && e.code === "tampered",
  );

  await kv.set("vault:a", { v: 2 });
  await assert.rejects(
    () => vault.open("a"),
    (e: unknown) => e instanceof VaultError && e.code === "unreadable",
  );
});

test("concurrent first use creates a single key", async () => {
  let puts = 0;
  const inner = memoryKeyStore();
  const keys = {
    get: () => inner.get(),
    put: async (k: CryptoKey) => {
      puts++;
      await inner.put(k);
    },
  };
  const vault = new Vault(keys, memoryKV());
  await Promise.all([vault.seal("a", 1), vault.seal("b", 2), vault.seal("c", 3)]);
  assert.equal(puts, 1);
  assert.equal(await vault.open("c"), 3);
});

test("nonces are fresh for every seal", async () => {
  const kv = memoryKV();
  const vault = new Vault(memoryKeyStore(), kv);
  await vault.seal("a", 1);
  const first = (await kv.get("vault:a")) as { iv: string };
  await vault.seal("a", 1);
  const second = (await kv.get("vault:a")) as { iv: string };
  assert.notEqual(first.iv, second.iv);
});
