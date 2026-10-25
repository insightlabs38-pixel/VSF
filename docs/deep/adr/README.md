# Architecture decision records

ADR-001 to ADR-024 are the provisional decisions in `docs/deep/plan/02_Architecture/03_ADRs.md` and stay in force. New decisions continue from ADR-025, one file each: `ADR-nnn-slug.md`.

## Workflow

1. State the current invariant and boundary.
2. Present measured alternatives on representative cases.
3. Evaluate correctness, latency, memory, maintenance, operational risk, security and capability scope.
4. Record impacts on versioned IR, contracts and existing users.
5. Require targeted tests, affected security checks and no-regression budgets.
6. Publish migration and rollback notes; mark superseded ADRs, never rewrite them.

Changes to shared contracts, compatibility, security, performance, release or the primitive registry need an ADR and a new queue task (`origin: "adaptive"`, `adr` set).

## Template

```text
# ADR-nnn: title
Status: proposed | accepted | superseded by ADR-mmm
Context: invariant and evidence
Decision: what changes
Alternatives: measured, with results
Consequences: contracts, security, performance, migration, rollback
Revisit when: trigger
```

## Index

| ADR | Title | Status |
|---|---|---|
| 001-024 | Initial provisional decisions | accepted for prototyping |
| [025](ADR-025-foundation-toolchain-and-layout.md) | Foundation toolchain, layout, canonical profile and dependencies | accepted |
| [027](ADR-027-versioned-ir-contracts.md) | Versioned IR contracts, semantics and identity | accepted |
| [028](ADR-028-schema-compatibility-and-promotion.md) | Schema compatibility and promotion policy | accepted |
| [029](ADR-029-primitive-registry-and-package.md) | Primitive registry, conformance harness and signed package | accepted |
| [030](ADR-030-browser-observation-and-epochs.md) | Browser observation, epochs and incremental events | accepted |
| [031](ADR-031-semantic-recognition-and-evidence-ledger.md) | Deterministic semantic recognition, evidence ledger and availability | accepted |
| [032](ADR-032-foundation-stabilization.md) | Foundation stabilization before the presentation renderer closes | accepted |
| [033](ADR-033-verified-deterministic-execution.md) | Verified deterministic execution | accepted |
| [034](ADR-034-version-registry-and-pins.md) | Version registry, semantic diff, pins and promotion | accepted |
| [035](ADR-035-ui-architecture-and-layout.md) | UI architecture checkpoint and region layout | accepted |
| [036](ADR-036-extension-integration-and-local-bridge.md) | Extension integration protocol and the authenticated local bridge | accepted |
| [037](ADR-037-api-cli-mcp-interfaces.md) | One operation service behind the REST, CLI, MCP and interface surfaces | accepted |
| [038](ADR-038-durable-storage.md) | Durable metadata and artifact storage | accepted |
| [039](ADR-039-auth-broker.md) | Authentication broker and managed sessions | accepted |
| [040](ADR-040-independent-corpus.md) | Independent application corpus and evaluation protocol | accepted |
