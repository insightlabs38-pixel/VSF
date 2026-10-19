#!/usr/bin/env bash
# Re-runs the B05 verification set and records checksummed logs in this bundle.
set -uo pipefail
cd "$(git rev-parse --show-toplevel)"
r() { node tools/queue/src/cli.ts evidence-run B05 "$@"; }
cg=(cargo test --manifest-path compiler/Cargo.toml --locked -p vsf-recognizer)
cli=(cargo run --quiet --manifest-path compiler/Cargo.toml --locked -p vsf-recognizer --)
snap=fixtures/recognition/snapshots

r VSF-B05-T01 index-equals-scan-and-report -- "${cg[@]}" --test index -- --nocapture
r VSF-B05-T01 index-bench-cli-table-orders -- "${cli[@]}" bench-index $snap/table-orders.json
r VSF-B05-T01 index-bench-cli-plain-form -- "${cli[@]}" bench-index $snap/plain-form.json
r VSF-B05-T02 binding-nesting-delegation-duplicates -- "${cg[@]}" --test semantics
r VSF-B05-T02 baseline-recognition-held-out -- "${cli[@]}" recognize $snap/heldout-shadow-ds.json --rules 1
r VSF-B05-T02 delegated-recognition-held-out -- "${cli[@]}" recognize $snap/heldout-shadow-ds.json --rules 2
r VSF-B05-T03 ledger-provenance-and-staleness -- "${cg[@]}" --test ledger
r VSF-B05-T03 ledger-document-plain-form -- "${cli[@]}" ledger $snap/plain-form.json --captured-at 1790000100000
r VSF-B05-T04 incremental-equals-fresh -- "${cg[@]}" --test incremental -- --nocapture
r VSF-B05-T05 candidates-determinism-no-model -- "${cg[@]}" --test candidates
r VSF-B05-T06 availability-withholds-unknown-conflicted -- "${cg[@]}" --test contract
r VSF-B05-T06 contract-document-held-out -- "${cli[@]}" contract $snap/heldout-shadow-ds.json
r VSF-B05-T07 reconciliation-conflicts-and-decisions -- "${cg[@]}" --test reconcile
r VSF-B05-T07 protocol-fixtures-are-valid-evidence -- pnpm --filter @vsf/observation-tests exec node --conditions=source --test test/recognition-protocol.test.ts
r VSF-B05-T07 ledger-with-conflicting-protocol -- "${cli[@]}" ledger $snap/plain-form.json --protocol fixtures/recognition/protocol/profile-conflict.json --captured-at 1790000100000
r VSF-B05-T08 improvement-fixes-two-held-out-apps -- "${cg[@]}" --test generalization
r VSF-B05-T08 snapshots-match-fixture-apps-in-chromium -- pnpm --filter @vsf/observation-tests run check:fixtures

r GATE recognizer-all-tests -- "${cg[@]}"
r GATE cargo-fmt -- cargo fmt --manifest-path compiler/Cargo.toml --all -- --check
r GATE cargo-clippy -- cargo clippy --manifest-path compiler/Cargo.toml --locked --workspace --all-targets -- -D warnings
r GATE full-rust-workspace -- cargo test --manifest-path compiler/Cargo.toml --locked --workspace
for job in hygiene native go ts wasm-browser cross-language browser observation canary; do
  r GATE "ci-$job" -- node tools/checks/src/cli.ts ci-run "$job"
done
r GATE clean-clone-reproducible -- scripts/clean-clone-check.sh
