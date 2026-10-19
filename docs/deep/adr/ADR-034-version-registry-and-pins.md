# ADR-034: Version registry, semantic diff, pins and promotion

Status: accepted (B08, gate G8). Code: `compiler/crates/vsf-versioning` (Rust authority), `backend/versions` (Go local store), `schemas/versioning`. Tasks `VSF-B08-T01`..`T09`.

## Decisions
1. **Identity and lineage (T01).** A manifest id is the typed digest of its document under its own schema version, independent of whitespace. The graph rejects forged ids, missing parents (unless named as pruned history), duplicates, self parents, cycles and more than 64 parents. Branches are multiple heads, merges are manifests with several ordered parents, merge bases are the lowest common ancestors ordered by generation then id, and topological order breaks ties by id so replicas agree in any insertion order.
2. **Diff (T02).** A deterministic structural diff of semantic, operation and presentation documents. Keyed lists match by key; each path is classified by a per-family rule table into cosmetic, additive, breaking, permission or unknown (ordered, the maximum is the diff class). Anything without a rule is unknown, never cosmetic. Any effect, authorization, freshness, verification, strategy, lifecycle, witness or action-binding change is a permission change regardless of direction.
3. **Impact (T03).** Reverse-dependency closure over semantic and presentation subjects selects the tests that cover changed subjects or what they directly depend on, plus tests marked always. Unknown class, unmapped path, a subject outside the graph or a changed subject no test covers falls back to the full suite.
4. **Pins and compatibility (T04, T07).** `resolve` evaluates the twelve version axes and the origin, principal and revocation checks and reports `compatible`, `degraded` or `blocked` with ordered findings. Degraded allows reads only; unobserved origin, principal or source are never compatible. A pin freezes the exposed operation versions; source drift changes state, never the exposed schema. A candidate with another contract is blocked by `pin_mismatch` while the pinned manifest stays usable.
5. **Promotion and storage (T05).** A promotion is verified by Rust (`vsf-versioning::api`, worker operation `P`): decodable, semantically valid, signature and trust, sequence, window. Go stores artifacts by content, writes a write-ahead intent, moves the channel head by atomic rename, and on open rolls forward only if the manifest and every artifact are intact, otherwise abandons the intent. A head only ever names a complete manifest. Stable and pinned heads advance only from the current head (`previous`); preview manifests are stored but never headed.
6. **Revocation (T06).** Revocations of manifests, artifacts and `operation@version` outrank pins, only grow, persist, and block promotion, head execution and the execution runtime (`contract_revoked`, checked at admission and again before the effect) on every runtime variant.
7. **Machine-readable diff (T08).** `vsf-diff-report-v1` carries class, per-change class and review flag, optional test selection, and a published JSON schema; the CLI is `vsf-versioning diff`.
8. **Freshness (T09).** IR 1.2.0 adds optional `PromotionRecord.sequence` and `not_after`; they are inside the signed digest. Package manifests carry the same two fields. A verifier given the last accepted sequence and digest refuses rollback, a different body at the same sequence, and expiry. The signing-key set is always an input from outside the thing verified.

## Limits
- One process owns a store (file lock); no distributed promotion yet.
- Sequences are per channel and signing key; the high-water mark lives in the channel pointer.
- `not_after` bounds accepting a promotion, not running an already stored version, so pins do not lapse.
- Only a development signing key exists; production key custody is not designed here.
- The diff rule tables cover the three renderable families; observation and environment diffs are unsupported and refused.
- Dependency edges between operations and semantic actions are not modelled (the IR has no such link), so operation changes select tests by operation id only.

## Migration and rollback
IR 1.2.0 is additive over 1.1.0 and 1.0.0; released documents decode unchanged. Cargo workspace gains `vsf-versioning`; the native worker gains operation `P`; `execution.Config` gains an optional `Revocations`. Rollback: revert the B08 merge; nothing stored depends on it.
