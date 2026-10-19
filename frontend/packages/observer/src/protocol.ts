import { redactText } from "./redact.ts";

export type EvidenceKind = "documented_api" | "published_schema" | "user_supplied_spec" | "observed_traffic";
export type Support = "unsupported" | "candidate" | "supported";
export type FieldType = "string" | "integer" | "number" | "boolean" | "object" | "array" | "null" | "unknown";

export interface FieldShape {
  name: string;
  type: FieldType;
  required: boolean;
}

export interface ProtocolEvidence {
  schema: "vsf-protocol-evidence-v1";
  id: string;
  source_app_id: string;
  origin: string;
  kind: EvidenceKind;
  stability: "documented" | "undocumented";
  support: Support;
  provenance: {
    method: "vendor_documentation" | "published_schema" | "user_assertion" | "runtime_observation";
    reference?: string;
    recorded_at: number;
    authorization: {
      basis: "public_documentation" | "source_terms" | "user_grant";
      principal_ref?: string;
      scope?: string;
      granted_at?: number;
    };
  };
  endpoint: {
    protocol: "http" | "graphql" | "websocket" | "grpc";
    method?: "GET" | "HEAD" | "POST" | "PUT" | "PATCH" | "DELETE";
    path_template: string;
    operation?: string;
  };
  shape: { request: FieldShape[]; response: FieldShape[] };
  promotion?: { decision_id: string; reviewed_by: string; documentation_reference: string; witnesses: string[] };
}

export interface Problem {
  code: string;
  path: string;
}

const METHOD_OF: Record<EvidenceKind, ProtocolEvidence["provenance"]["method"]> = {
  documented_api: "vendor_documentation",
  published_schema: "published_schema",
  user_supplied_spec: "user_assertion",
  observed_traffic: "runtime_observation",
};

const DOCUMENTED: ReadonlySet<EvidenceKind> = new Set(["documented_api", "published_schema"]);
const FIELD_TYPES = new Set(["string", "integer", "number", "boolean", "object", "array", "null", "unknown"]);
const NAME = /^[A-Za-z_][A-Za-z0-9_.-]{0,63}$/;
const SAFE_INT = (v: unknown): v is number => typeof v === "number" && Number.isSafeInteger(v) && v >= 0;

type Obj = Record<string, unknown>;
const isObj = (v: unknown): v is Obj => typeof v === "object" && v !== null && !Array.isArray(v);

class Checker {
  readonly problems: Problem[] = [];
  bad(code: string, path: string): void {
    this.problems.push({ code, path });
  }

  object(v: unknown, path: string, required: string[], allowed: string[]): Obj | null {
    if (v === undefined) return null;
    if (!isObj(v)) {
      this.bad("schema.type", path);
      return null;
    }
    for (const k of Object.keys(v)) if (!allowed.includes(k)) this.bad("schema.unknown_field", `${path}/${k}`);
    for (const k of required) if (!(k in v)) this.bad("schema.required", `${path}/${k}`);
    return v;
  }

  str(v: unknown, path: string, re?: RegExp, max = 300): string | null {
    if (v === undefined) return null;
    if (typeof v !== "string") {
      this.bad("schema.type", path);
      return null;
    }
    if (v.length === 0 || v.length > max || (re && !re.test(v))) {
      this.bad("schema.pattern", path);
      return null;
    }
    return v;
  }

  oneOf<T extends string>(v: unknown, path: string, values: readonly T[]): T | null {
    if (v === undefined) return null;
    if (typeof v === "string" && (values as readonly string[]).includes(v)) return v as T;
    this.bad("schema.enum", path);
    return null;
  }
}

function checkFields(c: Checker, v: unknown, path: string): void {
  if (!Array.isArray(v)) {
    c.bad("schema.type", path);
    return;
  }
  if (v.length > 256) c.bad("schema.too_many", path);
  for (const [i, f] of v.entries()) {
    const p = `${path}/${i}`;
    const o = c.object(f, p, ["name", "type", "required"], ["name", "type", "required"]);
    if (!o) continue;
    c.str(o.name, `${p}/name`, NAME);
    if (typeof o.type !== "string" || !FIELD_TYPES.has(o.type)) c.bad("schema.enum", `${p}/type`);
    if (typeof o.required !== "boolean") c.bad("schema.type", `${p}/required`);
  }
}

function httpsReference(c: Checker, value: string, path: string): void {
  let url: URL;
  try {
    url = new URL(value);
  } catch {
    c.bad("evidence.bad_reference", path);
    return;
  }
  if (url.protocol !== "https:") c.bad("evidence.bad_reference", path);
  if (url.username || url.password) c.bad("evidence.credentials_in_url", path);
  if (url.search) c.bad("evidence.credentials_in_url", path);
}

const CONCRETE = /^(\d+|[0-9a-f]{8,}|[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})$/i;

