import {
  type Availability,
  compilePresentation,
  type DispatchResult,
  Renderer,
  type Row,
  type Runtime,
  type SemanticView,
  Store,
  type ViewSpec,
} from "@vsf/renderer";
import semanticDoc from "../../../schemas/ir/fixtures/valid/semantic-orders.json";
import type { Composition } from "./compositions.ts";

export type Scheme = "light" | "dark";
export type Density = "compact" | "comfortable" | "spacious";

export interface Look {
  scheme: Scheme;
  density: Density;
}

export const semantic = semanticDoc.body as unknown as SemanticView;
const DIGEST = `sha256:${"29".repeat(32)}`;

export function orderRows(n: number): { key: string; values: Row }[] {
  const names = ["Ada Lovelace", "Grace Hopper", "Edsger Dijkstra", "Barbara Liskov", "Donald Knuth"];
  return Array.from({ length: n }, (_, i) => ({
    key: `o${String(i).padStart(5, "0")}`,
    values: {
      order_id: `SO-${String(i + 1).padStart(5, "0")}`,
      total: ((i * 7919) % 10000) / 100,
      placed_on: `2026-0${(i % 9) + 1}-${String((i % 27) + 1).padStart(2, "0")}`,
      customer_name: names[i % names.length] ?? "",
    },
  }));
}

/** What a source that was never connected can honestly say: nothing happened. */
export const NOT_CONNECTED: DispatchResult = { ok: false, message: "No source is connected to the gallery." };

export interface Mounted {
  renderer: Renderer;
  store: Store;
  press(action: string, entity: string, label: string): Promise<void>;
}

export interface Wiring {
  availability?: Record<string, Availability>;
  rows?: Record<string, { key: string; values: Row }[]>;
  values?: Record<string, string | number | boolean | null>;
  result?: (action: string) => DispatchResult | Promise<DispatchResult>;
  surfaces?: Composition["surfaces"];
}

export const DEFAULT_AVAILABILITY: Record<string, Availability> = {
  open_order: { state: "available" },
  submit_order: { state: "unknown", reason: "effect_unverified" },
  delete_order: { state: "conflicted", reason: "witness_conflict" },
};

export function mountViews(
  host: HTMLElement,
  views: ViewSpec[],
  look: Look,
  wiring: Wiring = {},
  layout?: Composition["layout"],
): Mounted {
  const store = new Store();
  for (const [k, v] of Object.entries(wiring.values ?? {})) store.set(k, v);
  for (const [entity, rows] of Object.entries(wiring.rows ?? {})) store.rows(entity).replace(rows);
  const availability = { ...DEFAULT_AVAILABILITY, ...wiring.availability };
  const rt: Runtime = {
    semantic,
    semanticDigest: DIGEST,
    store,
    availability: (a) => availability[a] ?? { state: "unknown", reason: "unlisted_action" },
    dispatch: (action) => (wiring.result ? wiring.result(action) : NOT_CONNECTED),
    choices: () => [],
    surface: (id) => {
      const s = wiring.surfaces?.[id];
      return s
        ? { name: s.name, origin: s.origin, permitted: s.permitted, ...(s.note ? { note: s.note } : {}) }
        : undefined;
    },
  };
  const body = compilePresentation(semantic, {
    semanticDigest: DIGEST,
    locale: "en-US",
    direction: "ltr",
    theme: { scheme: look.scheme, accent: "blue", density: look.density },
    views,
    navigation: views.length > 1 || layout !== undefined,
    ...(layout ? { layout } : {}),
  });
  const renderer = new Renderer(host, rt, body, { shadow: "open", hotkeys: false });
  return { renderer, store, press: (action, entity, label) => renderer.press(action, entity, label) };
}

export function mountComposition(host: HTMLElement, c: Composition, look: Look, rows = 40): Mounted {
  return mountViews(
    host,
    c.views,
    look,
    {
      rows: { order: orderRows(rows), customer: [{ key: "c1", values: { customer_name: "Ada Lovelace" } }] },
      values: {
        "order.order_id": "SO-00042",
        "order.total": "129.95",
        "order.placed_on": "2026-03-04",
        "customer.customer_name": "Ada Lovelace",
      },
      ...(c.surfaces ? { surfaces: c.surfaces } : {}),
    },
    c.layout,
  );
}
