import { ownUi } from "@vsf/observer";
import {
  type Availability,
  checkPresentation,
  compilePresentation,
  type DispatchContext,
  type DispatchResult,
  type Prefs,
  Renderer,
  type Row,
  type Runtime,
  type SemanticView,
  Store,
  type ViewSpec,
} from "@vsf/renderer";
import { type BodyOf, makeEnvelope, type PageKind, type RestoreReason } from "./envelope.ts";

interface Boot {
  session: string;
  seq: number;
  origin: string;
  prefs?: Prefs;
  assembly: {
    semantic: SemanticView;
    digest: string;
    views: ViewSpec[];
    rows: Record<string, { key: string; values: Row }[]>;
    actions: Record<string, { state: Availability["state"]; reason: string | null }>;
  };
}

interface Active {
  session: string;
  seq: number;
  overlay: HTMLElement;
  mount: HTMLElement;
  status: HTMLElement;
  renderer: Renderer;
  store: Store;
  actions: Record<string, Availability>;
  listeners: Set<() => void>;
  saved: { inert: string | null; ariaHidden: string | null };
  timer: ReturnType<typeof setInterval>;
  stop: () => void;
}

declare const __VSF_SHADOW__: "open" | "closed";
declare const __VSF_HEARTBEAT_MS__: number;
const HEARTBEAT_MS = __VSF_HEARTBEAT_MS__;
const LOCALE = /^[a-z]{2,3}(-[A-Za-z0-9]{2,8}){0,3}$/;
let active: Active | null = null;

const css = `
:host{all:initial}
.bar{position:fixed;inset:0 0 auto 0;height:44px;display:flex;align-items:center;gap:8px;padding:0 12px;font:14px system-ui,sans-serif;background:Canvas;color:CanvasText;border-bottom:1px solid GrayText;box-sizing:border-box;z-index:2}
.bar strong{margin-inline-end:auto}
.bar button{font:inherit;min-height:28px;min-width:28px;padding:2px 10px;color:ButtonText;background:ButtonFace;border:1px solid ButtonBorder;border-radius:4px}
.bar button:focus-visible{outline:2px solid Highlight;outline-offset:2px}
.status{font-size:12px;opacity:.8}
.mount{position:fixed;inset:44px 0 0 0;overflow:auto;background:Canvas;color:CanvasText}
`;

function send<K extends PageKind>(
  a: Active,
  kind: K,
  body: BodyOf[K],
): Promise<{ ok: boolean; error?: string; detail?: string; [k: string]: unknown }> {
  a.seq += 1;
  return chrome.runtime.sendMessage(makeEnvelope(kind, a.session, a.seq, body));
}

function restorePage(a: Active): void {
  clearInterval(a.timer);
  a.stop();
  try {
    a.renderer.destroy();
  } catch {
    // the page is restored regardless of what the renderer left behind
  }
  a.overlay.remove();
  const body = document.body;
  if (body) {
    if (a.saved.inert === null) body.removeAttribute("inert");
    else body.setAttribute("inert", a.saved.inert);
    if (a.saved.ariaHidden === null) body.removeAttribute("aria-hidden");
    else body.setAttribute("aria-hidden", a.saved.ariaHidden);
  }
  if (active === a) active = null;
}

function tell(a: Active, reason: RestoreReason): void {
  void send(a, "restore", { reason }).catch(() => undefined);
}

function emergency(a: Active, reason: RestoreReason): void {
  if (active !== a) return;
  restorePage(a);
  tell(a, reason);
}

function choice(
  label: string,
  options: readonly string[],
  initial: string,
  onChange: (value: string) => void,
): HTMLElement {
  const wrap = document.createElement("label");
  wrap.append(`${label} `);
  const select = document.createElement("select");
  for (const o of options) {
    const opt = document.createElement("option");
    opt.value = o;
    opt.textContent = o;
    select.append(opt);
  }
  select.value = initial;
  select.addEventListener("change", (e) => e.isTrusted && onChange(select.value));
  wrap.append(select);
  return wrap;
}

