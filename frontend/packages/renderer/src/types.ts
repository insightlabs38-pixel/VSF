import type { Announcer } from "./a11y.ts";
import type { ComponentKind, PComponent, PresentationBody } from "./model.ts";
import type { Runtime } from "./runtime.ts";
import type { Dom } from "./writes.ts";

export interface Inst {
  id: string;
  kind: ComponentKind;
  el: HTMLElement;
  /** Placed in the parent instead of `el` (list items). */
  outer?: HTMLElement;
  mount: HTMLElement;
  kids: Map<string, Inst>;
  /** The component definition last applied. */
  cur?: PComponent;
  /** Row an operation in this component would apply to, for grids. */
  activeRow?(): string | null;
  update(c: PComponent): void;
  refresh?(): void;
  dispose(): void;
}

export interface RendererApi {
  readonly dom: Dom;
  readonly rt: Runtime;
  readonly announcer: Announcer;
  readonly root: ShadowRoot;
  readonly body: PresentationBody;
  readonly rtl: boolean;
  activeEntity: string;
  syncKids(parent: Inst, ids: string[]): void;
  navigate(entity: string): void;
  press(action: string, entity: string, label: string, rowKey?: string): Promise<void>;
  dock(on: boolean): void;
  /** Elements currently shown inside the renderer; one element is never adopted twice. */
  readonly adopted: WeakSet<Element>;
}
