import { type Fabric, loadFabric } from "@vsf/wasm-host";
import { isCommandSender, normalizeOrigin, originPattern, parseCommand, pruneGrants, type Reply } from "./protocol.ts";

interface Stats {
  captures: number;
  denied: number;
  executeCalls: Record<string, number>;
}

interface Pending {
  tabId: number;
  origin: string;
  startedAt: number;
  state: "running" | "interrupted";
}

const EMPTY_STATS: Stats = { captures: 0, denied: 0, executeCalls: {} };
let fabric: Promise<Fabric> | undefined;
const bootId = crypto.randomUUID();

function getFabric(): Promise<Fabric> {
  fabric ??= fetch(chrome.runtime.getURL("vsf_wasm.wasm"))
    .then((r) => r.arrayBuffer())
    .then((bytes) => loadFabric(bytes));
  return fabric;
}

async function grants(): Promise<Record<string, { at: number }>> {
  const { grants: g } = await chrome.storage.local.get("grants");
  return (g as Record<string, { at: number }> | undefined) ?? {};
}

async function stats(): Promise<Stats> {
  const { stats: s } = await chrome.storage.session.get("stats");
  return (s as Stats | undefined) ?? structuredClone(EMPTY_STATS);
}

async function bump(mutate: (s: Stats) => void): Promise<void> {
  const s = await stats();
  mutate(s);
  await chrome.storage.session.set({ stats: s });
}

async function pending(): Promise<Record<string, Pending>> {
  const { pending: p } = await chrome.storage.session.get("pending");
  return (p as Record<string, Pending> | undefined) ?? {};
}

async function setPending(mutate: (p: Record<string, Pending>) => void): Promise<void> {
  const p = await pending();
  mutate(p);
  await chrome.storage.session.set({ pending: p });
}

const deny = async (error: string, detail?: string): Promise<Reply> => {
  await bump((s) => {
    s.denied += 1;
  });
  return detail === undefined ? { ok: false, error } : { ok: false, error, detail };
};

// A worker restart mid-capture leaves a record; surface it instead of silently losing the attempt.
async function reconcile(): Promise<void> {
  await setPending((p) => {
    for (const rec of Object.values(p)) if (rec.state === "running") rec.state = "interrupted";
  });
}
const ready = reconcile();

async function capture(tabId: number): Promise<Reply> {
  let origin: string | null = null;
  try {
    const tab = await chrome.tabs.get(tabId);
    origin = tab.url ? normalizeOrigin(new URL(tab.url).origin) : null;
  } catch {
    return deny("bad_request", "no such tab");
  }
  if (origin === null) return deny("origin_unobservable");
  if (!(origin in (await grants()))) return deny("origin_not_granted");
  const id = `${tabId}:${Date.now()}`;
  await setPending((p) => {
    p[id] = { tabId, origin: origin as string, startedAt: Date.now(), state: "running" };
  });
  await bump((s) => {
    s.executeCalls[origin as string] = (s.executeCalls[origin as string] ?? 0) + 1;
  });
  try {
    const t0 = performance.now();
    const results = await chrome.scripting.executeScript({ target: { tabId }, files: ["content.js"] });
    const t1 = performance.now();
    const r = results[0]?.result as
      | { origin: string; nodes: number; truncated: boolean; captureMs: number; packMs: number; b64: string }
      | undefined;
    if (!r) return deny("inject_failed", "no result");
    if (r.origin !== origin) return deny("origin_changed");
    const bin = atob(r.b64);
    const pack = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i++) pack[i] = bin.charCodeAt(i);
    const t2 = performance.now();
    const summary = JSON.parse(new TextDecoder().decode((await getFabric()).analyzePack(pack))) as Record<
      string,
      unknown
    >;
    const t3 = performance.now();
    await bump((s) => {
      s.captures += 1;
    });
    return {
      ok: true,
      origin,
      summary,
      packBytes: pack.length,
      timings: {
        captureMs: r.captureMs,
        packMs: r.packMs,
        injectRoundTripMs: t1 - t0,
        decodeMs: t2 - t1,
        recognizeMs: t3 - t2,
      },
    };
  } catch (e) {
    return deny("inject_failed", e instanceof Error ? e.message.slice(0, 200) : "unknown");
  } finally {
    await setPending((p) => void delete p[id]);
  }
}

