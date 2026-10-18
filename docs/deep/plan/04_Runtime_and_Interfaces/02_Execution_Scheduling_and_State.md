# Execution engine, resource-aware scheduler and consistency

## Execution plan representation

An operation comprises typed input validation, source-authorization precheck, required environment capabilities, fresh-state preconditions, source-facing execution steps, expected effects, progress events, verification strategy, ambiguity policy and estimated cost. Plans are signed/versioned artifacts validated before execution. The compiler cannot infer source idempotency without evidence.

## Execution state machine

`CREATED -> PREPARED -> ADMITTED -> RUNNING -> (VERIFIED | FAILED | UNKNOWN_COMMIT | CANCELED | EXPIRED)`; unknown-commit can reconcile to verified/failed or require human review. Cancellation is best-effort and does not imply rollback of external effects. Separate long-running task handles from synchronous requests; MCP Tasks if client extension support is negotiated.

## Scheduler queues

- **Interactive local**: renderer-only and simple source operations, latency priority.
- **Source execution**: read/write paths with session/record locks and source-provider rate budgets.
- **Compiler discovery**: CPU/RSS-limited, lower priority and preemptible at safe checkpoints.
- **Background maintenance**: version checks, dedup GC, stale artifact revalidation, reporting, audit export.
- **Hosted worker**: per org/client quotas, scheduling weights, cold-start budget and PAYG cost caps.

Go scheduler uses bounded weighted-fair admission with explicit CPU, memory, browser contexts, per-host requests and network egress tokens. Avoid unbounded goroutines and repeated active polling. Distinguish browser worker lease from source session lease. A session can require strict serialization even where multiple workers exist. Permit read parallelism only after testing.

## Source-backed consistency classes

- `LOCAL_ONLY`: renderer state with no source effect.
- `EVENTUAL_READ`: cached snapshot with recorded freshness and invalidation channel.
- `FRESH_READ`: authoritative source fetch required.
- `REVISION_GUARDED_WRITE`: compare-and-set/etag/record version if source supports it.
- `UNGUARDED_WRITE`: original source workflow with highest achievable verification.
- `IRREVERSIBLE_OR_UNKNOWN`: confirmation and no auto-retry absent explicit evidence.

Use source events/webhooks if documented, otherwise poll on explicit budget and invalidation trigger. Local optimistic UI may reflect pending state but never persist it as confirmed remote success without verification. No generic cross-service distributed transaction guarantee.

## Rate limits and economics

Every operation returns `execution_class`, estimated external actions, costed runtime class and source rate-limit policy. API gateway enforces user/org entitlements and adaptive per-source quotas without promising upstream speedups. Hosted metering measures occupied browser duration, worker CPU/memory, storage retention, egress and chargeable retries according to a transparent policy. Zero-charge failed startup/infra retries can be distinguished from source operation retries.

## Failure drills

1. Browser job crashes after submitting a form, before acknowledgement: status UNKNOWN_COMMIT; reconciliation/approval path, no duplicate submission.
2. MFA challenge mid headless job: PAUSED_AUTH_REQUIRED, user receives instruction, job handle retained within configured expiry.
3. Source app changes DOM during execution: invalidate plan, attempt safe re-recognition only at a checkpoint, stop if binding ambiguous.
4. Two clients update same row: use revision check or detectable conflict; if source offers neither, explicit last-write-wins risk disclosed and audit.
5. Hosted worker reaches budget: stop admitting new steps, attempt safe checkpoint, charge only actual metered usage and report unknown source effects correctly.
6. Native companion disconnect: clients reauthenticate/repair pairing, leases expire, operations don't silently execute through unauthenticated port.

## Observability and SLO decomposition

`T_total = T_gateway + T_queue + T_client + T_compiler/plan + T_source + T_verify + T_return`. Record per-segment traces without sensitive data payloads. p50/p95/p99 by environment and app family; measure stall count, false success count (must be zero in verified suite), unknown commit rate, CPU-s and RSS per execution, queue starvation, exception rate, user cancellation and task completions.
