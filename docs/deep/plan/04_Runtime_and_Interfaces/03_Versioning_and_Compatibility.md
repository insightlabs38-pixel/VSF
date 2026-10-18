# Versioning as a flagship VSF product subsystem

**Strategic priority: HIGH.** Versioning is independently valuable for UI modernizations, automation stability, generated API/CLI/MCP contracts, research reproducibility and paid retention/rollback. It must be designed before artifact formats and contract generation, not bolted onto snapshot storage later.

## 1. Independently versioned axes

| Axis | Version shape | Meaning | Invalidates |
|---|---|---|---|
| Source application | observed revision/fingerprint + provider release | Original site behavior and tenant capabilities | plans, mappings and interface compatibility |
| Source account variant | tenant/role/license/locale scope fingerprint | What this user may see/do | permission-scoped artifacts, available operations |
| Fabric compiler | SemVer + build digest | Recognition and optimizer logic | outputs, verification requirements |
| Primitive packs | pack semver + composite digest | Recognizer/renderer/transport behavior | dependent IR and plans |
| Observation IR | schema major/minor | Data acquisition format | deserialization/migrations |
| Semantic IR | schema major/minor | Application semantics | renderers, contracts |
| Operation contract | independent SemVer | API/CLI/MCP public behavior | clients and scripts |
| Presentation/layout | independent user/org revision | Theme, views, workflow layout, preferences | UI only when semantics unchanged |
| Cache/artifact | content digest + format revision | Physical immutable content | retention/access, restore |
| Runtime environment | binary semver + capability manifest | Which operations can run where | deployment support matrix |
| State snapshot | ordered sequence/revision | User-private ephemeral/persisted data | freshness and conflicts |
| Governance policy | revision/issuer | Limits, approval/retention, enterprise settings | available routing/caching |

A single `v2` label is insufficient. Version manifest records the full compatibility tuple and a dependency/lineage DAG.

## 2. Contract versioning rules

- Patch: bug fix with unchanged externally observable semantics and compatible schema.
- Minor: additive optional capability/field/tool with stable old meanings.
- Major: breaking arguments, required fields, effect type, authorization shape or removed operation.
- Source behavior can break without VSF releasing a new contract. Such a contract becomes `SOURCE_INCOMPATIBLE` or `DEGRADED`; a pin preserves the promised interface *shape*, not nonexistent functionality.
- MCP tool descriptions, annotations and permissions are part of the reviewed contract. Tool ordering and lazy namespace membership remain stable under pins.
- CLI flags, exit statuses, structured stdout/stderr and API error codes are versioned.

## 3. User-visible functionality

- Version timeline per application, with diff between source, compiler, primitive, contract and presentation changes.
- Side-by-side preview of old/new generated UI and operation schemas (with sanitized data fixtures).
- Pin and unpin contract version (qualified by plan entitlement and compatibility).
- Compatibility report: `supported`, `supported_with_translation`, `degraded`, `needs_reauth`, `source_incompatible`, `security_revoked`, `unsupported_runtime`.
- Snapshot and restore of presentation + compiled artifacts (not necessarily source records or external writes).
- Controlled release channels (`stable`, `preview`) and optional tenant staged promotion.
- API/MCP documentation keeps historical operation signatures and migration notes.
- Research mode: reproducible export of version manifest, primitive set, test witnesses and public/sanitized provenance for a known date.

## 4. Semantic diff algorithm

1. Normalize old/new canonical IR under a migration-aware common schema.
2. Match identities using stable IDs and verified structural/semantic similarity, with explicit ambiguity cases.
3. Classify `cosmetic`, `data shape`, `new feature`, `removed feature`, `contract breaking`, `auth change`, `behavior change`, `unknown`.
4. Determine impacted dependency closure (primitive -> operation -> UI -> CLI/API/MCP).
5. Run targeted differential regressions and effect-sensitive verifications, compile candidate version.
6. Promote new version atomically by manifest pointer update after policy and test gates; retain old artifact according to pin/retention policy.
7. If incompatible, publish migration edges and status; never silently reroute destructive operations to apparently similar controls.

