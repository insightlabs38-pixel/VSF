export interface MessageRule {
  id: string;
  message: string;
}

export const MAX_LINES = 9;

const FORBIDDEN: [string, RegExp][] = [
  ["attribution.co-author", /co-authored-by/i],
  ["attribution.signature", /signed-off-by/i],
  ["attribution.assistant", /claude|anthropic/i],
  ["attribution.ai", /\bAI\b/],
  ["attribution.generated", /generated[- ](with|by)/i],
  ["attribution.link", /https?:\/\//i],
];

/** Commit messages, PR titles/bodies and comments share one rule set: under 10 physical lines, no attribution. */
export function checkMessage(text: string): MessageRule[] {
  const out: MessageRule[] = [];
  const lines = text.replace(/\n+$/, "").split("\n");
  if (text.trim() === "") out.push({ id: "message.empty", message: "message is empty" });
  if (lines.length > MAX_LINES)
    out.push({ id: "message.too-long", message: `${lines.length} lines; maximum is ${MAX_LINES}` });
  for (const [id, re] of FORBIDDEN) {
    if (re.test(text)) out.push({ id, message: `forbidden content matched ${re}` });
  }
  return out;
}

export const REQUIRED_IDENTITY = { name: "insightlabs38-pixel", email: "insightlabs38@gmail.com" } as const;

export function checkIdentity(name: string, email: string): MessageRule[] {
  const out: MessageRule[] = [];
  if (name !== REQUIRED_IDENTITY.name || email !== REQUIRED_IDENTITY.email) {
    out.push({
      id: "identity.mismatch",
      message: `expected ${REQUIRED_IDENTITY.name} <${REQUIRED_IDENTITY.email}>, found ${name} <${email}>`,
    });
  }
  return out;
}
