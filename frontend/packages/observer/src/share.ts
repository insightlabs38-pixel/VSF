import { FLAG_ACCOUNT, FLAG_CREDENTIAL, FLAG_REDACTED, type NodeRec, type Replica } from "./model.ts";
import { redactText } from "./redact.ts";
import { looksAccountSpecific } from "./sensitive.ts";

/** Roles whose accessible name is interface vocabulary, so a policy may allow them to leave the device. */
export const SHAREABLE_ROLES: ReadonlySet<string> = new Set([
  "button",
  "link",
  "tab",
  "menuitem",
  "columnheader",
  "heading",
]);

/** Roles that carry a record's values; their text never leaves, whatever a policy says. */
const DATA_ROLES: ReadonlySet<string> = new Set([
  "cell",
  "gridcell",
  "rowheader",
  "listitem",
  "option",
  "treeitem",
  "textbox",
  "searchbox",
  "combobox",
  "spinbutton",
  "log",
  "feed",
  "status",
  "alert",
]);

export interface SharePolicy {
  schema: "vsf-share-policy-v1";
  id: string;
  origin: string;
  principal_ref: string;
  text_roles: string[];
  max_chars: number;
  issued_at: number;
  expires_at: number;
}

export interface ShareContext {
  origin: string;
  principal: string;
  now: number;
}

export type PolicyCheck = { ok: true; policy: SharePolicy } | { ok: false; reason: string };

const KEYS = ["schema", "id", "origin", "principal_ref", "text_roles", "max_chars", "issued_at", "expires_at"];
const MAX_LIFETIME_MS = 30 * 24 * 3600 * 1000;
const SKEW_MS = 5 * 60 * 1000;

const isInt = (v: unknown): v is number => typeof v === "number" && Number.isSafeInteger(v) && v >= 0;

/** A policy is configuration held by the user or the extension, never read from the page, and is bound to one origin and principal. */
export function validateSharePolicy(raw: unknown, ctx: ShareContext): PolicyCheck {
  const bad = (reason: string): PolicyCheck => ({ ok: false, reason });
  if (typeof raw !== "object" || raw === null || Array.isArray(raw)) return bad("policy.not_object");
  const o = raw as Record<string, unknown>;
  const keys = Object.keys(o);
  if (keys.length !== KEYS.length || !KEYS.every((k) => keys.includes(k))) return bad("policy.keys");
  if (o.schema !== "vsf-share-policy-v1") return bad("policy.schema");
  if (typeof o.id !== "string" || !/^spol_[0-9a-f]{16,64}$/.test(o.id)) return bad("policy.id");
  if (typeof o.origin !== "string" || o.origin !== ctx.origin) return bad("policy.origin");
  if (typeof o.principal_ref !== "string" || o.principal_ref !== ctx.principal) return bad("policy.principal");
  if (!Array.isArray(o.text_roles) || o.text_roles.length === 0 || o.text_roles.length > SHAREABLE_ROLES.size)
    return bad("policy.roles");
  const roles = o.text_roles as unknown[];
  if (!roles.every((r) => typeof r === "string" && SHAREABLE_ROLES.has(r)) || new Set(roles).size !== roles.length)
    return bad("policy.roles");
  if (!isInt(o.max_chars) || o.max_chars < 1 || o.max_chars > 80) return bad("policy.max_chars");
  if (!isInt(o.issued_at) || !isInt(o.expires_at)) return bad("policy.time");
  if (o.issued_at > ctx.now + SKEW_MS) return bad("policy.not_yet_valid");
  if (o.expires_at <= ctx.now) return bad("policy.expired");
  if (o.expires_at <= o.issued_at || o.expires_at - o.issued_at > MAX_LIFETIME_MS) return bad("policy.lifetime");
  return { ok: true, policy: o as unknown as SharePolicy };
}

export interface SharedNode {
  tag: string;
  role: string | null;
  name: string | null;
  text: string | null;
  flags: number;
  depth: number;
}

export interface SharedEvidence {
  nodes: SharedNode[];
  /** Nodes whose text or name was removed; structure is always kept. */
  withheld: number;
  policy: { applied: boolean; id: string | null; refusal: string | null };
}

const SENSITIVE = FLAG_ACCOUNT | FLAG_CREDENTIAL | FLAG_REDACTED;

function permitted(r: NodeRec, inData: boolean, policy: SharePolicy | null): boolean {
  if (!policy || inData || r.role === null || (r.flags & SENSITIVE) !== 0) return false;
  return policy.text_roles.includes(r.role);
}

function release(value: string | null, max: number): string | null {
  if (value === null) return null;
  const clipped = value.length > max ? value.slice(0, max) : value;
  const safe = redactText(clipped);
  return safe !== clipped || looksAccountSpecific(safe) ? null : safe;
}

/** What may leave the device: structure always; text only for roles a validated policy names, outside record data and sensitive scopes. */
export function sharedEvidence(replica: Replica, ctx: ShareContext, rawPolicy?: unknown): SharedEvidence {
  const check: PolicyCheck =
    rawPolicy === undefined ? { ok: false, reason: "policy.absent" } : validateSharePolicy(rawPolicy, ctx);
  const policy = check.ok ? check.policy : null;
  const nodes: SharedNode[] = [];
  let withheld = 0;
  const walk = (ids: number[], depth: number, inData: boolean): void => {
    for (const id of ids) {
      const r = replica.nodes.get(id) as NodeRec;
      const ok = permitted(r, inData, policy);
      const name = ok ? release(r.name, (policy as SharePolicy).max_chars) : null;
      const text = ok ? release(r.text, (policy as SharePolicy).max_chars) : null;
      if ((r.name !== null && name === null) || (r.text !== null && text === null)) withheld++;
      nodes.push({ tag: r.tag, role: r.role, name, text, flags: r.flags, depth });
      walk(replica.children.get(id) ?? [], depth + 1, inData || (r.role !== null && DATA_ROLES.has(r.role)));
    }
  };
  walk(replica.roots, 0, false);
  return {
    nodes,
    withheld,
    policy: { applied: policy !== null, id: policy?.id ?? null, refusal: check.ok ? null : check.reason },
  };
}
