# Decisions, constraints, and evidence gates

## D0: Product scope

Transform authorized existing web software into a consistent, high-performance and user-customizable interface, with generated CLI/API/MCP access where operation semantics can be demonstrated. Preserve specialized original controls when reconstruction cannot be verified. Do not claim ability to infer arbitrary server-side code from a DOM snapshot.

## D1: Chosen technology boundaries

- **TypeScript + native DOM/CSS**: browser observation, extension, tiny renderer and interface-specific event binding. Specialized terminals/editors/charts may use isolated mature dependencies when the conformance and long-term maintenance benefit outweigh the additional supply-chain risk.
- **Rust native and WASM**: observation normalization, feature indexing, semantic evidence graph, recognition, incremental dependency tracking, typed application IR, optimization, serialization and deterministic diff engine.
- **Go**: API gateway, scheduler, persistence coordination, session/account brokers, billing abstraction, hosted execution orchestration, CLI and MCP adapter (subject to SDK suitability).
- **Playwright/Chromium controlled via TypeScript**: hosted browser workers, tests and authorized automated interactions. Alternate browser backends implemented behind capability negotiation.
- **SQLite** for local hot metadata and test manifests; **PostgreSQL** for multi-tenant transactional metadata; **ZEROS3 evaluated** for immutable artifact storage; filesystem/object-store baseline must remain pluggable. Secrets live in a dedicated keychain/vault, not in general artifact storage.
- **Build/deploy**: monorepo, schema-generated language bindings, pinned toolchains and dependencies, reproducible release artifacts, isolated worker processes. No automatic microservice split until supported by scale/ownership evidence.

## D2: Distinguish truth from assumptions

| Decision or hypothesis | Status | Evidence required |
|---|---|---|
| Deterministic universal semantic understanding | Research hypothesis | Held-out unseen application corpus, safety/effect tests |
| Large catalog improves compatibility | Working thesis | Fix-to-unseen-app improvement rate, absence of adapter debt |
| Six execution modes desirable | Product requirement | Platform capability matrix and at least one real end-to-end demonstration per mode |
| Source authorization is authoritative | Hard invariant | Identity/effect test suite |
| Versioning is a paid differentiator | Hard product requirement | Pinned contracts, semantic diff, rollback and tier enforcement tests |
| Near-zero frontend runtime dependencies | Preference | Test cold/warm UI performance, accessibility and bundle size |
| ZEROS3 superior to alternatives | Unproven | Workload benchmark and operational failure drills |
| 75–85% eventual gross margin | Aspirational | Measured full cost attribution, support and churn |
| 99% compatibility | Aspirational | Pre-registered benchmark population and workflow denominator |

## D3: Release gates (required, cumulative)

- **Research gate (RG)**: foundational unknowns are demonstrated with spikes, no speculative dependencies baked into core IR.
- **Prototype gate (PG)**: 2 unseen architecturally distinct apps, typed deterministic recognizers, same operation contract in two modes; failure unknowns are explicit.
- **Alpha gate (AG)**: representative common capability families, fidelity fallbacks, local/extension path, auth boundaries, baseline performance, task-level observable correctness.
- **Private beta gate (BG)**: reliable installs/upgrades, recovery, crash telemetry opt-in, independent apps across at least five architecture classes, documented source-app support limits, signed artifacts.
- **Public release gate (PRG)**: billing entitlements and PAYG metering, data deletion/export, accessibility acceptance, support playbooks, independent privacy/security review for target risk level, tracked costs, measured product value.
- **Enterprise gate (EG)**: multi-tenant or customer-private operations, SSO/policy/audit/export/DR, per-tenant isolation tests, deployment instructions, support SLAs, private-server pricing.

## D4: Key invariants

1. Source application remains system of record. A generated action cannot promise stronger consistency or authorization than the source supports.
2. A click, keystroke or write with ambiguous semantics is not automatically promoted to an innocuous high-level API.
3. Mutation caused by observation/discovery alone must not create irreversible external effects.
4. Failed/timeout writes do not automatically retry unless effect/idempotency evidence permits it.
5. User/tenant credentials and private derived data are never globally deduplicated or shared.
6. Extension remote artifacts cannot act as arbitrary downloaded executable instructions contrary to distribution policies.
7. Every operation contract records source-app variant, required permissions and execution environment capabilities.
8. Tier differences alter managed persistence, scale, support and retention, not core security, correctness or basic responsiveness.
9. No arbitrary cross-environment transfer of browser cookies, passkeys or credentials.
10. Benchmarks separate VSF overhead from upstream response time, and cold initial display from exhaustive discovery.

## D5: Scope discipline

We want the first broad compatibility release to include a **very large registered capability inventory**, but implementation breadth must be verified, not simulated by stub registration. Dependencies can be many when justified. No arbitrary LOC cap, no blanket ban on frontend libraries; require explicit design reviews and dependency maintenance plans.

## D6: Compiled-code distribution caveat

Signed plans are not automatically safe for Chrome MV3. Execution-like instructions fetched remotely require separate store policy review. Browser extension includes only packaged, reviewable logic and narrowly defined configuration or observation inputs; native/hosted workers handle arbitrary compilation where permitted. Versioning must not turn remote artifacts into forbidden interpreters.
