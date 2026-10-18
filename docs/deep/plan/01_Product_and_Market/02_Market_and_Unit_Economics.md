# Market study and monetization hypotheses

## Positioning

VSF competes with digital-adoption guidance, internal-tool builders, browser automation, enterprise integration and vendor-native interfaces. The central thesis is that one verified semantic model can produce superior, persistent and programmable interfaces at lower per-application maintenance cost than bespoke integrations. No exact-equivalent absence or TAM number has been verified.

## Competitive test matrix

| Category | Representative providers | Strongest VSF hypothesis | Counterexample / research question |
|---|---|---|---|
| Digital adoption | WalkMe, Whatfix, Pendo | Full redesigned interface and programmable actions | Overlays may be more robust for arbitrary hidden behavior |
| Browser agents | Stagehand, Skyvern | No model inference per operation, persistent model and deterministic replay | Agents may cover unstructured exceptions more cheaply to develop |
| Internal tool builders | Retool, Superblocks, Mendix | Automated schema/UI discovery without manual form construction | Native APIs and manual editors may yield higher assurance |
| Integration/RPA | UiPath, Zapier | New human-facing interface with one shared contract | Mature RPA governance and vendor coverage |
| Vendor-native | GCP, Benchling etc. | Unified interface and user-centric operations across vendors | Existing APIs/CLIs may already be better for power users |

## Initial beachhead experiments

1. **Independent developer pilot**: compare 10 representative cloud/dev tasks, VSF vs native UI vs native CLI. Track success, task time, retries, installation friction and subscription interest.
2. **Academic lab pilot**: test a read-heavy ELN/inventory/scheduling workflow with mixed specialty widgets. Require lab authorization and privacy review; measure integration hours avoided and data fidelity.
3. **Operations department pilot**: frequent structured approvals/forms/grids across two systems. Track task correctness, onboarding, p95 latency, manual support, and payment willingness.

## Pricing hypothesis (not locked)

- Free/community: extension-based local transformations, modest local cache/version history and safety/security baselines. Explicit quotas for VSF-hosted services.
- Individual (~$10–$20/mo): more applications, full customization, local UI/desktop ergonomics, practical historical versions; do not cripple compiler correctness.
- Pro (~$30–$60/mo): persistent **localhost service**, higher version-retention and pinned contracts, advanced caching/snapshots, headless entitlements and PAYG hosted options.
- Lab/team (indicative ~$50–$250/mo plus seats/usage as validated): shared templates, collaboration and audit options, team contract version management.
- Business/enterprise: private server/VPC, customer-managed secrets, advanced policy, residency and multi-environment support; negotiated subscription/licensing and metered resources where relevant.
- Hosted sandbox/headless: pay-as-you-go browser-hours, storage, egress and any metered compute using transparent rates, budgets and hard stops. No deceptive local per-operation compute charges.

These are *experiments*, not final public pricing or promises. A billing provider must be selected later after evaluation of taxes, subscriptions, international coverage, merchant of record vs PSP, chargebacks, invoices, alternative processors, bank/paypal-like payment methods and data access. **Do not assume Stripe.**

## Unit economics model

Define fully loaded *direct delivery* cost by mode:

`C = browser_compute + orchestration + storage + egress + payment_fees + directly_allocated_support + directly_allocated_compatibility_maintenance + security_delivery`.

Track contribution by customer and application category; also track R&D, sales and admin for operating profitability. Values previously discussed near 80% gross margin remain hypotheses. Simulate high-support and low-support scenarios and idle persistent-browser costs before committing to plans.

Metrics: active paying individuals, lab adoption, enterprise pilot conversion, average source-app variants per account, time-to-first-value, paid advanced-versioning attachment, PAYG utilization, direct support minutes per customer/month, unique primitive improvements per cohort, churn by compatibility failure and actual contract expansion.

## Pricing constraints

- Higher plans buy advanced retention, multi-version pins, branchable snapshots, audit/provenance, managed compatibility, hosted persistence and private deployments. All plans retain minimal rollback/recovery and source correctness.
- Versioning is **not** a cosmetic checkbox. It must be visible in the product, CLI/API/MCP and billing/retention policy.
- Local/desktop basic UI and extension should not incur provider browser-hour charges. Persistent localhost API/MCP hosting may be gated for software capability/maintenance value, as directed.
- No account secrets or sensitive snapshots retained merely to improve VSF's global cache.

## Go/no-go evidence

Proceed with enterprise investment only if pilots show reproducible efficiency gains without substantial manual per-site fixes and unit economics hold under support sensitivity. Treat reduction in necessary future hires as possible benefit, not an asserted 25-to-5 staffing forecast. Test procurement/permissions needs with actual customers.

See `08_Research_and_Reviews/01_Primary_Sources.md` for standards and market source URLs.
