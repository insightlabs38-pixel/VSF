import type { Assembly, Fabric } from "@vsf/wasm-host";
import { assemble } from "@vsf/wasm-host";
import { type Envelope, parseEnvelope, type RestoreReason } from "./envelope.ts";
import { type KV, Lock } from "./kv.ts";
import { type CapabilityEntry, environmentDocument, localIdentity } from "./manifest.ts";
import type { Prefs, SettingsStore } from "./prefs.ts";
import {
  type Command,
  isCommandSender,
  normalizeOrigin,
  originPattern,
  parseCommand,
  pruneGrants,
  type Reply,
} from "./protocol.ts";
import { admit, checkSource, noteOp, SESSION_VERSION, type SenderLike, type SessionRecord } from "./session.ts";

export interface Stats {
  captures: number;
  denied: number;
  transforms: number;
  executeCalls: Record<string, number>;
}

interface Pending {
  tabId: number;
  origin: string;
  startedAt: number;
  state: "running" | "interrupted";
}

/** The only functions the worker may invoke inside a page; everything else is refused before injection. */
export const PAGE_CALLS = ["__vsfObserve", "__vsfRender", "__vsfUpdate", "__vsfRestore"] as const;
export type PageCall = (typeof PAGE_CALLS)[number];

export const MAX_SNAPSHOT_CHARS = 16 * 1024 * 1024;

export interface Companion {
  /** Forwards one authorized operation; resolves with the outcome the companion reports. */
  send(op: { session: string; id: string; action: string; entity: string; rowKey?: string }): Promise<{
    outcome: "committed" | "rejected" | "unknown";
    detail?: string;
  }>;
}

export interface LinkPort {
  call(path: string, body: unknown): Promise<{ status: number; json: Record<string, unknown> }>;
  pair(endpoint: string, code: string, name: string): Promise<{ clientId: string }>;
  unpair(): Promise<{ companionNotified: boolean }>;
  status(): Promise<Record<string, unknown>>;
}

export interface Ports {
  selfId: string;
  version: string;
  panelUrl: string;
  local: KV;
  session: KV;
  tab(tabId: number): Promise<{ url?: string | undefined } | null>;
  permissionHeld(pattern: string): Promise<boolean>;
  removePermission(pattern: string): Promise<boolean>;
  /** Runs packaged files in the tab and returns the last file's completion value. */
  inject(tabId: number, files: string[]): Promise<unknown>;
  call(tabId: number, fn: PageCall, args: unknown[]): Promise<{ result: unknown; documentId?: string } | undefined>;
  fabric(): Promise<Fabric>;
  sha256Hex(text: string): Promise<string>;
  now(): number;
  perf(): number;
  uuid(): string;
  companion?: Companion;
  link?: LinkPort;
  settings?: SettingsStore;
}

const EMPTY_STATS: Stats = { captures: 0, denied: 0, transforms: 0, executeCalls: {} };

interface ObserveResult {
  origin: string;
  epoch: number;
  nodes: number;
  state: string;
  captureMs: number;
  capabilities: CapabilityEntry[];
  missing: unknown[];
  shadow: { open: number; closed: number };
  env: unknown;
  b64: string;
  snapshot?: string;
}

interface RenderResult {
  ok: boolean;
  error?: string;
  problems?: string[];
}

const err = (e: unknown): string => (e instanceof Error ? e.message.slice(0, 200) : "unknown");

