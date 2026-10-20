import assert from "node:assert/strict";
import { test } from "node:test";
import type { Assembly, Fabric } from "@vsf/wasm-host";
import { type Companion, createCore, type LinkPort, type PageCall, type Ports } from "../src/core.ts";
import { memoryKV } from "../src/kv.ts";
import type { Prefs, SettingsStore } from "../src/prefs.ts";
import { envelope } from "./helpers.ts";

const ORIGIN = "http://127.0.0.1:9";
const SELF = "extid";
const PANEL = "chrome-extension://extid/panel.html";
const TAB = 11;

function assembly(states: Record<string, "available" | "unknown">, shape = "a"): Assembly {
  const sha = (c: string) => `sha256:${c.repeat(64)}`;
  return {
    schema: "vsf-assembly-v1",
    observation: sha("0"),
    semantic: { schema: "vsf.semantic/1.2.0", schema_digest: sha("1"), body: { entities: [], actions: [] } },
    semantic_digest: sha("2"),
    shape_digest: sha(shape),
    views: [{ entity: "t_orders", layout: "grid" }],
    rows: { t_orders: [{ key: "k1", values: { f: "1" } }] },
    actions: Object.fromEntries(
      Object.entries(states).map(([k, state]) => [
        k,
        { state, reason: state === "available" ? null : "effect_unverified", sources: [] },
      ]),
    ),
    withheld: [],
    limits: { nodes: 4, truncated: false },
  };
}

interface Rig {
  ports: Ports;
  calls: { fn: PageCall | "inject"; args: unknown[] }[];
  sent: string[];
  next: { assembly: Assembly; render: { ok: boolean; error?: string }; valid: boolean };
  local: ReturnType<typeof memoryKV>;
  session: ReturnType<typeof memoryKV>;
  panel: { id: string; url: string; frameId: number; tab?: undefined };
  page: (
    tab: number,
    doc?: string,
  ) => { id: string; url: string; origin: string; tab: { id: number }; frameId: number; documentId?: string };
}

function rig(
  opts: {
    local?: ReturnType<typeof memoryKV>;
    session?: ReturnType<typeof memoryKV>;
    companion?: Companion;
    link?: LinkPort;
    settings?: SettingsStore;
  } = {},
): Rig {
  const local = opts.local ?? memoryKV();
  const session = opts.session ?? memoryKV();
  const calls: Rig["calls"] = [];
  const sent: string[] = [];
  const next: Rig["next"] = { assembly: assembly({ act: "unknown" }), render: { ok: true }, valid: true };
  let n = 0;
  const fabric = {
    abiVersion: 4,
    analyzePack: () => new Uint8Array(),
    compile: () => new Uint8Array(),
    validateIr: () => new TextEncoder().encode(JSON.stringify({ valid: next.valid })),
    assemble: () => new TextEncoder().encode(JSON.stringify(next.assembly)),
  } as Fabric;
  const ports: Ports = {
    selfId: SELF,
    version: "0.0.1",
    panelUrl: PANEL,
    local,
    session,
    tab: async () => ({ url: `${ORIGIN}/page` }),
    permissionHeld: async () => true,
    removePermission: async () => true,
    inject: async (_t, files) => {
      calls.push({ fn: "inject", args: files });
      return files[0] === "observe.js" ? undefined : undefined;
    },
    call: async (_t, fn, args) => {
      calls.push({ fn, args });
      if (fn === "__vsfObserve")
        return {
          result: {
            origin: ORIGIN,
            epoch: 1,
            nodes: 4,
            state: "ok",
            captureMs: 1,
            capabilities: [],
            missing: [],
            shadow: { open: 0, closed: 0 },
            env: {},
            b64: "",
            snapshot: "{}",
          },
        };
      if (fn === "__vsfRender") return { result: next.render, documentId: "doc-1" };
      return { result: { ok: true } };
    },
    fabric: async () => fabric,
    sha256Hex: async (t) => String(t.length).padStart(64, "0"),
    now: () => 1_000 + n++,
    perf: () => 0,
    uuid: () => `00000000-0000-4000-8000-${String(++n).padStart(12, "0")}`,
    ...(opts.companion ? { companion: opts.companion } : {}),
    ...(opts.link ? { link: opts.link } : {}),
    ...(opts.settings ? { settings: opts.settings } : {}),
  };
  return {
    ports,
    calls,
    sent,
    next,
    local,
    session,
    panel: { id: SELF, url: PANEL, frameId: 0 },
    page: (tab, doc = "doc-1") => ({
      id: SELF,
      url: `${ORIGIN}/page`,
      origin: ORIGIN,
      tab: { id: tab },
      frameId: 0,
      documentId: doc,
    }),
  };
}

