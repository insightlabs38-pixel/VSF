# CLI, API, MCP and SDK interface generation

## One semantic contract, multiple surfaces

Generate programmatic surfaces from verified operation contracts, not by scraping button labels into names. Emit two access levels:

- **Universal interaction interface:** navigate, inspect, fill, activate, wait, export and report accessible state. Always acknowledge source restrictions and potentially ambiguous effects.
- **Semantic API:** stable, typed higher-order domain operations with evidence-backed inputs, effects, errors and verification.

## REST/OpenAPI

Version prefix or negotiated contract version; typed JSON/OpenAPI schema, stable operation IDs, pagination and streams, asynchronous operation handles, error taxonomy and idempotency guidance. Use source-derived rate limit/consistency info in docs, never claim atomicity where unsupported. Generated endpoint must validate both VSF caller and bound source identity.

## CLI

Go CLI with generated subcommands from manifest, consistent help, typed args, JSON output mode, stdout/stderr separation, machine-readable exit statuses, dry-run preview when supported, completion scripts, version pin flags, source profile selectors, offline schema discovery and command execution mode (`local`, `hosted`, `private`) with explicit routing policy. `--version` for client binary and `--contract-version` for operation schema are different.

## MCP

Implement current negotiated specification profile, including protocol `2026-07-28` features where supported by SDK; stateless core, tools/resources/prompts, authorization, version and extension negotiation, optional Tasks for long-running work when client supports extension. Serve scoped lazy tool catalogs to prevent dumping thousands of tools to agent contexts. Stable namespaces and bounded descriptions; signed tool contracts and provenance. Tool annotations are not sufficient authorization or safety policy.

## Tool safety

Tool calls are untrusted until caller authorization, source binding, execution-capability policy and effect confirmation check. Generated tool names/descriptions must not accept hostile source text as control instructions. Audit call intent, effect class and verification outcome without logging source confidential payloads by default.

## Generated SDKs (later)

Start with OpenAPI-compatible clients rather than maintaining a hand-coded SDK for each provider/language. Consider Go/TS/Python/Java packages only after real adoption evidence. Treat codegen generator outputs as versioned build artifacts and run compile + runtime contract checks on every release.

## Versioning and caching

- Cache schema/manifest by content digest and source-variant scope; expose ETag/cache semantics for list/discovery endpoints.
- Multiple contract versions coexist under valid pins; new source operations create additive version rather than silently expanding pinned tool sets.
- Paid tiers expand pin retention, historical docs, diff, migration tools and managed compatibility monitoring.
- Cache metadata and plan index, but verify sensitive operation freshness and source permissions at invocation.

## Integration acceptance

A user can generate and call equivalent operation via web UI, CLI, REST and MCP; all return the same semantic outcome categories and operate under identical source permissions. Reject malformed inputs; confirm no source write on validation failure. Simulate unknown source-side commit and check all surfaces report it rather than claiming failure/success incorrectly. Run version-skew tests and concurrency/rate-limit tests on each adapter.

## 2026-07-28 protocol details (verified)

Treat `2026-07-28` as a released MCP **core** protocol; Tasks is a separately negotiated extension and the extension specification may be draft. Implement full applicable JSON Schema 2020-12 input validation, stable namespaced tool names, collision resolution, and `tools/list` cache hints/TTL/scope. When a client lacks Tasks, a VSF-owned asynchronous job API may provide a polling fallback rather than claiming the client supports the extension. Avoid introducing new dependencies on deprecated legacy core features.
