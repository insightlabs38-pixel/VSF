import type { Availability } from "@vsf/renderer";
import { DEFAULT_AVAILABILITY, type Look, mountViews, orderRows } from "./scene.ts";

interface Panel {
  id: string;
  title: string;
  note: string;
  build(host: HTMLElement, look: Look): Promise<void> | void;
}

const grid = [{ entity: "order", layout: "grid" as const }];
const form = [{ entity: "order", layout: "form" as const }];

const unusable: Record<string, Availability> = {
  open_order: { state: "unusable", reason: "disabled by the source" },
  submit_order: { state: "unknown", reason: "effect_unverified" },
  delete_order: { state: "conflicted", reason: "witness_conflict" },
};

const PANELS: Panel[] = [
  {
    id: "normal",
    title: "Normal",
    note: "One operation is available; the others say why they are not.",
    build: (host, look) =>
      void mountViews(host, grid, look, { rows: { order: orderRows(4) }, availability: DEFAULT_AVAILABILITY }),
  },
  {
    id: "empty",
    title: "Empty",
    note: "A collection with no rows says so.",
    build: (host, look) => void mountViews(host, grid, look, { rows: { order: [] } }),
  },
  {
    id: "unavailable",
    title: "Unavailable, unknown and conflicted",
    note: "Nothing is enabled that the evidence does not support.",
    build: (host, look) => void mountViews(host, grid, look, { rows: { order: orderRows(3) }, availability: unusable }),
  },
  {
    id: "loading",
    title: "Loading",
    note: "An operation in flight is marked busy; the page stays usable.",
    build: async (host, look) => {
      const m = mountViews(host, form, look, {
        values: { "order.order_id": "SO-00042", "order.total": "129.95", "order.placed_on": "2026-03-04" },
        availability: { open_order: { state: "available" } },
        result: () => new Promise(() => undefined),
      });
      void m.press("open_order", "order", "Open order");
    },
  },
  {
    id: "error",
    title: "Error",
    note: "A refusal from the source is shown as the source worded it.",
    build: async (host, look) => {
      const m = mountViews(host, form, look, {
        values: { "order.order_id": "SO-00042", "order.total": "129.95", "order.placed_on": "2026-03-04" },
        availability: { open_order: { state: "available" } },
        result: () => ({ ok: false, message: "The source refused the request: the record changed since it was read." }),
      });
      await m.press("open_order", "order", "Open order");
    },
  },
];

export async function mountStates(root: HTMLElement, look: Look): Promise<void> {
  root.replaceChildren();
  const wrap = document.createElement("div");
  wrap.className = "states";
  for (const p of PANELS) {
    const section = document.createElement("section");
    section.setAttribute("aria-labelledby", `state-${p.id}`);
    section.dataset.state = p.id;
    const h = document.createElement("h2");
    h.id = `state-${p.id}`;
    h.textContent = p.title;
    const note = document.createElement("p");
    note.className = "note";
    note.textContent = p.note;
    const host = document.createElement("div");
    host.className = "panel";
    section.append(h, note, host);
    wrap.append(section);
    root.append(wrap);
    await p.build(host, look);
  }
}