async function transformed(r: Rig) {
  const core = createCore(r.ports);
  await core.handleCommand({ type: "grant", origin: ORIGIN }, r.panel);
  const reply = await core.handleCommand({ type: "transform", tabId: TAB }, r.panel);
  assert.equal(reply.ok, true, JSON.stringify(reply));
  const sessions = (await r.session.get("sessions")) as Record<string, { id: string }>;
  return { core, id: Object.keys(sessions)[0] as string };
}

test("a transform injects the renderer before calling it and records a rendered session bound to the document", async () => {
  const r = rig();
  const { id } = await transformed(r);
  const order = r.calls.map((c) => (c.fn === "inject" ? `inject:${(c.args as string[])[0]}` : c.fn));
  assert.deepEqual(order, ["inject:observe.js", "__vsfObserve", "inject:render.js", "__vsfRender"]);
  const rec = ((await r.session.get("sessions")) as Record<string, Record<string, unknown>>)[id];
  assert.equal(rec?.phase, "rendered");
  assert.equal(rec?.documentId, "doc-1");
  assert.equal(rec?.tabId, TAB);
});

test("a render failure restores the tab, removes the session and reports why", async () => {
  const r = rig();
  r.next.render = { ok: false, error: "presentation_rejected" };
  const core = createCore(r.ports);
  await core.handleCommand({ type: "grant", origin: ORIGIN }, r.panel);
  const reply = await core.handleCommand({ type: "transform", tabId: TAB }, r.panel);
  assert.deepEqual(reply, { ok: false, error: "render_failed", detail: "presentation_rejected" });
  assert.ok(r.calls.some((c) => c.fn === "__vsfRestore"));
  assert.deepEqual(await r.session.get("sessions"), {});
});

test("nothing recognized means nothing is rendered", async () => {
  const r = rig();
  r.next.assembly = { ...assembly({}), views: [] };
  const core = createCore(r.ports);
  await core.handleCommand({ type: "grant", origin: ORIGIN }, r.panel);
  assert.deepEqual(await core.handleCommand({ type: "transform", tabId: TAB }, r.panel), {
    ok: false,
    error: "nothing_recognized",
  });
  assert.ok(!r.calls.some((c) => c.fn === "__vsfRender"));
});

test("only the packaged panel may issue commands and a granted origin is required", async () => {
  const r = rig();
  const core = createCore(r.ports);
  const page = r.page(TAB);
  assert.equal((await core.handleCommand({ type: "transform", tabId: TAB }, page)).error, "sender_not_allowed");
  assert.equal((await core.handleCommand({ type: "transform", tabId: TAB }, r.panel)).error, "origin_not_granted");
});

test("an operation is sent to the companion once, even when the same envelope arrives twice at the same time", async () => {
  let sends = 0;
  const companion: Companion = {
    async send() {
      sends++;
      await new Promise((res) => setTimeout(res, 5));
      return { outcome: "committed" };
    },
  };
  const r = rig({ companion });
  r.next.assembly = assembly({ act_save: "available" });
  const { core, id } = await transformed(r);
  const e = envelope("dispatch", id, 5, { action: "act_save", entity: "t_orders" });
  const replies = await Promise.all([core.handlePage(e, r.page(TAB)), core.handlePage(e, r.page(TAB))]);
  assert.equal(sends, 1);
  assert.equal(replies.filter((x) => x.ok).length, 1);
  const again = await core.handlePage(e, r.page(TAB));
  assert.deepEqual(again, { ok: true, outcome: "committed" });
  assert.equal(sends, 1);
});

