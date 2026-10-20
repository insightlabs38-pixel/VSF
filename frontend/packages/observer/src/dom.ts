import { ownUi } from "./own.ts";
import { Builder, FLAG_EDITABLE, FLAG_INTERACTIVE, FLAG_REDACTED, FLAG_VISIBLE, NONE, type Snapshot } from "./pack.ts";
import { redactThenClip } from "./redact.ts";
import { intrinsicScope, looksAccountSpecific, markerScope, stricter } from "./sensitive.ts";

const own = ownUi();

export interface SnapshotOptions {
  maxNodes: number;
  maxTextChars: number;
}

export const DEFAULT_OPTIONS: SnapshotOptions = { maxNodes: 100_000, maxTextChars: 256 };

export interface DomSnapshot {
  snapshot: Snapshot;
  truncated: boolean;
}

const SKIP = new Set(["SCRIPT", "STYLE", "NOSCRIPT", "TEMPLATE", "HEAD"]);
const OPAQUE = new Set(["SVG", "CANVAS", "IFRAME", "OBJECT", "EMBED"]);
const INTERACTIVE_TAGS = new Set(["BUTTON", "A", "INPUT", "SELECT", "TEXTAREA", "SUMMARY"]);
const INTERACTIVE_ROLES = new Set([
  "button",
  "link",
  "textbox",
  "checkbox",
  "menuitem",
  "tab",
  "switch",
  "combobox",
  "option",
]);

function implicitRole(el: Element): string | null {
  const explicit = el.getAttribute("role");
  if (explicit) return explicit.trim().split(/\s+/, 1)[0] ?? null;
  switch (el.tagName) {
    case "BUTTON":
      return "button";
    case "A":
      return el.hasAttribute("href") ? "link" : null;
    case "INPUT": {
      const type = (el.getAttribute("type") ?? "text").toLowerCase();
      if (type === "checkbox") return "checkbox";
      if (type === "button" || type === "submit" || type === "reset") return "button";
      if (type === "hidden") return null;
      return "textbox";
    }
    case "TEXTAREA":
      return "textbox";
    case "TABLE":
      return "table";
    case "TR":
      return "row";
    case "TD":
      return "cell";
    case "TH":
      return "columnheader";
    case "UL":
    case "OL":
      return "list";
    case "FORM":
      return "form";
    default:
      return /^H[1-6]$/.test(el.tagName) ? "heading" : null;
  }
}

function clip(text: string, max: number): string {
  if (text.length <= max) return text;
  const cut = text.charCodeAt(max - 1);
  return text.slice(0, cut >= 0xd800 && cut < 0xdc00 ? max - 1 : max);
}

function accessibleName(el: Element): string | null {
  const label = el.getAttribute("aria-label");
  if (label?.trim()) return label.trim();
  const labels = (el as HTMLInputElement).labels;
  if (labels && labels.length > 0) {
    const text = (labels[0]?.textContent ?? "").replace(/\s+/g, " ").trim();
    if (text) return text;
  }
  for (const attr of ["alt", "title", "placeholder"]) {
    const v = el.getAttribute(attr);
    if (v?.trim()) return v.trim();
  }
  return null;
}

function ownText(el: Element): string | null {
  if (el.tagName === "INPUT" || el.tagName === "TEXTAREA" || el.tagName === "SELECT") return null;
  let out = "";
  for (let c = el.firstChild; c; c = c.nextSibling) {
    if (c.nodeType === 3) out += `${c.nodeValue ?? ""} `;
  }
  const text = out.replace(/\s+/g, " ").trim();
  return text === "" ? null : text;
}

function isVisible(el: Element): boolean {
  const check = (el as Element & { checkVisibility?: () => boolean }).checkVisibility;
  return check ? check.call(el) : (el as HTMLElement).offsetParent !== null;
}

/** Form control values are never read, and text from credential or account scopes never enters the pack. */
export function snapshotDocument(doc: Document, options: SnapshotOptions = DEFAULT_OPTIONS): DomSnapshot {
  const b = new Builder();
  const root = doc.documentElement;
  if (!root) return { snapshot: b.snapshot, truncated: false };
  const stack: { el: Element; parent: number }[] = [{ el: root, parent: NONE }];
  let truncated = false;
  while (stack.length > 0) {
    const item = stack.pop() as { el: Element; parent: number };
    const el = item.el;
    if (SKIP.has(el.tagName)) continue;
    if (b.snapshot.nodes.length >= options.maxNodes) {
      truncated = true;
      break;
    }
    const role = implicitRole(el);
    const accessible = accessibleName(el);
    const scope = stricter(markerScope(el), intrinsicScope(el, role, accessible));
    const sensitive = scope !== "public";
    const rawName = sensitive ? null : accessible;
    const rawText = sensitive ? null : ownText(el);
    let name = rawName === null ? null : redactThenClip(rawName, options.maxTextChars);
    let text = rawText === null ? null : redactThenClip(rawText, options.maxTextChars);
    if (name !== null && looksAccountSpecific(name)) name = null;
    if (text !== null && looksAccountSpecific(text)) text = null;
    const type = (el.getAttribute("type") ?? "").toLowerCase();
    const interactive =
      INTERACTIVE_TAGS.has(el.tagName) || (role !== null && INTERACTIVE_ROLES.has(role)) || el.hasAttribute("tabindex");
    const editable =
      el.tagName === "TEXTAREA" ||
      (el.tagName === "INPUT" && type !== "checkbox" && type !== "button" && type !== "submit" && type !== "hidden") ||
      (el as HTMLElement).isContentEditable === true;
    let flags = 0;
    if (isVisible(el)) flags |= FLAG_VISIBLE;
    if (interactive) flags |= FLAG_INTERACTIVE;
    if (editable) flags |= FLAG_EDITABLE;
    if (
      (name !== null && name !== clip(rawName ?? "", options.maxTextChars)) ||
      (text !== null && text !== clip(rawText ?? "", options.maxTextChars)) ||
      sensitive
    ) {
      flags |= FLAG_REDACTED;
    }
    const index = b.node(item.parent, el.tagName.toLowerCase(), role, name, text, flags);
    if (OPAQUE.has(el.tagName)) continue;
    for (let i = el.children.length - 1; i >= 0; i--) {
      const k = el.children[i] as Element;
      if (!own.has(k)) stack.push({ el: k, parent: index });
    }
  }
  return { snapshot: b.snapshot, truncated };
}
