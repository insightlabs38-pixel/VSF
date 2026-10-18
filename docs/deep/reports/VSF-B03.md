Batch: B03 | Gate: G3 | Decision: PROCEED
Branch: vsf/b03-primitive-registry | Base: 94322d3 | Commit: 69eac29
PR: https://github.com/insightlabs38-pixel/VSF/pull/4
Closed tasks: VSF-B03-T01 VSF-B03-T02 VSF-B03-T03 VSF-B03-T04 VSF-B03-T05 VSF-B03-T06 VSF-B03-T07 VSF-B03-T08
Contracts/primitive IDs: IR 1.0.0 unchanged; vsf-primitive-registry-v1, fixture-v1, status-v1, package-v1 new; TABLE-001 and TABLE-002 implemented
Changed modules: compiler/crates/vsf-registry registry/primitives fixtures/primitives ci docs/deep/adr
Tests: 21/21 evidence runs PASS; vsf-registry 40 tests (registry 16, harness 10, dashboard 6, manifest 8); workspace, 8 CI jobs and clean-clone PASS
Negative/adversarial: duplicate ids, cycles, ambiguous recognitions, CSS-grid lookalike, held-out seeds, altered witnesses, every covered file byte flip, forged or revoked keys, lying signed manifests, substituted trust all rejected
Performance: 924 entries, 2052 edges; full debug verification 0.19 s; one new dependency ed25519-compact
Security/session scope: unsupported claims fail package load; manifest checks signature, files, then recomputed digests; no source app contacted
Source compatibility: synthetic fixtures only; no real source application observed
Version/pin/retention: ADR-029; package 0.1.0; digests fold in dependency digests
Known limitations: 2 of 924 implemented, 0 validated or proven; 49 UNRESOLVED; dev signing key only, release keys at B14/B20
Evidence: docs/deep/evidence/VSF-B03/
Next eligible: VSF-B04-T01