test("after a worker restart an operation that may have happened is reported unknown and never resent", async () => {
  let sends = 0;
  const companion: Companion = {
    async send() {
      sends++;
      if (sends === 1) return new Promise(() => undefined);
      return { outcome: "committed" };
    },
  };
  const r = rig({ companion });
  r.next.assembly = assembly({ act_save: "available" });
  const { core, id } = await transformed(r);
  const e = envelope("dispatch", id, 5, { action: "act_save", entity: "t_orders" });
  void core.handlePage(e, r.page(TAB));
  await new Promise((res) => setTimeout(res, 20));
  assert.equal(sends, 1);

  const restarted = createCore({ ...r.ports, companion });
  await restarted.ready();
  const status = (await restarted.handleCommand({ type: "status" }, r.panel)) as unknown as {
    sessions: { ops: { state: string }[] }[];
  };
  assert.equal(status.sessions[0]?.ops[0]?.state, "unknown");
  assert.deepEqual(await restarted.handlePage(e, r.page(TAB)), { ok: false, error: "unknown" });
  assert.equal(sends, 1, "the interrupted request is not sent again");
  const fresh = await restarted.handlePage(
    envelope("dispatch", id, 6, { action: "act_save", entity: "t_orders" }),
    r.page(TAB),
  );
  assert.deepEqual(fresh, { ok: true, outcome: "committed" });
  assert.equal(sends, 2, "a new request is a new decision");
});

test("a session left half-built by a dead worker is undone on restart and stale credentials are not reused", async () => {
  const session = memoryKV();
  await session.set("sessions", {
    "00000000-0000-4000-8000-0000000000aa": {
      v: 1,
      id: "00000000-0000-4000-8000-0000000000aa",
      tabId: TAB,
      origin: ORIGIN,
      documentId: undefined,
      epoch: 1,
      semanticDigest: "d",
      shapeDigest: "s",
      phase: "assembling",
      lastSeq: 0,
      createdAt: 1,
      seenAt: 1,
      actions: {},
      ops: [],
    },
  });
  const r = rig({ session });
  const core = createCore(r.ports);
  await core.ready();
  assert.ok(r.calls.some((c) => c.fn === "__vsfRestore" && (c.args[1] as string) === "crash"));
  assert.deepEqual(await session.get("sessions"), {});
  const stale = await core.handlePage(envelope("hello", "00000000-0000-4000-8000-0000000000aa", 1), r.page(TAB));
  assert.deepEqual(stale, { ok: false, error: "session_unknown" });
});

test("removing the browser permission drops the grant and restores every page of that origin", async () => {
  const r = rig();
  const { core } = await transformed(r);
  await core.onPermissionsRemoved(["http://127.0.0.1/*"]);
  assert.deepEqual(await r.local.get("grants"), {});
  assert.deepEqual(await r.session.get("sessions"), {});
  assert.ok(r.calls.some((c) => c.fn === "__vsfRestore" && (c.args[1] as string) === "removed"));
});

test("refresh updates rows in place when the shape is unchanged and re-renders when it changes", async () => {
  const r = rig();
  const { core, id } = await transformed(r);
  const page = r.page(TAB);
  const first = await core.handlePage(envelope("refresh", id, 1, {}), page);
  assert.equal((first as { changed?: string }).changed, "rows");
  r.next.assembly = assembly({ act: "unknown" }, "b");
  const second = await core.handlePage(envelope("refresh", id, 2, {}), page);
  assert.equal((second as { changed?: string }).changed, "structure");
  const fns = r.calls.map((c) => c.fn);
  assert.equal(fns.filter((f) => f === "__vsfUpdate").length, 1);
  assert.equal(fns.filter((f) => f === "__vsfRender").length, 2);
  const render = r.calls.filter((c) => c.fn === "__vsfRender")[1] as Rig["calls"][number];
  assert.equal(
    (render.args as [{ seq: number }])[0].seq,
    2,
    "the page continues the sequence instead of restarting it",
  );
});

