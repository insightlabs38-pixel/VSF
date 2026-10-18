import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

interface PatternSpec {
  kind: string;
  regex: string;
  flags?: string;
}

export interface Pattern {
  kind: string;
  regex: RegExp;
}

const DEFAULT_PATH = fileURLToPath(new URL("../../../schemas/redaction/patterns.v1.json", import.meta.url));

export function loadPatterns(path: string = DEFAULT_PATH): Pattern[] {
  const doc = JSON.parse(readFileSync(path, "utf8")) as { patterns: PatternSpec[] };
  return doc.patterns.map((p) => ({ kind: p.kind, regex: new RegExp(p.regex, `g${p.flags ?? ""}`) }));
}

const patterns = loadPatterns();

export function redact(text: string, list: readonly Pattern[] = patterns): string {
  let current = text;
  for (const p of list) current = current.replace(p.regex, `[REDACTED:${p.kind}]`);
  return current;
}

export function findSecretShapes(text: string, list: readonly Pattern[] = patterns): string[] {
  const kinds = new Set<string>();
  for (const p of list) {
    p.regex.lastIndex = 0;
    if (p.regex.test(text)) kinds.add(p.kind);
    p.regex.lastIndex = 0;
  }
  return [...kinds].sort();
}

export function patternCount(): number {
  return patterns.length;
}
