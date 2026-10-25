# Minimal reproduction: inline SVG makes the shipped extension fail to transform

`plain.html` has a heading, a labelled input and a button. `svg.html` is identical except that the button also contains an inline `<svg>` icon, the usual way modern applications draw icons.

```sh
cd docs/deep/reviews/post-b12-viability/evidence/svg-repro
python3 -m http.server 18872 --bind 127.0.0.1 &
PLAYWRIGHT_BROWSERS_PATH=$HOME/pwb node repro.ts
```

Result with the audited implementation (commit 829ce9f): `plain.html` transforms (`ok: true`, one entity, one action `unknown`); `svg.html` fails with `inject_failed: no result` and a page error `TypeError: Error in invocation of dom.openOrClosedShadowRoot(HTMLElement element)`. Cause: `apps/extension/src/observe.ts` lines 26 to 28 hand every observed element, SVG elements included, to `chrome.dom.openOrClosedShadowRoot`, which accepts only HTML elements. The plain `capture` command, which does not use that capability, succeeds on the same page. B09's browser tests use fixture pages without SVG, and B11's corpus harness does not install the capability, so neither exercised this path.
