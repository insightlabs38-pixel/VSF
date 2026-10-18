# ADR-027: Versioned IR contracts, semantics and identity

Status: accepted (B02, gate G2). Evidence: `docs/deep/evidence/VSF-B02/`.

## Context
Six contract families (environment capabilities, observation, semantic, operation, presentation, version manifest) must be read by Rust, TypeScript and Go without drift, fail closed on anything unknown, and never let an optimistic default reach execution.

## Decisions
1. **One IDL is the schema authority** (`schemas/ir/*.idl.json`, `vsf-idl/1`, `ir_version 1.0.0`). `tools/idlgen` emits strict codecs for Rust, TypeScript and Go over the canonical value model; CI fails on drift. Closed enums, strict structs (unknown field rejected), typed ID newtypes with patterns, bounded lists and maps. A field introduced later carries `since`; a document declaring an older version may not contain it (`ir.field_from_future`).
2. **Envelope** `{schema:"vsf.<family>/<ver>", schema_digest, body}`. The digest covers the family's type closure with prose removed, so wording fixes never change wire identity. Failures use stable codes: `schema.malformed|unknown|incompatible_major|too_new|unknown_version|digest_mismatch`, then `{path, code}` decode errors. Typed digests are `sha256("vsf.digest.v1\0" kind "\0" bytes)`.
3. **Text and numbers keep meaning.** No Unicode normalization (NFC and NFD stay distinct); controls, bidi overrides, U+2028/9 and noncharacters are rejected in labels; length counts scalar values. Decimals are minimal exact strings (no exponent, no `-0`); instants are integer milliseconds; dates are Gregorian.
4. **Rust is the semantic authority.** Cross-field invariants live in `vsf-ir::semantics` and reach TypeScript through WASM (`vsf_ir_validate`, ABI 3) and Go through the native worker (op `V`, CLI `ir-validate`). Effect classes order `read_only < idempotent_write < conditional_write < non_idempotent_write < destructive < indeterminate`; combining keeps the more dangerous; unproven means `indeterminate`. A declared effect may never be weaker than any asserted effect. Heuristic or user-assertion evidence can support `inferred`, never `verified`; only runtime behavior, documented schema or protocol trace can prove what an action does. An executable (`active`) operation needs evidence, strategy, verified uncertainty, a determinate effect, and a passing witness whose app, variant class and version range cover the contract's source binding.
5. **Identity.** A source variant id is `var_` plus 32 hex of HMAC-SHA256 under a deployment key over the canonical, order-normalized `{app, tenant, roles, locale, flags}`; equivalent descriptors collapse, conflicting flags fail, and a different key yields different ids. Private cache entries bind variant and principal; public structure binds variant only.
6. **Version matrix.** Twelve axes evolve independently and are judged separately (`supported`, `supported_with_translation`, `degraded`, `needs_reauth`, `source_incompatible`, `unsupported_runtime`, `security_revoked`); the overall result is the most severe axis. A pin demands an exact value and never overrides a support check.

## Alternatives rejected
Hand-written per-language models (drift); JSON Schema alone (cannot express closed unions, `since`, text rules); semantic checks reimplemented in TypeScript and Go; optimistic `verified` defaults.

## Consequences
- `vsf_wasm` ABI is 3. Measured WASM size 1,005,067 -> 1,402,563 bytes (+397 KB) before any trimming; B18 owns the reduction.
- Go decodes, encodes and digests natively and reaches semantics via the native worker, not by reimplementation.
- Fixtures: 10 valid, 79 decode failures, 104 semantic mutations, 49 matrix cases, all agreeing across languages.

## Revisit when
A fourth language needs bindings, semantic checks become a latency bottleneck for Go, or the WASM budget is set.
