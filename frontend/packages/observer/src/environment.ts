import type { EnvironmentObservation, Observed, Viewport } from "./model.ts";

export interface FlagProbe {
  name: string;
  read(doc: Document): string | null;
}

/** A flag is read from `data-flag-<name>` on the root or body, or from a `meta[name=flag:<name>]` tag. */
export function dataFlagProbe(name: string): FlagProbe {
  const attr = `data-flag-${name}`;
  return {
    name,
    read(doc) {
      const holder = [doc.documentElement, doc.body].find((el) => el?.hasAttribute(attr));
      if (holder) return holder.getAttribute(attr);
      return doc.querySelector(`meta[name="flag:${name}"]`)?.getAttribute("content") ?? null;
    },
  };
}

const IR_LOCALE = /^[a-z]{2,3}(-[A-Za-z0-9]{2,8}){0,3}$/;

/** Same case rules as the Rust identity layer; a tag the IR Locale type would reject stays unknown. */
export function declaredLocale(raw: string | null | undefined): string | null {
  if (!raw) return null;
  const parts = raw.trim().replace(/_/g, "-").split("-");
  const lang = parts[0] ?? "";
  if (!/^[A-Za-z]{2,3}$/.test(lang) || parts.length > 6) return null;
  if (!parts.every((p) => /^[A-Za-z0-9]{1,8}$/.test(p))) return null;
  const out = [lang.toLowerCase()];
  for (const p of parts.slice(1)) {
    const alpha = /^[A-Za-z]+$/.test(p);
    if (p.length === 4 && alpha) out.push(p[0]?.toUpperCase() + p.slice(1).toLowerCase());
    else if (p.length === 2 && alpha) out.push(p.toUpperCase());
    else out.push(p.toLowerCase());
  }
  const tag = out.join("-");
  return IR_LOCALE.test(tag) ? tag : null;
}

export function viewportBucket(width: number): Viewport["bucket"] {
  if (width < 576) return "xs";
  if (width < 768) return "sm";
  if (width < 992) return "md";
  if (width < 1200) return "lg";
  return "xl";
}

const unknown = { state: "unknown" } as const;
const seen = <T>(value: T): Observed<T> => ({ state: "observed", value });

/** Only what the page itself declares counts: the browser's own language says nothing about the app's. */
export function observeEnvironment(doc: Document, probes: readonly FlagProbe[] = []): EnvironmentObservation {
  const win = doc.defaultView;
  const lang =
    declaredLocale(doc.documentElement?.getAttribute("lang")) ??
    declaredLocale(doc.querySelector('meta[http-equiv="content-language" i]')?.getAttribute("content"));
  const width = win?.innerWidth ?? 0;
  const height = win?.innerHeight ?? 0;
  const flags: Record<string, Observed<string>> = {};
  for (const p of probes) {
    const v = p.read(doc);
    flags[p.name] = v === null ? unknown : seen(v);
  }
  return {
    locale: lang === null ? unknown : seen(lang),
    viewport:
      width > 0 && height > 0
        ? seen({ width, height, dpr: win?.devicePixelRatio ?? 1, bucket: viewportBucket(width) })
        : unknown,
    flags,
  };
}

export interface CacheOverlay {
  /** Stable text to fold into a cache key's content part. */
  content: string;
  /** False when a required input is unknown: such an artifact must not be shared across variants. */
  shareable: boolean;
  unknown: string[];
}

export interface OverlayNeeds {
  locale: boolean;
  viewport: boolean;
  flags: readonly string[];
}

/** Unknown values are written as `?` so they can never collide with an observed value. */
export function cacheOverlay(env: EnvironmentObservation, needs: OverlayNeeds): CacheOverlay {
  const parts: string[] = [];
  const missing: string[] = [];
  if (needs.locale) {
    if (env.locale.state === "observed") parts.push(`loc=${env.locale.value}`);
    else {
      parts.push("loc=?");
      missing.push("locale");
    }
  }
  if (needs.viewport) {
    if (env.viewport.state === "observed") parts.push(`vp=${env.viewport.value.bucket}`);
    else {
      parts.push("vp=?");
      missing.push("viewport");
    }
  }
  for (const name of [...needs.flags].sort()) {
    const f = env.flags[name];
    if (f?.state === "observed") parts.push(`f.${name}=${encodeURIComponent(f.value)}`);
    else {
      parts.push(`f.${name}=?`);
      missing.push(`flag:${name}`);
    }
  }
  return { content: `env1;${parts.join(";")}`, shareable: missing.length === 0, unknown: missing };
}

export interface VariantInputs {
  /** Null when the page declares no locale: a variant id must not be minted from a guess. */
  locale: string | null;
  flags: [string, string][];
  unknownFlags: string[];
}

/** The observable part of an identity VariantDescriptor; unknown flags are listed, never defaulted. */
export function variantInputs(env: EnvironmentObservation): VariantInputs {
  const flags: [string, string][] = [];
  const unknownFlags: string[] = [];
  for (const name of Object.keys(env.flags).sort()) {
    const f = env.flags[name];
    if (f?.state === "observed") flags.push([name, f.value]);
    else unknownFlags.push(name);
  }
  return { locale: env.locale.state === "observed" ? env.locale.value : null, flags, unknownFlags };
}
