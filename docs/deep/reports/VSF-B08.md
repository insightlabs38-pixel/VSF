Batch: B08 | Gate: G8 | Decision: PROCEED
Branch: vsf/b08-versioning-ui-checkpoint | Base: 1430ef3 | Commit: 2eb19f6
PR: https://github.com/insightlabs38-pixel/VSF/pull/9
Closed tasks: VSF-B08-T01 VSF-B08-T02 VSF-B08-T03 VSF-B08-T04 VSF-B08-T05 VSF-B08-T06 VSF-B08-T07 VSF-B08-T08 VSF-B08-T09 VSF-B08-T10
Contracts/primitive IDs: IR 1.2.0 additive (PromotionRecord sequence/not_after, Region layout, Customization); worker op P; schema vsf-diff-report-v1; no primitive changes
Changed modules: compiler/crates/vsf-versioning (new), vsf-registry, vsf-ir, vsf-native; backend/versions (new), backend/execution; frontend/packages/renderer; schemas/ir; fixtures/versions, fixtures/ui; tests/ui
Tests: 49/49 evidence runs PASS locally; remote CI 13/13 on 2eb19f6 (evidence/remote-ci.md); Rust, Go race and integration, TS, 19 Chromium layout specs, 106+ renderer tests unchanged
Negative/adversarial: replayed, expired and re-signed promotions, forged lineage, crash at every store step, tampered artifacts, revoked pinned versions, invalid layouts, hideable risky actions; CI found a Chromium re-send of a dropped POST (ADR-033 limits), tests now count submissions
Performance: not measured in B08
Security/session scope: Rust alone verifies trust; freshness is inside the signed digest; revocation outranks pins and is rechecked before the effect
Source compatibility: fixtures only (simulated); real Chromium for layouts; no third-party application validated
Version/pin/retention: ADR-034 (registry, pins, revocation), ADR-035 (UI layout); IR 1.0.0 and 1.1.0 documents unaffected; released/1.2.0 snapshot added
Known limitations: single-process store; revocations only grow; no pixel regression until B09; no drag-resize; management shell specified, built in B09
Deferred: VSF-B09-T09 integrated gallery with screenshot regression
Evidence: docs/deep/evidence/VSF-B08/ (manifest.json, logs/, remote-ci.md, collect.sh)
Next eligible: VSF-B09-T01
