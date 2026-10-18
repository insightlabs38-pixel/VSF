# Architecture Decision Records (initial)

All are **provisional accepted-for-prototyping**, subject to evidence gates and recorded changes. An ADR change requires supporting benchmark/security/compatibility evidence, a migration plan and owner.

| ADR | Decision | Rejected alternative / reason | Revisit trigger |
|---|---|---|---|
| ADR-001 | Source app remains authoritative | VSF replicated data as system of record | Validated offline-first data contract demand |
| ADR-002 | Deterministic compiler by default | LLM on every interaction | Novel operations empirically require optional assistive discovery |
| ADR-003 | Rust structural/semantic core | All analysis in JS or Go | Real end-to-end profiling shows boundary overhead dominates |
| ADR-004 | TS observation and renderer | Rust direct DOM FFI | Browser JS APIs are native and high-frequency FFI is costly |
| ADR-005 | Go control plane | Rust microservices for everything | One-process maintainability or latency evidence favors consolidation |
| ADR-006 | Multi-source semantic evidence | DOM-only inference | No revisit; hard architectural requirement |
| ADR-007 | Large mixed candidate capability inventory | Small fixed widget list | Measurement changes ranking; not eliminate long-tail plan |
| ADR-008 | Safe preservation fallback | Attempt native reproduction of unknown widgets | When source component can be proven safely reconstructed |
| ADR-009 | Separate caller/source identities | Implicit reuse of browser auth for API | Never without explicit new security review |
| ADR-010 | Native renderer + selective specialist libs | Blanket no-dependency policy | Correctness/maintenance tradeoff demonstrated |
| ADR-011 | Extension packaged interpretation only | Remote executable operation DSL | Relevant store policy approval or native mode alternative |
| ADR-012 | Modular six-mode execution | One universal sandbox | Impossible to meet users' privacy/latency modes otherwise |
| ADR-013 | Independent multi-axis versioning | Single `v1` for everything | Never; source and contract change independently |
| ADR-014 | Version history differentiated by paid tiers | Versioning omitted or enterprise-only | Pricing tests and retention cost economics |
| ADR-015 | SQL hot metadata + blob storage | All state in object store | Real benchmark dictates alternate |
| ADR-016 | ZEROS3 as candidate immutable store | Mandatory unbenchmarked dependency | POC storage benchmark complete |
| ADR-017 | Provider-neutral billing abstraction | Hardwire Stripe | Provider selection by regional/tax/invoicing requirements |
| ADR-018 | Protocol-first *when supported* | Scrape undocumented private APIs universally | Vendor policy/security and documented API evidence |
| ADR-019 | User-selectable, policy-constrained routing | Silent failover to public cloud | Never without explicit enterprise exception |
| ADR-020 | Bounded scheduler/queues and fairness | Unbounded goroutines/browser processes | Capacity benchmark and source safety requirements |
| ADR-021 | Local-first/privacy-minimizing telemetry | Mandatory upload of source DOM | Never by default for sensitive applications |
| ADR-022 | Source-compatible version pins with explicit degradation | Promise forever-working old functionality | Impossible where source changed; expose status |
| ADR-023 | Benchmark uses held-out source apps | Test only known app demos | Prevent self-deceptive progress claims |
| ADR-024 | Efficiency > minimal LOC | Optimize for one-file/zero-dependency aesthetic | Never, unless measurements justify a refactor |

## ADR update procedure

1. State the current invariant and boundary.
2. Present measured alternatives on representative cases.
3. Evaluate correctness, latency, memory, maintenance, operational risk, security and capability scope.
4. Record impacts on versioned IR/contracts and existing users.
5. Require pass of targeted tests, affected security checks, and measured no-regression budgets.
6. Publish migration/rollback notes and mark old ADR superseded, never silently rewrite history.
