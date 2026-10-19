#!/usr/bin/env bash
# Re-runs the B06 verification set and records checksummed logs in this bundle.
set -uo pipefail
cd "$(git rev-parse --show-toplevel)"
r() { node tools/queue/src/cli.ts evidence-run B06 "$@"; }
mp=(--manifest-path compiler/Cargo.toml)
rec=(cargo test "${mp[@]}" --locked -p vsf-recognizer)
ui=(pnpm --filter @vsf/ui-tests exec playwright test)
obs=(pnpm --filter @vsf/observation-tests)
bundle=docs/deep/evidence/VSF-B06

r VSF-B06-T01 presentation-compile-and-constraints -- pnpm --filter @vsf/ui-tests run test:unit
r VSF-B06-T01 form-and-grid-render -- "${ui[@]}" specs/smoke.spec.ts
r VSF-B06-T02 tokens-schemes-accents-status -- "${ui[@]}" specs/tokens.spec.ts
r VSF-B06-T03 keyboard-focus-aria-rtl -- "${ui[@]}" specs/a11y.spec.ts
r VSF-B06-T04 keyed-incremental-write-counts -- "${ui[@]}" specs/incremental.spec.ts
r VSF-B06-T05 density-navigation-palette-reversible -- "${ui[@]}" specs/customization.spec.ts
r VSF-B06-T06 unsupported-widgets-preserved -- "${ui[@]}" specs/preserve.spec.ts
r VSF-B06-T07 source-style-isolation -- "${ui[@]}" specs/sourcestyle.spec.ts
r VSF-B06-T08 grid-1000-rows-responsiveness-memory -- "${ui[@]}" specs/perf.spec.ts
r VSF-B06-T08 visual-contract-states-schemes -- "${ui[@]}" specs/visual.spec.ts
r VSF-B06-T08 benchmark-json -- pnpm --filter @vsf/ui-tests exec node --conditions=source bench-ui.ts "$PWD/$bundle/bench/ui.json"

r VSF-B06-T09 correction-lane-queue-tests -- pnpm --filter @vsf/queue run test
r VSF-B06-T09 queue-validate -- node tools/queue/src/cli.ts validate
r VSF-B06-T10 observer-unit-and-share-policy -- "${obs[@]}" run test:unit
r VSF-B06-T10 boundary-and-sensitive-in-chromium -- "${obs[@]}" exec playwright test specs/boundary.spec.ts specs/sensitive.spec.ts
r VSF-B06-T11 ledger-identity-and-provenance -- "${rec[@]}" --test ledger
r VSF-B06-T11 incremental-equals-fresh -- "${rec[@]}" --test incremental
r VSF-B06-T12 ir-semantics-identity-isolation -- cargo test "${mp[@]}" --locked -p vsf-ir
r VSF-B06-T12 idl-generator-and-snapshots -- pnpm --filter @vsf/idlgen run test
r VSF-B06-T12 idl-compat -- node --conditions=source tools/idlgen/src/cli.ts compat
r VSF-B06-T12 go-ir-fixtures -- go -C backend test -count=1 ./ir/...
r VSF-B06-T12 ts-ir-fixtures -- pnpm --filter @vsf/ir run test
r VSF-B06-T13 protocol-evidence-validation -- "${rec[@]}" --test protocol
r VSF-B06-T13 reconciliation-conflicts -- "${rec[@]}" --test reconcile
r VSF-B06-T14 witness-bound-availability -- "${rec[@]}" --test contract
r VSF-B06-T15 hints-never-authorize -- "${rec[@]}" --test candidates
r VSF-B06-T16 gate-and-osv-checks -- pnpm --filter @vsf/checks run test
r VSF-B06-T16 workflow-check -- node tools/checks/src/cli.ts workflow-check
r VSF-B06-T16 vuln-scan -- node tools/checks/src/cli.ts vuln-scan
r VSF-B06-T17 renderer-hardening -- "${ui[@]}" specs/hardening.spec.ts
r VSF-B06-T17 redact-and-registry -- cargo test "${mp[@]}" --locked -p vsf-redact -p vsf-registry
r VSF-B06-T17 cross-language-interop -- pnpm --filter @vsf/cross-language-tests run test
r VSF-B06-T18 adr-032-present -- test -s docs/deep/adr/ADR-032-foundation-stabilization.md

r GATE ui-all -- pnpm --filter @vsf/ui-tests run test
r GATE cargo-test-workspace -- cargo test "${mp[@]}" --locked --workspace
r GATE cargo-fmt -- cargo fmt "${mp[@]}" --all -- --check
r GATE cargo-clippy -- cargo clippy "${mp[@]}" --locked --workspace --all-targets -- -D warnings
r GATE go-test -- go -C backend test -race -count=1 ./...
r GATE build-ts -- pnpm build:ts
r GATE typecheck -- pnpm typecheck
r GATE biome -- pnpm exec biome check .
r GATE comment-limit -- node tools/checks/src/cli.ts comments
r GATE idl-drift -- node --conditions=source tools/idlgen/src/cli.ts --check
r GATE registry-manifest -- cargo run --quiet "${mp[@]}" --locked -p vsf-registry -- manifest verify . --development
r GATE wasm-host -- pnpm --filter @vsf/wasm-host run test
