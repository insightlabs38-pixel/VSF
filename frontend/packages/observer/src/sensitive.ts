import { FLAG_ACCOUNT, FLAG_CREDENTIAL } from "./model.ts";

export type SourceScope = "credential" | "account" | "public";

const RANK: Record<SourceScope, number> = { public: 0, account: 1, credential: 2 };

export const stricter = (a: SourceScope, b: SourceScope): SourceScope => (RANK[b] > RANK[a] ? b : a);

const CREDENTIAL_AUTOCOMPLETE = /(^|\s)(current-password|new-password|one-time-code|cc-[a-z-]+|webauthn)(\s|$)/i;
const ACCOUNT_AUTOCOMPLETE =
  /(^|\s)(name|given-name|family-name|additional-name|nickname|username|email|tel[a-z-]*|street-address|address-line[123]|postal-code|bday[a-z-]*|sex|impp)(\s|$)/i;
const EMAIL = /[A-Za-z0-9._%+-]+@[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)+/;
const PHONE = /(?<![\d.])\+?\d[\d\s().-]{8,}\d(?![\d.])/;
const LONG_DIGITS = /\b\d{13,19}\b/;

const CREDENTIAL_TOKENS = new Set([
  "password",
  "passwd",
  "passcode",
  "passphrase",
  "pwd",
  "secret",
  "token",
  "apikey",
  "otp",
  "totp",
  "hotp",
  "mfa",
  "2fa",
  "cvv",
  "cvc",
  "csc",
  "cvn",
  "ssn",
  "pin",
  "iban",
]);
const CREDENTIAL_PAIRS = [
  ["api", "key"],
  ["access", "key"],
  ["secret", "key"],
  ["private", "key"],
  ["security", "code"],
  ["verification", "code"],
  ["recovery", "code"],
  ["backup", "code"],
  ["auth", "code"],
  ["one", "time"],
  ["card", "number"],
  ["credit", "card"],
  ["debit", "card"],
  ["routing", "number"],
  ["card", "expiry"],
  ["card", "expiration"],
  ["social", "security"],
];
const ACCOUNT_TOKENS = new Set([
  "email",
  "phone",
  "mobile",
  "telephone",
  "address",
  "postcode",
  "zip",
  "postal",
  "birthday",
  "birthdate",
  "birth",
  "dob",
  "username",
  "userid",
  "login",
]);
const ACCOUNT_PAIRS = [
  ["first", "name"],
  ["last", "name"],
  ["full", "name"],
  ["user", "name"],
  ["user", "id"],
  ["given", "name"],
  ["family", "name"],
  ["account", "number"],
];

/** Lowercase words of an identifier or label: camelCase and every non-alphanumeric run split words. */
export function wordsOf(text: string): string[] {
  return text
    .replace(/([a-z0-9])([A-Z])/g, "$1 $2")
    .toLowerCase()
    .split(/[^a-z0-9]+/)
    .filter(Boolean);
}

function hasPair(words: string[], pairs: string[][]): boolean {
  for (let i = 0; i + 1 < words.length; i++) {
    for (const [a, b] of pairs) if (words[i] === a && words[i + 1] === b) return true;
  }
  return false;
}

export function textNamesCredential(text: string | null): boolean {
  if (!text) return false;
  const words = wordsOf(text);
  const joined = words.join("");
  return (
    words.some((w) => CREDENTIAL_TOKENS.has(w)) ||
    hasPair(words, CREDENTIAL_PAIRS) ||
    /apikey|secretkey|privatekey|accesskey/.test(joined)
  );
}

export function textNamesAccount(text: string | null): boolean {
  if (!text) return false;
  const words = wordsOf(text);
  return words.some((w) => ACCOUNT_TOKENS.has(w)) || hasPair(words, ACCOUNT_PAIRS);
}

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

/** A page marker is an untrusted hint: an unknown value restricts, `public` changes nothing. */
function parseMarker(value: string): SourceScope {
  const v = value.trim().toLowerCase();
  return v === "public" ? "public" : v === "credential" ? "credential" : "account";
}

/** The strictest `data-vsf-scope` on the element, any ancestor and any shadow host above it. */
export function markerScope(el: Element): SourceScope {
  let worst: SourceScope = "public";
  let top: Element | null = el;
  while (top) {
    for (let a: Element | null = top; a; a = a.parentElement) {
      const v = a.getAttribute("data-vsf-scope");
      if (v !== null) worst = stricter(worst, parseMarker(v));
    }
    top = (top.getRootNode() as ShadowRoot).host ?? null;
  }
  return worst;
}

const FREE_TEXT_ROLES = new Set(["textbox", "searchbox", "combobox", "spinbutton"]);

export function isValueControl(el: Element, role: string | null): boolean {
  const tag = el.tagName;
  if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return true;
  if ((el as HTMLElement).isContentEditable === true) return true;
  return role !== null && FREE_TEXT_ROLES.has(role);
}

/** What the element is, regardless of what the page says about it: the floor no marker can lower. */
export function intrinsicScope(el: Element, role: string | null, label: string | null): SourceScope {
  if (!isValueControl(el, role)) return "public";
  const type = el.tagName === "INPUT" ? (el.getAttribute("type") ?? "text").toLowerCase() : "";
  const complete = el.getAttribute("autocomplete") ?? "";
  if (type === "password" || CREDENTIAL_AUTOCOMPLETE.test(complete)) return "credential";
  const ident = `${el.getAttribute("name") ?? ""} ${el.id}`;
  if (textNamesCredential(ident) || textNamesCredential(label)) return "credential";
  if (type === "email" || type === "tel" || ACCOUNT_AUTOCOMPLETE.test(complete)) return "account";
  if (textNamesAccount(ident) || textNamesAccount(label)) return "account";
  return "public";
}

/** The stricter of what the element is and what any enclosing marker says; markers can only add restriction. */
export function scopeOf(el: Element, role: string | null = null, label: string | null = null): SourceScope {
  return stricter(intrinsicScope(el, role, label), markerScope(el));
}

export function scopeFlag(scope: SourceScope): number {
  return scope === "credential" ? FLAG_CREDENTIAL : scope === "account" ? FLAG_ACCOUNT : 0;
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
