import { digestAt } from "@vsf/ir";

export interface CapabilityEntry {
  id: string;
  state: "available" | "degraded" | "unavailable" | "unknown";
  constraints?: string[];
  reason?: string;
}

export interface ManifestInput {
  runtimeId: string;
  version: string;
  compilerVersion: string;
  generatedAt: number;
  principalRef: string;
  origin: string;
  appId: string;
  variantId: string;
  observation: CapabilityEntry[];
}

export const MANIFEST_VERSION = [1, 2, 0] as const;

/** What this runtime can do for one origin, under the permissions the user gave it for that origin and no others. */
export function environmentDocument(i: ManifestInput): Record<string, unknown> {
  const digest = digestAt("environment", MANIFEST_VERSION);
  if (digest === undefined) throw new Error("environment schema is unknown");
  return {
    schema: `vsf.environment/${MANIFEST_VERSION.join(".")}`,
    schema_digest: digest,
    body: {
      runtime_id: i.runtimeId,
      mode: "extension",
      runtime_version: i.version,
      compiler_version: i.compilerVersion,
      generated_at: i.generatedAt,
      principal_ref: i.principalRef,
      permissions: [{ origin: i.origin, scopes: ["page.observe", "page.present"] }],
      observation: i.observation,
      execution: [
        { id: "present.overlay", state: "available", constraints: ["restorable at any time"] },
        { id: "source.operations", state: "unavailable", reason: "no operation has a behavioural witness yet" },
        { id: "source.input.synthetic", state: "unavailable", reason: "the extension does not drive the source page" },
      ],
      persistence: "local",
      credential_isolation: "browser_profile_bound",
      egress: "allowlist",
      limits: { max_payload_bytes: 16 * 1024 * 1024, max_nodes: 100_000, max_concurrency: 1, max_wallclock_ms: 30_000 },
      app_id: i.appId,
      variant_id: i.variantId,
    },
  };
}

/** Identifiers derived from a per-installation secret, so they name an installation without identifying a person. */
export async function localIdentity(
  extensionId: string,
  salt: string,
  sha256Hex: (text: string) => Promise<string>,
): Promise<{ principalRef: string; runtimeId: string }> {
  const principal = await sha256Hex(`vsf.principal.local\0${salt}`);
  const runtime = await sha256Hex(`vsf.runtime.extension\0${extensionId}\0${salt}`);
  return { principalRef: `prin_${principal.slice(0, 32)}`, runtimeId: `rt_ext${runtime.slice(0, 16)}` };
}
