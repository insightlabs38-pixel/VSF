import { REDACTION_PATTERNS } from "./generated.ts";

const compiled = REDACTION_PATTERNS.map((p) => ({ kind: p.kind, re: new RegExp(p.source, `g${p.flags}`) }));

/** Same patterns as the Rust and Node redactors; returns the input object identity when nothing matched. */
export function redactText(text: string): string {
  let out = text;
  for (const p of compiled) {
    p.re.lastIndex = 0;
    if (p.re.test(out)) {
      p.re.lastIndex = 0;
      out = out.replace(p.re, `[REDACTED:${p.kind}]`);
    }
  }
  return out;
}

/** Redacts a bounded read of the whole text first, so a secret straddling the clip point cannot leak its head. */
export function redactThenClip(raw: string, max: number): string {
  const redacted = redactText(raw.slice(0, max * 4));
  if (redacted.length <= max) return redacted;
  const cut = redacted.charCodeAt(max - 1);
  return redacted.slice(0, cut >= 0xd800 && cut < 0xdc00 ? max - 1 : max);
}