export function createCore(p: Ports) {
  const lock = new Lock();
  const bootId = p.uuid();

  const grants = async (): Promise<Record<string, { at: number }>> =>
    ((await p.local.get("grants")) as Record<string, { at: number }> | undefined) ?? {};
  const stats = async (): Promise<Stats> => ({
    ...structuredClone(EMPTY_STATS),
    ...((await p.session.get("stats")) as Partial<Stats> | undefined),
  });
  const bump = (mutate: (s: Stats) => void): Promise<void> =>
    lock.run(async () => {
      const s = await stats();
      mutate(s);
      await p.session.set("stats", s);
    });
  const pending = async (): Promise<Record<string, Pending>> =>
    ((await p.session.get("pending")) as Record<string, Pending> | undefined) ?? {};
  const setPending = (mutate: (x: Record<string, Pending>) => void): Promise<void> =>
    lock.run(async () => {
      const x = await pending();
      mutate(x);
      await p.session.set("pending", x);
    });
  const rawSessions = async (): Promise<Record<string, SessionRecord>> =>
    ((await p.session.get("sessions")) as Record<string, SessionRecord> | undefined) ?? {};
  const sessions = async (): Promise<Record<string, SessionRecord>> => {
    const all = await rawSessions();
    return Object.fromEntries(Object.entries(all).filter(([, s]) => s?.v === SESSION_VERSION));
  };
  const mutateSessions = <T>(fn: (all: Record<string, SessionRecord>) => T): Promise<T> =>
    lock.run(async () => {
      const everything = await rawSessions();
      const entries = Object.entries(everything);
      const mine = Object.fromEntries(entries.filter(([, s]) => s?.v === SESSION_VERSION));
      const foreign = Object.fromEntries(entries.filter(([, s]) => s?.v !== SESSION_VERSION));
      const out = fn(mine);
      await p.session.set("sessions", { ...foreign, ...mine });
      return out;
    });

  const deny = async (error: string, detail?: string): Promise<Reply> => {
    await bump((s) => {
      s.denied += 1;
    });
    return detail === undefined ? { ok: false, error } : { ok: false, error, detail };
  };

  const callPage = async (tabId: number, fn: PageCall, args: unknown[]) => {
    if (!PAGE_CALLS.includes(fn)) throw new Error("page call not allowed");
    return p.call(tabId, fn, args);
  };

  async function originOf(tabId: number): Promise<{ origin: string } | Reply> {
    let url: string | undefined;
    try {
      const tab = await p.tab(tabId);
      if (tab === null) return deny("bad_request", "no such tab");
      url = tab.url;
    } catch {
      return deny("bad_request", "no such tab");
    }
    const origin = url ? normalizeOrigin(new URL(url).origin) : null;
    if (origin === null) return deny("origin_unobservable");
    if (!(origin in (await grants()))) return deny("origin_not_granted");
    return { origin };
  }

  const countExecute = (origin: string): Promise<void> =>
    bump((s) => {
      s.executeCalls[origin] = (s.executeCalls[origin] ?? 0) + 1;
    });

  const unpack = (b64: string): Uint8Array => {
    const bin = atob(b64);
    const out = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
    return out;
  };

  async function capture(tabId: number): Promise<Reply> {
    const t = await originOf(tabId);
    if ("ok" in t) return t;
    const { origin } = t;
    const id = `${tabId}:${p.now()}`;
    await setPending((x) => {
      x[id] = { tabId, origin, startedAt: p.now(), state: "running" };
    });
    await countExecute(origin);
    try {
      const t0 = p.perf();
      const r = (await p.inject(tabId, ["content.js"])) as
        | { origin: string; nodes: number; truncated: boolean; captureMs: number; packMs: number; b64: string }
        | undefined;
      const t1 = p.perf();
      if (!r) return deny("inject_failed", "no result");
      if (r.origin !== origin) return deny("origin_changed");
      const pack = unpack(r.b64);
      const t2 = p.perf();
      const summary = JSON.parse(new TextDecoder().decode((await p.fabric()).analyzePack(pack))) as Record<
        string,
        unknown
      >;
      const t3 = p.perf();
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
      return deny("inject_failed", err(e));
    } finally {
      await setPending((x) => void delete x[id]);
    }
  }

  async function observe(tabId: number): Promise<Reply> {
    const t = await originOf(tabId);
    if ("ok" in t) return t;
    const { origin } = t;
    await countExecute(origin);
    try {
      await p.inject(tabId, ["observe.js"]);
      const got = await p.call(tabId, "__vsfObserve", [origin, false]);
      const r = got?.result as ObserveResult | undefined;
      if (!r) return deny("inject_failed", "no result");
      if (r.origin !== origin) return deny("origin_changed");
      const pack = unpack(r.b64);
      const summary = JSON.parse(new TextDecoder().decode((await p.fabric()).analyzePack(pack))) as Record<
        string,
        unknown
      >;
      await bump((s) => {
        s.captures += 1;
      });
      const { b64: _unused, snapshot: _snap, ...rest } = r;
      return { ok: true, ...rest, summary, packBytes: pack.length };
    } catch (e) {
      return deny("inject_failed", err(e));
    }
  }

  const variantId = async (origin: string): Promise<string> => {
    let salt = (await p.local.get("variant_salt")) as string | undefined;
    if (!salt) {
      salt = p.uuid();
      await p.local.set("variant_salt", salt);
    }
    return `var_${(await p.sha256Hex(`vsf.variant.local\0${salt}\0${origin}`)).slice(0, 32)}`;
  };

  /** One observation turned into an assembly; the page is only read. */
  async function observeAndAssemble(
    tabId: number,
    origin: string,
  ): Promise<
    | {
        assembly: Assembly;
        epoch: number;
        nodes: number;
        observeMs: number;
        assembleMs: number;
        capabilities: CapabilityEntry[];
      }
    | Reply
  > {
    await countExecute(origin);
    const t0 = p.perf();
    await p.inject(tabId, ["observe.js"]);
    const got = await p.call(tabId, "__vsfObserve", [origin, true]);
    const r = got?.result as ObserveResult | undefined;
    if (!r?.snapshot) return deny("inject_failed", "no result");
    if (r.origin !== origin) return deny("origin_changed");
    if (r.snapshot.length > MAX_SNAPSHOT_CHARS) return deny("snapshot_too_large");
    const t1 = p.perf();
    const assembly = assemble(
      await p.fabric(),
      { origin, variantId: await variantId(origin), observedAt: p.now(), epoch: r.epoch },
      new TextEncoder().encode(r.snapshot),
    );
    return {
      assembly,
      epoch: r.epoch,
      nodes: r.nodes,
      observeMs: t1 - t0,
      assembleMs: p.perf() - t1,
      capabilities: r.capabilities,
    };
  }

  const slim = (a: Assembly) => ({
    semantic: {
      entities: (a.semantic.body as { entities: unknown }).entities,
      actions: (a.semantic.body as { actions: unknown }).actions,
    },
    digest: a.semantic_digest,
    views: a.views,
    rows: a.rows,
    actions: Object.fromEntries(Object.entries(a.actions).map(([k, v]) => [k, { state: v.state, reason: v.reason }])),
    truncated: a.limits.truncated,
  });

  const actionStates = (a: Assembly): SessionRecord["actions"] =>
    Object.fromEntries(Object.entries(a.actions).map(([k, v]) => [k, { state: v.state, reason: v.reason }]));

  async function dropSession(id: string): Promise<void> {
    const rec = (await sessions())[id];
    await mutateSessions((all) => void delete all[id]);
    const b = rec?.bridge;
    if (b && "id" in b && p.link) await p.link.call("/v1/session/end", { session_id: b.id }).catch(() => undefined);
  }

  /** Asks the companion to bind this document's capability manifest; its absence or refusal never affects the page. */
  async function bind(
    rec: SessionRecord,
    assembly: Assembly,
    observation: CapabilityEntry[],
  ): Promise<SessionRecord["bridge"]> {
    if (!p.link || !rec.documentId) return undefined;
    try {
      if (!(await p.link.status())["paired"]) return undefined;
      let salt = (await p.local.get("variant_salt")) as string | undefined;
      if (!salt) {
        salt = p.uuid();
        await p.local.set("variant_salt", salt);
      }
      const ids = await localIdentity(p.selfId, salt, p.sha256Hex);
      const body = assembly.semantic.body as {
        compiler_version: string;
        source_app_id: string;
        source_variant_id: string;
      };
      const manifest = environmentDocument({
        runtimeId: ids.runtimeId,
        version: p.version,
        compilerVersion: body.compiler_version,
        generatedAt: p.now(),
        principalRef: ids.principalRef,
        origin: rec.origin,
        appId: body.source_app_id,
        variantId: body.source_variant_id,
        observation,
      });
      const report = JSON.parse(
        new TextDecoder().decode((await p.fabric()).validateIr(new TextEncoder().encode(JSON.stringify(manifest)))),
      ) as { valid?: boolean };
      if (report.valid !== true) return { error: "manifest_invalid" };
      const r = await p.link.call("/v1/session", { origin: rec.origin, document: rec.documentId, manifest });
      if (r.status !== 200) return { error: String(r.json["error"] ?? `status_${r.status}`) };
      return { id: String(r.json["session_id"]), expiresAt: Number(r.json["expires_at"]) };
    } catch (e) {
      const code = (e as { code?: unknown }).code;
      return { error: typeof code === "string" ? code : "bridge_unavailable" };
    }
  }

  async function renewBinding(rec: SessionRecord): Promise<void> {
    const b = rec.bridge;
    if (!b || !("id" in b) || !p.link || b.expiresAt * 1000 - p.now() > 5 * 60_000) return;
    try {
      const r = await p.link.call("/v1/session/renew", { session_id: b.id });
      await mutateSessions((all) => {
        const s = all[rec.id];
        if (s)
          s.bridge =
            r.status === 200
              ? { id: b.id, expiresAt: Number(r.json["expires_at"]) }
              : { error: String(r.json["error"] ?? "renew_failed") };
      });
    } catch {
      // the next heartbeat tries again; the binding simply lapses if the companion stays away
    }
  }

  async function restoreTab(tabId: number, id: string, reason: RestoreReason): Promise<void> {
    try {
      await callPage(tabId, "__vsfRestore", [id, reason]);
    } catch {
      // the tab may already be gone; the record is removed either way
    }
  }

  async function transform(tabId: number): Promise<Reply> {
    const t = await originOf(tabId);
    if ("ok" in t) return t;
    const { origin } = t;
    if (!(await p.permissionHeld(originPattern(origin)))) return deny("permission_not_held");
    const prior = Object.values(await sessions()).find((s) => s.tabId === tabId);
    if (prior) {
      await restoreTab(tabId, prior.id, "user");
      await dropSession(prior.id);
    }
    const pendingId = `transform:${tabId}:${p.now()}`;
    await setPending((x) => {
      x[pendingId] = { tabId, origin, startedAt: p.now(), state: "running" };
    });
    let sessionId: string | undefined;
    try {
      const built = await observeAndAssemble(tabId, origin);
      if ("ok" in built) return built;
      const { assembly } = built;
      if (assembly.views.length === 0) return deny("nothing_recognized");
      sessionId = p.uuid();
      const id = sessionId;
      const now = p.now();
      await mutateSessions((all) => {
        all[id] = {
          v: SESSION_VERSION,
          id,
          tabId,
          origin,
          documentId: undefined,
          epoch: built.epoch,
          semanticDigest: assembly.semantic_digest,
          shapeDigest: assembly.shape_digest,
          phase: "assembling",
          lastSeq: 0,
          createdAt: now,
          seenAt: now,
          actions: actionStates(assembly),
          ops: [],
        };
      });
      const t2 = p.perf();
      const stored = p.settings ? await p.settings.load(origin).catch(() => "unreadable" as const) : null;
      const prefs: Prefs = stored === null || stored === "unreadable" ? {} : stored;
      await p.inject(tabId, ["render.js"]);
      const rendered = await callPage(tabId, "__vsfRender", [
        { session: id, seq: 0, origin, assembly: slim(assembly), prefs },
      ]);
      const rr = rendered?.result as RenderResult | undefined;
      if (!rr?.ok) {
        await restoreTab(tabId, id, "crash");
        await dropSession(id);
        return deny("render_failed", rr?.error ?? "no result");
      }
      await mutateSessions((all) => {
        const s = all[id];
        if (s) {
          s.phase = "rendered";
          s.documentId = rendered?.documentId;
        }
      });
      const rec = (await sessions())[id];
      if (rec) {
        const bridge = await bind(rec, assembly, built.capabilities);
        if (bridge)
          await mutateSessions((all) => {
            const s = all[id];
            if (s) s.bridge = bridge;
          });
      }
      await bump((s) => {
        s.transforms += 1;
      });
      const rows = Object.values(assembly.rows).reduce((n, r) => n + r.length, 0);
      const states: Record<string, number> = {};
      for (const a of Object.values(assembly.actions)) states[a.state] = (states[a.state] ?? 0) + 1;
      return {
        ok: true,
        session: id,
        origin,
        entities: assembly.views.length,
        rows,
        actions: states,
        withheld: assembly.withheld.length,
        settings: stored === "unreadable" ? "unreadable" : stored === null ? "default" : "restored",
        truncated: assembly.limits.truncated,
        nodes: built.nodes,
        timings: { observeMs: built.observeMs, assembleMs: built.assembleMs, renderMs: p.perf() - t2 },
      };
    } catch (e) {
      if (sessionId) {
        const id = sessionId;
        await restoreTab(tabId, id, "crash");
        await dropSession(id);
      }
      return deny("transform_failed", err(e));
    } finally {
      await setPending((x) => void delete x[pendingId]);
    }
  }

  async function restore(tabId: number): Promise<Reply> {
    const found = Object.values(await sessions()).find((s) => s.tabId === tabId);
    if (!found) return { ok: true, restored: false };
    await restoreTab(tabId, found.id, "user");
    await dropSession(found.id);
    return { ok: true, restored: true, session: found.id };
  }

  async function handleCommand(raw: unknown, sender: SenderLike): Promise<Reply> {
    await ready();
    if (!isCommandSender(sender, p.selfId, p.panelUrl)) return deny("sender_not_allowed");
    const cmd: Command | null = parseCommand(raw);
    if (cmd === null) return deny("bad_request");
    switch (cmd.type) {
      case "status": {
        const all = await sessions();
        return {
          ok: true,
          bootId,
          grants: Object.keys(await grants()),
          stats: await stats(),
          pending: await pending(),
          sessions: Object.values(all).map((s) => ({
            id: s.id,
            tabId: s.tabId,
            origin: s.origin,
            phase: s.phase,
            lastSeq: s.lastSeq,
            bridge: s.bridge ?? null,
            ops: s.ops.map((o) => ({ id: o.id, kind: o.kind, state: o.state })),
          })),
        };
      }
      case "grant": {
        if (!(await p.permissionHeld(originPattern(cmd.origin)))) return deny("permission_not_held");
        await lock.run(async () => {
          const g = await grants();
          g[cmd.origin] = { at: p.now() };
          await p.local.set("grants", g);
        });
        return { ok: true, origin: cmd.origin };
      }
      case "revoke": {
        await lock.run(async () => {
          const g = await grants();
          delete g[cmd.origin];
          await p.local.set("grants", g);
        });
        for (const s of Object.values(await sessions()))
          if (s.origin === cmd.origin) {
            await restoreTab(s.tabId, s.id, "user");
            await dropSession(s.id);
          }
        let removed = false;
        try {
          removed = await p.removePermission(originPattern(cmd.origin));
        } catch {
          removed = false;
        }
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
      case "pair":
        return withLink(async (link) => ({
          ok: true,
          ...(await link.pair(cmd.endpoint, cmd.code, "Varifold extension")),
        }));
      case "unpair":
        return withLink(async (link) => ({ ok: true, ...(await link.unpair()) }));
      case "link":
        return withLink(async (link) => ({ ok: true, ...(await link.status()) }));
    }
  }

  async function withLink(run: (link: LinkPort) => Promise<Reply>): Promise<Reply> {
    if (!p.link) return deny("pairing_unavailable");
    try {
      return await run(p.link);
    } catch (e) {
      const code = (e as { code?: unknown }).code;
      return deny(typeof code === "string" ? code : "internal");
    }
  }

  async function currentPrefs(origin: string): Promise<Prefs> {
    const stored = p.settings ? await p.settings.load(origin).catch(() => null) : null;
    return stored === null || stored === "unreadable" ? {} : stored;
  }

  const refreshing = new Set<string>();

  async function refresh(session: SessionRecord): Promise<Reply> {
    if (refreshing.has(session.id)) return { ok: true, changed: "none", coalesced: true };
    refreshing.add(session.id);
    try {
      return await refreshNow(session);
    } finally {
      refreshing.delete(session.id);
    }
  }

  async function refreshNow(session: SessionRecord): Promise<Reply> {
    const built = await observeAndAssemble(session.tabId, session.origin);
    if ("ok" in built) return built;
    const { assembly } = built;
    const same = assembly.shape_digest === session.shapeDigest;
    const fn: PageCall = same ? "__vsfUpdate" : "__vsfRender";
    const arg = same
      ? { session: session.id, rows: assembly.rows, actions: slim(assembly).actions }
      : {
          session: session.id,
          seq: session.lastSeq,
          origin: session.origin,
          assembly: slim(assembly),
          prefs: await currentPrefs(session.origin),
        };
    await p.inject(session.tabId, ["render.js"]);
    const got = await callPage(session.tabId, fn, [arg]);
    const rr = got?.result as RenderResult | undefined;
    if (!rr?.ok) return deny("render_failed", rr?.error ?? "no result");
    await mutateSessions((all) => {
      const s = all[session.id];
      if (s) {
        s.semanticDigest = same ? s.semanticDigest : assembly.semantic_digest;
        s.shapeDigest = assembly.shape_digest;
        s.actions = actionStates(assembly);
        s.epoch = built.epoch;
      }
    });
    return {
      ok: true,
      changed: same ? "rows" : "structure",
      rows: Object.values(assembly.rows).reduce((n, r) => n + r.length, 0),
      timings: { observeMs: built.observeMs, assembleMs: built.assembleMs },
    };
  }

  const settle = (sessionId: string, id: string, kind: string, state: "done" | "unknown", reply: Reply) =>
    mutateSessions((all) => {
      const s = all[sessionId];
      if (s) noteOp(s, { id, kind, state, reply, at: p.now() });
    });

  async function dispatch(session: SessionRecord, env: Envelope<"dispatch">): Promise<Reply> {
    const { action, entity, rowKey } = env.body;
    const known = session.actions[action];
    let reply: Reply;
    let state: "done" | "unknown" = "done";
    if (!known) reply = await deny("unknown_action");
    else if (known.state !== "available") reply = await deny("action_unavailable", known.reason ?? known.state);
    else if (!p.companion) reply = await deny("companion_required");
    else {
      try {
        const sent: Parameters<Companion["send"]>[0] = { session: session.id, id: env.id, action, entity };
        if (rowKey !== undefined) sent.rowKey = rowKey;
        const out = await p.companion.send(sent);
        reply = out.outcome === "committed" ? { ok: true, outcome: out.outcome } : { ok: false, error: out.outcome };
        if (out.outcome === "unknown") state = "unknown";
      } catch {
        reply = { ok: false, error: "unknown" };
        state = "unknown";
      }
    }
    await settle(session.id, env.id, "dispatch", state, reply);
    return reply;
  }

  const RECORDED = new Set(["dispatch", "refresh", "restore"]);

  async function handlePage(raw: unknown, sender: SenderLike): Promise<Reply> {
    await ready();
    const parsed = parseEnvelope(raw);
    if (!parsed.ok) return deny(parsed.error);
    const env = parsed.env;
    const known = (await sessions())[env.session];
    if (!known && (await rawSessions())[env.session]) return deny("session_version_unsupported");
    const src = checkSource(sender, p.selfId, known);
    if (!src.ok) return deny(src.error);
    const verdict = await mutateSessions((all) => {
      const s = all[env.session];
      if (!s) return { kind: "refused", error: "session_unknown" } as const;
      const v = admit(s, env);
      if (v.kind !== "new") return v;
      s.lastSeq = Math.max(s.lastSeq, env.seq);
      s.seenAt = p.now();
      if (RECORDED.has(env.kind)) noteOp(s, { id: env.id, kind: env.kind, state: "sent", at: p.now() });
      return { kind: "new", session: structuredClone(s) } as const;
    });
    if (verdict.kind === "refused") return deny(verdict.error);
    if (verdict.kind === "duplicate") {
      const reply = verdict.op.reply as Reply | undefined;
      return reply ?? { ok: false, error: verdict.op.state === "done" ? "duplicate" : "unknown" };
    }
    const session = verdict.session;
    switch (env.kind) {
      case "hello":
      case "heartbeat":
        if (env.kind === "heartbeat") await renewBinding(session);
        return { ok: true, phase: session.phase };
      case "restore": {
        await dropSession(session.id);
        return { ok: true, restored: true, reason: (env.body as { reason: string }).reason };
      }
      case "refresh": {
        const reply = await refresh(session);
        await settle(session.id, env.id, "refresh", "done", reply);
        return reply;
      }
      case "prefs": {
        if (!p.settings) return { ok: true, saved: false };
        await p.settings.save(session.origin, (env.body as Envelope<"prefs">["body"]).prefs);
        return { ok: true, saved: true };
      }
      case "dispatch":
        return dispatch(session, env as Envelope<"dispatch">);
    }
  }

  async function onPermissionsRemoved(patterns: readonly string[]): Promise<void> {
    await ready();
    await lock.run(async () => p.local.set("grants", pruneGrants(await grants(), patterns)));
    for (const s of Object.values(await sessions())) {
      if (!(s.origin in (await grants()))) {
        await restoreTab(s.tabId, s.id, "removed");
        await dropSession(s.id);
      }
    }
  }

  /** After a worker restart: interrupted work is surfaced, half-built sessions are undone, sent operations are never resent. */
  async function reconcile(): Promise<void> {
    await setPending((x) => {
      for (const rec of Object.values(x)) if (rec.state === "running") rec.state = "interrupted";
    });
    const all = await sessions();
    for (const s of Object.values(all)) {
      if (s.phase === "assembling") {
        await restoreTab(s.tabId, s.id, "crash");
        await mutateSessions((m) => void delete m[s.id]);
      } else if (s.ops.some((o) => o.state === "sent")) {
        await mutateSessions((m) => {
          const rec = m[s.id];
          if (!rec) return;
          for (const o of rec.ops) if (o.state === "sent") o.state = "unknown";
        });
      }
    }
  }
  let started: Promise<void> | undefined;
  // Not at load: storage calls in the first tick of a worker left it running past idle termination in about half of the runs.
  const ready = (): Promise<void> => {
    started ??= reconcile();
    return started;
  };

  return { handleCommand, handlePage, onPermissionsRemoved, ready, bootId };
}

export type Core = ReturnType<typeof createCore>;
