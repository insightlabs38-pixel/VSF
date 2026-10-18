# ADR-029: Primitive registry, conformance harness and signed package

Status: accepted (B03, gate G3). Code: `compiler/crates/vsf-registry`; data: `registry/primitives/`, `fixtures/primitives/`.

## Context
The 924 catalog entries are requirements, not implementations. The registry has to keep that difference visible, reject unsupported claims and give every later batch a stable, verifiable unit to build on.

## Decisions
1. **Inventory is not maturity.** `registry.v1.json` is generated from the catalog and is always `candidate`. Claims above `candidate` live only in `status.v1.json`, each with witnesses; the registry and a fresh import must stay byte-identical.
2. **Classification** (atomic, composite, adapter, service, UNRESOLVED) comes from `policy.v1.json` family defaults and keyword rules, each with a recorded justification. Unclassifiable entries stay `UNRESOLVED`; they are never guessed.
3. **Registration** rejects duplicate ids, unknown ids, `UNRESOLVED` kinds, claims above `candidate` without a passing witness, higher maturity without independent, adversarial and (for production) field evidence, capability or mode mismatches, and ambiguous recognitions. Two recognizers may overlap only through exclusion or strict precedence.
4. **Graph.** Dependencies are explicit per entry; closure and cycles are checked with the exact missing id or cycle chain, and the topological order breaks ties by id so it is reproducible.
5. **Fixtures** are positive, negative or adversarial, seeded, and carry the source app and operations they exercise. Witness fixtures are named by registrations; seeded held-out fixtures stay unbound and test that a composite is not fitted to its witnesses.
6. **Dashboard.** The denominator is always the full catalog. Tiers are cumulative (listed, implemented, independently validated, production proven). Source-app rows count witness-bound fixtures; unbound ones are shown separately and never raise a tier.
7. **Package manifest.** `package.manifest.v1.json` lists every covered file with its SHA-256, a digest per primitive (entry, registration, witness and spec digests, implementation digest and the digests of its dependencies) and a root over all of them. It is signed with Ed25519 over a domain-separated canonical body. Verification is signature first, then files, then recomputed digests; any mismatch is a hard failure.
8. **Trust.** `trust.v1.json` is the verifier's key list. The key in this repository is a public, derived development key (`vsf-dev-1`, purpose `development`): it proves reproducibility and tamper evidence, not publisher identity. A manifest that verifies against a key list taken from the same tree proves only integrity.

## Consequences
- Editing any covered file, including the shared composite, requires regenerating the dashboard then the manifest; CI fails on drift.
- Release keys, rotation, revocation distribution and an out-of-tree trust anchor belong to host key management (B14) and release (B20); until then the package must not be presented as publisher-signed.
- Adding `ed25519-compact` (MIT, no dependencies, `std` only) is the only new dependency.

## Rollback
Revert the package files together; no stored artifact or contract depends on the manifest yet.
