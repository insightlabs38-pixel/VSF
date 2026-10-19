import { type Fabric, loadFabric } from "@vsf/wasm-host";
import { Chain, type Expected } from "./envelope.ts";
import { forward } from "./forward.ts";
import { Ledger, type Op } from "./ledger.ts";
import { type Deps, PipelineError, runStages } from "./pipeline.ts";
import {
  isCommandSender,
  normalizeOrigin,
  parseCommand,
  parseShellMessage,
  type Reply,
  type ShellMessage,
} from "./protocol.ts";
import { authenticate, newKey, type TabSession } from "./session.ts";

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

/** A VSF grant is only recorded for an origin the browser itself has granted the extension. */
async function holdsBrowserPermission(origin: string): Promise<boolean> {
  try {
    return await chrome.permissions.contains({ origins: [`${origin}/*`] });
  } catch {
    return false;
  }
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
const ledger = new Ledger({
  async get() {
    const { ops } = await chrome.storage.session.get("ops");
    return (ops as Record<string, Op> | undefined) ?? {};
  },
  async set(ops) {
    await chrome.storage.session.set({ ops });
  },
});
const sessions = new Map<number, TabSession>();
const ready = reconcile().then(() => ledger.reconcile());

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

interface Transform {
  epoch: string;
  origin: string;
  app: string;
  state: "ready" | "source_ui";
}

const chain = new Chain();

async function transforms(): Promise<Record<string, Transform>> {
  const { transforms: t } = await chrome.storage.session.get("transforms");
  return (t as Record<string, Transform> | undefined) ?? {};
}

async function setTransform(tabId: number, value: Transform | undefined): Promise<void> {
  const t = await transforms();
  if (value === undefined) delete t[String(tabId)];
  else t[String(tabId)] = value;
  await chrome.storage.session.set({ transforms: t });
}

async function deps(): Promise<Deps> {
  const f = await getFabric();
  return {
    analyze: (pack) => JSON.parse(new TextDecoder().decode(f.analyzePack(pack))) as Record<string, unknown>,
    newId: () => crypto.randomUUID(),
  };
}

/** Observation, compile and render as three validated envelopes; the page changes only if all three hold. */
async function transform(tabId: number): Promise<Reply> {
  let origin: string | null = null;
  let path = "/";
  try {
    const tab = await chrome.tabs.get(tabId);
    const url = tab.url ? new URL(tab.url) : null;
    origin = url ? normalizeOrigin(url.origin) : null;
    path = url?.pathname ?? "/";
  } catch {
    return deny("bad_request", "no such tab");
  }
  if (origin === null) return deny("origin_unobservable");
  if (!(origin in (await grants()))) return deny("origin_not_granted");
  const expect: Expected = { tab: tabId, frame: 0, origin, epoch: crypto.randomUUID() };
  await bump((s) => {
    s.executeCalls[origin as string] = (s.executeCalls[origin as string] ?? 0) + 1;
  });
  try {
    await chrome.scripting.executeScript({ target: { tabId }, files: ["content.js"] });
    const stamp = { tab: tabId, epoch: expect.epoch, seq: 1, id: crypto.randomUUID() };
    const [injected] = await chrome.scripting.executeScript({
      target: { tabId },
      func: (st) => (globalThis as unknown as { __vsfEnvelope(s: unknown): Promise<unknown> }).__vsfEnvelope(st),
      args: [stamp],
    });
    const observation = chain.accept(injected?.result, expect);
    if (!observation.ok) return deny(observation.code);
    const stages = await runStages(observation.envelope, path, await deps());
    for (const e of [stages.compiled, stages.render]) {
      const checked = chain.accept(e, expect);
      if (!checked.ok) return deny(checked.code);
    }
    await chrome.scripting.executeScript({ target: { tabId }, files: ["shell.js"] });
    const [mounted] = await chrome.scripting.executeScript({
      target: { tabId },
      func: (a) =>
        (globalThis as unknown as { __vsfShell: { mount(a: unknown): Promise<unknown> } }).__vsfShell.mount(a),
      args: [
        {
          render: stages.render,
          tab: tabId,
          epoch: expect.epoch,
          compiled: { id: stages.compiled.id, seq: stages.compiled.seq },
          session: openSession(tabId, origin, expect.epoch),
          enabled: await enabledActions(stages.bundle),
        },
      ],
    });
    const result = mounted?.result as { ok: boolean; error?: string; state?: "ready" | "source_ui" } | undefined;
    if (!result?.ok) {
      sessions.delete(tabId);
      return deny("render_failed", result?.error ?? "no result");
    }
    if (result.state === "source_ui") sessions.delete(tabId);
    const app = (stages.render.body as { app: string }).app;
    await setTransform(tabId, { epoch: expect.epoch, origin, app, state: result.state ?? "source_ui" });
    await bump((s) => {
      s.captures += 1;
    });
    return {
      ok: true,
      origin,
      app,
      state: result.state,
      match: (stages.compiled.body as { match: string }).match,
      reason: (stages.render.body as { reason?: string }).reason ?? null,
      ids: { observation: observation.envelope.id, compiled: stages.compiled.id, render: stages.render.id },
    };
  } catch (e) {
    if (e instanceof PipelineError) return deny(e.code);
    return deny("inject_failed", e instanceof Error ? e.message.slice(0, 200) : "unknown");
  }
}

function openSession(tabId: number, origin: string, epoch: string): { key: string; epoch: string } {
  const s: TabSession = { key: newKey(), epoch, seq: 0, origin };
  sessions.set(tabId, s);
  return { key: s.key, epoch: s.epoch };
}

async function service(): Promise<string | null> {
  const { service: s } = await chrome.storage.local.get("service");
  return typeof s === "string" ? s : null;
}

/** Actions the page-side renderer may offer: only with a connected service, and never ones the bundle marks unusable. */
async function enabledActions(
  bundle:
    | { semantic: { actions: { id: string; effect: { declared: string } }[] }; unavailable?: Record<string, string> }
    | undefined,
): Promise<string[]> {
  if (bundle === undefined || (await service()) === null) return [];
  const blocked = bundle.unavailable ?? {};
  return bundle.semantic.actions
    .filter((a) => !(a.id in blocked) && ["read_only", "idempotent_write"].includes(a.effect.declared))
    .map((a) => a.id);
}

/** Puts the source page back exactly as it was and ends the transform session. */
async function restore(tabId: number): Promise<Reply> {
  const t = (await transforms())[String(tabId)];
  if (t === undefined) return { ok: true, restored: false };
  try {
    await chrome.scripting.executeScript({
      target: { tabId },
      func: () => (globalThis as unknown as { __vsfShell?: { unmount(): boolean } }).__vsfShell?.unmount() ?? false,
    });
  } catch {
    // The tab may be gone or navigated away; the session still ends.
  }
  chain.end(tabId, t.epoch);
  sessions.delete(tabId);
  await setTransform(tabId, undefined);
  return { ok: true, restored: true, app: t.app };
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
      return {
        ok: true,
        bootId,
        grants: Object.keys(await grants()),
        stats: await stats(),
        pending: await pending(),
        transforms: await transforms(),
        ops: await ledger.all(),
        sessions: [...sessions.keys()],
        service: await service(),
      };
    case "grant": {
      if (!(await holdsBrowserPermission(cmd.origin))) return deny("permission_not_held", cmd.origin);
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
        removed = await chrome.permissions.remove({ origins: [`${cmd.origin}/*`] });
      } catch {
        removed = false;
      }
      for (const [tab, t] of Object.entries(await transforms()))
        if (t.origin === cmd.origin) await restore(Number(tab));
      return { ok: true, origin: cmd.origin, browserPermissionRemoved: removed };
    }
    case "capture":
      return capture(cmd.tabId);
    case "observe":
      return observe(cmd.tabId);
    case "transform":
      return transform(cmd.tabId);
    case "restore":
      return restore(cmd.tabId);
    case "connect":
      await chrome.storage.local.set({ service: cmd.url });
      return { ok: true, service: cmd.url };
    case "disconnect":
      await chrome.storage.local.remove("service");
      return { ok: true };
  }
}

