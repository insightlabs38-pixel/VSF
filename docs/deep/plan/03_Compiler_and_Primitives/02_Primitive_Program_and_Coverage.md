# Large primitive program, catalog governance and coverage

## Existing inventory

See `../09_Reference_Catalog/universal_interface_primitive_catalog.json` (924 candidate capabilities, 29 families) and companion `.md`/`.csv`. It is a **requirements map** only. It mixes different abstractions and cannot be used as a percentage-complete denominator until entries are classified and given conformance contracts.

## Classification and dependency metadata

Every catalog entry must include:

```yaml
id: GRID-EXAMPLE
name: Virtualized collection
kind: [observation | semantic | interaction | presentation | protocol | composite | runtime]
family: tables_grids
maturity: proposed # proposed, modeled, prototype, conformance, verified, maintained
scope: runtime_capabilities
requires: [OBS-..., STRUCT-..., INTERACT-...]
recognition: evidence_predicates_and_ambiguity
execution: [browser, authorized_protocol, preserved_surface]
presentation: render_contract
unknown_behavior_policy: preserve_or_degrade
fixtures: [table_virtualized_01, table_aria_02]
performance_budget: per_input_cost_model
maintainer: subsystem_owner
source_provenance: repo_or_spec
```

Do not count an alias or an empty entry as implemented. A composite's dependency closure and conformance witnesses are explicit. Use stable IDs, deprecation metadata and an unambiguous mapping from catalog entries to implementation artifacts and tests.

## Initial family structure

| Family group | Example emphasis | Reason to invest early |
|---|---|---|
| Foundation (observation, semantic, geometry, interaction, state) | shadow DOM, accessibility, labels, identity, events, evidence | Required for almost every application |
| Core UI (forms, navigation, grids, documents, files) | custom inputs, virtualization, rich text, uploads, validation | Covers frequent enterprise/research workflows |
| Advanced UI (charts, calendars, communications, code/terminals) | real-time charts, streaming, editor state, complex scheduling | Frequent specialized software breakpoints |
| Specialist domain (science, CAD, geospatial, finance, legacy) | sequence editor, plate grid, policy graphs, diagrams | Enables complex app categories and differentiates product |
| Protocol/runtime (identity, API, MCP, cache, workflows, validation) | typed operations and state lifecycle | Converts compatibility into dependable APIs |

## Explicit coverage gaps to add to baseline

1. Cross-context runtime capabilities and extension policy compliance.
2. Safe semantic discovery, evidence provenance, conflict classification.
3. Partial/ambiguous commit status, operation isolation and scheduling leases.
4. Authoritative application/source variant identity, feature flags and licenses.
5. WebRTC/SSE/presence protocols, streaming backpressure and reconnection.
6. Locale parsing and formatting, IME, Unicode, RTL, time zones, dimensional units.
7. Screen reader/assistive technology manual conformance harness.
8. Accessible large spatial editors, fallback for canvas/WebGL/remote desktops.
9. Portable contract manifests across execution environments; **not** portable credentials.
10. PDF forms/printing/export fidelity, embedded browser PDF viewer limits.
11. Threat-labeled artifacts, signed package provenance, protected bridge.
12. Native updater, crash recovery, entitlement modes, customer-controlled metering.
13. Domain data interchange (JSON, XML, CSV, Arrow, Parquet, schema fidelity, sequence formats) when relevant and authorized.
14. Source rate-limit/admission policy, unknown-effect retries and external consistency.
15. Versioned contract diff/migration/deprecation primitives and user-visible history.

## How to expand without spaghetti architecture

- Require atomic/composite distinction and dependency graph; reuse rules before writing domain-specific code.
- Add a capability pack when several products use shared semantics and tests demonstrate breadth. Named providers may furnish **fixtures**, not implicit custom hacks.
- Require independent public synthetic fixtures when a proprietary target is the original motivating example.
- Validate negative examples so detectors do not overmatch unrelated controls.
- Review an improvement by `new workflows recovered / added runtime cost / memory cost / maintenance cost`, not primitive count alone.
- Specialized library dependencies go behind adapters; component styles follow VSF tokens and accessibility contracts.

## Conformance maturity

`Proposed -> Modeled -> Implemented -> Unit conformant -> Compositional conformant -> Held-out validated -> Maintained`. A capability may be degraded by browser/source environment. Coverage dashboards show lifecycle, confidence and human review count without treating numeric heuristic scores as proof.

## Broad-beta launch coverage

- All **critical** baseline entries must either be implemented/verified **or** explicitly have a safe and disclosed fallback supported by the launch compatibility contract; no silent 'supported' based on stubs.
- Core entries measured against target corpus; preserve surface where native equivalence cannot be demonstrated.
- Specialist coverage prioritized based on observed app-family blockers and return on reusable implementation.
- Every cross-application improvement requires a regression witness and recorded failure-class elimination.
- Aspirational 99% means *at least 99% of weighted held-out authorized workflows meeting a defined fidelity threshold*, not 99% of hand-picked homepages. Also report per-app percentile and worst supported family.
