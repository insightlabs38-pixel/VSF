import {
  type Availability,
  compilePresentation,
  type DispatchContext,
  type DispatchResult,
  Renderer,
  type Runtime,
  Store,
} from "@vsf/renderer";
import type { AppBundle } from "./bundles.ts";
import { Chain, canonicalJson, sha256Hex } from "./envelope.ts";

interface MountArgs {
  render: unknown;
  tab: number;
  epoch: string;
  compiled: { id: string; seq: number };
  session: { key: string; epoch: string };
  enabled: string[];
}

type MountResult = { ok: true; app: string; state: "ready" | "source_ui" } | { ok: false; error: string };

interface Live {
  host: HTMLElement;
  renderer: Renderer;
  inertSet: boolean;
}

let live: Live | undefined;
const chain = new Chain();
let session = { key: "", epoch: "", seq: 0 };

type Reply = {
  ok: boolean;
  error?: string;
  state?: "done" | "unknown" | "in_flight";
  result?: { ok: boolean; message?: string };
};

async function ask(message: unknown): Promise<Reply | undefined> {
  try {
    return (await chrome.runtime.sendMessage(message)) as Reply | undefined;
  } catch {
    return undefined;
  }
}

/** The worker restarted, so the old key is gone: ask for a new one on the same tab and origin. */
async function reconnect(): Promise<boolean> {
  const r = await ask({ type: "reconnect", epoch: session.epoch });
  if (!r?.ok || r.result?.message === undefined) return false;
  const fresh = JSON.parse(r.result.message) as { key: string; epoch: string };
  session = { key: fresh.key, epoch: fresh.epoch, seq: 0 };
  return true;
}

const LOST = "Contact with the extension was lost. Check the source before trying again.";

/** One user action, one request id: a retry after a worker restart reuses the id, so it cannot act twice. */
async function send(requestId: string, action: string, ctx: DispatchContext, values: Record<string, unknown>) {
  for (let attempt = 0; attempt < 3; attempt++) {
    const reply = await ask({
      type: "dispatch",
      requestId,
      key: session.key,
      epoch: session.epoch,
      seq: ++session.seq,
      action,
      entity: ctx.entity,
      ...(ctx.rowKey === undefined ? {} : { rowKey: ctx.rowKey }),
      values,
    });
    if (reply !== undefined && reply.error !== "stale_session") return reply;
    if (!(await reconnect())) break;
  }
  return undefined;
}

function describe(reply: Reply | undefined): DispatchResult {
  if (reply === undefined) return { ok: false, message: LOST };
  if (!reply.ok) return { ok: false, message: `Not sent: ${reply.error ?? "refused"}.` };
  if (reply.state === "done") {
    const r = reply.result;
    return { ok: r?.ok === true, message: r?.message ?? (r?.ok ? "Done." : "Refused.") };
  }
  if (reply.state === "in_flight") return { ok: false, message: "Still running." };
  return { ok: false, message: "The outcome is unknown. Check the source before trying again." };
}

function unmount(): boolean {
  if (live === undefined) return false;
  live.renderer.destroy();
  live.host.remove();
  if (live.inertSet) document.body.removeAttribute("inert");
  live = undefined;
  return true;
}

/** Receives the render stage; it replaces the visible page only when the envelope and its bundle verify here. */
async function mount(args: MountArgs): Promise<MountResult> {
  chain.seed(args.tab, args.epoch, args.compiled.id, "compiled", args.compiled.seq);
  const checked = chain.accept(args.render, {
    tab: args.tab,
    frame: window === window.top ? 0 : 1,
    origin: location.origin,
    epoch: args.epoch,
  });
  if (!checked.ok) return { ok: false, error: checked.code };
  const body = checked.envelope.body as {
    app: string;
    bundle_digest: string;
    state: "ready" | "source_ui";
    bundle?: AppBundle;
  };
  if (body.state === "source_ui") return { ok: true, app: body.app, state: "source_ui" };
  const bundle = body.bundle as AppBundle;
  if ((await sha256Hex(canonicalJson(bundle))) !== body.bundle_digest || bundle.id !== body.app)
    return { ok: false, error: "envelope.digest" };
  unmount();
  session = { key: args.session.key, epoch: args.session.epoch, seq: 0 };
  const enabled = new Set(args.enabled);
  const store = new Store();
  const digest = `sha256:${body.bundle_digest}`;
  const unusable = bundle.unavailable ?? {};
  const rt: Runtime = {
    semantic: bundle.semantic,
    semanticDigest: digest,
    store,
    availability: (action: string): Availability =>
      action in unusable
        ? { state: "unusable", reason: unusable[action] as string }
        : enabled.has(action)
          ? { state: "available" }
          : { state: "unknown", reason: "The local service is not connected." },
    dispatch: async (action: string, ctx: DispatchContext): Promise<DispatchResult> => {
      if (!enabled.has(action)) return { ok: false, message: "This operation is not available." };
      const values: Record<string, unknown> = {};
      for (const f of bundle.semantic.entities.find((e) => e.id === ctx.entity)?.fields ?? []) {
        const v = store.get(`${ctx.entity}.${f.id}`);
        if (v !== null) values[f.id] = v;
      }
      return describe(await send(crypto.randomUUID(), action, ctx, values));
    },
    choices: (entity: string, field: string): string[] => bundle.choices?.[`${entity}.${field}`] ?? [],
  };
  const host = document.createElement("div");
  host.setAttribute("data-vsf-shell", body.app);
  host.style.cssText = "position:fixed;inset:0;z-index:2147483647;overflow:auto;background:Canvas";
  document.documentElement.append(host);
  const presentation = compilePresentation(bundle.semantic, {
    semanticDigest: digest,
    locale: "en-US",
    direction: "auto",
    theme: { scheme: "system", accent: "blue", density: "comfortable" },
    views: bundle.views,
    navigation: false,
  });
  let renderer: Renderer;
  try {
    renderer = new Renderer(host, rt, presentation, { shadow: "closed", hotkeys: false });
  } catch (e) {
    host.remove();
    return { ok: false, error: e instanceof Error ? e.message.slice(0, 120) : "render_failed" };
  }
  const inertSet = !document.body.hasAttribute("inert");
  document.body.setAttribute("inert", "");
  live = { host, renderer, inertSet };
  return { ok: true, app: body.app, state: "ready" };
}

(globalThis as { __vsfShell?: unknown }).__vsfShell = {
  mount,
  unmount,
  active: (): string | null => live?.host.getAttribute("data-vsf-shell") ?? null,
};
