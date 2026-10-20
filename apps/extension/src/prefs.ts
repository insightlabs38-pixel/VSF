export interface Prefs {
  scheme?: "light" | "dark" | "system";
  accent?: "none" | "mint" | "blue";
  density?: "compact" | "comfortable" | "spacious";
  nav?: "top" | "side" | "hidden";
  layout?: { order?: string[]; collapsed?: string[]; sizes?: Record<string, number> };
}

const ENUMS: Record<string, readonly string[]> = {
  scheme: ["light", "dark", "system"],
  accent: ["none", "mint", "blue"],
  density: ["compact", "comfortable", "spacious"],
  nav: ["top", "side", "hidden"],
};
const ID = /^[a-z][a-z0-9_-]{0,63}$/;
const MAX_IDS = 64;

function idList(v: unknown): string[] | null {
  if (!Array.isArray(v) || v.length > MAX_IDS) return null;
  return v.every((x) => typeof x === "string" && ID.test(x)) ? (v as string[]) : null;
}

/** Display preferences only: closed vocabulary, bounded, and nothing that names data. */
export function parsePrefs(raw: unknown): Prefs | null {
  if (typeof raw !== "object" || raw === null || Array.isArray(raw)) return null;
  const out: Prefs = {};
  for (const [k, v] of Object.entries(raw)) {
    if (k in ENUMS) {
      if (typeof v !== "string" || !ENUMS[k]?.includes(v)) return null;
      (out as Record<string, string>)[k] = v;
    } else if (k === "layout") {
      if (typeof v !== "object" || v === null || Array.isArray(v)) return null;
      const layout: NonNullable<Prefs["layout"]> = {};
      for (const [lk, lv] of Object.entries(v)) {
        if (lk === "order" || lk === "collapsed") {
          const list = idList(lv);
          if (list === null) return null;
          layout[lk] = list;
        } else if (lk === "sizes") {
          if (typeof lv !== "object" || lv === null || Array.isArray(lv)) return null;
          const entries = Object.entries(lv);
          if (entries.length > MAX_IDS) return null;
          const sizes: Record<string, number> = {};
          for (const [id, n] of entries) {
            if (!ID.test(id) || typeof n !== "number" || !Number.isFinite(n) || n < 0 || n > 1000) return null;
            sizes[id] = n;
          }
          layout.sizes = sizes;
        } else return null;
      }
      out.layout = layout;
    } else return null;
  }
  return out;
}

export const SETTINGS_VERSION = 1;

export interface SettingsStore {
  /** `null` when nothing is stored; `"unreadable"` when a record exists that cannot be trusted. */
  load(origin: string): Promise<Prefs | null | "unreadable">;
  save(origin: string, prefs: Prefs): Promise<void>;
}
