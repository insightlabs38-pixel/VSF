import {
  type Availability,
  type CompileOptions,
  checkPresentation,
  compilePresentation,
  type DispatchContext,
  type DispatchResult,
  type LayoutPlan,
  type PresentationBody,
  Renderer,
  type Row,
  type SemanticView,
  SourceStyles,
  type SourceSurface,
  Store,
  type Value,
  type ViewSpec,
} from "@vsf/renderer";

export interface Boot {
  semantic: SemanticView;
  views: ViewSpec[];
  navigation?: boolean;
  layout?: LayoutPlan;
  locale?: string;
  direction?: "ltr" | "rtl" | "auto";
  theme?: PresentationBody["theme"];
  values?: Record<string, Value>;
  rows?: Record<string, { key: string; values: Row }[]>;
  availability?: Record<string, Availability>;
  choices?: Record<string, string[]>;
  results?: Record<string, DispatchResult>;
  surfaces?: Record<
    string,
    {
      name: string;
      origin: string;
      selector?: string;
      permitted: { inPlace: boolean; sideBySide: boolean };
      note?: string;
    }
  >;
  hotkeys?: boolean;
  mount?: string;
  shadow?: "open" | "closed";
  synthetic?: boolean;
  runtimeDigest?: string;
}

export interface Dispatched {
  action: string;
  entity: string;
  rowKey?: string;
}

function boot(cfg: Boot) {
  const store = new Store();
  for (const [k, v] of Object.entries(cfg.values ?? {})) store.set(k, v);
  for (const [e, rows] of Object.entries(cfg.rows ?? {})) store.rows(e).replace(rows);
  const avail: Record<string, Availability> = { ...(cfg.availability ?? {}) };
  const listeners = new Set<() => void>();
  const dispatches: Dispatched[] = [];
  const pending = new Map<string, (r: DispatchResult) => void>();
  const prefsLog: unknown[] = [];
  const digest = `sha256:${"29".repeat(32)}`;
  const rt = {
    semantic: cfg.semantic,
    semanticDigest: cfg.runtimeDigest ?? digest,
    store,
    availability: (a: string): Availability => avail[a] ?? { state: "available" },
    dispatch: (action: string, ctx: DispatchContext): DispatchResult | Promise<DispatchResult> => {
      dispatches.push(
        ctx.rowKey === undefined ? { action, entity: ctx.entity } : { action, entity: ctx.entity, rowKey: ctx.rowKey },
      );
      const fixed = cfg.results?.[action];
      if (fixed) return fixed;
      return { ok: true, message: `${action} sent` };
    },
    onAvailability: (fn: () => void) => {
      listeners.add(fn);
      return () => listeners.delete(fn);
    },
    choices: (e: string, f: string) => cfg.choices?.[`${e}.${f}`] ?? [],
    surface: (id: string): SourceSurface | undefined => {
      const s = cfg.surfaces?.[id];
      if (!s) return undefined;
      const element = s.selector ? (document.querySelector(s.selector) ?? undefined) : undefined;
      const out: SourceSurface = { name: s.name, origin: s.origin, permitted: s.permitted };
      if (element) out.element = element;
      if (s.note) out.note = s.note;
      return out;
    },
  };
  const opts = (views: ViewSpec[]): CompileOptions => ({
    semanticDigest: digest,
    locale: cfg.locale ?? "en-US",
    direction: cfg.direction ?? "ltr",
    theme: cfg.theme ?? { scheme: "light", accent: "blue", density: "comfortable" },
    views,
    navigation: cfg.navigation ?? false,
    ...(cfg.layout ? { layout: cfg.layout } : {}),
  });
  let body = compilePresentation(cfg.semantic, opts(cfg.views));
  const hostEl = document.querySelector<HTMLElement>(cfg.mount ?? "#vsf-host") as HTMLElement;
  const renderer = new Renderer(hostEl, rt, body, {
    hotkeys: cfg.hotkeys ?? true,
    shadow: cfg.shadow ?? "open",
    onPrefs: (p) => prefsLog.push(p),
    allowSynthetic: cfg.synthetic ?? false,
  });
  const app = {
    renderer,
    store,
    rt,
    dispatches,
    prefsLog,
    pending,
    host: hostEl,
    body: () => body,
    problems: () => checkPresentation(cfg.semantic, body),
    setAvailability(action: string, a: Availability) {
      avail[action] = a;
      for (const f of listeners) f();
    },
    relayout(views: ViewSpec[]) {
      body = compilePresentation(cfg.semantic, opts(views));
      renderer.update(body);
    },
    retheme(theme: PresentationBody["theme"]) {
      body = { ...body, theme };
      renderer.update(body);
    },
    recompile(over: Partial<CompileOptions>, views = cfg.views) {
      body = compilePresentation(cfg.semantic, { ...opts(views), ...over });
      renderer.update(body);
    },
    styles: new SourceStyles(document),
    dispose() {
      renderer.destroy();
    },
  };
  (window as unknown as { app: typeof app }).app = app;
  return app;
}

(window as unknown as { VSF: { boot: typeof boot } }).VSF = { boot };
