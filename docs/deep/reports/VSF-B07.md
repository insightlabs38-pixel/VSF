Batch: B07 | Gate: G7 | Decision: PROCEED
Branch: vsf/b07-verified-execution | Base: 29d2a92 | Commit: 42193ab
PR: https://github.com/insightlabs38-pixel/VSF/pull/8
Closed tasks: VSF-B07-T01 VSF-B07-T02 VSF-B07-T03 VSF-B07-T04 VSF-B07-T05 VSF-B07-T06 VSF-B07-T07 VSF-B07-T08 VSF-B07-T09 VSF-B07-T10 VSF-B07-T11 VSF-B07-T12 VSF-B07-T13 VSF-B07-T14
Contracts/primitive IDs: IR 1.1.0 unchanged; native worker op X; journal gains optional create metadata; no primitive changes
Changed modules: backend/execution (+internal/fixtureapp); backend/ir; frontend/packages/ir wasm-host; compiler/crates/vsf-ir vsf-native; fixtures/source-apps; tests/e2e; ci
Tests: 47/47 evidence runs PASS locally; remote CI for the evidence head 9a10060 in evidence/remote-ci.md; Go race tests, live Chromium and Rust worker integration, WASM browser job
Negative/adversarial: two request ids behind an ambiguous write (guard removal fails them), scope overlaps, cancellation, journal failure, restart, equal values from another actor, stale reads, overclaiming executors, lapsed sessions, torn journal, covered controls
Performance: not measured in B07
Security/session scope: Rust alone authorizes; authorization repeats just before the effect; reconciliation re-authorizes and closes only on receipts
Source compatibility: fixture API and Chromium only (simulated source, real Chromium); no third-party application validated
Version/pin/retention: ADR-033 corrections C1-C6; IR 1.0.0 and 1.1.0 documents unaffected
Known limitations: exclusion is single-process; sources without receipts leave ambiguous writes unresolved with no operator override yet; field values still set by script events
Deferred: VSF-B08-T09 manifest sequence and expiry
Evidence: docs/deep/evidence/VSF-B07/ (manifest.json, logs/, remote-ci.md, collect.sh)
Next eligible: VSF-B08-T09