interface ObserveResult {
  origin: string;
  epoch: number;
  nodes: number;
  state: string;
  captureMs: number;
  capabilities: unknown[];
  missing: unknown[];
  shadow: { open: number; closed: number };
  env: unknown;
  b64: string;
}

async function observe(tabId: number): Promise<Reply> {
  let origin: string | null = null;
  try {
    const tab = await chrome.tabs.get(tabId);
    origin = tab.url ? normalizeOrigin(new URL(tab.url).origin) : null;
  } catch {
    return deny("bad_request", "no such tab");
  }
  if (origin === null) return deny("origin_unobservable");
  if (!(origin in (await grants()))) return deny("origin_not_granted");
  await bump((s) => {
    s.executeCalls[origin as string] = (s.executeCalls[origin as string] ?? 0) + 1;
  });
  try {
    await chrome.scripting.executeScript({ target: { tabId }, files: ["observe.js"] });
    const results = await chrome.scripting.executeScript({
      target: { tabId },
      func: (o: string) => (globalThis as unknown as { __vsfObserve(o: string): unknown }).__vsfObserve(o),
      args: [origin],
    });
    const r = results[0]?.result as ObserveResult | undefined;
    if (!r) return deny("inject_failed", "no result");
    if (r.origin !== origin) return deny("origin_changed");
    const bin = atob(r.b64);
    const pack = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i++) pack[i] = bin.charCodeAt(i);
    const summary = JSON.parse(new TextDecoder().decode((await getFabric()).analyzePack(pack))) as Record<
      string,
      unknown
    >;
    await bump((s) => {
      s.captures += 1;
    });
    const { b64: _unused, ...rest } = r;
    return { ok: true, ...rest, summary, packBytes: pack.length };
  } catch (e) {
    return deny("inject_failed", e instanceof Error ? e.message.slice(0, 200) : "unknown");
  }
}

async function handle(raw: unknown, sender: chrome.runtime.MessageSender): Promise<Reply> {
  await ready;
  if (!isCommandSender(sender, chrome.runtime.id, chrome.runtime.getURL("panel.html")))
    return deny("sender_not_allowed");
  const cmd = parseCommand(raw);
  if (cmd === null) return deny("bad_request");
  switch (cmd.type) {
    case "status":
      return { ok: true, bootId, grants: Object.keys(await grants()), stats: await stats(), pending: await pending() };
    case "grant": {
      if (!(await chrome.permissions.contains({ origins: [originPattern(cmd.origin)] })))
        return deny("permission_not_held");
      const g = await grants();
      g[cmd.origin] = { at: Date.now() };
      await chrome.storage.local.set({ grants: g });
      return { ok: true, origin: cmd.origin };
    }
    case "revoke": {
      const g = await grants();
      delete g[cmd.origin];
      await chrome.storage.local.set({ grants: g });
      let removed = false;
      try {
        removed = await chrome.permissions.remove({ origins: [originPattern(cmd.origin)] });
      } catch {
        removed = false;
      }
      return { ok: true, origin: cmd.origin, browserPermissionRemoved: removed };
    }
    case "capture":
      return capture(cmd.tabId);
    case "observe":
      return observe(cmd.tabId);
  }
}

chrome.permissions.onRemoved.addListener((removed) => {
  void grants().then((g) => chrome.storage.local.set({ grants: pruneGrants(g, removed.origins ?? []) }));
});

chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
  handle(message, sender).then(sendResponse, () => sendResponse({ ok: false, error: "internal" }));
  return true;
});