type ShellReply =
  | {
      ok: true;
      state: "done" | "unknown" | "in_flight";
      result?: { ok: boolean; message?: string };
      reason?: string;
      replayed?: boolean;
    }
  | { ok: false; error: string };

async function operate(
  msg: Extract<ShellMessage, { type: "dispatch" }>,
  tab: number,
  origin: string,
): Promise<ShellReply> {
  const base = await service();
  if (base === null) return { ok: false, error: "service_not_connected" };
  const begun = await ledger.begin(msg.requestId, tab, msg.action);
  if (!begun.fresh) {
    const op = begun.op;
    if (op.state === "done")
      return { ok: true, state: "done", ...(op.result ? { result: op.result } : {}), replayed: true };
    return {
      ok: true,
      state: op.state === "started" ? "in_flight" : "unknown",
      ...(op.reason ? { reason: op.reason } : {}),
    };
  }
  const outcome = await forward(base, {
    requestId: msg.requestId,
    action: msg.action,
    entity: msg.entity,
    ...(msg.rowKey === undefined ? {} : { rowKey: msg.rowKey }),
    values: msg.values,
    origin,
  });
  if (outcome.kind === "lost") {
    await ledger.lose(msg.requestId, outcome.reason);
    return { ok: true, state: "unknown", reason: outcome.reason };
  }
  const result = { ok: outcome.ok, ...(outcome.message === undefined ? {} : { message: outcome.message }) };
  await ledger.finish(msg.requestId, result);
  return { ok: true, state: "done", result };
}

