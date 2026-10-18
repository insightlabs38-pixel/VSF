#!/usr/bin/env bash
# Re-runs the B03 verification set and records checksummed logs in this bundle.
set -uo pipefail
cd "$(git rev-parse --show-toplevel)"
r() { node tools/queue/src/cli.ts evidence-run B03 "$@"; }
scripts/build-wasm.sh >/dev/null && scripts/build-native.sh >/dev/null && pnpm build:ts >/dev/null

r VSF-B03-T01 all-924-import-unchanged-once-and-classified -- cargo test --manifest-path compiler/Cargo.toml --locked -p vsf-registry --test registry -- all_924_catalog_entries import_makes_no_maturity_claim import_rejects_duplicates committed_registry_equals_a_fresh_import
r VSF-B03-T02 ownership-dependencies-and-advancement -- cargo test --manifest-path compiler/Cargo.toml --locked -p vsf-registry --test registry -- ownership_dependencies_and_advancement_are_complete higher_maturity_levels_demand
r VSF-B03-T03 registration-and-negative-match-rules -- cargo test --manifest-path compiler/Cargo.toml --locked -p vsf-registry --test registry -- registration_ capability_mismatches ambiguous_recognitions malformed_recognitions recognition_prefers a_conformant_composite
r VSF-B03-T04 graph-closure-cycles-and-stable-order -- cargo test --manifest-path compiler/Cargo.toml --locked -p vsf-registry --test registry -- graph_reports the_real_registry_has_complete_acyclic
r VSF-B03-T05 seeded-positive-negative-adversarial-fixtures -- cargo test --manifest-path compiler/Cargo.toml --locked -p vsf-registry --test harness -- every_fixture_descriptor_round_trips malformed_descriptors seeded_fixtures_are_reproducible
r VSF-B03-T06 dashboard-denominator-and-tiers -- cargo test --manifest-path compiler/Cargo.toml --locked -p vsf-registry --test dashboard
r VSF-B03-T06 dashboard-matches-committed-file -- cargo run --quiet --manifest-path compiler/Cargo.toml --locked -p vsf-registry -- dashboard . --check
r VSF-B03-T07 manifest-tamper-matrix -- cargo test --manifest-path compiler/Cargo.toml --locked -p vsf-registry --test manifest
r VSF-B03-T07 manifest-verifies-cli -- cargo run --quiet --manifest-path compiler/Cargo.toml --locked -p vsf-registry -- manifest verify .
r VSF-B03-T08 shared-composite-two-independent-sources -- cargo test --manifest-path compiler/Cargo.toml --locked -p vsf-registry --test harness -- both_tables_share_one_composition two_independent_sources every_fixture_passes_the_shared_composite changing_an_expectation at_most_one_collection an_implemented_claim_without_a_passing_witness

r GATE registry-package-check -- cargo run --quiet --manifest-path compiler/Cargo.toml --locked -p vsf-registry -- check .
r GATE full-rust-workspace -- cargo test --manifest-path compiler/Cargo.toml --locked --workspace
for job in hygiene native go ts wasm-browser cross-language browser canary; do
  r GATE "ci-$job" -- node tools/checks/src/cli.ts ci-run "$job"
done
r GATE clean-clone-reproducible -- scripts/clean-clone-check.sh
