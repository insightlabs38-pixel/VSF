import type { Dom } from "./writes.ts";

export interface Spoken {
  level: "polite" | "assertive";
  message: string;
}

/** Two live regions: status for progress and results, alert for refusals and failures. */
export class Announcer {
  readonly log: Spoken[] = [];
  private flip = false;
  private dom: Dom;
  private polite: HTMLElement;
  private alert: HTMLElement;

  constructor(dom: Dom, polite: HTMLElement, alert: HTMLElement) {
    this.dom = dom;
    this.polite = polite;
    this.alert = alert;
  }

  say(message: string, level: Spoken["level"] = "polite"): void {
    this.log.push({ level, message });
    if (this.log.length > 200) this.log.shift();
    const el = level === "assertive" ? this.alert : this.polite;
    this.flip = !this.flip;
    // A trailing zero-width mark makes a repeated message a real text change, so it is announced again.
    this.dom.text(el, this.flip ? message : `${message}​`);
  }

  clear(): void {
    this.dom.text(this.polite, "");
    this.dom.text(this.alert, "");
  }
}

export interface FocusMemory {
  id: string;
  chain: string[];
}

const ID = "data-vsf-id";

/** Focus identity is semantic (an operation or a field), so it survives a switch of layout. */
export function captureFocus(root: ShadowRoot): FocusMemory | null {
  const active = root.activeElement;
  if (!active) return null;
  const own = active.closest(`[${ID}]`);
  if (!own) return null;
  const chain: string[] = [];
  for (let p = own.parentElement?.closest(`[${ID}]`) ?? null; p; p = p.parentElement?.closest(`[${ID}]`) ?? null) {
    chain.push(p.getAttribute(ID) as string);
  }
  return { id: own.getAttribute(ID) as string, chain };
}

function findById(root: ShadowRoot, id: string): HTMLElement | null {
  for (const e of root.querySelectorAll<HTMLElement>(`[${ID}]`)) if (e.getAttribute(ID) === id) return e;
  return null;
}

function focusable(e: HTMLElement): boolean {
  if (e.matches("input,select,textarea,button,a[href]")) return !(e as HTMLInputElement).disabled;
  return e.hasAttribute("tabindex");
}

/** Refocuses the same thing if it still exists, else its nearest surviving container. */
export function restoreFocus(root: ShadowRoot, mem: FocusMemory | null, fallback: HTMLElement | null): boolean {
  if (!mem) return false;
  if (root.activeElement && root.activeElement.closest(`[${ID}]`)?.getAttribute(ID) === mem.id) return true;
  for (const id of [mem.id, ...mem.chain]) {
    const e = findById(root, id);
    if (e && focusable(e)) {
      e.focus({ preventScroll: true });
      return true;
    }
  }
  if (fallback) {
    fallback.focus({ preventScroll: true });
    return true;
  }
  return false;
}

const RTL_LANGS = new Set(["ar", "he", "fa", "ur", "ps", "sd", "ug", "yi", "dv", "ckb"]);

export function resolveDirection(direction: "ltr" | "rtl" | "auto", locale: string): "ltr" | "rtl" {
  if (direction !== "auto") return direction;
  const lang = locale.split("-")[0]?.toLowerCase() ?? "";
  return RTL_LANGS.has(lang) ? "rtl" : "ltr";
}
