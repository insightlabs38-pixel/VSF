# ADR-033: Verified deterministic execution

Status: accepted (B07, gate G7). Code: `backend/execution`; Rust authority: `vsf_ir::api::authorize_report`; TypeScript gate: `@vsf/wasm-host` `validate`.

## Context
B05 and B06 can say what an operation is and when it may run. B07 runs it without claiming more than the source showed: a write is Verified only after an authoritative read, and anything ambiguous stays Unknown.

## Decisions
1. **One authority for "may run".** Go and TypeScript decoders return `Unvalidated`. Rust validates the contract and checks its witnesses against the exact environment (`authorizes_execution`) through a new native-worker operation `X`; only that report mints `Authorized` (Go) or `Validated` (TypeScript, from the WASM report). Both sides must agree on the document digest. Go has no copy of the witness rules. Authorization is repeated just before the side effect.
2. **Lifecycle.** `prepared -> admitted -> running -> verified | failed | unknown`; `unknown -> verified | failed` only through reconciliation. The journal is append-only and hash-chained; replay refuses a broken chain, a gap or an illegal transition. A restart settles leftovers: running becomes `unknown/UNKNOWN_COMMIT`, earlier states become failed.
3. **Binding.** Caller scopes and entitlement, session ownership, authentication, expiry, source scopes, application, variant class, version range and the pinned contract version and digest are checked before any source contact, and again before the side effect (a revoked role stops a queued run).
4. **Preconditions.** `source_read_required` reads and compares the version the caller saw; `observation_within` bounds the age; the write is conditional on that version. A source that requires confirmation propagates it; policies that always prompt need one at admission. Confirmations are HMAC-bound to one request digest, expire, and work once.
5. **Executors and taxonomy.** Each executor reports `committed | rejected | conflict | throttled | unavailable | unknown`; transport errors after anything was sent are `unknown`. The documented-API executor and the Chromium executor (DevTools protocol, no dependency) give identical outcomes on equivalent fixtures, including the same source state.
6. **No hidden retries.** The runtime retries only outcomes the source proved changed nothing (`throttled`; `unavailable` with idempotency evidence or a dedup token), with bounds, backoff and the source's hint. Ambiguous writes are never retried, whatever the contract. The HTTP client's own replay and redirects are disabled.
7. **Distrust no-effect claims.** A `rejected` or `conflict` result is downgraded to `unknown` when a fresh read shows a new version already holding the expected values.
8. **Long tasks and cancellation.** Handles expose state and ordered progress events. Cancelling before the write is a real cancellation; during the write the result is `unknown` and the handle says the effect is unknown; after the commit the run still verifies.
9. **Locks and budgets.** Serialization follows the contract's concurrency scope (session, session and record, source). Calls per source are capped and paced; a refusal doubles the pacing interval and honours the retry hint, sustained success shortens it.
10. **UNKNOWN_COMMIT.** A resubmitted request returns the existing operation; a new request for the same session, operation and record is refused until reconciliation. `Reconcile` verifies, fails or reports inconclusive from an authoritative read.

## Found by running against a real browser
Chromium resends a POST when a reused connection drops; if the first attempt committed, the page reports a conflict. Go's HTTP client replays writes that carry an idempotency key. Both would have recorded a committed write as failed or hidden an ambiguity; decisions 6 and 7 are the fixes, each with a regression test.

## Limits
- The journal detects edits and gaps but not deletion of its last lines without an external anchor.
- Locks and the budget are per process. Session lookup is an interface; real sessions come with pairing.
- Reconciliation treats an unchanged version as proof of no write, so it needs monotonic version tokens.
- A browser plan relies on the source's own conditional write; a page that sends unconditional writes can be resent by the browser.
- The TypeScript brand cannot stop code in another realm that bypasses the module.

## Migration and rollback
No schema change. The native worker gains operation `X`; older workers answer `serve.unknown_op` and execution refuses. TypeScript and Go decoders now return `Unvalidated` (callers use `unsafe()` / `Unsafe()`). Rollback: revert the B07 merge; nothing stored depends on it.
