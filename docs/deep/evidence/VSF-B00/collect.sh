#!/usr/bin/env bash
# Re-runs the B00 verification set and records checksummed logs in this bundle.
set -uo pipefail
cd "$(git rev-parse --show-toplevel)"
r() { node tools/queue/src/cli.ts evidence-run B00 "$@"; }
scripts/build-wasm.sh >/dev/null && scripts/build-native.sh >/dev/null && pnpm build:ts >/dev/null

r VSF-B00-T01 toolchain-pins -- node tools/checks/src/cli.ts toolchain
r VSF-B00-T01 toolchain-and-lockfile-negatives -- pnpm --filter @vsf/checks --filter @vsf/adversarial-tests run test
r VSF-B00-T01 dep-review-lockfiles -- node tools/checks/src/cli.ts dep-review
r VSF-B00-T01 clean-clone-reproducible -- scripts/clean-clone-check.sh

r VSF-B00-T02 rust-schema-canon -- cargo test --manifest-path compiler/Cargo.toml --locked --workspace
r VSF-B00-T02 ts-schema -- pnpm --filter @vsf/schema --filter @vsf/wasm-host run test
r VSF-B00-T02 go-schema -- go -C backend test -count=1 ./...
r VSF-B00-T02 rust-ts-go-chain -- pnpm --filter @vsf/cross-language-tests run test
r VSF-B00-T02 generated-drift -- node scripts/gen-schema-constants.ts --check

r VSF-B00-T03 fixture-server -- pnpm --filter @vsf/fixture-apps run test
r VSF-B00-T03 browser-e2e-and-wasm-abi -- pnpm --filter @vsf/e2e run test
r VSF-B00-T03 wasm-host-abi-negative -- pnpm --filter @vsf/wasm-host run test

r VSF-B00-T04 golden-byte-identical -- node scripts/golden.ts check
r VSF-B00-T04 native-golden-and-redaction -- cargo test --manifest-path compiler/Cargo.toml --locked -p vsf-native -p vsf-redact
r VSF-B00-T04 redaction-parity -- pnpm --filter @vsf/redact run test
r VSF-B00-T04 secret-scan -- node tools/checks/src/cli.ts secrets

r VSF-B00-T05 biome -- pnpm exec biome check .
r VSF-B00-T05 clippy -- cargo clippy --manifest-path compiler/Cargo.toml --locked --workspace --all-targets -- -D warnings
r VSF-B00-T05 rustfmt -- cargo fmt --manifest-path compiler/Cargo.toml --all -- --check
r VSF-B00-T05 gofmt -- scripts/gofmt-check.sh
r VSF-B00-T05 go-vet -- go -C backend vet ./...
r VSF-B00-T05 comment-limit -- node tools/checks/src/cli.ts comments
r VSF-B00-T05 artifact-digest -- node tools/checks/src/cli.ts digest artifacts/wasm artifacts/bin

for job in hygiene native go ts wasm-browser cross-language canary; do
  r VSF-B00-T06 "ci-$job" -- node tools/checks/src/cli.ts ci-run "$job"
done
r VSF-B00-T06 induced-failure-is-detected -- bash -c 'VSF_INDUCE_FAILURE=1 node tools/checks/src/cli.ts ci-run canary; test $? -eq 1'
r VSF-B00-T06 ci-verify-canary -- node tools/checks/src/cli.ts ci-verify artifacts/ci/canary

r VSF-B00-T07 queue-tests -- pnpm --filter @vsf/queue run test
r VSF-B00-T07 queue-validate -- node tools/queue/src/cli.ts validate --no-git
r VSF-B00-T07 catalog-check -- node tools/checks/src/cli.ts catalog --check
