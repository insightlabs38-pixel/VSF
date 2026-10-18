# VSF Pre-Implementation Gap Register

Status: research synthesis, October 18, 2026. **Not** a completed implementation plan. P0 = architectural prerequisite, P1 = release-enabling, P2 = expansion/commercialization. "New" means missing as an explicit subsystem in the prior 924-capability catalog, not that no related primitive exists.

## Existing catalog baseline

924 named capabilities across 29 families. This inventory mixes primitives, composites, adapters and runtime services; count is not independent implementation work or evidence of compatibility.

## Decisions and acceptance criteria

| Area | Priority | Catalog assessment | Proposed decision | Minimum verification |
|---|---|---|---|---|
| Semantic inference | P0 | Expanded | Evidence-backed semantic IR with unknown state | Ambiguous operations cannot be silently promoted |
| Safe exploration | P0 | Expanded | Read-only discovery planner and consent for effects | No irreversible action in automatic discovery |
| Source reconciliation | P0 | New emphasis | DOM + contract + protocol evidence precedence | Conflicting observations force review or degrade |
| Capability negotiation | P0 | New | Per-environment permission and feature manifest | Plans fail closed when environment lacks capability |
| Browser extension policy | P0 | Expanded | Packaged interpreter plus approved data schemas | Chrome MV3 policy review and store acceptance |
| Native bridge authentication | P0 | New | Authenticated paired extension and companion | Untrusted webpage cannot invoke localhost API |
| Hosted URL egress policy | P0 | New | Public sandbox network allowlist / prohibited ranges | SSRF and DNS rebinding tests pass |
| Session boundaries | P0 | Expanded | Separate source authentication from VSF client authorization | No cross-user cross-tenant capability exposure |
| Source action policy | P0 | Expanded | Effect classes and confirmation preservation | Destructive/irreversible operations cannot auto-retry |
| Operation lifecycle | P0 | Expanded | Prepared/running/unknown/verified/failed states | Ambiguous timeouts never report unverified success |
| Concurrency isolation | P0 | Expanded | Session-scoped sequencing and source limits | No overlapping stateful actions corrupt source session |
| Artifact execution trust | P0 | Expanded | Signed primitive libraries and provenance-checked plans | Tampered compiled plan cannot execute |
| Data minimization | P0 | Expanded | Scrubbing and selective capture; secure diagnostics | Secrets never appear in logs by default |
| UI fidelity escape hatch | P0 | Expanded | Embedded or side-by-side original when necessary | No loss of accessible critical workflow |
| Compatibility score design | P0 | Expanded | Per operation and per app metrics with held-out corpus | No 99% claims without stratified denominator |
| Primitive specification | P0 | Expanded | Typed registration, preconditions, evidence, tests | New primitive must pass contract harness |
| Browser state identity | P1 | Expanded | Cookies, IDB, session, partition key, passkeys | No assumption of cross-context portability |
| Version axes | P1 | Expanded | Compiler, source app, contract, tenant, locale, runtime | Pinned API contract states compatibility status |
| Multi-source permission | P1 | New emphasis | Track user+source+OAuth scope+capability per operation | No permission bleed through shared compiled artifacts |
| Raw vs structured API | P1 | Expanded | Universal interaction fallback vs semantic operations | Unverified business semantics not named as verified |
| Tool catalog scalability | P1 | Expanded | Lazy namespaces and per-client MCP selection | Agent tools remain discoverable with 1k operations |
| MCP 2026 compatibility | P1 | Expanded | Stateless core, cache hints, tasks, explicit auth | Integration against current Tier-1 SDKs |
| Offline/local billing | P1 | Expanded | Separate product entitlements and provider compute | No per-use fictitious infrastructure fee |
| Cost attribution | P1 | Expanded | Billing by browser-minutes, GB, egress, retries, support | P&L per customer and execution mode observable |
| Perceived latency | P1 | Expanded | Fast paths, ready UI, progress for upstream work | p95 VSF overhead budget excluding source latency |
| Renderer framework size | P1 | Expanded | Native primitives plus selective mature editors | No custom reimplementation of complex terminal semantics |
| Internationalization | P1 | Missing explicit coverage | Locales, pluralization, RTL, units, time zones | Generate usable UI in multilingual test apps |
| Assistive tech | P1 | Missing explicit coverage | Screen readers, live regions, focus and drag alternatives | Automated + manual WCAG 2.2 audits |
| Real-time transports | P1 | Missing explicit coverage | WebRTC, SSE, live-collaboration, backpressure | Streaming and multi-editor fixture suite |
| Binary document/printing | P1 | Missing explicit coverage | Print fidelity, PDF forms, scan/OCR adapter | Export and print results remain usable |
| Extension updates | P1 | New | Install, update, rollback, permission change UX | No broken client/server version skew |
| Desktop portability | P1 | New | OS-specific browser bridge and credential lifecycle | Windows/macOS/Linux supported matrix |
| Conflict and staleness | P1 | Expanded | Operation freshness rules and source events | Cross-session updates never silently overwrite |
| Cross-app workflow | P2 | Expanded | Composable operations with provenance and approvals | No implied distributed transaction guarantee |
| Scientific capability packs | P2 | Expanded | Standards-aware file and editor models | Sequence/plate data round-trip tests |
| Visual automation boundary | P2 | Expanded | Preserve canvas when no trusted structural model | Unrecognized canvas operation stays explicit |
| Pricing differentiation | P2 | Expanded | Pay for persistence, deployment, teams, hosted compute | Core compiler correctness invariant by tier |
| Procurement/vendor relations | P2 | Expanded | Provider terms review, deployment allowance | Distribution checklist before public connector |
| Performance portability | P2 | Expanded | Native Rust/WASM structural IR and compact IPC | Native and WASM outputs match bit-for-bit when specified |
| Storage efficiency | P2 | Expanded | SQLite hot metadata + ZEROS3 immutable artifacts | Compare against simple FS and object storage baseline |
| Enterprise governance | P2 | Expanded | SSO, audit exports, residency, policy, RBAC | Org-level kill switch and traceability tests |

