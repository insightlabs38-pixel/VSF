# VSF task queue

Derived from `docs/deep/queue/state.json` by `node tools/queue/src/cli.ts render`; do not edit by hand.

**Next eligible:** `VSF-B09-T01`

| Batch | Gate | Tasks | Status | Decision |
|---|---|---:|---|---|
| B00 | G0 | 7/7 | DONE | PROCEED |
| B01 | G1 | 7/7 | DONE | PROCEED |
| B02 | G2 | 8/8 | DONE | PROCEED |
| B03 | G3 | 8/8 | DONE | PROCEED |
| B04 | G4 | 8/8 | DONE | PROCEED |
| B05 | G5 | 8/8 | DONE | PROCEED |
| B06 | G6 | 18/18 | DONE | PROCEED |
| B07 | G7 | 14/14 | DONE | PROCEED |
| B08 | G8 | 10/10 | DONE | PROCEED |
| B09 | G9 | 0/9 | IN_PROGRESS | - |
| B10 | G10 | 0/8 | TODO | - |
| B11 | G11 | 0/6 | TODO | - |
| B12 | G12 | 0/6 | TODO | - |
| B13 | G13 | 0/5 | TODO | - |
| B14 | G14 | 0/5 | TODO | - |
| B15 | G15 | 0/5 | TODO | - |
| B16 | G16 | 0/4 | TODO | - |
| B17 | G17 | 0/5 | TODO | - |
| B18 | G18 | 0/5 | TODO | - |
| B19 | G19 | 0/5 | TODO | - |
| B20 | G20 | 0/4 | TODO | - |
| B21 | G21 | 0/3 | TODO | - |
| B22 | G22 | 0/4 | TODO | - |
| B23 | G23 | 0/4 | TODO | - |
| B24 | G24 | 0/5 | TODO | - |

### B00 · Build foundation and reproducible validation

- [x] `VSF-B00-T01` Create pinned Rust/Go/Node version manifest and lockfiles
- [x] `VSF-B00-T02` Build an empty cross-language schema integration smoke test
- [x] `VSF-B00-T03` Set up Playwright synthetic source app and minimal WASM smoke fixture
- [x] `VSF-B00-T04` Add deterministic golden fixtures and secret-free test logs
- [x] `VSF-B00-T05` Configure lints, formatting, supply-chain scans, dependency review, reproducible artifact digest
- [x] `VSF-B00-T06` Implement CI matrix with mandatory artifacts and failure summaries
- [x] `VSF-B00-T07` Create evidence bundle template for every subsequent batch

### B01 · Validate browser and language boundaries

- [x] `VSF-B01-T01` Build a packaged extension that can read permitted basic DOM snapshot
- [x] `VSF-B01-T02` Demonstrate permissions origin-by-origin and prevent page-origin misuse
- [x] `VSF-B01-T03` Observe MV3 service worker teardown/restart and preserve no sensitive secrets in global state
- [x] `VSF-B01-T04` Pass compact typed arrays between TS and Rust/WASM with repeatable hashes
- [x] `VSF-B01-T05` Benchmark capture versus parse versus transport, including 1k/10k/50k nodes
- [x] `VSF-B01-T06` Compare Node/browser/Go boundary options for native compiler execution
- [x] `VSF-B01-T07` Prototype isolated service bind with origin/CSRF/rebinding check

### B02 · Define and validate versioned IR contracts

- [x] `VSF-B02-T01` Specify EnvironmentCapabilityManifest, ObservationIR, SemanticIR, OperationContract, PresentationIR, VersionManifest
- [x] `VSF-B02-T02` Define unknown/conflict and effect taxonomy with conservative defaults
- [x] `VSF-B02-T03` Build deterministic canonical encoding and hash fixtures, including UTF-8/decimal/time cases
- [x] `VSF-B02-T04` Generate TS/Go/Rust bindings and round-trip interoperability tests
- [x] `VSF-B02-T05` Describe application tenant/role/locale/source-variant identity
- [x] `VSF-B02-T06` Draft compiler/primitive/contract/runtime/source/presentation version matrix
- [x] `VSF-B02-T07` Add compatibility/test witnesses to operation contract model
- [x] `VSF-B02-T08` Author schema compatibility and promotion policy ADR