function bar(
  a: { status: HTMLElement },
  root: ShadowRoot,
  prefs: Prefs,
  on: { refresh(): void; restore(): void; pref(change: Prefs): void },
): void {
  const header = document.createElement("header");
  header.className = "bar";
  header.setAttribute("role", "banner");
  const title = document.createElement("strong");
  title.textContent = "Varifold";
  const refresh = document.createElement("button");
  refresh.type = "button";
  refresh.textContent = "Refresh from source";
  refresh.addEventListener("click", (e) => e.isTrusted && on.refresh());
  const original = document.createElement("button");
  original.type = "button";
  original.textContent = "Show original";
  original.addEventListener("click", (e) => e.isTrusted && on.restore());
  a.status.className = "status";
  a.status.setAttribute("role", "status");
  a.status.setAttribute("aria-live", "polite");
  header.append(
    title,
    a.status,
    choice("Theme", ["system", "light", "dark"], prefs.scheme ?? "system", (v) =>
      on.pref({ scheme: v as NonNullable<Prefs["scheme"]> }),
    ),
    choice("Density", ["compact", "comfortable", "spacious"], prefs.density ?? "comfortable", (v) =>
      on.pref({ density: v as NonNullable<Prefs["density"]> }),
    ),
    refresh,
    original,
  );
  root.append(header);
}

function build(boot: Boot): { problems: string[]; make: () => Active } {
  const { semantic, digest, views, rows, actions } = boot.assembly;
  const lang = document.documentElement.lang;
  const body = compilePresentation(semantic, {
    semanticDigest: digest,
    locale: LOCALE.test(lang) ? lang : "en-US",
    direction: "auto",
    theme: { scheme: "system", accent: "blue", density: "comfortable" },
    views,
    navigation: views.length > 1,
  });
  const problems = checkPresentation(semantic, body).map((p) => `${p.code}@${p.at}`);
  const make = (): Active => {
    const store = new Store();
    for (const [entity, list] of Object.entries(rows)) store.rows(entity).replace(list);
    const avail: Record<string, Availability> = {};
    for (const [id, v] of Object.entries(actions))
      avail[id] = v.reason ? { state: v.state, reason: v.reason } : { state: v.state };
    const listeners = new Set<() => void>();
    const overlay = document.createElement("div");
    const root = overlay.attachShadow({ mode: __VSF_SHADOW__ });
    const style = document.createElement("style");
    style.textContent = css;
    root.append(style);
    const status = document.createElement("span");
    const mount = document.createElement("div");
    mount.className = "mount";
    overlay.style.cssText = "all:initial;position:fixed;inset:0;z-index:2147483647;display:block";
    overlay.setAttribute("data-vsf-overlay", boot.session);
    const self: Active = {
      session: boot.session,
      seq: boot.seq,
      overlay,
      mount,
      status,
      renderer: undefined as unknown as Renderer,
      store,
      actions: avail,
      listeners,
      saved: { inert: null, ariaHidden: null },
      timer: undefined as unknown as ReturnType<typeof setInterval>,
      stop: () => undefined,
    };
    const rt: Runtime = {
      semantic,
      semanticDigest: digest,
      store,
      availability: (action) => self.actions[action] ?? { state: "unknown", reason: "unlisted_action" },
      onAvailability: (fn) => {
        listeners.add(fn);
        return () => listeners.delete(fn);
      },
      async dispatch(action: string, ctx: DispatchContext): Promise<DispatchResult> {
        const body: BodyOf["dispatch"] = { action, entity: ctx.entity };
        if (ctx.rowKey !== undefined) body.rowKey = ctx.rowKey;
        const reply = await send(self, "dispatch", body);
        if (reply.ok) return { ok: true, message: "Done" };
        const why = reply.detail ?? reply.error ?? "refused";
        return { ok: false, message: `Not performed: ${why}` };
      },
    };
    const prefs = boot.prefs ?? {};
    bar(self, root, prefs, {
      refresh: () => void refresh(self),
      restore: () => emergency(self, "user"),
      pref: (change) => self.renderer.setPreference(change),
    });
    root.append(mount);
    ownUi().add(overlay);
    document.documentElement.append(overlay);
    let applying = true;
    self.renderer = new Renderer(mount, rt, body, {
      shadow: __VSF_SHADOW__,
      onPrefs: (p) => {
        if (!applying) void send(self, "prefs", { prefs: p }).catch(() => undefined);
      },
    });
    if (Object.keys(prefs).length > 0) self.renderer.setPreference(prefs);
    applying = false;
    return self;
  };
  return { problems, make };
}

