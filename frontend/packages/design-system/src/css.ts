import { ACCENTS, DENSITY, type Density, PALETTE, type Scheme, STATUS_NAMES } from "./tokens.ts";

function schemeVars(s: Scheme): string {
  const p = PALETTE[s];
  const lines = [
    `--vsf-bg:${p.bg}`,
    `--vsf-surface:${p.surface}`,
    `--vsf-surface-2:${p.surface2}`,
    `--vsf-text:${p.text}`,
    `--vsf-muted:${p.muted}`,
    `--vsf-border:${p.border}`,
    `--vsf-control-border:${p.controlBorder}`,
    `--vsf-focus:${p.focus}`,
  ];
  for (const n of STATUS_NAMES) {
    lines.push(`--vsf-${n}:${p.status[n]}`, `--vsf-${n}-bg:${p.statusBg[n]}`);
  }
  return lines.join(";");
}

function accentVars(s: Scheme, a: "none" | "mint" | "blue"): string {
  const t = ACCENTS[s][a];
  return `--vsf-accent:${t.accent};--vsf-on-accent:${t.onAccent};--vsf-accent-soft:${t.soft}`;
}

function densityRule(d: Density): string {
  const t = DENSITY[d];
  return `:host([data-density="${d}"]){--vsf-target:${t.target}px;--vsf-pad-y:${t.padY}px;--vsf-pad-x:${t.padX}px;--vsf-gap:${t.gap}px;--vsf-font:${t.fontSize}px;--vsf-row:${t.rowHeight}px}`;
}

/** Tokens only: scheme, accent and density are independent attributes on the host element. */
export function tokensCss(): string {
  const out: string[] = [];
  out.push(`:host{--vsf-target:44px;--vsf-pad-y:10px;--vsf-pad-x:14px;--vsf-gap:12px;--vsf-font:15px;--vsf-row:44px}`);
  out.push(`:host{${schemeVars("light")};${accentVars("light", "blue")}}`);
  out.push(`:host([data-scheme="light"]){${schemeVars("light")}}`);
  out.push(`:host([data-scheme="dark"]){${schemeVars("dark")}}`);
  out.push(`@media (prefers-color-scheme:dark){:host([data-scheme="system"]){${schemeVars("dark")}}}`);
  for (const a of ["none", "mint", "blue"] as const) {
    out.push(`:host([data-accent="${a}"]){${accentVars("light", a)}}`);
  }
  out.push(`:host([data-scheme="dark"][data-accent="none"]){${accentVars("dark", "none")}}`);
  out.push(`:host([data-scheme="dark"][data-accent="blue"]){${accentVars("dark", "blue")}}`);
  out.push(`:host([data-scheme="dark"][data-accent="mint"]){${accentVars("dark", "mint")}}`);
  out.push(
    `@media (prefers-color-scheme:dark){:host([data-scheme="system"][data-accent="none"]){${accentVars("dark", "none")}}:host([data-scheme="system"][data-accent="blue"]){${accentVars("dark", "blue")}}:host([data-scheme="system"][data-accent="mint"]){${accentVars("dark", "mint")}}}`,
  );
  for (const d of ["compact", "comfortable", "spacious"] as const) out.push(densityRule(d));
  return out.join("\n");
}