### B03 · Build primitive registry and conformance harness

- [x] `VSF-B03-T01` Import and classify all 924 catalog entries as atomic/composite/adapter/service
- [x] `VSF-B03-T02` Assign stable ownership, dependencies and required evidence/test semantics
- [x] `VSF-B03-T03` Define primitive registration interface and negative-match semantics
- [x] `VSF-B03-T04` Implement graph closure verification and cycle detection
- [x] `VSF-B03-T05` Create isolated positive/negative fixture generation format
- [x] `VSF-B03-T06` Add coverage dashboard by maturity, source app and source-operation count
- [x] `VSF-B03-T07` Build signed/versioned package manifest and primitive digest
- [x] `VSF-B03-T08` Demonstrate two reusable compositions with independently tested inputs

### B04 · Implement browser observation and incremental events

- [x] `VSF-B04-T01` Implement document/frame/shadow-root enumerators including opaque boundaries
- [x] `VSF-B04-T02` Capture role/name/labels and selected geometry lazily
- [x] `VSF-B04-T03` Implement mutation batching with sequence IDs and missing-event resync
- [x] `VSF-B04-T04` Track locale, viewport and feature flags where observable
- [x] `VSF-B04-T05` Mark source scopes and redact likely sensitive text before diagnostics
- [x] `VSF-B04-T06` Define schema for source protocol/API evidence rather than undocumented traffic
- [x] `VSF-B04-T07` Build stable fixture runner across SPA and traditional multi-page navigation
- [x] `VSF-B04-T08` Report collector CPU/RSS and cold/steady-state overhead

### B05 · Build semantic recognition and evidence engine

- [x] `VSF-B05-T01` Create feature bitset/inverted index; test against naive full-scan baseline
- [x] `VSF-B05-T02` Implement structural relationships, field/label binding and operation hypothesis pipeline
- [x] `VSF-B05-T03` Add evidence ledger with claim-level provenance
- [x] `VSF-B05-T04` Build dependency graph and incremental invalidation closure
- [x] `VSF-B05-T05` Implement lexical/structural/role candidate matching without using model calls
- [x] `VSF-B05-T06` Produce UNKNOWN and CONFLICTED when semantics cannot be demonstrated
- [x] `VSF-B05-T07` Support protocol evidence reconciliation with UI evidence in fixture
- [x] `VSF-B05-T08` Demonstrate primitive fix helping two independent held-out apps

### B06 · Build presentation compiler and minimal accessible renderer

- [x] `VSF-B06-T01` Define PresentationIR constraints and reusable field/grid/nav/form components
- [x] `VSF-B06-T02` Implement light/dark/system tokens and blue/mint accents; status colors distinct
- [x] `VSF-B06-T03` Add keyboard/focus restoration/ARIA announcements and basic RTL handling
- [x] `VSF-B06-T04` Build keyed incremental rendering and local state subscriptions
- [x] `VSF-B06-T05` Add basic compact/spacious density, navigation and command palette
- [x] `VSF-B06-T06` Preserve source widgets in place/side-by-side when unsupported and permitted
- [x] `VSF-B06-T07` Apply per-source styles without destructive DOM changes
- [x] `VSF-B06-T08` Benchmark pure UI operation responsiveness and memory
- [x] `VSF-B06-T09` Queue correction lane and G6 closure rule (queue tooling)
- [x] `VSF-B06-T10` F1 Credential classification and sensitive-data boundaries (observer)
- [x] `VSF-B06-T11` F3 Evidence-ledger identity and provenance (recognizer)
- [x] `VSF-B06-T12` F7 Trust context, revocation and version-invalidation foundations (IR 1.1.0 additive)
- [x] `VSF-B06-T13` F2 Protocol evidence authority and complete validation (recognizer)
- [x] `VSF-B06-T14` F4 Typed behavioural witness contract (recognizer)
- [x] `VSF-B06-T15` F5 Separate intent, declared effect, demonstrated effect and authorization (recognizer)
- [x] `VSF-B06-T16` F6 CI gate integrity and reproducible vulnerability checking
- [x] `VSF-B06-T17` F8 Foundational review findings (B00-B06 interfaces)
- [x] `VSF-B06-T18` Foundation-stabilization ADR, migrations and closing evidence

