import {
  type Availability,
  compilePresentation,
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
}

type MountResult = { ok: true; app: string; state: "ready" | "source_ui" } | { ok: false; error: string };

interface Live {
  host: HTMLElement;
  renderer: Renderer;
  inertSet: boolean;
}

let live: Live | undefined;
const chain = new Chain();

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
  const digest = `sha256:${body.bundle_digest}`;
  const unusable = bundle.unavailable ?? {};
  const rt: Runtime = {
    semantic: bundle.semantic,
    semanticDigest: digest,
    store: new Store(),
    availability: (action: string): Availability =>
      action in unusable
        ? { state: "unusable", reason: unusable[action] as string }
        : { state: "unknown", reason: "The local service is not connected." },
    dispatch: (): DispatchResult => ({ ok: false, message: "The local service is not connected." }),
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
