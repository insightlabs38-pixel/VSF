import assert from "node:assert/strict";
import { test } from "node:test";
import {
  ACCENTS,
  type Accent,
  contrast,
  DENSITY,
  deltaE,
  PALETTE,
  type Scheme,
  STATUS_GLYPH,
  STATUS_NAMES,
  tokensCss,
} from "../src/index.ts";

const SCHEMES: Scheme[] = ["light", "dark"];
const ACCENT_NAMES: Accent[] = ["none", "mint", "blue"];

test("contrast math matches known WCAG reference values", () => {
  assert.equal(Math.round(contrast("#000000", "#ffffff")), 21);
  assert.equal(contrast("#777777", "#777777"), 1);
  assert.ok(Math.abs(contrast("#767676", "#ffffff") - 4.54) < 0.02);
});

test("body text, muted text and control borders meet WCAG AA in both schemes", () => {
  for (const s of SCHEMES) {
    const p = PALETTE[s];
    for (const bg of [p.bg, p.surface, p.surface2]) {
      assert.ok(contrast(p.text, bg) >= 7, `${s} text on ${bg}`);
      assert.ok(contrast(p.muted, bg) >= 4.5, `${s} muted on ${bg}`);
    }
    assert.ok(contrast(p.controlBorder, p.bg) >= 3, `${s} control border`);
    assert.ok(contrast(p.focus, p.bg) >= 3 && contrast(p.focus, p.surface) >= 3, `${s} focus ring`);
  }
});

test("every accent keeps text readable on it and is visible as a UI colour", () => {
  for (const s of SCHEMES) {
    for (const a of ACCENT_NAMES) {
      const t = ACCENTS[s][a];
      assert.ok(contrast(t.onAccent, t.accent) >= 4.5, `${s}/${a} label on accent`);
      assert.ok(contrast(t.accent, PALETTE[s].bg) >= 3, `${s}/${a} accent on page`);
      assert.ok(contrast(PALETTE[s].text, t.soft) >= 7, `${s}/${a} text on soft`);
    }
  }
});

test("blue and mint differ clearly from each other and from the neutral accent", () => {
  for (const s of SCHEMES) {
    const a = ACCENTS[s];
    assert.ok(deltaE(a.blue.accent, a.mint.accent) >= 30, `${s} blue/mint`);
    assert.ok(deltaE(a.blue.accent, a.none.accent) >= 20, `${s} blue/none`);
    assert.ok(deltaE(a.mint.accent, a.none.accent) >= 20, `${s} mint/none`);
  }
});

test("red, orange, yellow and green status colours stay pairwise distinct and readable", () => {
  for (const s of SCHEMES) {
    const p = PALETTE[s];
    for (let i = 0; i < STATUS_NAMES.length; i++) {
      const a = STATUS_NAMES[i] as (typeof STATUS_NAMES)[number];
      assert.ok(contrast(p.status[a], p.bg) >= 4.5, `${s} ${a} on page`);
      assert.ok(contrast(p.status[a], p.statusBg[a]) >= 4.5, `${s} ${a} on its tint`);
      for (const b of STATUS_NAMES.slice(i + 1)) {
        assert.ok(deltaE(p.status[a], p.status[b]) >= 25, `${s} ${a}/${b} fg ΔE ${deltaE(p.status[a], p.status[b])}`);
        assert.ok(deltaE(p.statusBg[a], p.statusBg[b]) >= 8, `${s} ${a}/${b} tint ΔE`);
      }
    }
  }
});

test("status never relies on colour alone: each has its own glyph", () => {
  const glyphs = new Set(STATUS_NAMES.map((n) => STATUS_GLYPH[n]));
  assert.equal(glyphs.size, STATUS_NAMES.length);
});

test("status colours are not reused as accents", () => {
  for (const s of SCHEMES) {
    for (const a of ACCENT_NAMES) {
      for (const n of STATUS_NAMES) {
        assert.notEqual(ACCENTS[s][a].accent, PALETTE[s].status[n]);
        assert.ok(deltaE(ACCENTS[s][a].accent, PALETTE[s].status[n]) >= 15, `${s} ${a} vs ${n}`);
      }
    }
  }
});

test("density presets grow monotonically and the default meets the 44px target", () => {
  assert.ok(DENSITY.compact.target < DENSITY.comfortable.target);
  assert.ok(DENSITY.comfortable.target < DENSITY.spacious.target);
  assert.ok(DENSITY.compact.target >= 24);
  assert.ok(DENSITY.comfortable.target >= 44);
});

test("scheme, accent and density are separate selectors in the generated CSS", () => {
  const css = tokensCss();
  assert.match(css, /:host\(\[data-scheme="dark"\]\)/);
  assert.match(css, /:host\(\[data-accent="mint"\]\)/);
  assert.match(css, /:host\(\[data-density="compact"\]\)/);
  assert.match(css, /@media \(prefers-color-scheme:dark\)\{:host\(\[data-scheme="system"\]\)/);
  assert.equal(tokensCss(), css);
  assert.ok(!/data-scheme="[a-z]+"\]\[data-density/.test(css));
});
