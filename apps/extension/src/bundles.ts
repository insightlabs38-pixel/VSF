import type { SemanticView, ViewSpec } from "@vsf/renderer";
import forms from "../bundles/forms.json" with { type: "json" };
import orders from "../bundles/orders.json" with { type: "json" };
import { canonicalJson, sha256Hex } from "./envelope.ts";

/** Compiled application data: it describes a UI to render and never carries logic. */
export interface AppBundle {
  id: string;
  title: string;
  paths: string[];
  match_prefix?: string;
  expect: { classes: Record<string, number> };
  semantic: SemanticView;
  views: ViewSpec[];
  unavailable?: Record<string, string>;
  choices?: Record<string, string[]>;
}

export const BUNDLES: readonly AppBundle[] = [forms, orders] as unknown as AppBundle[];

export function bundleFor(pathname: string, bundles: readonly AppBundle[] = BUNDLES): AppBundle | undefined {
  return bundles.find(
    (b) => b.paths.includes(pathname) || (b.match_prefix !== undefined && pathname.startsWith(b.match_prefix)),
  );
}

export const bundleDigest = (b: AppBundle): Promise<string> => sha256Hex(canonicalJson(b));

/** The observed page must still look like what the bundle was written for; otherwise the source UI stays. */
export function mismatch(b: AppBundle, classes: Record<string, number>): string | null {
  for (const [cls, min] of Object.entries(b.expect.classes))
    if ((classes[cls] ?? 0) < min) return `expected at least ${min} ${cls}, saw ${classes[cls] ?? 0}`;
  return null;
}
