import {
  FLAG_ACCOUNT,
  FLAG_CREDENTIAL,
  FLAG_DISABLED,
  FLAG_EDITABLE,
  FLAG_INTERACTIVE,
  FLAG_REDACTED,
  FLAG_RTL,
  FLAG_VISIBLE,
  type NodeFacts,
  type Rel,
  type RelKind,
} from "./model.ts";
import { redactText } from "./redact.ts";
import { looksAccountSpecific, type SourceScope, scopeFlag, scopeOf } from "./sensitive.ts";

export const SKIP_TAGS = new Set(["SCRIPT", "STYLE", "NOSCRIPT", "TEMPLATE", "HEAD", "META", "LINK", "TITLE"]);
const LEAF_TAGS = new Set(["SVG", "svg", "CANVAS", "OBJECT", "EMBED"]);
const INTERACTIVE_TAGS = new Set(["BUTTON", "A", "INPUT", "SELECT", "TEXTAREA", "SUMMARY"]);
const INTERACTIVE_ROLES = new Set([
  "button",
  "link",
  "textbox",
  "checkbox",
  "radio",
  "menuitem",
  "tab",
  "switch",
  "combobox",
  "option",
  "slider",
]);
export const NAME_FROM_CONTENT = new Set([
  "button",
  "link",
  "heading",
  "tab",
  "menuitem",
  "option",
  "cell",
  "columnheader",
]);
const BUTTON_INPUTS = new Set(["button", "submit", "reset"]);
const NO_VALUE_INPUTS = new Set(["checkbox", "radio", "button", "submit", "reset", "hidden", "image", "file", "range"]);

export interface CaptureOptions {
  maxTextChars: number;
}

export const isLeaf = (el: Element): boolean => LEAF_TAGS.has(el.tagName);

function inputType(el: Element): string {
  return (el.getAttribute("type") ?? "text").toLowerCase();
}

export function roleOf(el: Element): string | null {
  const explicit = el.getAttribute("role");
  if (explicit?.trim()) return explicit.trim().split(/\s+/, 1)[0] ?? null;
  const tag = el.tagName.toLowerCase();
  switch (tag) {
    case "button":
    case "summary":
      return "button";
    case "a":
      return el.hasAttribute("href") ? "link" : null;
    case "input": {
      const type = inputType(el);
      if (type === "checkbox" || type === "radio") return type;
      if (BUTTON_INPUTS.has(type) || type === "image") return "button";
      if (type === "hidden") return null;
      if (type === "range") return "slider";
      return "textbox";
    }
    case "textarea":
      return "textbox";
    case "select":
      return el.hasAttribute("multiple") || Number(el.getAttribute("size")) > 1 ? "listbox" : "combobox";
    case "option":
      return "option";
    case "table":
      return "table";
    case "tr":
      return "row";
    case "td":
      return "cell";
    case "th":
      return el.getAttribute("scope") === "row" ? "rowheader" : "columnheader";
    case "ul":
    case "ol":
      return "list";
    case "li":
      return "listitem";
    case "form":
      return "form";
    case "nav":
      return "navigation";
    case "main":
      return "main";
    case "img":
      return el.getAttribute("alt") === "" ? "presentation" : "img";
    case "dialog":
      return "dialog";
    case "progress":
      return "progressbar";
    default:
      return /^h[1-6]$/.test(tag) ? "heading" : null;
  }
}

function clip(text: string, max: number): string {
  if (text.length <= max) return text;
  const cut = text.charCodeAt(max - 1);
  return text.slice(0, cut >= 0xd800 && cut < 0xdc00 ? max - 1 : max);
}

const collapse = (s: string): string => s.replace(/\s+/g, " ").trim();

/** Descendant text with an early stop, so a huge region under a button costs only what is kept. */
function contentText(el: Element, max: number): string {
  let out = "";
  const stack: Node[] = [];
  for (let c = el.lastChild; c; c = c.previousSibling) stack.push(c);
  while (stack.length > 0 && out.length <= max) {
    const n = stack.pop() as Node;
    if (n.nodeType === 3) out += `${n.nodeValue ?? ""} `;
    else if (n.nodeType === 1) {
      const e = n as Element;
      if (SKIP_TAGS.has(e.tagName) || e.getAttribute("aria-hidden") === "true") continue;
      for (let c = e.lastChild; c; c = c.previousSibling) stack.push(c);
    }
  }
  return collapse(out);
}

function rootLookup(el: Element, id: string): Element | null {
  const root = el.getRootNode() as Document | ShadowRoot;
  return root.getElementById ? root.getElementById(id) : null;
}

export interface Described {
  facts: NodeFacts;
  /** Elements this node's name or relations were computed from. */
  deps: Element[];
  relTargets: { kind: RelKind; el: Element }[];
}

export interface DescribeCtx {
  opts: CaptureOptions;
  idOf(el: Element): number;
}

function textAlternative(el: Element, opts: CaptureOptions): string {
  const label = el.getAttribute("aria-label");
  if (label?.trim()) return collapse(label);
  return contentText(el, opts.maxTextChars);
}

