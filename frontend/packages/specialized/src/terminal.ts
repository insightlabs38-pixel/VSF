import { FitAddon } from "@xterm/addon-fit";
import { Unicode11Addon } from "@xterm/addon-unicode11";
import { Terminal } from "@xterm/xterm";
import {
  AdapterError,
  type AdapterOptions,
  checkSize,
  type Lifecycle,
  type Size,
  type TerminalAdapter,
} from "./adapters.ts";

/** A terminal view backed by xterm.js. Input, selection, composition and rendering stay the library's own. */
class XtermAdapter implements TerminalAdapter {
  readonly kind = "terminal" as const;
  private state: Lifecycle = "open";
  private readonly term: Terminal;
  private readonly fitter = new FitAddon();
  private readonly subs: { dispose(): void }[] = [];

  private readonly host: HTMLElement;

  constructor(host: HTMLElement, options: AdapterOptions["terminal"]) {
    this.host = host;
    this.term = new Terminal({
      cols: options.cols ?? 80,
      rows: options.rows ?? 24,
      scrollback: options.scrollback ?? 1000,
      // The Unicode width tables are a proposed API; nothing it exposes is reachable from terminal output.
      allowProposedApi: true,
      // Pages that print escape sequences must not be able to ask the terminal to open links or change the title.
      windowOptions: {},
    });
    this.term.loadAddon(this.fitter);
    this.term.loadAddon(new Unicode11Addon());
    this.term.unicode.activeVersion = "11";
    this.term.open(host);
  }

  get lifecycle(): Lifecycle {
    return this.state;
  }

  private live(): Terminal {
    if (this.state === "disposed") throw new AdapterError("adapter.disposed");
    return this.term;
  }

  write(data: string | Uint8Array): Promise<void> {
    const t = this.live();
    return new Promise((resolve) => t.write(data, resolve));
  }

  onInput(cb: (data: string) => void): () => void {
    const s = this.live().onData(cb);
    this.subs.push(s);
    return () => s.dispose();
  }

  onResize(cb: (size: Size) => void): () => void {
    const s = this.live().onResize((e) => cb({ cols: e.cols, rows: e.rows }));
    this.subs.push(s);
    return () => s.dispose();
  }

  fit(): Size {
    const t = this.live();
    this.fitter.fit();
    return { cols: t.cols, rows: t.rows };
  }

  resize(size: Size): void {
    const { cols, rows } = checkSize(size);
    this.live().resize(cols, rows);
  }

  selection(): string {
    return this.live().getSelection();
  }

  hasFocus(): boolean {
    this.live();
    return this.host.contains(document.activeElement);
  }

  focus(): void {
    this.live().focus();
  }

  lines(): string[] {
    const t = this.live();
    const out: string[] = [];
    const buf = t.buffer.active;
    for (let i = 0; i < buf.length; i++) out.push(buf.getLine(i)?.translateToString(true) ?? "");
    while (out.length > 0 && out[out.length - 1] === "") out.pop();
    return out;
  }

  cursor(): { x: number; y: number } {
    const b = this.live().buffer.active;
    return { x: b.cursorX, y: b.cursorY + b.baseY };
  }

  dispose(): void {
    if (this.state === "disposed") return;
    this.state = "disposed";
    for (const s of this.subs) s.dispose();
    this.term.dispose();
    this.host.replaceChildren();
  }
}

export function create(host: HTMLElement, options: AdapterOptions["terminal"]): TerminalAdapter {
  return new XtermAdapter(host, options);
}
