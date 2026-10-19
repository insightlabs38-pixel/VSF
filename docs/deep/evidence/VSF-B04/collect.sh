#!/usr/bin/env bash
# Re-runs the B04 verification set and records checksummed logs in this bundle.
set -uo pipefail
cd "$(git rev-parse --show-toplevel)"
r() { node tools/queue/src/cli.ts evidence-run B04 "$@"; }
obs=(pnpm --filter @vsf/observation-tests exec)
scripts/build-wasm.sh >/dev/null && scripts/build-native.sh >/dev/null && pnpm build:ts >/dev/null

r VSF-B04-T01 scope-enumeration-unit -- "${obs[@]}" node --conditions=source --test test/scope.test.ts
r VSF-B04-T01 scope-enumeration-browser -- "${obs[@]}" playwright test specs/scope.spec.ts
r VSF-B04-T01 extension-closed-shadow-and-limits -- pnpm --filter @vsf/browser-tests exec playwright test specs/extension-observe.spec.ts
r VSF-B04-T01 extension-protocol-unit -- pnpm --filter @vsf/extension run test
r VSF-B04-T02 role-name-label-geometry-browser -- "${obs[@]}" playwright test specs/capture.spec.ts
r VSF-B04-T03 replica-protocol-unit -- "${obs[@]}" node --conditions=source --test test/replica.test.ts
r VSF-B04-T03 mutation-batching-and-resync-browser -- "${obs[@]}" playwright test specs/mutations.spec.ts
r VSF-B04-T04 environment-overlay-unit -- "${obs[@]}" node --conditions=source --test test/environment.test.ts
r VSF-B04-T04 environment-browser -- "${obs[@]}" playwright test specs/environment.spec.ts
r VSF-B04-T05 sensitive-scopes-redaction-and-local-mode -- "${obs[@]}" playwright test specs/sensitive.spec.ts
r VSF-B04-T06 protocol-evidence-schema-and-rules -- "${obs[@]}" node --conditions=source --test test/protocol.test.ts
r VSF-B04-T07 epoch-allocation-unit -- "${obs[@]}" node --conditions=source --test test/epoch.test.ts
r VSF-B04-T07 navigation-fixture-runner-run1 -- "${obs[@]}" playwright test specs/navigation.spec.ts
r VSF-B04-T07 navigation-fixture-runner-run2 -- "${obs[@]}" playwright test specs/navigation.spec.ts
r VSF-B04-T07 navigation-fixture-runner-run3 -- "${obs[@]}" playwright test specs/navigation.spec.ts
r VSF-B04-T08 overhead-benchmark-1k-10k-50k -- pnpm --filter @vsf/observation-tests run bench
r VSF-B04-T08 overhead-result-copied -- cp artifacts/observation/overhead.json docs/deep/evidence/VSF-B04/overhead.json

r GATE registry-package-still-verifies -- cargo run --quiet --manifest-path compiler/Cargo.toml --locked -p vsf-registry -- manifest verify .
r GATE full-rust-workspace -- cargo test --manifest-path compiler/Cargo.toml --locked --workspace
for job in hygiene native go ts wasm-browser cross-language browser observation canary; do
  r GATE "ci-$job" -- node tools/checks/src/cli.ts ci-run "$job"
done
r GATE clean-clone-reproducible -- scripts/clean-clone-check.sh
