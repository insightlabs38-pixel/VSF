import { type Body, decodeDocument, documentDigestAt, type Unvalidated } from "@vsf/ir";
import type { Fabric } from "./index.ts";

const MINT = Symbol("vsf.validated");
const decoder = new TextDecoder();

export interface ReportedViolation {
  code: string;
  path: string;
}

export class ValidationRefused extends Error {
  readonly code: string;
  readonly violations: ReportedViolation[];

  constructor(code: string, violations: ReportedViolation[] = []) {
    super(code);
    this.name = "ValidationRefused";
    this.code = code;
    this.violations = violations;
  }
}

/** A document the Rust validator accepted; `validate` is the only way to get one, and execution takes nothing else. */
export class Validated {
  readonly #body: Body;
  readonly digest: string;
  readonly version: Unvalidated["version"];

  constructor(token: symbol, body: Body, digest: string, version: Unvalidated["version"]) {
    if (token !== MINT) throw new TypeError("a Validated value can only come from validate()");
    this.#body = body;
    this.digest = digest;
    this.version = version;
  }

  body(): Body {
    return this.#body;
  }

  /** A brand check on the private field: neither a lookalike nor a forged prototype passes. */
  static is(value: unknown): value is Validated {
    return typeof value === "object" && value !== null && #body in value;
  }
}

interface Report {
  decode?: { ok?: boolean; digest?: string; code?: string };
  valid?: boolean;
  violations?: ReportedViolation[];
}

/** Has the Rust validator (through the WASM module) decide, and mints authority only if it accepts and agrees on the digest. */
export async function validate(fabric: Fabric, bytes: Uint8Array): Promise<Validated> {
  let report: Report;
  try {
    report = JSON.parse(decoder.decode(fabric.validateIr(bytes))) as Report;
  } catch {
    throw new ValidationRefused("validate.unavailable");
  }
  if (report.decode?.ok !== true) throw new ValidationRefused(report.decode?.code ?? "validate.undecodable");
  if (report.valid !== true) throw new ValidationRefused("validate.invalid", report.violations ?? []);
  const doc = decodeDocument(bytes);
  const body = doc.unsafe();
  const digest = await documentDigestAt(body, doc.version);
  if (digest === undefined || digest !== report.decode.digest)
    throw new ValidationRefused("validate.digest_disagreement");
  return new Validated(MINT, body, digest, doc.version);
}

/** What an executor calls first: anything that did not come from `validate` is refused. */
export function requireValidated(value: unknown): Validated {
  if (!Validated.is(value)) throw new ValidationRefused("validate.not_validated");
  return value;
}
