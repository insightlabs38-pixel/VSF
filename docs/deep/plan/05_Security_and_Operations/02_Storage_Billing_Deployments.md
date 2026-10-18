# Storage, deployments, metering, billing abstraction and release operations

## Storage architecture

- Hot renderer state: browser memory and bounded indexed store.
- Active plan/metadata: SQLite local or PostgreSQL hosted.
- Compiled immutable IR/manifest/plan chunks: replaceable object backend, **evaluate ZEROS3** against filesystem/SQLite and ordinary S3.
- Sensitive user/tenant derived artifacts: explicitly segregated encrypted objects and scoped metadata; not globally deduplicated.
- Credentials and auth sessions: dedicated keychain/vault with explicit TTL/rotation, not ZEROS3 public cache.
- Immutable version DAG: manifests, roots, annotations, retention pins and signed provenance; GC acknowledges active workers, legal holds, version retention entitlements.

## ZEROS3 qualification

Run corpus: 1M tiny artifacts, 10k large and similar compiled IR versions, mixed reads and writes, concurrent version promotions, snapshot import/export, crash consistency, GC compaction, retention and tiering. Compare local filesystem/SQLite, commodity object store and ZEROS3 on cold start, memory, write amplification, read latency p95/p99, total bytes and maintenance overhead. Select using measured performance and fault behavior, not project affinity.

## Advanced caching product capabilities

Basic plan: correct immutable compiled cache, short history, safe invalidation and local working-set memory. Paid tiers: longer history, pinning, schema replay, remote synchronization, cross-device artifact transfer, hot/cold tier controls, advanced partial recompilation index, region/private cache and managed compatibility monitoring. Do not gate fundamental responsiveness or basic correctness. Hosted storage overages are metered with transparent retention and deletion controls.

## Billing interface

Use an internal `EntitlementService` and `UsageLedger` abstraction. Candidate payment provider (PSP, merchant-of-record or direct invoicing) selected via documented evaluation of tax handling, subscription upgrade/downgrade, proration, chargebacks, invoices, geographical payment methods, webhooks, refunds and security. **Do not assume Stripe**. Billing service should implement provider-specific adapter in one isolated module.

Usage events: immutable event_id, tenant, customer, environment, product capability, measured unit (browser-second, CPU-second if charged, retained GB-day, egress byte, premium storage operations if justified), meter version, job reference, rate-card version and reconciliation status. Use idempotent aggregation, replayable billing, non-billable infra failures, hard budget caps, user notifications and monthly bill preview.

## Subscription entitlements

- `core_transform`, `local_basic_cache`, `basic_version_history`, `desktop_ui`.
- `persistent_local_gateway`, `advanced_cache`, `history_retention`, `contract_pinning`, `history_diff_export`.
- `hosted_browser`, `scheduled_headless`, `private_deployment`, `team_templates`, `org_audit`, `customer_managed_keys`.

Never bind raw effect correctness or source authorization to payment entitlements. A disabled premium feature should fail clearly, never silently degrade an operation to unauthorized cloud execution.

## Release and deployments

- Extension signed packages and Web Store policy/security checks; staged rollouts, observed failures and rollback.
- Local desktop/companion installers for each OS, code signing/notarization as applicable, update mechanisms and uninstall/data export.
- Hosted: isolated workers, internal service network, database migrations, version promotion, rate limiting, secrets/KMS, incident response and backups.
- Private: customer-signed installation bundles, no outbound telemetry option, entitlement/license grace, health check and documented rollback window.
- All: SBOM, pinned dependency versions, supply-chain scans, compatibility tests with last supported runtime and IR versions.

## Operations controls

Metrics: cost per workflow, support min/customer, artifact cache hit rate, source operation success/unknown rate, queue wait p95, hosted browser idle utilization, compaction/GC debt, retention size by plan, billing ledger drift and source-specific failure rate. Provide kill switches for compromised primitive pack, source connector and contract version.

## Disaster recovery

Test database point-in-time restore, immutable artifact manifest reconstruction, key backup and revocation, user-owned local restoration, cached artifact tamper detection, export/import verified SHA digests, and rollback without rewinding external source actions. Measure RPO/RTO by hosted tier and publish after testing, not beforehand.

## Version entitlement downgrade and history lifecycle

Specify retention-by-tier as a published policy with user export and grace periods, reserved/legal-hold exclusions, pinned-version authorization and exact GC sweep after expiry. An entitlement downgrade must not cause unexpected data loss; notify before deletion, distinguish local user-owned versions from hosted storage, and test `VER-009`/`VER-010`. Payment-provider selection remains open and must include tax/merchant-of-record/invoicing as a documented ADR.

## PAYG pricing and charge transparency

**PAYG** applies primarily to hosted browser, on-demand headless compute, storage retention and explicitly contracted infrastructure. User-supplied local compute is not billed as rented browser time. Model bill previews for active sessions versus idling/suspended sessions; maintain cost alerts, hard limits, dispute reconciliation and credits for provider-side infrastructure failures. A commercial user may choose subscription plus PAYG, BYO infrastructure with a premium software license, or an enterprise commitment.