test("a session record from another version is never interpreted, never deleted, and its page is told to restore", async () => {
  const session = memoryKV();
  const id = "00000000-0000-4000-8000-0000000000bb";
  const newer = { v: 2, id, tabId: TAB, origin: ORIGIN, phase: "rendered", future: { field: true } };
  await session.set("sessions", { [id]: newer });
  const r = rig({ session });
  const core = createCore(r.ports);
  const reply = await core.handlePage(envelope("heartbeat", id, 1, { at: 1 }), r.page(TAB));
  assert.deepEqual(reply, { ok: false, error: "session_version_unsupported" });
  const status = (await core.handleCommand({ type: "status" }, r.panel)) as unknown as { sessions: unknown[] };
  assert.deepEqual(status.sessions, []);
  assert.deepEqual(((await session.get("sessions")) as Record<string, unknown>)[id], newer);
  await core.handleCommand({ type: "grant", origin: ORIGIN }, r.panel);
  const built = await core.handleCommand({ type: "transform", tabId: TAB }, r.panel);
  assert.equal(built.ok, true);
  const all = (await session.get("sessions")) as Record<string, unknown>;
  assert.deepEqual(all[id], newer, "writing another session must not drop the record it cannot read");
});

function fakeLink(
  over: Partial<LinkPort> & { paired?: boolean; reply?: { status: number; json: Record<string, unknown> } } = {},
) {
  const calls: { path: string; body: Record<string, unknown> }[] = [];
  const link: LinkPort = {
    async call(path, body) {
      calls.push({ path, body: body as Record<string, unknown> });
      if (path === "/v1/session") return over.reply ?? { status: 200, json: { session_id: "bs_1", expires_at: 2 } };
      if (path === "/v1/session/renew") return { status: 200, json: { session_id: "bs_1", expires_at: 99_999 } };
      return { status: 200, json: { ok: true } };
    },
    pair: async () => ({ clientId: "cli_x" }),
    unpair: async () => ({ companionNotified: true }),
    status: async () => ({ paired: over.paired ?? true }),
    ...(over.call ? { call: over.call } : {}),
  };
  return { link, calls };
}

const bridgeOf = async (r: Rig): Promise<unknown> =>
  Object.values((await r.session.get("sessions")) as Record<string, { bridge?: unknown }>)[0]?.bridge;

test("a paired companion is given the document's capability manifest, bound to its origin and document", async () => {
  const { link, calls } = fakeLink();
  const r = rig({ link });
  await transformed(r);
  assert.deepEqual(
    calls.map((c) => c.path),
    ["/v1/session"],
  );
  const sent = calls[0]?.body as {
    origin: string;
    document: string;
    manifest: { schema: string; body: Record<string, unknown> };
  };
  assert.equal(sent.origin, ORIGIN);
  assert.equal(sent.document, "doc-1");
  assert.equal(sent.manifest.schema, "vsf.environment/1.2.0");
  const body = sent.manifest.body as {
    mode: string;
    permissions: { origin: string; scopes: string[] }[];
    principal_ref: string;
    runtime_id: string;
    app_id: string;
  };
  assert.equal(body.mode, "extension");
  assert.deepEqual(body.permissions, [{ origin: ORIGIN, scopes: ["page.observe", "page.present"] }]);
  assert.match(body.principal_ref, /^prin_[0-9a-f]{32}$|^prin_[0-9]{32}$/);
  assert.match(body.runtime_id, /^rt_ext[0-9a-f]{16}$|^rt_ext[0-9]{16}$/);
  assert.deepEqual(await bridgeOf(r), { id: "bs_1", expiresAt: 2 });
});

