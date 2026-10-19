export type Scheme = "light" | "dark";
export type Accent = "none" | "mint" | "blue";
export type Density = "compact" | "comfortable" | "spacious";
export type StatusName = "danger" | "warning" | "caution" | "success";

export const STATUS_NAMES: readonly StatusName[] = ["danger", "warning", "caution", "success"];

/** Glyphs that carry status without relying on colour. */
export const STATUS_GLYPH: Record<StatusName, string> = {
  danger: "✕",
  warning: "▲",
  caution: "!",
  success: "✓",
};

export interface Palette {
  bg: string;
  surface: string;
  surface2: string;
  text: string;
  muted: string;
  border: string;
  controlBorder: string;
  focus: string;
  status: Record<StatusName, string>;
  statusBg: Record<StatusName, string>;
}

export interface AccentTones {
  accent: string;
  onAccent: string;
  soft: string;
}

export const PALETTE: Record<Scheme, Palette> = {
  light: {
    bg: "#ffffff",
    surface: "#f6f8fa",
    surface2: "#eaeef2",
    text: "#1f2328",
    muted: "#57606a",
    border: "#d0d7de",
    controlBorder: "#6e7781",
    focus: "#0550ae",
    status: { danger: "#b3151f", warning: "#9a4a00", caution: "#6b5200", success: "#116329" },
    statusBg: { danger: "#ffd8d6", warning: "#ffe0c2", caution: "#fff3a3", success: "#c9f2d4" },
  },
  dark: {
    bg: "#0d1117",
    surface: "#161b22",
    surface2: "#21262d",
    text: "#e6edf3",
    muted: "#9198a1",
    border: "#30363d",
    controlBorder: "#8b949e",
    focus: "#79c0ff",
    status: { danger: "#ff7b72", warning: "#f0883e", caution: "#f2e04a", success: "#56d364" },
    statusBg: { danger: "#3d1214", warning: "#3a2008", caution: "#3a2f06", success: "#0f2d17" },
  },
};

export const ACCENTS: Record<Scheme, Record<Accent, AccentTones>> = {
  light: {
    none: { accent: "#424a53", onAccent: "#ffffff", soft: "#eaeef2" },
    blue: { accent: "#0b5cd5", onAccent: "#ffffff", soft: "#ddf4ff" },
    mint: { accent: "#0a7d66", onAccent: "#ffffff", soft: "#d8f5ec" },
  },
  dark: {
    none: { accent: "#b1bac4", onAccent: "#0d1117", soft: "#21262d" },
    blue: { accent: "#58a6ff", onAccent: "#0d1117", soft: "#12263f" },
    mint: { accent: "#2dd4a7", onAccent: "#04130f", soft: "#0b2a22" },
  },
};

export interface DensityTokens {
  /** Minimum interactive target edge in CSS pixels. */
  target: number;
  padY: number;
  padX: number;
  gap: number;
  fontSize: number;
  rowHeight: number;
}

export const DENSITY: Record<Density, DensityTokens> = {
  compact: { target: 32, padY: 4, padX: 8, gap: 6, fontSize: 13, rowHeight: 32 },
  comfortable: { target: 44, padY: 10, padX: 14, gap: 12, fontSize: 15, rowHeight: 44 },
  spacious: { target: 52, padY: 14, padX: 18, gap: 18, fontSize: 16, rowHeight: 56 },
};
