import type { LayoutPlan, ViewSpec } from "@vsf/renderer";
import type { Boot } from "./page-entry.ts";

export interface Composition {
  title: string;
  views: ViewSpec[];
  layout: LayoutPlan;
  surfaces?: Boot["surfaces"];
}

const allow = { reorder: true, resize: true, collapse: true, density: true, scheme: true };

/** Four ways to arrange the same semantics; none of them needs a different renderer. */
export const compositions: Record<string, Composition> = {
  "enterprise-grid": {
    title: "Dense enterprise application: navigation, grid and a detail rail",
    views: [
      { entity: "order", layout: "grid" },
      { entity: "customer", layout: "grid" },
    ],
    layout: {
      customization: allow,
      regions: [
        {
          id: "shell_nav",
          role: "navigation",
          nav: true,
          dock: { edge: "start", collapsible: true, resizable: true },
          size: { min: 16, basis: 20, max: 32 },
          responsive: [{ below: 70, arrange: "stack", collapse: true }],
        },
        { id: "shell_main", role: "main", arrange: "stack", views: ["order"] },
        {
          id: "shell_rail",
          role: "aside",
          views: ["customer"],
          dock: { edge: "end", collapsible: true, resizable: true },
          size: { min: 24, basis: 30, max: 44 },
          responsive: [{ below: 100, arrange: "stack", collapse: true }],
        },
      ],
    },
  },
  "grouped-form": {
    title: "Grouped form with navigation",
    views: [
      {
        entity: "order",
        layout: "form",
        groups: [
          { id: "identity", name: "Identity", fields: ["order_id"] },
          { id: "money", name: "Amounts and dates", fields: ["total", "placed_on"] },
        ],
      },
      { entity: "customer", layout: "form" },
    ],
    layout: {
      customization: allow,
      regions: [
        {
          id: "form_nav",
          role: "navigation",
          nav: true,
          dock: { edge: "start", collapsible: true, resizable: false },
          size: { min: 16, basis: 18, max: 24 },
          responsive: [{ below: 60, arrange: "stack", collapse: true }],
        },
        { id: "form_main", role: "main", arrange: "stack", views: ["order"] },
        {
          id: "form_side",
          role: "aside",
          views: ["customer"],
          dock: { edge: "end", collapsible: true, resizable: false },
          size: { min: 24, basis: 28 },
          responsive: [{ below: 90, arrange: "stack", collapse: true }],
        },
      ],
    },
  },
  "workbench-split": {
    title: "Two panels: a form beside a preserved original surface",
    views: [
      { entity: "order", layout: "form", preserved: [{ id: "notebook", name: "Notebook" }] },
      { entity: "customer", layout: "form" },
    ],
    layout: {
      customization: allow,
      regions: [
        { id: "bench", role: "main", arrange: "split", responsive: [{ below: 90, arrange: "tabs", collapse: false }] },
        { id: "bench_form", parent: "bench", order: 0, views: ["order"], size: { min: 36 } },
        { id: "bench_who", parent: "bench", order: 1, views: ["customer"], size: { min: 28 } },
      ],
    },
    surfaces: {
      notebook: {
        name: "Notebook",
        origin: "https://notes.example.test",
        permitted: { inPlace: false, sideBySide: false },
        note: "Opens in the original page.",
      },
    },
  },
  "master-detail": {
    title: "Master and detail that become tabs on a narrow screen",
    views: [
      { entity: "order", layout: "grid" },
      { entity: "customer", layout: "form" },
    ],
    layout: {
      customization: allow,
      regions: [
        { id: "md", role: "main", arrange: "split", responsive: [{ below: 100, arrange: "tabs", collapse: false }] },
        { id: "md_list", parent: "md", order: 0, views: ["order"], size: { min: 40 } },
        { id: "md_detail", parent: "md", order: 1, views: ["customer"], size: { min: 28 } },
      ],
    },
  },
};