### B07 · Implement verified deterministic execution

- [x] `VSF-B07-T01` Implement prepared/admitted/running/verified/failed/unknown states
- [x] `VSF-B07-T02` Bind VSF caller, source session, environment and exact contract
- [x] `VSF-B07-T03` Add preconditions, source-read freshness check, confirmation propagation
- [x] `VSF-B07-T04` Implement browser executor and mock documented API executor
- [x] `VSF-B07-T05` Implement safe retry policy requiring evidence of idempotency
- [x] `VSF-B07-T06` Provide long-task handle, cancellation and progress events
- [x] `VSF-B07-T07` Add session-scoped locks and basic rate-limit adaptation
- [x] `VSF-B07-T08` Inject crash/timeout after remote commit and record UNKNOWN_COMMIT
- [x] `VSF-B07-T09` Execution consumes only Validated<T> minted by Rust validation (F8 finding)
- [x] `VSF-B07-T10` Browser fixtures resolve the complete module graph and report load failures (wasm-browser finding)
- [x] `VSF-B07-T11` Unresolved-write exclusion is atomic with admission to Running (concurrency finding)
- [x] `VSF-B07-T12` Reconciliation revalidates authorization and closes only on evidence (reconciliation finding)
- [x] `VSF-B07-T13` Execution-boundary review corrections (adjacent findings)
- [x] `VSF-B07-T14` ADR-033 corrections, regenerated evidence and CI agreement

### B08 · Build version registry, semantic diff and pins

- [x] `VSF-B08-T01` Implement content-digest immutable manifest and version lineage DAG
- [x] `VSF-B08-T02` Create deterministic IR diff and classify cosmetic/additive/breaking/permission/unknown changes
- [x] `VSF-B08-T03` Compute impacted dependency closure and targeted test selection
- [x] `VSF-B08-T04` Support contract-version pin and compatibility state on source drift
- [x] `VSF-B08-T05` Implement local version storage, promotion transaction and artifact hash verification
- [x] `VSF-B08-T06` Create security revocation that overrides old pin
- [x] `VSF-B08-T07` Test old contract plus new source feature, missing required field and rollback
- [x] `VSF-B08-T08` Add draft diff result format for future Studio/CLI UI
- [x] `VSF-B08-T09` Signed manifests carry monotonic sequence and expiry; trust set is injected out of band (F8 finding)
- [x] `VSF-B08-T10` UI architecture contract and PresentationIR layout extension (UI checkpoint)

### B09 · Integrate extension and secure local bridge

- [ ] `VSF-B09-T01` Package MV3 executable logic, narrow origin permissions and user activation _(ACTIVE)_
- [ ] `VSF-B09-T02` Connect browser observation and IR compiler/renderer behind stable message protocol
- [ ] `VSF-B09-T03` Implement reconnect/restart after service worker death
- [ ] `VSF-B09-T04` Prototype authenticated pairing with native companion
- [ ] `VSF-B09-T05` Deny untrusted webpage access to native gateway and exercise DNS-rebinding/CSRF fixture
- [ ] `VSF-B09-T06` Bind local worker capability manifest and source identity
- [ ] `VSF-B09-T07` Add local encrypted settings and emergency fallback to source UI
- [ ] `VSF-B09-T08` Create extension distribution policy checklist and reviewer-friendly architecture
- [ ] `VSF-B09-T09` Integrated VSF shell and reference gallery with screenshot regression (UI checkpoint)

