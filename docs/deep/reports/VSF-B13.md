Batch: B13 | Gate: G13 | Decision: PROCEED
Branch: vsf/b13-durable-storage | Base: 048661a | Commit: db356bb (evidence regenerated on the final PR head)
PR: https://github.com/insightlabs38-pixel/VSF/pull/12 (stacked on the B11 blocker commits that carry the G10 merge record)
Closed tasks: VSF-B13-T01 VSF-B13-T02 VSF-B13-T03 VSF-B13-T04 VSF-B13-T05
Contracts/primitive IDs: metadata schema 2, manifest vsf.manifest v1, bundle v1; sealed private artifacts use keyed identifiers; no IR or primitive change
Changed modules: backend/storage/{meta,blob,artifact,bench,gc,bundle,secret,tcache} (new), cmd/vsfstoragebench, go.mod (modernc.org/sqlite), supply-chain ignore GO-2026-5024, ADR-038
Tests: 42/42 evidence runs PASS locally; 71 Go test functions in the storage packages plus backend conformance subtests; full Go suites, cargo, clippy and all 10 CI jobs pass
Negative/adversarial: GC of pinned, held, leased and ancestor versions; collector killed at every phase; GC racing publishes; torn writes and kill -9 (0 lost of 18010 acknowledged); tenant probing and transplanted objects; damaged or re-sealed bundles
Performance: tiny read p50 fs 0.03 ms, sqlite 0.23, s3 1.9; device writes per logical byte fs 4.3x, sqlite 33.3x, s3 8.7x; similar versions 1.00x whole-object; sealing adds 17 bytes per private artifact
Security/session scope: private artifacts keyed and encrypted per tenant; disk scans find no plaintext, plaintext hash or key bytes; key file 0600; imports verify digests and signatures before writing
Source compatibility: not applicable to storage; no third-party application involved
Version/pin/retention: pins, holds, leases, kept root history and ancestors protect versions; retention window is a collector policy, not yet bound to plans
Known limitations: ZEROS3 not measured (no endpoint); publish gate is in process; bundle roots, pins and holds are unsigned; execution's contract cache is process-wide; no key rewrap or cross-install key move
Follow-ups queued: VSF-B14-T07, VSF-B15-T06, VSF-B18-T06, VSF-B18-T07
Evidence: docs/deep/evidence/VSF-B13/ (manifest.json, logs/, storage-benchmark.{json,txt,md}, collect.sh)
Next eligible: VSF-B14-T01 after G13 merges (B11 stays BLOCKED on an independent corpus)
