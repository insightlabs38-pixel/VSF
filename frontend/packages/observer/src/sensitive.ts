import { FLAG_ACCOUNT, FLAG_CREDENTIAL, type NodeRec, type Replica } from "./model.ts";
import { redactText } from "./redact.ts";

export type SourceScope = "credential" | "account" | "public";

const CREDENTIAL_AUTOCOMPLETE =
  /(^|\s)(current-password|new-password|one-time-code|cc-(number|csc|exp)|webauthn)(\s|$)/i;
const CREDENTIAL_NAME = /pass(word|wd|code)?|secret|token|api[-_]?key|cvv|cvc|ssn|pin\b|otp/i;
const EMAIL = /[A-Za-z0-9._%+-]+@[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)+/;
const PHONE = /(?<![\d.])\+?\d[\d\s().-]{8,}\d(?![\d.])/;
const LONG_DIGITS = /\b\d{13,19}\b/;

function luhn(digits: string): boolean {
  let sum = 0;
  let dbl = false;
  for (let i = digits.length - 1; i >= 0; i--) {
    let d = digits.charCodeAt(i) - 48;
    if (dbl) {
      d *= 2;
      if (d > 9) d -= 9;
    }
    sum += d;
    dbl = !dbl;
  }
  return sum % 10 === 0;
}

/** Text that looks like personal data even when nothing marked its region. */
export function looksAccountSpecific(text: string): boolean {
  if (EMAIL.test(text)) return true;
  const digits = LONG_DIGITS.exec(text)?.[0];
  if (digits && luhn(digits)) return true;
  return PHONE.test(text) && !/^\d{1,4}([.,]\d+)?$/.test(text.trim());
}

function markerOf(el: Element): string | null | undefined {
  let cur: Element | null = el;
  while (cur) {
    const hit = cur.closest("[data-vsf-scope]");
    if (hit) return hit.getAttribute("data-vsf-scope");
    const root = cur.getRootNode() as ShadowRoot;
    cur = root.host ?? null;
  }
  return null;
}

/** Explicit `data-vsf-scope` (also across shadow boundaries) wins; otherwise form controls that conventionally hold secrets are credentials. */
export function scopeOf(el: Element): SourceScope {
  const marked = markerOf(el);
  if (marked === "credential" || marked === "account" || marked === "public") return marked;
  const tag = el.tagName;
  if (tag === "INPUT") {
    const type = (el.getAttribute("type") ?? "text").toLowerCase();
    if (type === "password") return "credential";
    if (CREDENTIAL_AUTOCOMPLETE.test(el.getAttribute("autocomplete") ?? "")) return "credential";
    if (CREDENTIAL_NAME.test(`${el.getAttribute("name") ?? ""} ${el.id}`)) return "credential";
  }
  if (tag === "TEXTAREA" && CREDENTIAL_NAME.test(`${el.getAttribute("name") ?? ""} ${el.id}`)) return "credential";
  return "public";
}

export function scopeFlag(scope: SourceScope): number {
  return scope === "credential" ? FLAG_CREDENTIAL : scope === "account" ? FLAG_ACCOUNT : 0;
}

const withheld = (r: NodeRec): boolean => (r.flags & (FLAG_ACCOUNT | FLAG_CREDENTIAL)) !== 0;

export interface SharedEvidence {
  nodes: { tag: string; role: string | null; name: string | null; text: string | null; flags: number; depth: number }[];
  withheld: number;
}

/** What may leave the device in routine diagnostics: structure everywhere, text only from public scopes. */
export function sharedEvidence(replica: Replica): SharedEvidence {
  const nodes: SharedEvidence["nodes"] = [];
  let hidden = 0;
  const walk = (ids: number[], depth: number): void => {
    for (const id of ids) {
      const r = replica.nodes.get(id) as NodeRec;
      const drop = withheld(r);
      if (drop) hidden++;
      nodes.push({
        tag: r.tag,
        role: r.role,
        name: drop || r.name === null ? null : redactText(r.name),
        text: drop || r.text === null ? null : redactText(r.text),
        flags: r.flags,
        depth,
      });
      walk(replica.children.get(id) ?? [], depth + 1);
    }
  };
  walk(replica.roots, 0);
  return { nodes, withheld: hidden };
}

export type DiagField = number | boolean;

/** Diagnostics carry codes and numbers only, so page text has no path into them. */
export class Diagnostics {
  readonly events: { code: string; fields: Record<string, DiagField> }[] = [];
  emit(code: string, fields: Record<string, DiagField> = {}): void {
    if (!/^[a-z][a-z0-9_.]{0,63}$/.test(code)) throw new Error("diagnostic code must be a short identifier");
    for (const v of Object.values(fields)) {
      if (typeof v !== "number" && typeof v !== "boolean")
        throw new Error("diagnostic fields must be numbers or booleans");
    }
    this.events.push({ code, fields: { ...fields } });
  }
}
