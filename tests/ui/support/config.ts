import { readFileSync } from "node:fs";
import type { SemanticView } from "@vsf/renderer";
import type { Boot } from "./page-entry.ts";

const dir = new URL("../../../schemas/ir/fixtures/valid/", import.meta.url);
export const semantic = (): SemanticView => JSON.parse(readFileSync(new URL("semantic-orders.json", dir), "utf8")).body;

export function orderRows(n: number): { key: string; values: Record<string, string | number> }[] {
  const rows = [];
  for (let i = 0; i < n; i++) {
    rows.push({
      key: `o${String(i).padStart(5, "0")}`,
      values: {
        order_id: `SO-${String(i + 1).padStart(5, "0")}`,
        total: ((i * 7919) % 10000) / 100,
        placed_on: `2026-0${(i % 9) + 1}-${String((i % 27) + 1).padStart(2, "0")}`,
      },
    });
  }
  return rows;
}

export function formBoot(over: Partial<Boot> = {}): Boot {
  return {
    semantic: semantic(),
    views: [{ entity: "order", layout: "form" }],
    values: { "order.order_id": "SO-00042", "order.total": "129.95", "order.placed_on": "2026-03-04" },
    ...over,
  };
}

export function gridBoot(n: number, over: Partial<Boot> = {}): Boot {
  return {
    semantic: semantic(),
    views: [{ entity: "order", layout: "grid" }],
    rows: { order: orderRows(n) },
    ...over,
  };
}
