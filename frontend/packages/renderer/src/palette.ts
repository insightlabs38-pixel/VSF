import { captureFocus, restoreFocus } from "./a11y.ts";
import type { Availability } from "./runtime.ts";
import type { Dom } from "./writes.ts";

export interface Command {
  id: string;
  label: string;
  hint?: string;
  availability: Availability;
  run(): Promise<void> | void;
}

export interface PaletteHost {
  dom: Dom;
  root: ShadowRoot;
  app: HTMLElement;
  main: HTMLElement;
  commands(): Command[];
  say(message: string, level: "polite" | "assertive"): void;
}

let seq = 0;

function matches(label: string, query: string): number {
  const l = label.toLowerCase();
  const parts = query.toLowerCase().split(/\s+/).filter(Boolean);
  if (!parts.every((p) => l.includes(p))) return -1;
  return l.startsWith(parts[0] ?? "") ? 0 : 1;
}

/** Keyboard-first command list: a modal combobox that returns focus to where it came from. */
export class Palette {
  private host: PaletteHost;
  private scrim: HTMLElement | null = null;
  private memory: ReturnType<typeof captureFocus> = null;
  private shown: Command[] = [];
  private index = 0;
  private input: HTMLInputElement | null = null;
  private listEl: HTMLElement | null = null;
  private all: Command[] = [];

  constructor(host: PaletteHost) {
    this.host = host;
  }

  get isOpen(): boolean {
    return this.scrim !== null;
  }

  open(): void {
    if (this.scrim) return;
    const { dom, root, app } = this.host;
    this.memory = captureFocus(root);
    this.all = this.host.commands();
    const n = ++seq;
    const scrim = dom.el("div", "palette-scrim");
    const box = dom.el("div", "palette");
    dom.attr(box, "role", "dialog");
    dom.attr(box, "aria-modal", "true");
    dom.attr(box, "aria-label", "Command palette");
    const input = dom.el("input");
    dom.attr(input, "type", "text");
    dom.attr(input, "role", "combobox");
    dom.attr(input, "aria-label", "Search commands");
    dom.attr(input, "aria-expanded", "true");
    dom.attr(input, "aria-autocomplete", "list");
    dom.attr(input, "aria-controls", `vsf-cmds-${n}`);
    dom.attr(input, "autocomplete", "off");
    const ul = dom.el("ul");
    dom.attr(ul, "role", "listbox");
    dom.attr(ul, "id", `vsf-cmds-${n}`);
    dom.attr(ul, "aria-label", "Commands");
    dom.append(box, input);
    dom.append(box, ul);
    dom.append(scrim, box);
    scrim.addEventListener("mousedown", (e) => {
      if (e.target === scrim) this.close();
    });
    input.addEventListener("input", () => this.filter(input.value));
    input.addEventListener("keydown", (e) => this.onKey(e));
    dom.append(root, scrim);
    dom.prop(app, "inert", true);
    this.scrim = scrim;
    this.input = input;
    this.listEl = ul;
    this.filter("");
    input.focus();
    this.host.say(`Command palette, ${this.shown.length} commands`, "polite");
  }

  close(): void {
    const { dom, root, app, main } = this.host;
    if (!this.scrim) return;
    dom.remove(this.scrim);
    this.scrim = null;
    this.input = null;
    this.listEl = null;
    dom.prop(app, "inert", false);
    restoreFocus(root, this.memory, main);
  }

  private filter(q: string): void {
    const ranked = this.all
      .map((c) => ({ c, rank: matches(c.label, q) }))
      .filter((x) => x.rank >= 0)
      .sort((a, b) => a.rank - b.rank);
    this.shown = ranked.map((x) => x.c);
    this.index = Math.min(this.index, Math.max(0, this.shown.length - 1));
    this.paint();
    if (q) this.host.say(`${this.shown.length} results`, "polite");
  }

  private paint(): void {
    const { dom } = this.host;
    const ul = this.listEl as HTMLElement;
    const input = this.input as HTMLInputElement;
    while (ul.firstChild) ul.firstChild.remove();
    this.shown.forEach((c, i) => {
      const li = dom.el("li");
      dom.attr(li, "role", "option");
      dom.attr(li, "id", `${ul.id}-${i}`);
      dom.attr(li, "aria-selected", i === this.index ? "true" : "false");
      if (c.availability.state !== "available") dom.attr(li, "aria-disabled", "true");
      const label = dom.el("span");
      dom.text(label, c.label);
      dom.append(li, label);
      const hint =
        c.hint ??
        (c.availability.state !== "available" ? `Unavailable: ${c.availability.reason ?? c.availability.state}` : "");
      if (hint) {
        const h = dom.el("span");
        dom.text(h, hint);
        dom.append(li, h);
      }
      li.addEventListener("mousedown", (e) => {
        e.preventDefault();
        this.index = i;
        void this.run();
      });
      dom.append(ul, li);
    });
    dom.attr(input, "aria-activedescendant", this.shown.length ? `${ul.id}-${this.index}` : null);
  }

  private async run(): Promise<void> {
    const c = this.shown[this.index];
    if (!c) return;
    if (c.availability.state !== "available") {
      this.host.say(`${c.label} is unavailable: ${c.availability.reason ?? c.availability.state}`, "assertive");
      return;
    }
    this.close();
    await c.run();
  }

  private onKey(e: KeyboardEvent): void {
    const n = this.shown.length;
    if (e.key === "Escape") {
      e.preventDefault();
      this.close();
    } else if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      if (n) this.index = (this.index + (e.key === "ArrowDown" ? 1 : n - 1)) % n;
      this.paint();
    } else if (e.key === "Enter") {
      e.preventDefault();
      void this.run();
    } else if (e.key === "Tab") {
      e.preventDefault();
    }
  }
}
