Batch: B09 | Gate: G9 | Decision: PROCEED
Branch: vsf/b09-extension-bridge | Base: a86a667 | Commit: 0dd3cb6 (evidence regenerated on the final PR head)
PR: https://github.com/insightlabs38-pixel/VSF/pull/10
Closed tasks: VSF-B09-T01 VSF-B09-T02 VSF-B09-T03 VSF-B09-T04 VSF-B09-T05 VSF-B09-T06 VSF-B09-T07 VSF-B09-T08 VSF-B09-T09
Contracts/primitive IDs: WASM ABI 4 (vsf_assemble), protocol vsf-ext-v1, vsf-assembly-v1, bridge pairing and session endpoints v1; no IR or primitive change
Changed modules: apps/extension, apps/gallery (new); compiler/crates/vsf-assemble (new), vsf-wasm, vsf-native; backend/bridge, cmd/vsfbridge; frontend/packages/observer, wasm-host, renderer; tools/checks; tests/browser, ui, cross-language
Tests: 48/48 evidence runs PASS locally; remote CI 13/13 on 0dd3cb6 (evidence/remote-ci.md); cargo 285 pass, extension units 45, cross-language 15, UI Chromium 239, bridge hostile-page specs 4
Negative/adversarial: hostile page (fetch, form, element, socket), DNS rebinding with spoofed Host, content script in a hostile page, stolen token, replayed and foreign envelopes, worker death, crash and downgrade fallback, impostor settings, planted secrets
Performance: idle worker terminates (eager storage calls had kept it alive 30-50% of the time; reconcile is now lazy); other numbers not measured
Security/session scope: pairing code never on the wire, tokens bound to the extension origin, refresh reuse revokes the client, bindings tie client, origin, document, principal and manifest digest
Source compatibility: fixture applications only; no third-party application validated
Version/pin/retention: ADR-036; IR 1.2.0 documents unaffected; 102 screenshot baselines under tests/ui/golden/gallery
Known limitations: write operations stay unavailable (no witnesses, no companion executor for the user's tab); a local process that can read the code can pair; baselines hold only for the pinned Chromium and font flags
CI findings fixed: ui baselines differed on the runner (font flags pinned); an attribution footer was appended to the PR text (removed); a heartbeat race in one test (fixed)
Deferred: native-worker coverage for assemble op S; real-gesture permission prompt checked by hand
Evidence: docs/deep/evidence/VSF-B09/ (manifest.json, logs/, gallery/, remote-ci.md, collect.sh)
Next eligible: VSF-B10-T01