### B10 · Expose API, CLI and MCP vertical slice

- [ ] `VSF-B10-T01` Define API error taxonomy, contract version routing, OpenAPI doc generation
- [ ] `VSF-B10-T02` Implement Go CLI typed argument grammar, structured output and version pin flag
- [ ] `VSF-B10-T03` Implement minimal MCP tools/resources with protocol version negotiation
- [ ] `VSF-B10-T04` Authenticate each caller separately, enforce source context at runtime
- [ ] `VSF-B10-T05` Add lazy namespaced tool discoverability and stable ordering
- [ ] `VSF-B10-T06` Cross-check effect results/unknown status across UI/REST/CLI/MCP
- [ ] `VSF-B10-T07` Test client older contract, bad arguments, revoked source role and long-running task fallback
- [ ] `VSF-B10-T08` Measure per-layer dispatch overhead excluding source

### B11 · Expand common application coverage

- [ ] `VSF-B11-T01` Prioritize top failure clusters from corpus rather than vendor branding
- [ ] `VSF-B11-T02` Grow real conformance implementations for critical catalog families
- [ ] `VSF-B11-T03` Evaluate custom combobox/select, virtualization and modal form dependencies
- [ ] `VSF-B11-T04` Add upload/download and export with security policy
- [ ] `VSF-B11-T05` Improve bulk operation and pagination semantics with source rate limits
- [ ] `VSF-B11-T06` Re-run independent app corpus and measure fix leverage

### B12 · Expand advanced UI and developer/research tools

- [ ] `VSF-B12-T01` Adopt conformance-tested terminal/editor adapters and lazy load them
- [ ] `VSF-B12-T02` Model streaming, process ownership, cancel/reconnect and source session
- [ ] `VSF-B12-T03` Build notebook document/cell state mapping and preserving kernel behavior
- [ ] `VSF-B12-T04` Add charts with accessible alternate presentation and source filters
- [ ] `VSF-B12-T05` Implement WebSocket/SSE/WebRTC capability declarations where useful
- [ ] `VSF-B12-T06` Run large source fixtures and negative fallback scenarios

### B13 · Implement durable storage and ZEROS3 qualification

- [ ] `VSF-B13-T01` Build SQLite hot store and PostgreSQL hosted metadata schema
- [ ] `VSF-B13-T02` Implement content-addressed artifact abstraction and signed manifest writes
- [ ] `VSF-B13-T03` Measure filesystem/SQLite/commodity S3 versus ZEROS3 under workload suite
- [ ] `VSF-B13-T04` Add pin-aware GC, crash recovery and export/import bundles
- [ ] `VSF-B13-T05` Encrypt/isolate account-derived caches and remove secrets from shared structures

### B14 · Harden authentication and managed sessions

- [ ] `VSF-B14-T01` Implement explicit identity binding and token audience validation
- [ ] `VSF-B14-T02` Test OAuth PKCE and source-managed browser sign-in lifecycle
- [ ] `VSF-B14-T03` Integrate OS keychain or vault, redaction and scoped credential injection
- [ ] `VSF-B14-T04` Handle MFA/reauth pause and revoke behavior
- [ ] `VSF-B14-T05` Add per-account capability profile and session expiry

### B15 · Build hosted and on-demand execution

- [ ] `VSF-B15-T01` Implement worker lifecycle and memory/CPU/browser-hour limits
- [ ] `VSF-B15-T02` Enforce DNS/redirect/SSRF and cloud metadata defense
- [ ] `VSF-B15-T03` Separate interactive and background queues and per-session locks
- [ ] `VSF-B15-T04` Add task pause/resume for authentication and unknown commits
- [ ] `VSF-B15-T05` Meter resource usage with stable event IDs

### B16 · Build desktop and premium localhost service