async function refresh(a: Active): Promise<void> {
  a.status.textContent = "Reading the source…";
  try {
    const r = await send(a, "refresh", {});
    a.status.textContent = r.ok
      ? r.changed === "none"
        ? "Already refreshing"
        : "Up to date"
      : `Refresh failed: ${r.detail ?? r.error}`;
  } catch {
    emergency(a, "orphaned");
  }
}

const INCOMPATIBLE = new Set(["unsupported_version", "session_version_unsupported"]);
const LOST = new Set([
  "session_unknown",
  "tab_mismatch",
  "frame_mismatch",
  "origin_mismatch",
  "document_mismatch",
  "sender_not_allowed",
  "stale_seq",
]);

function arm(a: Active): void {
  const onKey = (e: KeyboardEvent): void => {
    if (e.isTrusted && e.altKey && e.shiftKey && e.code === "KeyO") emergency(a, "escape");
  };
  // Blink delivers error events only to listeners of the world that raised them, so the page's own faults never arrive here.
  const onError = (): void => emergency(a, "crash");
  window.addEventListener("keydown", onKey, true);
  window.addEventListener("error", onError);
  window.addEventListener("unhandledrejection", onError);
  a.stop = () => {
    window.removeEventListener("keydown", onKey, true);
    window.removeEventListener("error", onError);
    window.removeEventListener("unhandledrejection", onError);
  };
  a.timer = setInterval(() => {
    if (!chrome.runtime?.id) return emergency(a, "orphaned");
    if (!a.overlay.isConnected) {
      clearInterval(a.timer);
      a.stop();
      if (active === a) active = null;
      tell(a, "removed");
      return;
    }
    send(a, "heartbeat", { at: Date.now() }).then(
      (r) => {
        if (r.ok) return;
        const why = String(r.error);
        if (INCOMPATIBLE.has(why)) emergency(a, "downgrade");
        else if (LOST.has(why)) emergency(a, "orphaned");
      },
      () => emergency(a, "orphaned"),
    );
  }, HEARTBEAT_MS);
}

function show(boot: Boot): { ok: boolean; error?: string; problems?: string[] } {
  let built: ReturnType<typeof build>;
  try {
    built = build(boot);
  } catch (e) {
    return { ok: false, error: e instanceof Error ? e.message.slice(0, 160) : "compile_failed" };
  }
  if (built.problems.length > 0)
    return { ok: false, error: "presentation_rejected", problems: built.problems.slice(0, 5) };
  if (active) restorePage(active);
  let a: Active;
  try {
    a = built.make();
  } catch (e) {
    document.querySelector(`[data-vsf-overlay="${boot.session}"]`)?.remove();
    return { ok: false, error: e instanceof Error ? e.message.slice(0, 160) : "render_failed" };
  }
  const body = document.body;
  a.saved = { inert: body.getAttribute("inert"), ariaHidden: body.getAttribute("aria-hidden") };
  body.setAttribute("inert", "");
  body.setAttribute("aria-hidden", "true");
  active = a;
  arm(a);
  a.status.textContent = `${Object.values(boot.assembly.rows).reduce((n, r) => n + r.length, 0)} rows from the source`;
  return { ok: true };
}

function update(u: { session: string; rows: Boot["assembly"]["rows"]; actions: Boot["assembly"]["actions"] }): {
  ok: boolean;
  error?: string;
} {
  const a = active;
  if (!a || a.session !== u.session) return { ok: false, error: "no_active_session" };
  for (const [entity, list] of Object.entries(u.rows)) a.store.rows(entity).replace(list);
  const next: Record<string, Availability> = {};
  for (const [id, v] of Object.entries(u.actions))
    next[id] = v.reason ? { state: v.state, reason: v.reason } : { state: v.state };
  a.actions = next;
  for (const fn of [...a.listeners]) fn();
  a.status.textContent = "Up to date";
  return { ok: true };
}

function restore(session: string, _reason: RestoreReason): { ok: boolean } {
  if (active && active.session === session) restorePage(active);
  return { ok: true };
}

const g = globalThis as Record<string, unknown>;
// Injecting twice must not replace the first copy: it owns the live overlay and its timers.
if (typeof g.__vsfRender !== "function") {
  g.__vsfRender = show;
  g.__vsfUpdate = update;
  g.__vsfRestore = restore;
}