## 5. Version persistence and economics

Shared non-sensitive primitive binaries and structure may use CAS/dedup. User-private source data and credentials are tenant-isolated. Version retention policy is enforced on logical manifest references; GC must respect active pins, rollback windows, legal retention and open operations. Account cancellation, export and deletion interact with retention policies and must be tested.

## 6. Tier strategy (illustrative, to validate)

| Benefit | Free/Individual | Pro | Lab/Team | Enterprise |
|---|---|---|---|---|
| Automatic safe recompile and baseline rollback | yes | yes | yes | yes |
| Recent basic revision history | bounded | expanded | expanded | policy controlled |
| Manual contract pin for API/CLI/MCP | limited/basic depending plan | yes | shared team pins | governance + long-term support |
| Deep history and cross-version diffs | limited | advanced | shared | advanced/export/audit |
| Source-change compatibility monitoring | basic local | continuous/premium | shared notifications | managed SLAs |
| Branchable interface layouts/snapshot archives | basic export | yes | collaboration | org policy and snapshots |
| Long-lived hosted archival retention | not included | metered | allowance + metered | negotiated |
| Migration automation and compatibility report exports | basic notice | advanced | team workflows | compliance-grade and approvals |

Every plan gets security patches, safe invalidation, and clear failure statuses. No tier permits executing a known unsafe stale plan merely to preserve a pin.

## 7. Rollback and migrations

Artifact rollback: switch manifest to previously signed compatible artifacts. Presentation rollback: restore earlier layout. Public-contract rollback: switch routing to previous verified contract implementation if source still supports it. **Source data rollback:** only if the source exposes an authorized, verified operation; do not imply automatic rollback of outside-world mutations.

Data schema migrations must be idempotent when possible, forward/backward compatibility-tested across supported runtime versions and backed up before modification. Use two-phase publish for multi-object artifact manifests; verify digests after export/import.

## 8. Test scenarios

- New UI-only source change preserves operation contract and version pin.
- Source removes a field required by pinned v1: report source incompatibility; do not fabricate it.
- User loses source permission after version cached: operation fails authorization, not a global-cache leak.
- Security-revoked version: deny execution even when pinned, show reason and export/migration path.
- Two layouts branch from same semantic model and can merge nonconflicting presentation changes.
- Online/offline compiled artifacts have identical recorded hashes and deterministic semantic diffs.
- GC retains legal/pinned roots and removes unreachable blobs without touching active operations.
- API v1 and v2 served concurrently from same execution service with versioned translation when faithful.

## 9. Development gates

- **B02/B03** define version identities, manifests, content addressing and compatibility grammar.
- **B07/B08** produce semantic diff, version lineage and promotion/rollback prototype.
- **B13/B14** enforce API/CLI/MCP pins and compatible migrations.
- **B16/B19** implement paid retention and advanced caching quotas, metering and user-visible management.
- **B22+** evaluate tenant history, shared revisions, evidence-graded long-term contract support.

The release cannot claim “versioning supported” until end-to-end **pin → source change → validate → diff → migrate/degrade → rollback/export** has passed controlled fixtures.

## 10. Downgrade and entitlement-expiry contract

When an account loses a premium version-retention entitlement, distinguish **availability of historical artifacts**, **right to execute pinned contracts**, **export rights**, **legal retention** and **billing status**. Define a published grace/export window; never quietly discard active legal-hold/pinned roots or allow unsafe execution. An overdue hosted retention charge may stop *new* archive creation before garbage-collecting eligible old roots. Add `VER-009` (paid-to-free downgrade, export then governed retention) and `VER-010` (old contract pin plus source drift, cache invalidation and restoration) to the release fixture suite. Rate-card and entitlement changes have their own versioned policy manifest.