- [ ] `VSF-B16-T01` Evaluate lightweight desktop shell against browser+companion
- [ ] `VSF-B16-T02` Build installers/updaters and credentials lifecycle for target OS
- [ ] `VSF-B16-T03` Introduce persistent local API/MCP gated by product entitlement
- [ ] `VSF-B16-T04` Test offline startup, keychain errors, OS suspend/resume and revoke

### B17 · Ship user-visible version control features

- [ ] `VSF-B17-T01` Design version timeline/diff/compatibility status presentation
- [ ] `VSF-B17-T02` Add contract pins and migration preview in UI/CLI/API
- [ ] `VSF-B17-T03` Implement history quotas and pin retention by entitlement
- [ ] `VSF-B17-T04` Add provenance/sanitized export and snapshot restore conditions
- [ ] `VSF-B17-T05` Simulate incompatible source, revoked version and tier downgrade

### B18 · Profile and optimize entire runtime

- [ ] `VSF-B18-T01` Profile browser capture, TS↔WASM transfer, recognition, render, source execution
- [ ] `VSF-B18-T02` Benchmark indexing, incremental DAG invalidation and compact IR
- [ ] `VSF-B18-T03` Evaluate warm plan specialization and lazy module loading
- [ ] `VSF-B18-T04` Test persistent memory growth, cache eviction and GC churn
- [ ] `VSF-B18-T05` Optimize source-app browsing without changing semantics

### B19 · Launch provider-agnostic billing and entitlements

- [ ] `VSF-B19-T01` Build entitlement and consumption interfaces independent of PSP
- [ ] `VSF-B19-T02` Compare merchant-of-record, PSP, invoice/bank payment options
- [ ] `VSF-B19-T03` Implement signed/immutable metering event ledger with idempotency
- [ ] `VSF-B19-T04` Implement rate-card versioning, refunds/credits/limits and cost preview
- [ ] `VSF-B19-T05` Test downgrade/pin retention, no unauthorized cloud failover and billing reconciliation

### B20 · Specialized science, finance and legacy packs

- [ ] `VSF-B20-T01` Choose packs by repeated failure class, not named flagship demos
- [ ] `VSF-B20-T02` Build typed units/decimal/sequence fidelity fixtures
- [ ] `VSF-B20-T03` Add semantic/preserved-mode behavior for scientific editors and remote desktops
- [ ] `VSF-B20-T04` Run cross-domain generalization and independent negative examples

### B21 · Cross-browser compatibility and transport evolution

- [ ] `VSF-B21-T01` Build alternate observation adapter behind capability manifest
- [ ] `VSF-B21-T02` Run conformance matrix and isolate browser-specific behaviors
- [ ] `VSF-B21-T03` Validate fallback when debugger/CDP permission unavailable

### B22 · Team and academic lab release

- [ ] `VSF-B22-T01` Add team-level template/version ownership and user-specific source credentials
- [ ] `VSF-B22-T02` Implement collaboration/merges for presentation profiles
- [ ] `VSF-B22-T03` Add project audit and private export/import
- [ ] `VSF-B22-T04` Pilot research use with authorized non-sensitive fixture data

### B23 · Private / enterprise deployment

- [ ] `VSF-B23-T01` Create private worker/deployment manifests and tested upgrade/rollback
- [ ] `VSF-B23-T02` Support org policies, customer-managed secrets and deny-outbound mode
- [ ] `VSF-B23-T03` Run tenant boundary and backup/DR tests
- [ ] `VSF-B23-T04` Evaluate enterprise licensing and support model

### B24 · Validate beta/public launch

- [ ] `VSF-B24-T01` Execute independent held-out app benchmark and UX studies
- [ ] `VSF-B24-T02` Run security threat suite, provenance checks and release signing
- [ ] `VSF-B24-T03` Validate store policy, installation/uninstallation and error reporting
- [ ] `VSF-B24-T04` Audit pricing/support margins and privacy/retention
- [ ] `VSF-B24-T05` Prepare incident response, version revocation and customer support runbooks
