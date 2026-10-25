export type AdapterKind = "terminal" | "editor";

export type Lifecycle = "open" | "disposed";

export interface Size {
  cols: number;
  rows: number;
}

export interface TerminalAdapter {
  readonly kind: "terminal";
  readonly lifecycle: Lifecycle;
  /** Output from the source process; bytes are decoded as UTF-8 by the terminal, never reinterpreted here. */
  write(data: string | Uint8Array): Promise<void>;
  /** Called with exactly what the terminal produces for a key, paste or composition. */
  onInput(cb: (data: string) => void): () => void;
  onResize(cb: (size: Size) => void): () => void;
  /** Fits the terminal to its container and reports the size the source process must be told. */
  fit(): Size;
  resize(size: Size): void;
  selection(): string;
  hasFocus(): boolean;
  focus(): void;
  /** The text of the visible buffer, one string per line, for accessibility and for checks. */
  lines(): string[];
  cursor(): { x: number; y: number };
  dispose(): void;
}

export interface Range {
  /** Offsets in UTF-16 code units, as the page's strings use them. */
  from: number;
  to: number;
}

export interface EditorAdapter {
  readonly kind: "editor";
  readonly lifecycle: Lifecycle;
  getValue(): string;
  /** Replaces the whole document from the source; it does not count as a user edit. */
  setValue(text: string): void;
  onChange(cb: (value: string, userEdit: boolean) => void): () => void;
  selections(): Range[];
  select(range: Range): void;
  readOnly(on: boolean): void;
  composing(): boolean;
  hasFocus(): boolean;
  focus(): void;
  dispose(): void;
}

export interface AdapterOptions {
  terminal: { cols?: number; rows?: number; scrollback?: number };
  editor: { value?: string; readOnly?: boolean };
}

export interface AdapterOf {
  terminal: TerminalAdapter;
  editor: EditorAdapter;
}

type Loader<K extends AdapterKind> = () => Promise<{
  create(host: HTMLElement, options: AdapterOptions[K]): AdapterOf[K];
}>;

/** The libraries are fetched on first use; a page that never shows a terminal or an editor never loads them. */
const loaders: { [K in AdapterKind]: Loader<K> } = {
  terminal: () => import("./terminal.ts"),
  editor: () => import("./editor.ts"),
};

const loaded = new Map<AdapterKind, Promise<unknown>>();

/** Which adapters have been requested in this page, for budgets and checks. */
export function loadedAdapters(): AdapterKind[] {
  return [...loaded.keys()];
}

export async function openAdapter<K extends AdapterKind>(
  kind: K,
  host: HTMLElement,
  options: AdapterOptions[K],
): Promise<AdapterOf[K]> {
  let mod = loaded.get(kind) as Promise<Awaited<ReturnType<Loader<K>>>> | undefined;
  if (!mod) {
    mod = loaders[kind]() as Promise<Awaited<ReturnType<Loader<K>>>>;
    loaded.set(kind, mod);
    // A failed load is not remembered here; the browser keeps the failure for that URL until the page reloads.
    mod.catch(() => loaded.delete(kind));
  }
  return (await mod).create(host, options);
}

export class AdapterError extends Error {
  readonly code: "adapter.disposed" | "adapter.bad_size" | "adapter.bad_range";
  constructor(code: "adapter.disposed" | "adapter.bad_size" | "adapter.bad_range") {
    super(code);
    this.code = code;
  }
}

export function checkSize(size: Size): Size {
  if (
    !Number.isInteger(size.cols) ||
    !Number.isInteger(size.rows) ||
    size.cols < 2 ||
    size.rows < 1 ||
    size.cols > 1000 ||
    size.rows > 500
  )
    throw new AdapterError("adapter.bad_size");
  return size;
}
