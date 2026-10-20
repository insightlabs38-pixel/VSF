import assert from "node:assert/strict";
import { type ChildProcess, spawn } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { createServer } from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { after, test } from "node:test";
import { fileURLToPath } from "node:url";
import { memoryKV } from "@vsf/extension/kv";
import { BridgeLink } from "@vsf/extension/link";
import { memoryKeyStore, Vault } from "@vsf/extension/vault";

const root = new URL("../../", import.meta.url);
const bin = process.env.VSF_BRIDGE_BIN ?? fileURLToPath(new URL("artifacts/bin/vsfbridge", root));
const EXT = "chrome-extension://lgnekpogndcjggibkbaekekanfonjhie";
const OTHER = "chrome-extension://aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";
const cleanup: (() => void)[] = [];
after(() => {
  for (const f of cleanup) f();
});

interface Companion {
  endpoint: string;
  code: string;
  proc: ChildProcess;
  stderr: string[];
  stop(): Promise<void>;
}

async function freePort(): Promise<number> {
  return new Promise((resolve) => {
    const s = createServer();
    s.listen(0, "127.0.0.1", () => {
      const p = (s.address() as { port: number }).port;
      s.close(() => resolve(p));
    });
  });
}

async function startCompanion(state: string, port: number): Promise<Companion> {
  const proc = spawn(bin, ["-origins", EXT, "-state", state, "-pair", "-port", String(port)], {
    stdio: ["ignore", "pipe", "pipe"],
  });
  const stderr: string[] = [];
  proc.stderr?.on("data", (d: Buffer) => stderr.push(...d.toString().split("\n").filter(Boolean)));
  const first = await new Promise<string>((resolve) => proc.stdout?.once("data", (d: Buffer) => resolve(d.toString())));
  const out = JSON.parse(first) as { addr: string; pair_code: string };
  const c: Companion = {
    endpoint: out.addr,
    code: out.pair_code,
    proc,
    stderr,
    stop: async () => {
      const done = new Promise<void>((r) => proc.once("exit", () => r()));
      proc.kill();
      await done;
    },
  };
  cleanup.push(() => proc.kill());
  return c;
}

/** A fetch that speaks as the extension would: a fixed Origin and nothing else added. */
const asOrigin =
  (origin: string): typeof fetch =>
  (url, init) =>
    fetch(url, { ...init, headers: { ...(init?.headers as Record<string, string>), Origin: origin } });

function makeLink(origin: string, kv = memoryKV(), now = () => Date.now()) {
  const vault = new Vault(memoryKeyStore(), kv);
  return { link: new BridgeLink({ vault, origin, fetch: asOrigin(origin), now }), vault, kv };
}

test("the extension client pairs with the real companion, calls it, renews and revokes", async () => {
  const state = mkdtempSync(join(tmpdir(), "vsf-pair-"));
  cleanup.push(() => rmSync(state, { recursive: true, force: true }));
  const comp = await startCompanion(state, 0);
  const { link, kv, vault } = makeLink(EXT);
  const { clientId } = await link.pair(comp.endpoint, comp.code, "test");
  assert.match(clientId, /^cli_[0-9a-f]{24}$/);
  assert.equal((await link.status()).paired, true);

  const stored = JSON.stringify(kv.dump());
  assert.ok(!stored.includes(comp.code.replaceAll("-", "")), "the code is never stored");
  const secrets = await vault.open<{ refresh: string; access: string }>("bridge");
  assert.ok(
    secrets && !stored.includes(secrets.refresh) && !stored.includes(secrets.access),
    "credentials are only stored sealed",
  );

  const reply = await link.call("/v1/unknown-route", {});
  assert.equal(reply.status, 404, "an authenticated call reaches the router");

  const stale = secrets.refresh;
  const again = new BridgeLink({ vault, origin: EXT, fetch: asOrigin(EXT), now: () => Date.now() + 3 * 3600 * 1000 });
  const live = await again.accessToken();
  assert.ok(live && live.access !== secrets.access, "an expired access token is renewed with the refresh secret");

  const replay = await fetch(`http://${comp.endpoint}/v1/token/refresh`, {
    method: "POST",
    headers: { "content-type": "application/json", "x-vsf-bridge": "1", origin: EXT },
    body: JSON.stringify({ client_id: clientId, refresh: stale }),
  });
  assert.equal(replay.status, 401);
  assert.equal(((await replay.json()) as { error: string }).error, "pair.refresh_reused");
  const afterTheft = await fetch(`http://${comp.endpoint}/v1/token/refresh`, {
    method: "POST",
    headers: { "content-type": "application/json", "x-vsf-bridge": "1", origin: EXT },
    body: JSON.stringify({ client_id: clientId, refresh: (await vault.open<{ refresh: string }>("bridge"))?.refresh }),
  });
  assert.equal(afterTheft.status, 403, "a replayed refresh secret revokes the legitimate holder too");
  await comp.stop();
});

