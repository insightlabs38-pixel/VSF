Batch: B06 | Gate: G6 | Decision: PROCEED
Branch: vsf/b06-presentation-renderer | Base: c2736fd | Commit: 5f03e38
PR: https://github.com/insightlabs38-pixel/VSF/pull/7
Closed tasks: VSF-B06-T01 VSF-B06-T02 VSF-B06-T03 VSF-B06-T04 VSF-B06-T05 VSF-B06-T06 VSF-B06-T07 VSF-B06-T08 VSF-B06-T09 VSF-B06-T10 VSF-B06-T11 VSF-B06-T12 VSF-B06-T13 VSF-B06-T14 VSF-B06-T15 VSF-B06-T16 VSF-B06-T17 VSF-B06-T18
Contracts/primitive IDs: IR 1.1.0 additive over 1.0.0 (trust context, revocation, rollback, witness); evidence ledger v2; no primitive changes
Changed modules: frontend/packages/renderer observer ir; compiler/crates/vsf-recognizer vsf-ir vsf-redact vsf-registry; backend/ir; tools/queue checks idlgen; tests/ui; ci
Tests: 45/45 evidence runs PASS; 106 UI browser tests incl. grid filter (290 row writes + 1 announcement), 14 visual-contract cases; full Rust, Go, TS and cross-language suites
Negative/adversarial: credential markers, forged protocol and witnesses, tampered ledger, skipped CI jobs, scanner outage, source CSS collisions and script hooks, untrusted clicks all PASS
Performance: 1,000 rows render 90 ms, filter 4 ms, cell update 1 write, 84 KB heap growth over 30 churn cycles, 0 DOM or subscription growth; 300 ms source delay reported apart
Security/session scope: closed shadow root; semantic-digest binding; credential floor and structure-only sharing; hints never authorize; unavailable scans never clean
Source compatibility: synthetic fixtures only; unsupported widgets kept in permitted context, no cross-origin embedding
Version/pin/retention: ADR-032; IR 1.0.0 documents still decode; snapshots 1.0.0 and 1.1.0 frozen; G5 PROCEED stays historically accurate
Known limitations: visual regression is a style and ARIA contract not pixels; source rules hiding html or body are not preventable; English-only lexicon
Deferred: VSF-B07-T09 Validated<T> execution, VSF-B08-T09 manifest sequence and expiry
Evidence: docs/deep/evidence/VSF-B06/ (manifest.json, logs/, bench/ui.json, collect.sh)
Next eligible: VSF-B07-T09
