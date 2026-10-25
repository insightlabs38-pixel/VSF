import { defaultKeymap, history, historyKeymap } from "@codemirror/commands";
import { Annotation, EditorState, type Extension } from "@codemirror/state";
import { EditorView, keymap } from "@codemirror/view";
import { AdapterError, type AdapterOptions, type EditorAdapter, type Lifecycle, type Range } from "./adapters.ts";

const FromSource = Annotation.define<boolean>();

/** A text editor view backed by CodeMirror 6, which owns composition, grapheme-aware movement and selection. */
class CodeMirrorAdapter implements EditorAdapter {
  readonly kind = "editor" as const;
  private state: Lifecycle = "open";
  private readonly view: EditorView;
  private readonly listeners = new Set<(value: string, userEdit: boolean) => void>();
  private readOnlyMode = false;

  private readonly host: HTMLElement;

  constructor(host: HTMLElement, options: AdapterOptions["editor"]) {
    this.host = host;
    this.readOnlyMode = options.readOnly === true;
    const extensions: Extension[] = [
      history(),
      keymap.of([...defaultKeymap, ...historyKeymap]),
      EditorView.lineWrapping,
      EditorState.readOnly.of(false),
      EditorView.editable.of(true),
      EditorView.updateListener.of((u) => {
        if (!u.docChanged) return;
        const fromSource = u.transactions.some((t) => t.annotation(FromSource) === true);
        const value = u.state.doc.toString();
        for (const l of this.listeners) l(value, !fromSource);
      }),
      EditorState.transactionFilter.of((tr) =>
        this.readOnlyMode && tr.docChanged && tr.annotation(FromSource) !== true ? [] : tr,
      ),
    ];
    this.view = new EditorView({ state: EditorState.create({ doc: options.value ?? "", extensions }), parent: host });
  }

  get lifecycle(): Lifecycle {
    return this.state;
  }

  private live(): EditorView {
    if (this.state === "disposed") throw new AdapterError("adapter.disposed");
    return this.view;
  }

  getValue(): string {
    return this.live().state.doc.toString();
  }

  setValue(text: string): void {
    const v = this.live();
    v.dispatch({ changes: { from: 0, to: v.state.doc.length, insert: text }, annotations: FromSource.of(true) });
  }

  onChange(cb: (value: string, userEdit: boolean) => void): () => void {
    this.live();
    this.listeners.add(cb);
    return () => void this.listeners.delete(cb);
  }

  selections(): Range[] {
    return this.live().state.selection.ranges.map((r) => ({ from: r.from, to: r.to }));
  }

  select(range: Range): void {
    const v = this.live();
    const len = v.state.doc.length;
    if (
      !Number.isInteger(range.from) ||
      !Number.isInteger(range.to) ||
      range.from < 0 ||
      range.to > len ||
      range.from > range.to
    )
      throw new AdapterError("adapter.bad_range");
    v.dispatch({ selection: { anchor: range.from, head: range.to } });
  }

  readOnly(on: boolean): void {
    this.live();
    this.readOnlyMode = on;
  }

  composing(): boolean {
    return this.live().composing;
  }

  hasFocus(): boolean {
    return this.live().hasFocus;
  }

  focus(): void {
    this.live().focus();
  }

  dispose(): void {
    if (this.state === "disposed") return;
    this.state = "disposed";
    this.listeners.clear();
    this.view.destroy();
    this.host.replaceChildren();
  }
}

export function create(host: HTMLElement, options: AdapterOptions["editor"]): EditorAdapter {
  return new CodeMirrorAdapter(host, options);
}