export function templatizePath(path: string): string {
  return path
    .split("/")
    .map((seg) =>
      seg === "" || /^\{[A-Za-z_][A-Za-z0-9_]*\}$/.test(seg)
        ? seg
        : CONCRETE.test(seg) || seg.length > 40
          ? "{id}"
          : seg,
    )
    .join("/");
}

function checkPath(c: Checker, value: string, path: string): void {
  for (const seg of value.split("/").slice(1)) {
    if (/\{|\}/.test(seg) && !/^\{[A-Za-z_][A-Za-z0-9_]*\}$/.test(seg)) c.bad("evidence.bad_template", path);
    else if (!seg.startsWith("{") && (CONCRETE.test(seg) || seg.length > 40))
      c.bad("evidence.concrete_identifier", path);
  }
}

function secretShaped(c: Checker, v: unknown, path: string): void {
  if (typeof v === "string") {
    if (redactText(v) !== v) c.bad("evidence.secret_shaped", path);
  } else if (Array.isArray(v)) {
    for (const [i, x] of v.entries()) secretShaped(c, x, `${path}/${i}`);
  } else if (isObj(v)) for (const [k, x] of Object.entries(v)) secretShaped(c, x, `${path}/${k}`);
}

/** Structural checks mirror schemas/protocol-evidence/v1.schema.json; the rest are the promotion rules. */
export function validateProtocolEvidence(doc: unknown): Problem[] {
  const c = new Checker();
  const top = c.object(
    doc,
    "",
    ["schema", "id", "source_app_id", "origin", "kind", "provenance", "endpoint", "shape", "stability", "support"],
    [
      "schema",
      "id",
      "source_app_id",
      "origin",
      "kind",
      "provenance",
      "endpoint",
      "shape",
      "stability",
      "support",
      "promotion",
    ],
  );
  if (!top) {
    if (c.problems.length === 0) c.bad("schema.type", "");
    return c.problems;
  }
  if (top.schema !== "vsf-protocol-evidence-v1") c.bad("schema.enum", "/schema");
  c.str(top.id, "/id", /^pev_[0-9a-f]{16,64}$/);
  c.str(top.source_app_id, "/source_app_id", /^[a-z0-9][a-z0-9-]{0,62}:[a-z0-9][a-z0-9.-]{0,127}$/);
  c.str(top.origin, "/origin", /^https?:\/\/[a-z0-9.-]{1,253}(:[0-9]{1,5})?$/);
  const kind = c.oneOf(top.kind, "/kind", [
    "documented_api",
    "published_schema",
    "user_supplied_spec",
    "observed_traffic",
  ] as const);
  const stability = c.oneOf(top.stability, "/stability", ["documented", "undocumented"] as const);
  const support = c.oneOf(top.support, "/support", ["unsupported", "candidate", "supported"] as const);

  const prov = c.object(
    top.provenance,
    "/provenance",
    ["method", "recorded_at", "authorization"],
    ["method", "reference", "recorded_at", "authorization"],
  );
  let method: string | null = null;
  let basis: string | null = null;
  let authz: Obj | null = null;
  if (prov) {
    method = c.oneOf(prov.method, "/provenance/method", [
      "vendor_documentation",
      "published_schema",
      "user_assertion",
      "runtime_observation",
    ] as const);
    if (prov.reference !== undefined) c.str(prov.reference, "/provenance/reference");
    if (!SAFE_INT(prov.recorded_at)) c.bad("schema.type", "/provenance/recorded_at");
    authz = c.object(
      prov.authorization,
      "/provenance/authorization",
      ["basis"],
      ["basis", "principal_ref", "scope", "granted_at"],
    );
    if (authz) {
      basis = c.oneOf(authz.basis, "/provenance/authorization/basis", [
        "public_documentation",
        "source_terms",
        "user_grant",
      ] as const);
      if (authz.principal_ref !== undefined)
        c.str(authz.principal_ref, "/provenance/authorization/principal_ref", /^prin_[0-9a-f]{32}$/);
      if (authz.scope !== undefined) c.str(authz.scope, "/provenance/authorization/scope", /^[a-z][a-z0-9_.:-]{0,95}$/);
      if (authz.granted_at !== undefined && !SAFE_INT(authz.granted_at))
        c.bad("schema.type", "/provenance/authorization/granted_at");
    }
  }
  const ep = c.object(
    top.endpoint,
    "/endpoint",
    ["protocol", "path_template"],
    ["protocol", "method", "path_template", "operation"],
  );
  if (ep) {
    c.oneOf(ep.protocol, "/endpoint/protocol", ["http", "graphql", "websocket", "grpc"] as const);
    if (ep.method !== undefined)
      c.oneOf(ep.method, "/endpoint/method", ["GET", "HEAD", "POST", "PUT", "PATCH", "DELETE"] as const);
    const path = c.str(ep.path_template, "/endpoint/path_template", /^\/[A-Za-z0-9_.{}/-]{0,198}$/);
    if (path) checkPath(c, path, "/endpoint/path_template");
    if (ep.operation !== undefined) c.str(ep.operation, "/endpoint/operation", /^[A-Za-z_][A-Za-z0-9_.]{0,63}$/);
  }
  const shape = c.object(top.shape, "/shape", ["request", "response"], ["request", "response"]);
  if (shape) {
    checkFields(c, shape.request, "/shape/request");
    checkFields(c, shape.response, "/shape/response");
  }
  const promo =
    top.promotion === undefined
      ? null
      : c.object(
          top.promotion,
          "/promotion",
          ["decision_id", "reviewed_by", "documentation_reference", "witnesses"],
          ["decision_id", "reviewed_by", "documentation_reference", "witnesses"],
        );
  if (promo) {
    c.str(promo.decision_id, "/promotion/decision_id", /^[a-z][a-z0-9_.:-]{0,95}$/);
    c.str(promo.reviewed_by, "/promotion/reviewed_by", /^prin_[0-9a-f]{32}$/);
    const ref = c.str(promo.documentation_reference, "/promotion/documentation_reference");
    if (ref) httpsReference(c, ref, "/promotion/documentation_reference");
    if (!Array.isArray(promo.witnesses) || promo.witnesses.length === 0 || promo.witnesses.length > 64) {
      c.bad("evidence.promotion_invalid", "/promotion/witnesses");
    }
  }
  if (c.problems.length > 0) return c.problems;

  if (kind && method && METHOD_OF[kind] !== method) c.bad("evidence.kind_method_mismatch", "/provenance/method");
  const documented = kind !== null && DOCUMENTED.has(kind);
  if (kind && stability && (stability === "documented") !== documented)
    c.bad("evidence.stability_mismatch", "/stability");
  const reference = typeof prov?.reference === "string" ? prov.reference : null;
  if (reference !== null) httpsReference(c, reference, "/provenance/reference");
  if (documented && reference === null) c.bad("evidence.documented_without_reference", "/provenance/reference");
  const recorded = prov?.recorded_at as number;
  if (documented) {
    if (basis === "user_grant") c.bad("evidence.authorization_basis", "/provenance/authorization/basis");
  } else {
    if (basis !== "user_grant") c.bad("evidence.missing_authorization", "/provenance/authorization/basis");
    if (!authz || authz.principal_ref === undefined || authz.scope === undefined || authz.granted_at === undefined) {
      c.bad("evidence.missing_authorization", "/provenance/authorization");
    } else if ((authz.granted_at as number) > recorded) {
      c.bad("evidence.authorization_after_record", "/provenance/authorization/granted_at");
    }
  }
  if (support === "supported") {
    if (!promo && !(documented && basis !== "user_grant")) c.bad("evidence.undocumented_supported", "/support");
  } else if (promo) {
    c.bad("evidence.promotion_without_support", "/promotion");
  }
  secretShaped(c, top, "");
  return c.problems;
}

