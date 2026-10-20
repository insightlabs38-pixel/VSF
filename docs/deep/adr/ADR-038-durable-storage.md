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
7. **Objects are immutable and content-addressed behind `blob.Backend`.** Put is idempotent for identical bytes and refuses different bytes under a held key; keys are lower-case path segments that cannot climb out of a directory. The filesystem backend writes a temporary file, syncs it, renames it and syncs the directory, owns its root with an exclusive lock and clears a dead writer's temporary files when it reopens.
8. **Object before record, record before manifest.** An artifact's object is written before its metadata row, so a row never lacks its object; a crash between the two leaves an orphan object that garbage collection reclaims. A manifest becomes visible in one metadata transaction after every artifact it names has been read back and hashed, so a visible manifest is complete.
9. **Public and private objects are placed differently.** Public artifacts share one object across tenants (`pub/<digest>`); private artifacts live under the tenant (`ten/<tenant>/...`) and are never deduplicated across tenants.
10. **Manifests are signed by the storage layer with Ed25519 over canonical JSON.** The signature covers tenant, kind, version, parent, classification, each artifact's digest, size and class, provenance, time and key id. Reading a manifest checks its identity (digest of the stored bytes), the signature, the key against a trust set and a revocation list, and that the tenant in the document is the tenant asking. This is separate from the Rust-verified promotion of IR version manifests (ADR-034), which it does not replace.
11. **Backend choice, from `docs/deep/evidence/VSF-B13/storage-benchmark.md`** (200,000 tiny objects, 200 similar 512 KiB versions, one machine; ZEROS3 not measured):

| | filesystem | sqlite | s3 (MinIO) |
|---|---|---|---|
| tiny write p50 / p99 | 2.6 / 7.4 ms | 3.3 / 33.5 ms | 6.8 / 17.5 ms |
| tiny read p50 / p99 | 0.03 / 0.18 ms | 0.23 / 4.7 ms | 1.9 / 7.6 ms |
| mixed 70/30 p99 | 5.1 ms | 31.5 ms | 14.5 ms |
| large read p99 | 0.68 ms | 29.9 ms | 8.7 ms |
| bytes at rest / logical, tiny | 3.91x | 2.88x | 3.91x |
| device writes / logical, tiny | 4.3x | 33.3x | 8.7x |
| delete half, then maintain | 28.5 s | 48.1 s + 14.7 s | 358.5 s |
| cold start | 0 ms | 8 ms | 516 ms |
| kill -9 (5 runs): acknowledged lost / corrupt / torn | 0 / 0 / 0 of 8031 | 0 / 0 / 0 of 5807 | 0 / 0 / 0 of 4172 |
| similar versions at rest / logical | 1.00x | 1.00x | 1.02x |

The filesystem is the default local object backend: lowest latency at every percentile, no maintenance, instant start and no loss in the crash runs. SQLite is rejected for the hot path (33x device writes from one commit per object, large reads about 30 times slower) but is the most compact for tiny objects, so packing small objects is queued (VSF-B18-T06). The S3 dialect is kept for hosted and team use; it costs 2.4x to 60x the filesystem's latency for tiny objects and deletes slowly. Whole-object storage keeps similar versions at 1.00x, while content-defined chunking (16 KiB average) would hold 37% of the bytes on this synthetic corpus; whether real compiled artifacts share that much is open. ZEROS3 stays a candidate behind `blob.Backend` and is qualified by VSF-B18-T07 when an endpoint exists; the rule is that it must beat the filesystem or S3 numbers above on tiny-object p99 and bytes at rest, not that it exists.

## Consequences
- Adding SQLite adds about 10 modules to `go.sum`; they are covered by `go mod verify`, the vulnerability scan and the dependency review.
- Row-level security is enabled only for the application role; migrations run as the owner.
