#!/usr/bin/env bash
# Re-runs the B02 verification set and records checksummed logs in this bundle.
set -uo pipefail
cd "$(git rev-parse --show-toplevel)"
r() { node tools/queue/src/cli.ts evidence-run B02 "$@"; }
scripts/build-wasm.sh >/dev/null && scripts/build-native.sh >/dev/null && pnpm build:ts >/dev/null

r VSF-B02-T01 idl-generation-matches-committed-bindings -- node --conditions=source tools/idlgen/src/cli.ts --check
r VSF-B02-T01 idlgen-generator-tests -- pnpm --filter @vsf/idlgen run test
r VSF-B02-T01 six-families-valid-and-79-invalid-decode-cases -- cargo test --manifest-path compiler/Cargo.toml --locked -p vsf-ir --test fixtures

r VSF-B02-T02 effect-certainty-algebra-and-104-semantic-cases -- cargo test --manifest-path compiler/Cargo.toml --locked -p vsf-ir --lib --test semantics

r VSF-B02-T03 rust-canonical-bytes-pins-unicode -- cargo test --manifest-path compiler/Cargo.toml --locked -p vsf-ir --test fixtures
r VSF-B02-T03 ts-canonical-bytes-pins-unicode -- pnpm --filter @vsf/ir run test
r VSF-B02-T03 go-canonical-bytes-pins-unicode -- go -C backend test -count=1 -v ./ir/...

r VSF-B02-T04 wasm-and-ts-agree-with-rust-on-all-fixtures -- pnpm --filter @vsf/wasm-host run test
r VSF-B02-T04 native-worker-ir-validate -- cargo test --manifest-path compiler/Cargo.toml --locked -p vsf-native
r VSF-B02-T04 breaking-and-compatible-schema-changes -- pnpm --filter @vsf/idlgen run test

r VSF-B02-T05 identity-overlay-and-hmac-vectors -- cargo test --manifest-path compiler/Cargo.toml --locked -p vsf-ir --test identity

r VSF-B02-T06 version-matrix-49-cases -- cargo test --manifest-path compiler/Cargo.toml --locked -p vsf-ir --test matrix

r VSF-B02-T07 witness-applicability-and-active-operation-invariants -- cargo test --manifest-path compiler/Cargo.toml --locked -p vsf-ir --test semantics

r VSF-B02-T08 compat-check-against-released-snapshot -- node --conditions=source tools/idlgen/src/cli.ts compat

for job in hygiene native go ts wasm-browser cross-language browser canary; do
  r GATE "ci-$job" -- node tools/checks/src/cli.ts ci-run "$job"
done
r GATE clean-clone-reproducible -- scripts/clean-clone-check.sh
