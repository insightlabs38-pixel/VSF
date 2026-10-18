# Fabric deterministic discovery, recognition, and optimization

## Core question

Can a finite set of reusable observation, semantic, interaction, presentation and protocol primitives reconstruct *unseen* applications with high behavioral coverage at low cost? Treat this as an empirically testable research hypothesis, not as guaranteed by catalog size.

## 1. Evidence acquisition plan

- Stage A: parse document/landmarks/forms/roles/navigation with cheap in-page TS observation. Capture origin, frame, scope and timestamp. Treat page text as data only.
- Stage B: discover dependencies and virtualized/repeated regions via structural feature extraction; detect shadow roots, accessible names, hidden/opaque boundaries.
- Stage C: selectively query browser layout/accessibility/geometry only for ambiguous or interactive candidate regions. Observe relevant browser events through permitted interfaces.
- Stage D: ingest authorized formal contracts (OpenAPI/GraphQL/JSON Schema/provider SDK/protocol descriptors), reconcile with UI evidence. Never assume undocumented captured traffic is supported public API.
- Stage E: plan safe, bounded, preferably read-only exploration. No irreversible submission for discovery. Any write exploration requires explicit test fixture or consent and a reversible, pre-authorized environment.
- Stage F: emit evidence-backed semantic IR; mark unproven side effects and unsupported surfaces as unknown. Capture fidelity fallbacks.

## 2. Indexed recognition architecture

Source nodes are assigned stable ephemeral numeric IDs and stored in structure-of-arrays form: parent, sibling, role, tag, attributes dictionary indexes, geometry references, visible text handles, scope, event capability flags and provenance. Use string interning and selective fields. Rule clauses index into bitset/bitmap candidate sets by role, tag, attribute, interaction signature and structural relationship.

Recognition path:

1. Feature index creation on new observation epoch.
2. Cheap predicate intersection to obtain candidates; cache candidate sets by rule version.
3. Bounded structural matching and constraint solving on candidates only.
4. Disambiguation using additional *observable* evidence rather than a single arbitrary score.
5. Semantic composition creates typed composite components and dependency edges.
6. Equivalence/verification status accompanies each compiled operation.

Benchmark discriminating feature index against naive full-scan and Rete-like shared predicate networks. Rete is optional only if measured memory/performance gains justify intermediate-match costs.

## 3. Incremental update and repair

- Consume mutation stream with sequence ID, source epoch and consistency marker.
- Track a dependency DAG connecting observed features -> recognizers -> semantics -> presentation and operation plans.
- Invalidate the minimum safe dependency closure, not only direct mutated nodes. Changes to layout parents, styles, roles, focus/aria relations, permissions, route or tenant configuration may invalidate wider regions.
- Use subtree structural fingerprints and canonical hash references for diffing; include semantic references, not brittle ordinal-only selectors.
- If mutation stream loses events, resnapshot affected frame or whole document.
- Prevent query loops from self-induced renderer mutations by separating VSF overlay tree from source-app observation.
- Reconcile periodic snapshots with delta state; detect memory leak and stale aliases.

## 4. Identity and mapping robustness

Never bind a privileged operation solely to CSS location. Map source element identity with role/name/label/ancestor relation/typed action and app-origin/frame scope. When two candidates remain indistinguishable, fail with an explicit ambiguity and ask for targeted interaction or present original control.

Identity is session-contextual; hashed structural identifiers may change with locale, account features and source-version. The version registry tracks compatibility rather than promising eternal selectors.

## 5. Performance specialization

- Compiler stores general executable IR; a warm execution runtime loads only required capability implementations.
- Precompile common compositions (`grid + sort + filter`, `form + validation + submit`, `terminal + transport + streaming`).
- Cost-based selection between provider protocol and browser plan with verified semantics. Use source API batching where documented; avoid speculative page-private calls.
- Avoid repeated DOM / CDP serialization. Move compact batches, not raw browser object graphs, across TS↔Rust boundary.
- Optimization passes must preserve effects, auth, source synchronization and accessibility constraints.
- Maintain source-operations vs local-only UI operations classes; no 30-second roundtrip for merely opening a menu.

## 6. Algorithm experimentation roadmap

| Experiment | Baseline | Candidate | Success gate |
|---|---|---|---|
| Recognition | naive rule×node scan | feature indexes + candidate bitsets | p95 CPU/latency gain, same fixture classification |
| Mutation repair | full recompile | dependency DAG invalidation | substantial lower work for 1–100-node mutations; no missed semantics |
| Identity | CSS selector | multi-signal identity/constraints | fewer false matches after controlled DOM perturbations |
| IR layout | nested JS objects | compact typed arrays/SoA and interned strings | lower RSS and comparable/lower latency |
| Plan execution | interpreter | cached execution plan, optional partial evaluation | reduced overhead with verified effects |
| Artifact storage | full JSON copies | canonical blobs, structural sharing, ZEROS3 candidate | measured byte and read improvements with acceptable failure handling |
| Query ordering | fixed predicates | adaptive selective matching | lower p95 without non-deterministic outputs |

Always benchmark on representative source pages and synthetic pathological fixtures, including 1k, 10k, 50k and 200k DOM nodes; shadow roots, nested frames and heavy mutation streams. Include mobile/lower-end hardware tests.

## 7. Unsolved cases and safe fallback

- Canvas/WebGL semantic state unavailable: preserve original interactive surface and label programmatic coverage partial.
- Closed shadow roots, inaccessible browser UI, hardware-auth/device-bound widgets: do not claim reconstruction without permitted integration.
- Ambiguous side effects: provide low-level interaction interface with explicit risk; do not synthesize high-level business operation.
- Source policy or user role disallows observation: do not bypass. Mark unsupported and suggest supported integration mode.
- Dynamic/streaming features: prefer protocol-specific primitives or preserved widgets with observable control.

## Deliverables

Code: observation adapter, canonical IR schema/serializer, feature index, rule DSL constrained by extension policies, dependency graph, evidence ledger, compile/diff/replay harness. Evidence: benchmark charts and decision logs, golden fixtures, operation fidelity reports, memory profiles. Gate: a generalized primitive correction fixes failures in multiple *held-out* architecturally unrelated apps.