async function handleShell(raw: unknown, sender: chrome.runtime.MessageSender): Promise<ShellReply> {
  await ready;
  const msg = parseShellMessage(raw);
  if (msg === null) return { ok: false, error: "bad_request" };
  const granted = Object.keys(await grants());
  if (msg.type === "reconnect") {
    const tab = sender.tab?.id;
    const record = tab === undefined ? undefined : (await transforms())[String(tab)];
    const origin = sender.origin ?? (sender.url ? new URL(sender.url).origin : undefined);
    if (sender.id !== chrome.runtime.id || sender.frameId !== 0 || tab === undefined || record === undefined)
      return { ok: false, error: "sender_not_allowed" };
    if (origin !== record.origin) return { ok: false, error: "origin_changed" };
    if (!granted.includes(record.origin)) return { ok: false, error: "origin_not_granted" };
    const old = sessions.get(tab);
    if (old !== undefined && old.epoch !== msg.epoch) return { ok: false, error: "stale_session" };
    const fresh = { key: newKey(), epoch: crypto.randomUUID(), seq: 0, origin: record.origin };
    sessions.set(tab, fresh);
    return {
      ok: true,
      state: "done",
      result: { ok: true, message: JSON.stringify({ key: fresh.key, epoch: fresh.epoch }) },
    };
  }
  const checked = authenticate(msg, sender, chrome.runtime.id, sessions, granted);
  if (!checked.ok) return { ok: false, error: checked.error };
  checked.auth.session.seq = msg.seq;
  return operate(msg, checked.auth.tab, checked.auth.session.origin);
}

chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
  const type = (message as { type?: unknown } | null)?.type;
  const fromShell = sender.tab !== undefined && (type === "dispatch" || type === "reconnect");
  const run = fromShell ? handleShell(message, sender) : handle(message, sender);
  run.then(sendResponse, () => sendResponse({ ok: false, error: "internal" }));
  return true;
});