test("wrong codes burn the offer, a new offer works, and the wrong extension or origin is turned away", async () => {
  const state = mkdtempSync(join(tmpdir(), "vsf-pair-"));
  cleanup.push(() => rmSync(state, { recursive: true, force: true }));
  const comp = await startCompanion(state, 0);
  const { link } = makeLink(EXT);
  for (let i = 0; i < 3; i++)
    await assert.rejects(() => link.pair(comp.endpoint, "AAAA-AAAA-AAAA-AAAA", "x"), { code: "pair_denied" });
  await assert.rejects(() => link.pair(comp.endpoint, comp.code, "x"), { code: "pair_denied" });

  comp.proc.kill("SIGUSR1");
  const fresh = await new Promise<string>((resolve) => {
    const look = (): void => {
      const line = comp.stderr.find((l) => l.includes("pair_code") && !l.includes(comp.code));
      if (line) resolve((JSON.parse(line) as { pair_code: string }).pair_code);
      else setTimeout(look, 25);
    };
    look();
  });
  const stranger = makeLink(OTHER).link;
  await assert.rejects(() => stranger.pair(comp.endpoint, fresh, "x"), { code: "pair_denied" });
  const noOrigin = new BridgeLink({
    vault: new Vault(memoryKeyStore(), memoryKV()),
    origin: EXT,
    fetch: (u, i) => fetch(u, i),
    now: Date.now,
  });
  await assert.rejects(() => noOrigin.pair(comp.endpoint, fresh, "x"), { code: "pair_denied" });
  await link.pair(comp.endpoint, fresh, "x");
  assert.equal((await link.status()).paired, true);
  await comp.stop();
});

test("unpairing ends access on the companion and a restarted companion still honours a renewal", async () => {
  const state = mkdtempSync(join(tmpdir(), "vsf-pair-"));
  cleanup.push(() => rmSync(state, { recursive: true, force: true }));
  const port = await freePort();
  let comp = await startCompanion(state, port);
  const { link, vault } = makeLink(EXT);
  await link.pair(comp.endpoint, comp.code, "x");
  const creds = await vault.open<{ access: string }>("bridge");

  await comp.stop();
  comp = await startCompanion(state, port);
  assert.ok(!readFileSync(join(state, "pairing.json"), "utf8").includes(creds?.access ?? "?"));
  const later = new BridgeLink({ vault, origin: EXT, fetch: asOrigin(EXT), now: () => Date.now() + 3 * 3600 * 1000 });
  const renewed = await later.accessToken();
  assert.ok(renewed, "the registry survived the restart");

  const raw = (access: string) =>
    fetch(`http://${comp.endpoint}/v1/envelope/verify`, {
      method: "POST",
      headers: {
        "content-type": "application/json",
        "x-vsf-bridge": "1",
        origin: EXT,
        authorization: `Bearer ${access}`,
      },
      body: "{}",
    });
  assert.notEqual((await raw(renewed.access)).status, 401);
  assert.deepEqual(await link.unpair(), { companionNotified: true });
  assert.equal((await raw(renewed.access)).status, 401);
  assert.deepEqual(await link.status(), { paired: false });
  await comp.stop();
});
