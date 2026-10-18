# Master development sequence: batches, gates, branching decisions

**Plan orientation:** front-load architecture experiments, then build broad primitives on a tested shared runtime. Early batches B00–B10 are task-level commitments. B11 onward have defined output and gates but are *scope-adaptive* based on observed compatibility failures, vendor policies, user demand and measured costs. Batch IDs are stable labels. Do not run multiple agents against the same core schema or migration without one designated integrator.

**Executable task queue:** [03_Executable_Task_Queue/README.md](03_Executable_Task_Queue/README.md) converts the 152 documented steps into individually testable tasks. B00–B10 are committed, B11–B24 retain conditional scope and can acquire appended IDs with ADR-backed evidence. Agent prompts live under `10_Agent_Prompts/`.

## Waves and batches

| Batch | Wave | Outcome | Depends on | Gate |
|---|---|---|---|---|
| B00 | Foundations | Repo/toolchains, baselines, fixtures and ADR registry | none | G0 reproducible baseline |
| B01 | Research spikes | Browser extension, CDP/Playwright, TS↔Rust/WASM boundary | B00 | G1 technology viability |
| B02 | Contracts | Observation/Semantic/Operation/Presentation IR + multi-axis versions | B01 | G2 canonical schema acceptance |
| B03 | Registry | Primitive metadata, dependency graph, content digests and golden harness | B02 | G3 primitive contract |
| B04 | Observation | Real DOM/AX/layout/mutation adapters, scope and environment manifest | B02 | G4 safe capture |
| B05 | Recognition | Indexed deterministic matching and evidence ledger | B03, B04 | G5 held-out recognition |
| B06 | Rendering | Neutral fast renderer + preservation fallback, accessible UI | B05 | G6 fidelity UI |
| B07 | Execution | Source-bound typed deterministic operations and unknown-commit states | B05 | G7 verified action |
| B08 | Version core | Semantic diff, version DAG, pins, compatibility and rollback | B03, B07 | G8 version lifecycle |
| B09 | Local mode | Extension integration and secured local bridge spike | B06–B08 | G9 extension/local |
| B10 | Programmatic | Same contract via minimal REST/CLI/MCP | B07–B09 | G10 interface parity |
| B11 | Common coverage | Forms/navigation/grids/files/dates; expand verified families | B10 | G11 broad common workflows |
| B12 | Specialized I | Terminals/docs/code/notebooks/charts/workflow/collaboration | B11 | G12 specialized parity |
| B13 | Storage | SQLite/Postgres metadata, ZEROS3 benchmark, version persistence | B08, B10 | G13 crash/version durability |
| B14 | Authentication | OAuth connections, managed sessions, vault + cross-context identity | B09, B13 | G14 source isolation |
| B15 | Hosted runtime | Isolated sandbox, headless, queue and PAYG metering | B07, B14 | G15 safe hosted run |
| B16 | Desktop/localhost | Desktop packaging and gated persistent local API/MCP | B09, B14 | G16 portable service |
| B17 | User version UX | Visual diffs, snapshots, managed pins and migration paths | B08, B13 | G17 version value |
| B18 | Performance | Algorithm profiling, specialization, memory, streaming and resource caps | B11–B17 | G18 p95/RSS budgets |
| B19 | Commercial | Provider-agnostic entitlements/billing, plans and trials, PAYG safeguards | B15–B18 | G19 metering parity |
| B20 | Specialized II | Science, CAD, spatial, finance and legacy packs driven by blockers | B12 | G20 observed coverage uplift |
| B21 | Cross-browser | BiDi adapter and browser parity exploration | B04, B11 | G21 capability matrix |
| B22 | Team/lab | Shared profiles, lab governance, collaborative versioning | B14, B17 | G22 multiuser |
| B23 | Private/VPC | Private execution and enterprise SSO/policy/residency | B15, B22 | G23 private isolation |
| B24 | Beta/launch | Adversarial, accessibility, security and business validation | B19–B23 as applicable | G24 launch acceptance |
| B25+ | Adaptive | Cross-app workflows, SDKs, advanced science and optimized adapters | empirical | per-proposal ADR |

## Program milestones

**M0 (B00–B02)**: Language/tool/browser viability, schemas and versioning model. **M1 (B03–B06)**: A new site's structure is rendered using primitives. **M2 (B07–B10)**: One verified operation executed through multiple interfaces, with version pins and source permissions. **M3 (B11–B14)**: Real-world common workflows, persisted artifacts and session isolation. **M4 (B15–B19)**: Multiple paid modes, versioning differentiated by tiers, transparent resource costs. **M5 (B20–B24)**: Broad held-out coverage, organizational deployment and release qualification. Specialized expansion follows actual failure clusters.

## Priority and parallelism

- Only one schema authority for ObservationIR/SemanticIR/OperationContract/VersionManifest at once. Parallel workers may develop independent fixtures, UI tokens, SDK adapters and benchmark corpora against a pinned schema version.
- Every merged batch: reproduce core test harness, schema evolution diff, security regression relevant to changes, release/changelog notes and cost/performance delta when measurable.
- For multi-week work, use intermediate checkpoint commits with tests and concise evidence, not an unreviewable massive implementation.
- No new vendor-specific adapter solely to pass a demo; demonstrate generalization with an independent fixture.

## Change gates

- If Chrome MV3 design fails a policy audit, move the affected logic into an explicit native/hosted worker, **do not** work around review by disguising executable commands as JSON.
- If Rust/WASM boundary overhead dominates, reduce crossing frequency, improve batch encoding or move hot DOM-adjacent logic to TS with profiling evidence.
- If ZEROS3 performs worse under small-object workloads, retain the storage abstraction and use SQLite/filesystem or ordinary object store for that workload.
- If source semantics are unknowable, stop high-level API generation and preserve the surface. Record the uncovered class.
- If managed browser costs/support invalidate plans, change entitlements/pricing before scaling hosted mode.
- If privacy/security controls aren't defensible, restrict alpha to local, non-sensitive apps.

## Batch-specific files

See `batches/B00.md` through `batches/B24.md` for the work definition. Detailed early acceptance is intentionally more rigid; later work specifies constrained outcomes, dependencies, stop criteria and decisions.
