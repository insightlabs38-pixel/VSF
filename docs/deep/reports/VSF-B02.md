Batch: B02 | Gate: G2 | Decision: PROCEED
Branch: vsf/b02-versioned-ir-contracts | Base: a8f278d | Commit: 849801f
PR: https://github.com/insightlabs38-pixel/VSF/pull/3
Closed tasks: VSF-B02-T01 VSF-B02-T02 VSF-B02-T03 VSF-B02-T04 VSF-B02-T05 VSF-B02-T06 VSF-B02-T07 VSF-B02-T08
Contracts/primitive IDs: IR 1.0.0 (six families, vsf-idl/1) new; wasm ABI 2 -> 3 (adds vsf_ir_validate); worker op V; no primitive changes
Changed modules: schemas/ir tools/idlgen compiler/crates/vsf-ir frontend/packages/ir backend/ir wasm-host vsf-wasm vsf-native ci
Tests: 23/23 evidence runs PASS; rust vsf-ir 27, native 9, wasm-host 10, ir ts 6, idlgen 55, go ir; GitHub CI 10 checks green on 849801f
Negative/adversarial: 79 decode failures, 104 semantic mutations, 49 version-matrix cases, 21 rejected schema changes (15 breaking, 6 unversioned additive), schema downgrade, unknown variants, NFC/NFD, decimal drift, conflicting effects all PASS
Performance: wasm size 1,005,067 -> 1,402,563 bytes (+397 KB, untrimmed); validators linear in document size; RFC 4231 HMAC cases pass
Security/session scope: effects default to indeterminate; verified needs behavioral evidence; active operations need an applicable passing witness; variant ids are keyed; private cache binds principal and variant
Source compatibility: contracts only; no real source app contacted; Go reaches semantics through the native worker
Version/pin/retention: ADR-027 (IR, semantics, identity), ADR-028 (compat and promotion); released 1.0.0 snapshot frozen; CODEOWNERS covers schemas, idlgen, ADRs
Known limitations: wasm size trimming B18; host key management and real host support descriptions B14; witness execution later; code owner review needs branch protection
Evidence: docs/deep/evidence/VSF-B02/ (manifest.json, logs/, collect.sh)
Next eligible: VSF-B03-T01
