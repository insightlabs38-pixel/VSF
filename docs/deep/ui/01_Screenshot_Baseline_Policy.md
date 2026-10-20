# Screenshot baseline policy (B09, task `VSF-B09-T09`)

## 1. What is compared
`apps/gallery` renders the reference scenes as static files (`pnpm --filter @vsf/gallery run build`, output `artifacts/gallery/`). `tests/ui/specs/gallery.spec.ts` loads each scene in Chromium and compares a full-page screenshot with a committed baseline in `tests/ui/golden/gallery/`.
| Axis | Values |
|---|---|
| Scene | `enterprise-grid`, `grouped-form`, `workbench-split`, `master-detail`, `states`, shell sections `interface`, `versions`, `permissions`, `environments`, `storage`, `activity` |
| Scheme | light, dark |
| Viewport | desktop 1280x800, tablet 768x1024, narrow 390x844 |
| Density | compact and comfortable (the five non-interface shell sections use comfortable only) |
That is 102 baselines. Scenes are reached by `#/scene[/section]?scheme=&density=`; the page sets `#app[data-ready]` after fonts load and two animation frames.

## 2. Pinned environment
- Chromium from the Playwright version pinned in `pnpm-lock.yaml`, Linux, device scale factor 1, reduced motion, animations disabled, caret hidden. Launch flags `--font-render-hinting=none --disable-font-subpixel-positioning --disable-lcd-text --force-color-profile=srgb` remove the host's font and colour settings from the result.
- Fonts are embedded (`apps/gallery/fonts/`, licenses in `LICENSE.md`); the renderer takes its family from `--vsf-font-family`, which the gallery sets. Each test fails if Chromium reports any painted text in a font that is not an embedded one (`systemFontsUsed`), so a missing glyph cannot fall back to a machine-specific font silently.
- Subset ranges: Inter regular and semibold U+0020-007E, U+00A0-00FF, U+2013, U+2014, U+2018, U+2019, U+201C, U+201D, U+2022, U+2026, U+2190-2193, U+2212, U+2039, U+203A (features `kern,liga,calt,tnum,case`); DejaVu Sans symbols U+2713, U+2715, U+25B2, U+25BC, U+25CF, U+25C6, U+2190-2193, U+2022; DejaVu Sans Mono U+0020-007E, U+2026. A glyph outside these ranges needs a new subset, not a fallback.
- Baselines are platform neutral on purpose (`snapshotPathTemplate`): they are valid only on the pinned Linux Chromium, which is what CI runs.

## 3. Thresholds
`maxDiffPixelRatio` 0.0005 of the page and per-pixel colour `threshold` 0.1, set in `tests/ui/playwright.config.ts`. A change that moves text, resizes a box or recolours a surface exceeds both; anti-aliasing noise does not. Baselines are re-rendered twice before commit to show they are stable.

## 4. Structural checks on every scene
Independent of pixels, `tests/ui/support/visual.ts` inspects the live page across open shadow roots and fails on: horizontal overflow outside a scroll container, text clipped by `overflow` or an ellipsis, overlapping controls (measured after clipping by their scroll containers), text below WCAG contrast (4.5, or 3 for large text; translucent layers are composited), controls without an accessible name, and layout shift after the scene is ready (limit 0.001). `tests/ui/specs/visual-defects.spec.ts` builds a page with each defect and requires the check to find it, so the checks cannot pass vacuously.

## 5. Updating a baseline
1. Change the code. Run `pnpm --filter @vsf/ui-tests exec playwright test gallery.spec`; failures write `actual`, `expected` and `diff` images under `artifacts/logs/ui-output/`, which CI uploads with the job logs.
2. Look at every changed image. A diff caused by an intended change is accepted; anything else is a regression to fix.
3. Update with `pnpm --filter @vsf/ui-tests exec playwright test gallery.spec --update-snapshots=all`, run the suite again without the flag, and commit the changed baselines in the same commit as the code that changed them.
4. State in the commit body which scenes changed and why. Never update baselines to make a failing structural check pass.

## 6. Reviewed set
`docs/deep/evidence/VSF-B09/gallery/` holds reviewed copies of representative baselines and `manifest.json` with their SHA-256 digests; the complete set is `tests/ui/golden/gallery/`.
