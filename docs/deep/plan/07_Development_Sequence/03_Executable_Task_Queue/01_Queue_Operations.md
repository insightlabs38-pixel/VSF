# Queue operations and task receipt contract

## State and eligibility

**Initialization:** `state.bootstrap.json` is a reference snapshot. On first real run, copy to `docs/deep/queue/state.json` and create root `TASKS.md`; never overwrite live state with bootstrap data. Preserve original `tasks.json` and detailed queue pages under `docs/deep/queue/` for offline inspection.

A task is *eligible* when its prerequisites are `DONE` and the prerequisite batch gates are accepted. Its explicit status should be `READY` or `ACTIVE`, unless it is already `DONE`. The **selected task** is the lowest ordered unfinished eligible ID in the earliest unfinished required batch. Any `BLOCKED`, `CORRECTIONS`, or `REVIEW` task in that earlier batch retains priority until resolved. One invocation processes at most one whole batch.

- `TODO`: known, not started. `READY`: prerequisites passed, unclaimed.
- `ACTIVE`: claimed by current execution; record branch/base commit.
- `REVIEW`: implementation exists, verification incomplete or under review.
- `CORRECTIONS`: evidence rejected or a fix is needed; do not mark completed.
- `BLOCKED`: hard dependency, unapproved decision or failing invariant; stop.
- `DONE`: acceptance is verified and a durable receipt identifies code and evidence.
- Gate `PENDING | ACCEPTED | BLOCKED | REVISE | REVERT` governs batch progress. The final decision shown to users is `PROCEED | HOLD | REVISE | REVERT`. Only `ACCEPTED/PROCEED` opens dependent batches.

## Task receipt example

Store receipts under `docs/deep/queue/receipts/VSF-Bnn-Txx.json` or append equivalent structured records to `state.json`; do not store raw secrets, business records, browser sessions or unredacted source DOM in receipts.

```json
{
  "task_id": "VSF-B00-T01",
  "status": "DONE",
  "branch": "vsf/b00-foundation",
  "base_commit": "<actual git SHA>",
  "commit": "<actual git SHA>",
  "paths": ["<real changed path>"],
  "acceptance": "<criterion verified against real test>",
  "positive": {"fixture": "<fixture ID>", "command": "<command>", "result": "PASS"},
  "negative": {"fixture": "<fixture ID>", "command": "<command>", "result": "PASS"},
  "regression": [{"command": "<command>", "result": "PASS"}],
  "versions": {"schema": "<version or unchanged>", "primitive": "<IDs or none>"},
  "security_and_source_permissions": "<effect, unchanged, or limitation>",
  "performance_memory_storage": "<numbers + units where measured; otherwise not measured>",
  "known_limitations": [],
  "evidence_paths": ["docs/deep/evidence/VSF-B00/<verified log>"],
  "review_result": "ACCEPTED"
}
```

The angle-bracket values above are placeholders **only**; never treat the example as an actual completed receipt. Use actual local Git SHA and tests. A task can be closed only when acceptance tests and evidence are genuine. For exploratory or ADR tasks, a deterministic reference check, executable reproducibility assertion or documented negative example may substitute for a normal unit test, with justification.

## Gate closing

1. All currently scoped task receipts for that batch are `DONE`, with dependencies and prior gates satisfied.
2. Run the batch adversarial matrix, plus affected milestone/full tests. Confirm source permissions, versioning, code sign/deployment/policy constraints as applicable.
3. Record reproducible evidence and specific `PASS`, `FAIL`, `SKIPPED` with reason; a required unexecuted check is not a pass.
4. Update root `TASKS.md`, `state.json`, the batch gate, and the relevant ADR/migration files. Check the next eligible ID without beginning it.
5. Produce one concise report at `docs/deep/reports/VSF-Bnn.md` with **at most 24 physical lines**. One branch, cohesive commits and, if available and authorized, one scoped PR.

## Report skeleton: fewer than 25 lines

```text
Batch: Bnn | Gate: Gnn | Decision: PROCEED/HOLD/REVISE/REVERT
Branch: <actual> | Base: <SHA> | Commit: <SHA>
PR: <actual URL or none>
Closed tasks: <exact IDs>
Contracts/primitive IDs: <changed IDs, version impact>
Changed modules: <concise paths>
Tests: <commands, numerical pass/fail outcomes>
Negative/adversarial: <fixtures/results>
Performance: <measured VSF-only latency, memory/storage deltas or n/a>
Security/session scope: <verified invariant or issue>
Source compatibility: <supported/partial/unknown and fallback>
Version/pin/retention: <verified effect>
Known limitations: <compact factual statement>
Evidence: <paths under docs/deep/evidence/VSF-Bnn/>
Next eligible: <exact task ID or blocked decision>
```

## Message discipline

Commit and PR messages **must each have fewer than 10 physical lines** including title; use 1–3 descriptive lines wherever possible. A PR comment, if essential, must likewise be under 10 lines. Source comments are a single line and only for nonobvious invariants. No automatic assistant/provider attribution, co-author or links/signatures appended as footers. Use repository-local Git identity `insightlabs38-pixel <insightlabs38@gmail.com>` for both author and committer.

## Maintaining the queue

For a newly discovered generalized prerequisite, append an ID in the affected batch (`VSF-Bnn-T09`, etc.) or create an ADR-approved remediation batch with a stable ID, explicit dependencies and tests. Do not renumber completed work. Any new task becomes `TODO` until evidence permits `READY`. Later conditional deliverables must be decomposed against observed blockers, and explicit feature deferrals must remain visible in launch-scope records rather than being misreported as implemented.
