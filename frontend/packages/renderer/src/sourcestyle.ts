export interface StyleProblem {
  code: string;
  detail?: string;
}

export const MAX_STYLE_BYTES = 64 * 1024;
const MAX_RULES = 2000;

function strip(css: string): string {
  return css.replace(/\/\*[\s\S]*?\*\//g, " ");
}

/** A per-source stylesheet may restyle the page; it may not load anything, run anything or hide where it came from. */
export function validateSourceStyle(css: string): StyleProblem[] {
  const p: StyleProblem[] = [];
  if (new TextEncoder().encode(css).length > MAX_STYLE_BYTES) p.push({ code: "style.too_large" });
  const text = strip(css);
  if (/@import\b/i.test(text)) p.push({ code: "style.import" });
  if (/@font-face\b/i.test(text)) p.push({ code: "style.font_face" });
  for (const m of text.matchAll(/url\(\s*(['"]?)([^'")]*)\1\s*\)/gi)) {
    const target = (m[2] ?? "").trim();
    if (!/^(#|data:image\/(png|gif|jpeg|webp);base64,)/i.test(target))
      p.push({ code: "style.external_url", detail: target.slice(0, 40) });
  }
  if (/expression\s*\(|behavior\s*:|-moz-binding|javascript:/i.test(text)) p.push({ code: "style.script_hook" });
  if (/@(namespace|charset)\b/i.test(text)) p.push({ code: "style.at_rule" });
  const open = (text.match(/\{/g) ?? []).length;
  const close = (text.match(/\}/g) ?? []).length;
  if (open !== close) p.push({ code: "style.unbalanced" });
  if (open > MAX_RULES) p.push({ code: "style.too_many_rules" });
  return p;
}

interface Stored {
  origin: string;
  css: string;
}

/** Constructed stylesheets leave the source DOM untouched, so switching off is exact; a sheet applies only to its origin. */
export class SourceStyles {
  private sheets = new Map<string, { stored: Stored; sheet: CSSStyleSheet }>();
  private enabled = true;
  private doc: Document;

  constructor(doc: Document) {
    this.doc = doc;
  }

  private origin(): string {
    return this.doc.defaultView?.location.origin ?? "";
  }

  apply(origin: string, id: string, css: string): StyleProblem[] {
    const problems = validateSourceStyle(css);
    if (!/^https?:\/\/[a-z0-9.-]+(:\d+)?$/i.test(origin)) problems.push({ code: "style.bad_origin" });
    if (problems.length) return problems;
    this.remove(id);
    const view = this.doc.defaultView as (Window & typeof globalThis) | null;
    const sheet = new (view as Window & typeof globalThis).CSSStyleSheet();
    sheet.replaceSync(css);
    this.sheets.set(id, { stored: { origin, css }, sheet });
    this.sync();
    return [];
  }

  remove(id: string): void {
    const had = this.sheets.delete(id);
    if (had) this.sync();
  }

  setEnabled(on: boolean): void {
    this.enabled = on;
    this.sync();
  }

  get isEnabled(): boolean {
    return this.enabled;
  }

  ids(): string[] {
    return [...this.sheets.keys()];
  }

  /** Sheets currently attached to the document. */
  attached(): CSSStyleSheet[] {
    const mine = new Set([...this.sheets.values()].map((s) => s.sheet));
    return this.doc.adoptedStyleSheets.filter((s) => mine.has(s));
  }

  private sync(): void {
    const mine = new Set([...this.sheets.values()].map((s) => s.sheet));
    const others = this.doc.adoptedStyleSheets.filter((s) => !mine.has(s));
    const want: CSSStyleSheet[] = [];
    if (this.enabled) {
      for (const { stored, sheet } of this.sheets.values()) if (stored.origin === this.origin()) want.push(sheet);
    }
    const next = [...others, ...want];
    const cur = this.doc.adoptedStyleSheets;
    if (next.length === cur.length && next.every((s, i) => s === cur[i])) return;
    this.doc.adoptedStyleSheets = next;
  }
}
