# Review pass 1 — baseline ZIP inspection (before targeted research)

**Artifact reviewed:** `vsf_plan_review1.zip` (draft snapshot, not intended for user release). **Review protocol:** read ZIP central directory and individual Markdown files, count batches/documents, check local link resolution and duplicate assumptions, audit core requirements against user decisions and prior gap register, identify external assumptions to verify in a fresh research pass.

## Mechanical checks

- Draft ZIP contains 55 non-directory files, including 51 Markdown files, 25 numbered batch documents, two Mermaid diagrams and the 924-candidate catalog in MD/CSV/JSON.
- Approx. 28.6k words of Markdown; no broken relative Markdown links, no empty/under-200-word Markdown files, and no TODO/TBD placeholders detected by script.
- The JSON inventory's declared total (924) was present, but the initial audit script's `items` field assumption was wrong. **Review finding:** validate actual JSON schema and compute count from the correct nested field during final review.
- B00–B10 are task-level; B11–B24 are milestone/batch-level. No exact calendar dates or false guarantees attached to later stages.

## Conceptual review findings

| Issue | Severity | Observed gap | Requested correction |
|---|---|---|---|
| R1-01 | P0 | Versioning spec strong, but entitlement downgrade and multi-version source drift need explicit joint test | Add version downgrade/migration fixture and retention/GC paid entitlement state transitions |
| R1-02 | P0 | MV3 policy reasoning correct direction but ambiguous around remotely fetched interpreter/config | Confirm official policy; add decision table for pure declarative data vs executable plans |
| R1-03 | P0 | MCP feature set references 2026-07-28; need verify actual released profile and extensions | Source primary official docs and narrow feature promises by negotiated capability |
| R1-04 | P0 | Localhost origin/CSRF/pairing noted but no concrete local threat model acceptance workflow | Add malicious-origin and rebinding fixture plus native bridge version protocol |
| R1-05 | P1 | Unclear if '1,100–1,300 capabilities' is target | Make baseline 924 candidates, expansion driven by evidence rather than numeric promise |
| R1-06 | P1 | Billing provider-neutral but not explicit taxes/invoicing decision gate | Validate billing architecture requirements in mid-review research |
| R1-07 | P1 | Runtime complexity vs code efficiency needs measurable architectural debt indicators | Add dependency/coupling, test speed, latency/memory regression metrics without LOC ceiling |
| R1-08 | P1 | Capability packs could imply custom integration proliferation | Require independent cross-domain fixtures and generic adapter semantics |
| R1-09 | P1 | Risk of too many execution modes before any useful product | Explicit milestone parity matrix and limited beta supported-mode labeling |
| R1-10 | P2 | Market pricing points not validated and no standalone competitor feature scorecard | Include vendor-supported comparison and customer validation script; label assumptions |

## Required action before review 2

1. Recheck Chrome MV3, MCP specification/version and tool contract semantics, OAuth/browser states, security on localhost, and source API version notices with primary sources.
2. Fix catalog count check and add operational validation to final ZIP review.
3. Apply any material changes to implementation batches and supporting specs.
4. Run second ZIP review with source claims/links/required-file checks, archive CRC validation, ID/dependency crosschecks and versioning test coverage.