export interface ObservedRequest {
  sourceAppId: string;
  origin: string;
  method: NonNullable<ProtocolEvidence["endpoint"]["method"]>;
  url: string;
  requestJson?: unknown;
  responseJson?: unknown;
  principalRef: string;
  scope: string;
  grantedAt: number;
  recordedAt: number;
  idHex: string;
}

function typeOf(v: unknown): FieldType {
  if (v === null) return "null";
  if (Array.isArray(v)) return "array";
  if (typeof v === "string" || typeof v === "boolean") return typeof v as FieldType;
  if (typeof v === "number") return Number.isInteger(v) ? "integer" : "number";
  return typeof v === "object" ? "object" : "unknown";
}

/** Top-level names and types only; values, and keys that look like data rather than field names, are dropped. */
export function shapeOfJson(v: unknown): FieldShape[] {
  if (!isObj(v)) return [];
  return Object.keys(v)
    .filter((k) => NAME.test(k) && redactText(k) === k)
    .sort()
    .slice(0, 256)
    .map((name) => ({ name, type: typeOf(v[name]), required: true }));
}

/** Observed traffic is evidence of what happened, not a contract: it always starts unsupported and undocumented. */
export function observedTrafficEvidence(r: ObservedRequest): ProtocolEvidence {
  const url = new URL(r.url);
  if (url.origin !== r.origin) throw new Error("request origin differs from the stated origin");
  if (url.username || url.password) throw new Error("credentials in the request URL");
  return {
    schema: "vsf-protocol-evidence-v1",
    id: `pev_${r.idHex}`,
    source_app_id: r.sourceAppId,
    origin: r.origin,
    kind: "observed_traffic",
    stability: "undocumented",
    support: "unsupported",
    provenance: {
      method: "runtime_observation",
      recorded_at: r.recordedAt,
      authorization: { basis: "user_grant", principal_ref: r.principalRef, scope: r.scope, granted_at: r.grantedAt },
    },
    endpoint: { protocol: "http", method: r.method, path_template: templatizePath(url.pathname) },
    shape: { request: shapeOfJson(r.requestJson), response: shapeOfJson(r.responseJson) },
  };
}
