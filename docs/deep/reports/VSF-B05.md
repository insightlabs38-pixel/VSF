Batch: B05 | Gate: G5 | Decision: PROCEED
Branch: vsf/b05-semantic-layer | Base: 3ff2bf4 | Commit: 6e226bb
PR: https://github.com/insightlabs38-pixel/VSF/pull/6
Closed tasks: VSF-B05-T01 VSF-B05-T02 VSF-B05-T03 VSF-B05-T04 VSF-B05-T05 VSF-B05-T06 VSF-B05-T07 VSF-B05-T08
Contracts/primitive IDs: IR 1.0.0 unchanged; vsf-observation-snapshot-v1, vsf-recognition-v1, vsf-evidence-ledger-v1, vsf-availability-v1 new; no primitive changes
Changed modules: compiler/crates/vsf-recognizer frontend/packages/observer fixtures/recognition tests/observation ci
Tests: 31/31 evidence runs PASS; 57 recognizer tests plus 1 protocol fixture test; 9 CI jobs incl. clean-clone PASS
Negative/adversarial: duplicated labels, hidden and credential controls, overlapping roles, stale evidence, conflicting API and DOM claims all PASS
Held-out: one delegation rule takes usable unbound fields from 3 and 6 to 0 in two unrelated apps; 4 regression apps unchanged; adversarial page has 2 ambiguous and 0 false bindings
Performance: index equals full scan; 20000 nodes, narrow query 1698 candidates vs 20000, 10 B per node; one label edit recomputes 1 of 8 units
Incremental: random edit sequences equal fresh runs; unlogged reads are detected by mutation
Availability: UNKNOWN and CONFLICTED withhold; no write tool without a behavioural witness; unsupported or stale protocol evidence cannot conflict
Security/session scope: no model or network dependency; credential fields never read, matched or usable; conflicts close only by a recorded decision
Source compatibility: synthetic fixtures only; no vendor or framework names in the recognizer (checked)
Version/pin/retention: ADR-031; rules V1 and V2 coexist; snapshot fixtures are generated and drift-checked in CI
Known limitations: English verb lexicon; no required-flag or enum comparison; column labels name the column, not the row; witnesses come from a later batch
Evidence: docs/deep/evidence/VSF-B05/ (manifest.json, logs/, collect.sh)
Next eligible: VSF-B06-T01
