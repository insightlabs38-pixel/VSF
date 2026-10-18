# Execution policy: canonical VSF task queue

## Scope, authority and workflow

- The source plan is authoritative for architecture, gates and product boundaries. This task queue makes the existing B00–B24 implementation steps executable; it does not replace architectural decisions or grant permission to weaken release gates.
- **One coherent batch per invocation.** Initially start B00; later resume the earliest unfinished task in the earliest unfinished eligible batch. No speculative leap to later batches. Re-open blocked dependencies rather than skipping them.
- Status lifecycle: `TODO → READY → ACTIVE → REVIEW → DONE`, with `BLOCKED` or `CORRECTIONS` where necessary. `READY` is dependency-derived; do not bulk-mark later tasks READY without prerequisite completion. Use the exact stable task ID, never reorder IDs to obscure history.
- B00–B10 are task-level committed foundations. B11–B24 are **outcome-driven provisional tasks**: before beginning a later batch, review current failure clusters, capability gaps, ADRs, requirements and prior gates. Modify its task-level design *within its documented goal*; append newly scoped `VSF-Bnn-Txx` tasks with rationale and edges; do not falsely claim frozen details can be known years in advance.
- Tasks are sequential within each batch by default, even where source code is independent. Parallel work is permitted only on explicitly nonconflicting, approved lanes with isolated branches/worktrees and one schema owner. Do not discover, message, control or inspect other agent sessions.
- Scope expansion and architecture changes: add an ADR and a queued task when they affect shared contracts, compatibility, security, performance, release, business assumptions or the primitive registry. Fix small local defects within the current task; never hide systemic gaps.

## Canonical working records

- When establishing the implementation repository, copy this queue into `docs/deep/queue/` and maintain a concise root `TASKS.md` for batch/task status and the next executable ID. Prefer `docs/deep/queue/state.json` for authoritative machine-readable receipts and dependencies. This planning archive's queue is a bootstrap snapshot, not a live tracker once development begins.
- `docs/deep/reports/VSF-Bnn.md`: **fewer than 25 physical lines total** (24 maximum), summarizing batch outcome and pointing to test artifacts, commits, decisions and blockers. If there is no deep docs folder, create it. Large test logs/trace artifacts belong under `docs/deep/evidence/VSF-Bnn/`, not inside a short report. Don't add verbose routine reports elsewhere.
- Each task has a machine-readable receipt with ID, predecessor status, files changed, test commands/results, negative fixture, code/contract and primitive IDs changed, performance/size deltas if applicable, security/version impact, commit SHA, and reviewer outcome. Use compact JSON or JSONL, not lengthy prose. Only mark `DONE` after acceptance and evidence are recorded.
- The final user-facing handoff is concise and states branch, commit/PR, tasks/gate, tests, limitations, next eligible batch. End after **one batch** even if more work is available.

## Evidence and gates

- G0–G24 are binding; a batch cannot be marked DONE until *all* task acceptances, focused/adversarial checks and the batch's source plan acceptance are met. Relevant G0/G1/G2... regression gates continue to apply whenever affected. Record a decision `PROCEED | HOLD | REVISE | REVERT` and preserve evidence.
- Use targeted inspections: `rg --files`, `rg -n`, `git grep`, `sed -n 'start,endp'`, `tail -n`, `git status`, `git diff`. Avoid recursively dumping entire files/trees; never read unrelated agent sessions. Read only relevant plan sections before coding.
- Prefer focused tests first, then impacted package checks, then milestone/full regression where appropriate. Write adversarial fixtures, deterministic seeds and performance comparisons; do not fake or silently skip tests. State environment limitations.
- No application-specific hardcode solely to pass a demo; any exception must be documented as an explicit adapter subject to review and independent compatibility tests.
- No destructive repo action, force push, history rewrite, credential exfiltration, security bypass, or optimistic retry of uncertain write effects. Source permissions and original approvals remain authoritative.

## Coding, Git and PR hygiene

- Git author/committer identity in this repository: `insightlabs38-pixel` / `insightlabs38@gmail.com`. Set local repo config and verify before committing; do not change global config.
- Make cohesive commits at completed testable checkpoints. Push an appropriately scoped branch and open/update a PR when a configured remote and permission exist; never invent a PR or assert merge. If repository or remote is not present, do the best local work and report the blocker.
- Commit subject/body, PR title/body and PR comments are **strictly fewer than 10 physical lines each**. Keep concise technical statements; do not add AI tool attribution, co-author lines, generated-by notices, tracking/link footers or provider signatures. Commit as the owner only.
- Source-code comments: **strictly fewer than 2 physical lines each, and only when essential to a nonobvious invariant**; prefer clear names and tests. Keep documentation useful and substantive without forcing an artificial code-size/LOC ceiling.
- No claim that all 924 catalog entries are implemented: registry inventory is not conformance. Record maturity and independent tests per capability.
- Maintain source/test table of contents or other repository navigation index where necessary, without boilerplate or unnecessary duplication.

## Plan gating and scope variations

- B24 may scope release to an explicitly chosen subset of capabilities, but cannot silently waive fundamental security, correctness or source-permission requirements. Any deferred B20–B23 deliverable remains visible, not marked DONE. Use an ADR explaining exactly which release cohort/mode is being certified.
- A `BLOCKED` task stops the current batch; generate a concise blocker/owner-decision packet in `docs/deep/evidence/`, and exit. The next invocation must resolve or obtain authorization for that same blocking decision, not bypass it.
- Paid entitlement requirements: keep the same accurate core primitive engine on all tiers; higher plans may gate extended version retention/pins, advanced caching, persistent localhost gateway and private infrastructure. Hosted sandbox/headless compute can be PAYG; billing provider is not predetermined.
- Rollback of a compiled artifact is not a guarantee that the source application data is rolled back. A security-revoked artifact cannot be revived by a historic pin.
