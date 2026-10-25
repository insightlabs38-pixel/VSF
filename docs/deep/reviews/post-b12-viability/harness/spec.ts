import type { Download, Page } from "@playwright/test";
import type { Running } from "./apps.ts";

export type StepKind = "fill" | "choose" | "toggle" | "click" | "upload" | "press" | "type" | "drag";

/** One declarative step. `target` is the accessible name a person sees; the other fields only steer the reference driver. */
export interface Step {
  do: StepKind;
  target?: string;
  /** ARIA role of the control, used by the reference driver only. */
  role?: string;
  /** Playwright selector overriding role and name for the reference driver when the control has no usable accessible name. */
  sel?: string;
  /** Scope for the reference driver, a Playwright selector. */
  within?: string;
  nth?: number;
  value?: string;
  checked?: boolean;
  key?: string;
  text?: string;
  /** Member of a composite (a tab, a menu item) named by `target`. */
  item?: string;
  /** Composite kind for `item`, as the B11 operation grammar needs it. */
  kind?: string;
  /** The control has no accessible name at all; the target is a description for the report. */
  unnamed?: boolean;
  /** Milliseconds a person waits after the step. */
  wait?: number;
  /** Drag destination for the reference driver. */
  to?: string;
}

export type Effect = "read" | "write" | "destructive" | "navigation";

export interface Workflow {
  id: string;
  app: string;
  /** Variant of the application started (multi-demo apps). */
  variant?: string;
  title: string;
  objective: string;
  effect: Effect;
  /** Declared categories from the pre-registered coverage list. */
  categories: string[];
  /** 1 single interaction chain, 2 multi-field or multi-step, 3 needs dialogs, conditional controls or specialist widgets. */
  difficulty: 1 | 2 | 3;
  start: string;
  /** Milliseconds a person waits after the page loads before acting. */
  settle?: number;
  steps: Step[];
  /** Name of the source-state oracle in oracles.ts. */
  oracle: string;
  /** Files a download step is expected to produce, by oracle. */
  download?: boolean;
}

export interface Ctx {
  run: Running;
  page: Page;
  origin: string;
  downloads: Download[];
  /** Files written by download steps: name to bytes. */
  files: Map<string, Buffer>;
}

export interface OracleResult {
  ok: boolean;
  detail: string;
}

export type Oracle = (c: Ctx) => Promise<OracleResult>;
