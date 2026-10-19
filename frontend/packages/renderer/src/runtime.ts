import type { SemanticView } from "./model.ts";
import type { Store } from "./store.ts";

export interface Availability {
  state: "available" | "unknown" | "conflicted" | "unusable";
  reason?: string;
}

export interface DispatchContext {
  entity: string;
  rowKey?: string;
}

export interface DispatchResult {
  ok: boolean;
  message?: string;
}

/** A source widget the renderer cannot reproduce, handed over only when the runtime has permission to show it. */
export interface SourceSurface {
  name: string;
  origin: string;
  /** The live element, only when it is in this document; never an embedding of another origin. */
  element?: Element;
  permitted: { inPlace: boolean; sideBySide: boolean };
  note?: string;
}

export interface Runtime {
  semantic: SemanticView;
  store: Store;
  /** The only permission signal the renderer reads; it never grants or widens anything. */
  availability(action: string): Availability;
  /** The only way an operation leaves the renderer. */
  dispatch(action: string, ctx: DispatchContext): undefined | DispatchResult | Promise<undefined | DispatchResult>;
  onAvailability?(fn: () => void): () => void;
  choices?(entity: string, field: string): string[];
  surface?(componentId: string): SourceSurface | undefined;
}
