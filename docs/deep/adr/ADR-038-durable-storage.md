# ADR-038: Durable metadata and artifact storage

Status: in progress (B13). Code: `backend/storage`. Extends ADR-025 (dependencies) and ADR-034 (version registry).

## Context
B08 keeps versions in a directory of files. B13 makes metadata and immutable artifacts durable, tenant-isolated and replaceable, and chooses an artifact backend from measurements, not affinity (ADR-016).

## Decisions
1. **One third-party Go dependency: `modernc.org/sqlite` v1.44.3.** ADR-025 said Go standard library only; SQLite has no standard-library driver. The pure-Go driver keeps `CGO_ENABLED=0` builds (the repository's reproducible-build rule) and needs Go 1.24, which the toolchain pin allows (v1.55 and later require 1.25). Its transitive modules are all BSD or MIT. `golang.org/x/sys` 0.37.0 carries GO-2026-5024, a Windows-only function that no code here calls; the fix needs Go 1.25, so the advisory is ignored until 2027-01-15 in `supply-chain/vuln-ignores.json` and revisited with the toolchain bump.
2. **One logical model, two dialects.** `storage/meta` holds tenants, artifact records, signed manifests with their artifact links, version roots with an append-only history, pins, legal holds, worker leases, recovery intents and (from schema 2) manifest parents and annotations. SQLite implements `meta.Store` in Go; PostgreSQL ships the same schema as migrations and a generated psql script (`PostgresScript`). A hosted Go driver is not added until the hosted service exists (B15), so the PostgreSQL leg is tested at the SQL level against a real cluster.
3. **Migrations are numbered, immutable and checked.** Each applied step records its SHA-256; a changed step is refused (`ErrMigrationDrift`), a database from a newer version is refused (`ErrSchemaTooNew`), and an interrupted step leaves nothing behind (one transaction each, re-checked under the writer lock).
4. **Tenant isolation is structural.** Every key starts with `tenant_id`, every foreign key is composite and includes it (a manifest cannot link another tenant's artifact, a root cannot point at another tenant's manifest), and every method takes a validated tenant. PostgreSQL adds forced row-level security keyed on `vsf.tenant` for a non-owner application role that cannot alter the schema or the migration history. RLS trusts the data layer to set the tenant per transaction; it does not defend against a compromised application process.
5. **SQLite settings.** WAL, `synchronous=FULL` by default, foreign keys on, one serialized immediate-mode writer and a pool of read-only readers; a commit survives a process kill without a close.
6. **A manifest id is the digest of its document**, checked by the store, so a record cannot claim an identity its bytes do not have.

## Consequences
- Adding SQLite adds about 10 modules to `go.sum`; they are covered by `go mod verify`, the vulnerability scan and the dependency review.
- Row-level security is enabled only for the application role; migrations run as the owner.