## Architectural decisions recommended before implementation

1. Versioned multi-source Observation IR -> Verified Semantic IR -> Presentation IR and Operation Contract.
2. A runtime capability manifest for extension, desktop, local companion, hosted browser, headless worker, and private/VPC worker.
3. Hard separation of browser/page identity, VSF client identity, tenant identity, and original-source authorization.
4. Source operations with effect classes (read, reversible write, irreversible write, unknown), freshness requirements, and explicit unknown-commit states.
5. Small packaged browser interpreter, large native Rust compiler, Go control plane, optional hosted browser worker; each deployment negotiates capabilities.
6. Protocol-first when authorized, DOM fallback and preserved surfaces when useful. Never assume discovered undocumented network traffic is a supported public API.
7. A corpus stratified by application family, browser, authentication, interface type, and environment, with held-out applications and behavioral-differential tests.

## Market and risk references (primary sources)

- Chrome MV3: https://developer.chrome.com/docs/webstore/program-policies/mv3-requirements
- Browser automation: https://playwright.dev/docs/auth
- MCP 2026-07-28: https://blog.modelcontextprotocol.io/posts/2026-07-28/
- OAuth RFC 10017: https://www.rfc-editor.org/rfc/rfc10017.html
- WebDriver BiDi: https://www.w3.org/TR/webdriver-bidi/
- Benchling V3: https://docs.benchling.com/docs/v3-api-overview
- OWASP SSRF: https://cheatsheetseries.owasp.org/cheatsheets/Server_Side_Request_Forgery_Prevention_Cheat_Sheet.html
- OWASP MCP: https://github.com/OWASP/www-project-mcp-top-10
- Whatfix pricing: https://whatfix.com/products/digital-adoption-platform/
- Browserbase pricing: https://www.browserbase.com/pricing
- Cloudflare browser pricing: https://developers.cloudflare.com/browser-run/pricing/

## Suggested first experimental gates

- G1: Two unseen apps with different frontend architectures compile common primitives without app-specific hardcoding.
- G2: Browser extension and companion execute the *same* operation contract with identical verified effects.
- G3: Deliberately ambiguous and failed workflows are safely rejected or reported unknown, not falsely successful.
- G4: An unknown canvas editor stays usable via native-preservation path.
- G5: A verified primitive improvement fixes multiple held-out failures.
- G6: Baseline p50/p95/p99 latency, RSS, storage per compiled revision, hosted cost and compatibility coverage are published internally.
