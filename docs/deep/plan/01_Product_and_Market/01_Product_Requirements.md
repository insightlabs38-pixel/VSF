# Product Requirements Document (PRD)

## Problem

Organizations and individuals rely on complex cloud, enterprise, finance, sales, government, academic and scientific software. Interfaces are inconsistent and cumbersome; workflows often require many steps; programmatic access may be incomplete. Replacing the original software is usually impossible, expensive or unjustified. VSF provides alternative *authorized* interfaces over existing software without becoming the underlying system of record.

## Primary jobs to be done

1. User opens an authorized website and obtains a usable, neutral, fast transformation with minimal setup.
2. User customizes the layout (density, navigation, shortcuts, tables, colors and presentation) without changing source semantics.
3. User discovers actions using universal search/command palette, rather than memorizing original navigation.
4. User generates a CLI/API/MCP against verified source operations, pins the interface contract and caches relevant execution artifacts.
5. User selects extension, desktop, localhost, hosted, on-demand/headless or private runtime according to privacy, latency, deployment and persistence needs.
6. Organization shares verified interface templates and version policies while users maintain separate source permissions and sessions.
7. When the source changes, VSF detects drift, validates affected operations and migrates or preserves compatible interface versions with explicit status.

## Target segments and test personas

- Individual developer or cloud user: low-cost local extension, keyboard navigation, cached UI; may upgrade for persistent localhost API/MCP and advanced caching.
- Computational researcher: scientific formats, terminals, notebooks, datasets, conversion reproducibility, low overhead and version history.
- Small lab: team templates, scientific software workflows, mixed credential policies, versioned contracts and data boundaries.
- Sales/operations department: repetitive bulk tasks and workflow compression; policy, managed connectors and audits.
- Enterprise IT: private/VPC, hosted/service execution, rollback, SSO, security reporting, managed compatibility SLAs.

## Core features and priority

| Feature | Before alpha | Before public release | Expansion |
|---|---|---|---|
| Browser extension + original surface fallback | Demonstrated | Hardened | Additional browsers |
| Primitive catalog (924 baseline candidates) | Structured registry + initial implementations | Critical/core families validated by coverage | Specialized long tail |
| Semantic and presentation IRs | Typed v0 | Versioned stable profile | Multi-application IR composition |
| UI layout/theme/customization | Basic monochrome/dark | Full user preferences, density and accessibility | Shared templates/optimizer |
| Generated CLI/API/MCP | Vertical slice | Stable versioned contracts, auth and discovery | Domain SDKs/cross-app |
| Versioning | Multi-axis contracts, diff and retention model | User-visible revisions, pins, compat/deprecate/rollback | Differential replay/migration support |
| Identity and connections | Browser + OAuth concept tested | Secure broker, vault, managed session options | Enterprise identity |
| Caching | Compiled state, simple invalidation | Progressive hot/warm/cold and tenant segregation | More advanced algorithms on paid plans |
| Modes | Extension + local proof | Local, desktop, hosted and on-demand supported tiers | Full private/VPC program |
| Billing | Entitlement/payment abstraction | Affordable plans, non-Stripe-specific adapter, PAYG guardrails | Usage commitments/invoicing |

## Nonfunctional requirements

- Fidelity: source operation effects equivalent within source-provided guarantees; preserve unknown status rather than invent proof.
- Availability: source outage is not masked as successful execution; native vs hosted service objectives documented separately.
- Privacy: default local-only where applicable, opt-in diagnostics and hosted storage, no secret leakage via caches.
- Portability: each execution mode publishes runtime capabilities and supported version ranges.
- Performance: p95 local UI interactions target <100 ms VSF work for ordinary cached actions, while overall responsiveness is separately measured. Large apps cannot be assigned unrealistic complete-discovery SLOs.
- Reproducibility: pinned compiler/primitive/IR/contract versions, deterministic fixtures, artifact provenance.
- Upgradeability: explicit compatibility windows, migrations, rollbacks, security revocations, no unsupported old versions silently marked healthy.
- Accessibility/i18n: WCAG 2.2-oriented test suite, screen-reader manual checks, RTL, units, time zones, IME and locale-specific parsing.

## Deliberate non-goals (early)

- Rebuilding source app backend or bypassing its authorization/terms.
- Inferring hidden server behavior from a single DOM snapshot with guaranteed correctness.
- Full replacement of every specialist editor on first encounter; preserved source surface is valid with honest reporting.
- Mandatory LLM inference or prompt-driven runtime action execution.
- Monetizing basic correctness and credential security by reserving them for expensive tiers.
- Building a new browser engine, full password manager or payment processor.

## Acceptance examples

- Cloud console: original AI assistant and terminal remain usable when source allows; resource listing and filters use native primitives; supported API operations prefer documented contracts over inferred traffic.
- Scientific system: notebook, registries and files transformed; unknown sequence editor preserved; data units and identifiers survive typed round trip.
- Legacy form: translated into responsive groups with validations, source permissions and confirmations retained; submitted record read back where possible.
- Schema change: additive application feature produces new contract variant; pinned old client either works on source or reports specific incompatibility, never pretends feature exists.
