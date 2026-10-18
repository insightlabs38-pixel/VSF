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
