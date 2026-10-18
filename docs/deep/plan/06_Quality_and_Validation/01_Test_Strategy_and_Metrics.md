# Quality program, compatibility benchmarks and release metrics

## General principle

No demo-only compatibility claims. Measure what percentage of **authorized, identified workflows** can be completed correctly, and separately how much of the UI is reconstructed, preserved, or programmatically exposed. Publish explicit source-app sample and exclusions. 99% is an aspirational gated target, not today's result.

## Test levels

1. Rust/TS/Go unit tests: parser and matcher properties, indexes, string/decimal/time handling, effect classification, security policy.
2. Primitive contract tests: positive and adversarial negative examples, recognition and generated output with golden semantic fixtures.
3. Compositional tests: nested frames, shadow DOM, virtual grids, editors, mutation race, modal inside table, RTL, high zoom, custom canvas.
4. Browser differential: run original source vs VSF with equivalent initial conditions, compare observable effects and source validation; record UNKNOWN if not provable.
5. Cross-runtime: extension, native companion, managed browser, headless/private fixture; environment negotiation, auth, state and version behavior.
6. Upgrade/migration: compiler/primitive/runtime/contract/version pin and backward-compatible artifact serialization.
7. Fault injection: timed-out writes, lost mutation event, stale cache, browser crash, auth expiry, source rate limits, blocked native bridge, storage outage.
8. User acceptance: time-on-task, errors, task completion, training burden, a11y keyboard/screen reader and locale accuracy.
9. Operational/performance: p50/p95/p99 and variability, RSS, CPU, disk, storage dedup benefit, source-vs-VSF latency and PAYG cost.

## Corpus splits

- **Synthetic benchmark:** adversarial source apps with test oracle and seeded behavior; include structured roles, hidden conditional behavior, unknown side effects and deliberately misleading markup.
- **Open-source applications:** real-world apps where automated authorized testing is permissible; version pinned.
- **Documented vendor/API fixtures:** contract-derived mock apps for cloud/scientific/ERP/CRM behavior; distinguish from real-vendor compatibility.
- **Held-out real applications:** not seen during rule creation, selected by architecture/domain/browser and with explicit permission; used to estimate generalization.

Use stratified sampling by source frontend style, complexity, app domain, data sensitivity, browser, auth mode and interaction type. Keep a frozen evaluation set; update with versioned methodology to avoid optimizing to the benchmark. Do not let many small similar sites dominate the denominator.

## Coverage formulas

- `WorkflowCorrect = correctly_completed_authorized_workflows / attempted_authorized_workflows` (with UNKNOWN separately counted, not success).
- `NativeReconstruction = source_visible_workflows presented natively and verified / eligible_visible_workflows`.
- `PreservedCoverage = original functions still accessible through preserved source surfaces`.
- `ProgrammaticCoverage = verified semantic operations exposed through API/CLI/MCP / discoverable authorized operations`.
- `AppCompatibilityAtThreshold(t) = apps with workflow correctness >= t / apps in specified sample`.
- Also report confidence intervals, worst app family, median and p10 app-level correctness, manual touches, source-version sensitivity, app changes per month and non-regression failures.

## Hard safety gates

- Zero confirmed cross-tenant permission leakage, secret logging, unauthorized source write or false-verified external effect in controlled adversarial suites.
- No automatic irreversible discovery step; all unknown side effects conservative.
- No automatic retry of unknown-commit write absent verified idempotency contract.
- Every release supports emergency revocation of a compromised primitive/plan and records affected versions.
- Security gates remain independent of subscription.

## Perf benchmark fixtures

| Test | Size | Evaluate |
|---|---:|---|
| Small site | 1k DOM nodes | cold compile and warm render |
| Medium site | 10k | indexed matcher CPU/RSS |
| Large enterprise site | 50k | mutation repair and p95 response |
| Stress site | 200k | peak memory, stack safety, cold throughput |
| Mutation storm | >=5k mutations/sec synthetic | delta sequence, backpressure and correctness |
| Multi-session source | >=100 simulated operations | fairness, locking, unknown commit |
| Version registry | 1k revisions / application fixture | CAS dedup, GC, diff, pinned history |
| Per-run budgets | controlled data | VSF overhead vs source latency |

These are configurable fixture sizes, not a claim that any real vendor has exactly that many nodes. P95 targets for ordinary local operations are proposed, not guarantees under pathological cases.

## Regression rules

Any rule/primitive modification runs dependent positive/negative/held-out tests. For trusted releases, no regression in certified workload correctness without explicit ADR/waiver. Performance budgets may have tolerance windows to account for hardware variability, but obvious p99 stalls require investigation. CI reports deltas in tasks solved, unsupported fallback rates and manual intervention counts.
