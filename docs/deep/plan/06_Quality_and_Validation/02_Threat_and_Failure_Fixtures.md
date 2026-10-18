# Adversarial fixture inventory and acceptance oracles

## Browser/DOM fixtures

- `BROW-001`: deeply nested open and closed shadow roots; safe partial recognition and unknown boundary.
- `BROW-002`: cross-origin iframes and sandboxed frames with non-embeddable top-level origin.
- `BROW-003`: custom elements and slot reassignment; no stale label binding.
- `BROW-004`: virtualized 200k-row table with dynamic node reuse and filtering.
- `BROW-005`: server-rendered multipart form, page reload and partial navigation.
- `BROW-006`: canvas/WebGL editor without accessible semantic model; preserved native interaction, no invented semantic API.
- `BROW-007`: cloned buttons with same accessible name but different side effects; ambiguous mapping must stop.
- `BROW-008`: adversarial page injects tool/policy instructions in DOM content; ignored as policy.
- `BROW-009`: thousands of mutations while source app updates several nested accessible relationships; dependency invalidation catches all.

## Auth and source permission fixtures

- `AUTH-001`: read-only account obtains only read operations despite cached admin artifact.
- `AUTH-002`: Google OAuth token with wrong audience cannot authenticate unrelated website.
- `AUTH-003`: expired browser cookie/partitioned storage/passkey transition triggers normal reauth flow.
- `AUTH-004`: malicious webpage requests localhost write via cross-origin fetch, DNS rebound hostname and fake extension pairing; all denied.
- `AUTH-005`: managed browser egress tries cloud metadata and RFC1918 targets, including redirect and DNS rebind; denied absent explicit private policy.
- `AUTH-006`: host extension service worker terminates and restarts; no secret loss or double action.

## Operations and state fixtures

- `OPS-001`: kill source connection after remote write before reply; status UNKNOWN_COMMIT, no automatic duplicate.
- `OPS-002`: source API provides ETag; conflict detected, no silent overwrite.
- `OPS-003`: live shared editor while VSF changes document; preserve original collaboration and reconcile with bounded event ordering.
- `OPS-004`: source rate limit while many API clients submit; bounded admission and backoff.
- `OPS-005`: cancel long running task after remote operation started; no false rollback claim.
- `OPS-006`: auth revoked mid-operation; terminate at safe checkpoint, verify impact and protect residual output.

## Versioning fixtures

- `VER-001`: CSS-only update, patch-compatible reconstruction, stable pinned API.
- `VER-002`: added optional source field, minor contract and compatible v1 pin.
- `VER-003`: removed required field, old pin becomes SOURCE_INCOMPATIBLE.
- `VER-004`: primitive security vulnerability, old version revoked despite user pin.
- `VER-005`: offline export/import into older runtime fails with explicit incompatibility and no corruption.
- `VER-006`: tenant loses feature license; avoid execution from cached global IR.
- `VER-007`: retention GC with live pinned and legal-hold roots, no deletion of reachable artifacts.
- `VER-008`: edit presentation layout while source semantic schema changes; layout migration and conflict resolution.

## Data fidelity fixtures

- `DATA-001`: locale `1,234` vs `1.234`, date timezone offset and DST boundary.
- `DATA-002`: scientific molarity units and sequence orientation annotations round-trip.
- `DATA-003`: financial decimal with thousands digits, no binary float error.
- `DATA-004`: XML/CSV/JSON/Parquet export typed null and Unicode/RTL content.
- `DATA-005`: PDF print and form export preserving original critical fields where supported.

## Operations harness

Fixtures must include source-app oracle, setup/reset procedure, user roles, expected evidence, effect classes, reproducible seed, runtime/browser matrix, negative assertions and expected source-version status. Every bugfix should add one regression fixture and, where possible, one structurally unrelated negative case.

- `VER-009`: paid history entitlement expires or downgrades; export/grace/retention/GC policy honored, no unannounced loss.
- `VER-010`: cached pinned contract meets source incompatibility; stale content cannot falsely authorize a source write, recovery accurately reports compatibility.
- `AUTH-007`: native host sends artifact larger than 1MiB; chunking or paired local download verified with digests, no untrusted cross-origin access.
- `MCP-001`: tool schema JSON Schema 2020-12 features validate and no namespace collision; Tasks only when negotiated; absent extension gets documented fallback.
