export interface StyleProblem {
  code: string;
  detail?: string;
}

export const MAX_STYLE_BYTES = 64 * 1024;
const MAX_RULES = 2000;
const MAX_NESTING = 8;

/** Functions that cannot load anything; every other function is refused, so a new loading function cannot slip in. */
const SAFE_FUNCTIONS = new Set([
  "rgb",
  "rgba",
  "hsl",
  "hsla",
  "hwb",
  "lab",
  "lch",
  "oklab",
  "oklch",
  "color",
  "color-mix",
  "light-dark",
  "calc",
  "min",
  "max",
  "clamp",
  "var",
  "env",
  "abs",
  "sign",
  "round",
  "mod",
  "rem",
  "sin",
  "cos",
  "tan",
  "asin",
  "acos",
  "atan",
  "atan2",
  "pow",
  "sqrt",
  "hypot",
  "log",
  "exp",
  "linear-gradient",
  "radial-gradient",
  "conic-gradient",
  "repeating-linear-gradient",
  "repeating-radial-gradient",
  "repeating-conic-gradient",
  "translate",
  "translatex",
  "translatey",
  "translatez",
  "translate3d",
  "rotate",
  "rotatex",
  "rotatey",
  "rotatez",
  "rotate3d",
  "scale",
  "scalex",
  "scaley",
  "scalez",
  "scale3d",
  "skew",
  "skewx",
  "skewy",
  "matrix",
  "matrix3d",
  "perspective",
  "cubic-bezier",
  "steps",
  "linear",
  "minmax",
  "repeat",
  "fit-content",
  "counter",
  "counters",
  "blur",
  "brightness",
  "contrast",
  "drop-shadow",
  "grayscale",
  "hue-rotate",
  "invert",
  "opacity",
  "saturate",
  "sepia",
  "polygon",
  "circle",
  "ellipse",
  "inset",
  "rect",
  "xywh",
  "format",
  "local",
  "not",
  "is",
  "where",
  "has",
  "nth-child",
  "nth-last-child",
  "nth-of-type",
  "nth-last-of-type",
  "lang",
  "dir",
  "supports",
  "selector",
  "min-width",
  "max-width",
]);

const SAFE_AT_RULES = new Set(["media", "supports", "layer", "container", "keyframes", "scope", "starting-style"]);

function tokens(css: string): { kind: "word" | "fn" | "at" | "punct" | "string"; text: string }[] | null {
  const out: { kind: "word" | "fn" | "at" | "punct" | "string"; text: string }[] = [];
  let i = 0;
  while (i < css.length) {
    const c = css[i] as string;
    if (c === "/" && css[i + 1] === "*") {
      const end = css.indexOf("*/", i + 2);
      if (end < 0) return null;
      i = end + 2;
    } else if (c === '"' || c === "'") {
      let j = i + 1;
      while (j < css.length && css[j] !== c && css[j] !== "\n") j++;
      if (css[j] !== c) return null;
      out.push({ kind: "string", text: css.slice(i + 1, j) });
      i = j + 1;
    } else if (c === "@") {
      const m = /^@-?[a-zA-Z][a-zA-Z0-9-]*/.exec(css.slice(i, i + 64));
      out.push({ kind: "at", text: (m?.[0] ?? "@").toLowerCase() });
      i += m?.[0].length ?? 1;
    } else if (/[a-zA-Z_-]/.test(c)) {
      const m = /^-?[a-zA-Z_][a-zA-Z0-9_-]*/.exec(css.slice(i, i + 128)) ?? /^-+/.exec(css.slice(i, i + 4));
      const word = m?.[0] ?? c;
      i += word.length;
      if (css[i] === "(") {
        out.push({ kind: "fn", text: word.toLowerCase() });
        i++;
      } else out.push({ kind: "word", text: word.toLowerCase() });
    } else if (/\s/.test(c)) i++;
    else {
      out.push({ kind: "punct", text: c });
      i++;
    }
  }
  return out;
}