/** Simplified accessible-name steps: labelledby, aria-label, native labels, alt/title/placeholder, content. */
function nameOf(
  el: Element,
  role: string | null,
  opts: CaptureOptions,
  deps: Element[],
  targets: { kind: RelKind; el: Element }[],
): string | null {
  const refs = el.getAttribute("aria-labelledby");
  if (refs) {
    const parts: string[] = [];
    for (const id of refs.split(/\s+/).filter(Boolean)) {
      const t = rootLookup(el, id);
      if (!t) continue;
      deps.push(t);
      targets.push({ kind: "labelledby", el: t });
      const a = textAlternative(t, opts);
      if (a) parts.push(a);
    }
    if (parts.length > 0) return parts.join(" ");
  }
  const own = el.getAttribute("aria-label");
  if (own?.trim()) return collapse(own);
  const labels = (el as HTMLInputElement).labels;
  if (labels && labels.length > 0) {
    const parts: string[] = [];
    for (const l of Array.from(labels)) {
      deps.push(l);
      targets.push({ kind: "label", el: l });
      const t = contentText(l, opts.maxTextChars);
      if (t) parts.push(t);
    }
    if (parts.length > 0) return parts.join(" ");
  }
  const tag = el.tagName;
  if (tag === "INPUT" && BUTTON_INPUTS.has(inputType(el))) {
    const v = el.getAttribute("value");
    if (v?.trim()) return collapse(v);
  }
  for (const attr of tag === "IMG" || tag === "AREA" || inputType(el) === "image" ? ["alt", "title"] : ["title"]) {
    const v = el.getAttribute(attr);
    if (v?.trim()) return collapse(v);
  }
  if (role !== null && NAME_FROM_CONTENT.has(role)) {
    const t = contentText(el, opts.maxTextChars);
    if (t) return t;
  }
  const ph = el.getAttribute("placeholder");
  return ph?.trim() ? collapse(ph) : null;
}

function ownText(el: Element): string | null {
  const tag = el.tagName;
  if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return null;
  let out = "";
  for (let c = el.firstChild; c; c = c.nextSibling) {
    if (c.nodeType === 3) out += `${c.nodeValue ?? ""} `;
  }
  const text = collapse(out);
  return text === "" ? null : text;
}

function visible(el: Element): boolean {
  const check = (el as Element & { checkVisibility?: () => boolean }).checkVisibility;
  return check ? check.call(el) : (el as HTMLElement).offsetParent !== null;
}

function rtl(el: Element): boolean {
  try {
    return el.matches(":dir(rtl)");
  } catch {
    return el.closest("[dir]")?.getAttribute("dir")?.toLowerCase() === "rtl";
  }
}

function disabled(el: Element): boolean {
  if (el.getAttribute("aria-disabled") === "true") return true;
  try {
    return el.matches(":disabled");
  } catch {
    return el.hasAttribute("disabled");
  }
}

/** Facts for one element. Form-control values are never read; credential regions yield no text at all. */
export function describeElement(el: Element, ctx: DescribeCtx): Described {
  const role = roleOf(el);
  const scope: SourceScope = scopeOf(el);
  const deps: Element[] = [];
  const relTargets: { kind: RelKind; el: Element }[] = [];
  let name: string | null = null;
  let text: string | null = null;
  let flags = scopeFlag(scope);
  const control = el.tagName === "INPUT" || el.tagName === "TEXTAREA" || el.tagName === "SELECT";
  if (scope !== "credential" || control) {
    const rawName = nameOf(el, role, ctx.opts, deps, relTargets);
    const rawText = scope === "credential" ? null : ownText(el);
    name = rawName === null ? null : redactText(clip(rawName, ctx.opts.maxTextChars));
    text = rawText === null ? null : redactText(clip(rawText, ctx.opts.maxTextChars));
    if (
      (name !== null && name !== clip(rawName ?? "", ctx.opts.maxTextChars)) ||
      (text !== null && text !== clip(rawText ?? "", ctx.opts.maxTextChars))
    ) {
      flags |= FLAG_REDACTED;
    }
    if (
      scope === "public" &&
      ((text !== null && looksAccountSpecific(text)) || (name !== null && looksAccountSpecific(name)))
    ) {
      flags |= FLAG_ACCOUNT;
    }
  }
  const described = el.getAttribute("aria-describedby");
  if (described) {
    for (const id of described.split(/\s+/).filter(Boolean)) {
      const t = rootLookup(el, id);
      if (t) {
        deps.push(t);
        relTargets.push({ kind: "describedby", el: t });
      }
    }
  }
  const tag = el.tagName;
  const type = inputType(el);
  const interactive =
    INTERACTIVE_TAGS.has(tag) || (role !== null && INTERACTIVE_ROLES.has(role)) || el.hasAttribute("tabindex");
  const editable =
    tag === "TEXTAREA" ||
    (tag === "INPUT" && !NO_VALUE_INPUTS.has(type)) ||
    (el as HTMLElement).isContentEditable === true;
  if (visible(el)) flags |= FLAG_VISIBLE;
  if (interactive) flags |= FLAG_INTERACTIVE;
  if (editable) flags |= FLAG_EDITABLE;
  if (disabled(el)) flags |= FLAG_DISABLED;
  if (rtl(el)) flags |= FLAG_RTL;
  const rels: Rel[] = relTargets.map((r) => [r.kind, ctx.idOf(r.el)] as const);
  return { facts: { tag: tag.toLowerCase(), role, name, text, flags, rels }, deps, relTargets };
}

export interface GeometryProbe {
  x: number;
  y: number;
  width: number;
  height: number;
}

/** Layout is read only for the elements asked about; nothing in capture or mutation handling calls it. */
export function probeGeometry(el: Element): GeometryProbe {
  const r = el.getBoundingClientRect();
  return { x: r.x, y: r.y, width: r.width, height: r.height };
}

export { FLAG_ACCOUNT, FLAG_CREDENTIAL };