test("without a pairing, with a refusing or absent companion, or with an invalid manifest the page still works", async () => {
  const none = rig();
  await transformed(none);
  assert.equal(await bridgeOf(none), undefined);

  const unpaired = fakeLink({ paired: false });
  const a = rig({ link: unpaired.link });
  await transformed(a);
  assert.deepEqual(unpaired.calls, []);

  const refusing = fakeLink({ reply: { status: 403, json: { error: "session.denied" } } });
  const b = rig({ link: refusing.link });
  await transformed(b);
  assert.deepEqual(await bridgeOf(b), { error: "session.denied" });

  const down = fakeLink({
    call: async () => {
      throw Object.assign(new Error("down"), { code: "bridge_unreachable" });
    },
  });
  const c = rig({ link: down.link });
  await transformed(c);
  assert.deepEqual(await bridgeOf(c), { error: "bridge_unreachable" });

  const invalid = fakeLink();
  const d = rig({ link: invalid.link });
  d.next.valid = false;
  await transformed(d);
  assert.deepEqual(await bridgeOf(d), { error: "manifest_invalid" });
  assert.deepEqual(invalid.calls, [], "a manifest the Rust validator rejects is never sent");
});

test("ending the session, by any route, ends the binding; a lapsing binding is renewed by the heartbeat", async () => {
  const { link, calls } = fakeLink();
  const r = rig({ link });
  const { core, id } = await transformed(r);
  await core.handleCommand({ type: "restore", tabId: TAB }, r.panel);
  assert.deepEqual(calls.at(-1), { path: "/v1/session/end", body: { session_id: "bs_1" } });

  const second = fakeLink();
  const s = rig({ link: second.link });
  const built = await transformed(s);
  await built.core.handlePage(envelope("heartbeat", built.id, 1, { at: 1 }), s.page(TAB));
  assert.ok(
    second.calls.some((c) => c.path === "/v1/session/renew"),
    "the binding expires within five minutes of the fake clock",
  );
  await built.core.handlePage(envelope("restore", built.id, 2, { reason: "user" }), s.page(TAB));
  assert.equal(second.calls.at(-1)?.path, "/v1/session/end");
  void id;
});

function fakeSettings(initial: Prefs | null | "unreadable" = null) {
  const saved: { origin: string; prefs: Prefs }[] = [];
  const store: SettingsStore = {
    load: async () => initial,
    save: async (origin, prefs) => {
      saved.push({ origin, prefs });
    },
  };
  return { store, saved };
}

test("stored preferences travel with the render request and are reported as restored", async () => {
  const { store } = fakeSettings({ density: "compact" });
  const r = rig({ settings: store });
  const core = createCore(r.ports);
  await core.handleCommand({ type: "grant", origin: ORIGIN }, r.panel);
  const reply = await core.handleCommand({ type: "transform", tabId: TAB }, r.panel);
  assert.equal((reply as { settings?: string }).settings, "restored");
  const render = r.calls.find((c) => c.fn === "__vsfRender");
  assert.deepEqual((render?.args[0] as { prefs: Prefs }).prefs, { density: "compact" });
});

test("preferences nobody can vouch for are ignored, not applied, and the user is told", async () => {
  const { store } = fakeSettings("unreadable");
  const r = rig({ settings: store });
  const core = createCore(r.ports);
  await core.handleCommand({ type: "grant", origin: ORIGIN }, r.panel);
  const reply = await core.handleCommand({ type: "transform", tabId: TAB }, r.panel);
  assert.equal(reply.ok, true);
  assert.equal((reply as { settings?: string }).settings, "unreadable");
  assert.deepEqual((r.calls.find((c) => c.fn === "__vsfRender")?.args[0] as { prefs: Prefs }).prefs, {});
});

test("a preference change from the page is saved for that session's origin only", async () => {
  const { store, saved } = fakeSettings();
  const r = rig({ settings: store });
  const { core, id } = await transformed(r);
  const reply = await core.handlePage(envelope("prefs", id, 1, { prefs: { scheme: "dark" } }), r.page(TAB));
  assert.deepEqual(reply, { ok: true, saved: true });
  assert.deepEqual(saved, [{ origin: ORIGIN, prefs: { scheme: "dark" } }]);
  const bad = await core.handlePage(envelope("prefs", id, 2, { prefs: { scheme: "plaid" } }), r.page(TAB));
  assert.deepEqual(bad, { ok: false, error: "bad_body" });
  assert.equal(saved.length, 1);
  const stranger = await core.handlePage(envelope("prefs", id, 3, { prefs: { scheme: "light" } }), r.page(TAB + 1));
  assert.equal(stranger.ok, false);
  assert.equal(saved.length, 1);
});