/** A per-source stylesheet may restyle the page; it may not load anything, run anything or hide where it came from. */
export function validateSourceStyle(css: string): StyleProblem[] {
  const p: StyleProblem[] = [];
  if (new TextEncoder().encode(css).length > MAX_STYLE_BYTES) p.push({ code: "style.too_large" });
  if (css.includes("\\")) p.push({ code: "style.escape" });
  for (let i = 0; i < css.length; i++) {
    const code = css.charCodeAt(i);
    if ((code < 32 && code !== 9 && code !== 10 && code !== 13) || code === 127) {
      p.push({ code: "style.control_character" });
      break;
    }
  }
  const toks = tokens(css);
  if (!toks) return [...p, { code: "style.unterminated" }];
  let depth = 0;
  let rules = 0;
  let maxDepth = 0;
  for (const t of toks) {
    if (t.kind === "at") {
      if (t.text === "@import") p.push({ code: "style.import" });
      else if (t.text === "@font-face") p.push({ code: "style.font_face" });
      else if (!SAFE_AT_RULES.has(t.text.slice(1))) p.push({ code: "style.at_rule", detail: t.text.slice(0, 30) });
    } else if (t.kind === "fn") {
      if (t.text === "expression") p.push({ code: "style.script_hook" });
      else if (t.text === "url" || /image|src|element|paint|cross-fade|attr/.test(t.text))
        p.push({ code: "style.external_url", detail: t.text });
      else if (!SAFE_FUNCTIONS.has(t.text)) p.push({ code: "style.unknown_function", detail: t.text.slice(0, 30) });
    } else if (t.kind === "word" && /^(expression|behavior|-moz-binding)$/.test(t.text)) {
      p.push({ code: "style.script_hook" });
    } else if (t.kind === "string" && /^\s*javascript:/i.test(t.text)) {
      p.push({ code: "style.script_hook" });
    } else if (t.kind === "punct") {
      if (t.text === "{") {
        depth++;
        rules++;
        maxDepth = Math.max(maxDepth, depth);
      } else if (t.text === "}") {
        depth--;
        if (depth < 0) {
          p.push({ code: "style.unbalanced" });
          depth = 0;
        }
      }
    }
  }
  if (depth !== 0) p.push({ code: "style.unbalanced" });
  if (maxDepth > MAX_NESTING) p.push({ code: "style.too_deep" });
  if (rules > MAX_RULES) p.push({ code: "style.too_many_rules" });
  return p;
}

/** Declared in an earlier layer than any source rule, so its `!important` outranks theirs. */
export const GUARD_CSS =
  "@layer vsf-guard{[data-vsf-host]{display:block!important;visibility:visible!important;opacity:1!important;" +
  "transform:none!important;filter:none!important;clip-path:none!important;mask:none!important;" +
  "pointer-events:auto!important;content-visibility:visible!important;contain:none!important}}";

interface Stored {
  origin: string;
  css: string;
}

/** Constructed stylesheets leave the source DOM untouched, so switching off is exact; a sheet applies only to its origin. */
export class SourceStyles {
  private sheets = new Map<string, { stored: Stored; sheet: CSSStyleSheet }>();
  private ours = new WeakSet<CSSStyleSheet>();
  private guard: CSSStyleSheet | null = null;
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
    this.ours.add(sheet);
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

  private guardSheet(): CSSStyleSheet {
    if (!this.guard) {
      const view = this.doc.defaultView as Window & typeof globalThis;
      this.guard = new view.CSSStyleSheet();
      this.guard.replaceSync(GUARD_CSS);
      this.ours.add(this.guard);
    }
    return this.guard;
  }

  private sync(): void {
    const others = this.doc.adoptedStyleSheets.filter((s) => !this.ours.has(s));
    const want: CSSStyleSheet[] = [];
    if (this.enabled) {
      for (const { stored, sheet } of this.sheets.values()) if (stored.origin === this.origin()) want.push(sheet);
    }
    const next = [...others, ...(want.length > 0 ? [this.guardSheet(), ...want] : [])];
    const cur = this.doc.adoptedStyleSheets;
    if (next.length === cur.length && next.every((s, i) => s === cur[i])) return;
    this.doc.adoptedStyleSheets = next;
  }
}
