#!/usr/bin/env bash
# Regenerates the B08 evidence bundle from the current tree; every run is recorded with a checksummed log.
set -uo pipefail
cd "$(git rev-parse --show-toplevel)"
export VSF_CHROMIUM="${VSF_CHROMIUM:-$(node -e "console.log(require('@playwright/test').chromium.executablePath())")}"
export VSF_FABRIC="${VSF_FABRIC:-$PWD/artifacts/bin/vsf-fabric}"
rm -rf docs/deep/evidence/VSF-B08/logs docs/deep/evidence/VSF-B08/manifest.json
node tools/queue/src/cli.ts evidence-init B08 >/dev/null
r() { node tools/queue/src/cli.ts evidence-run B08 "$@"; }
cm=(--manifest-path compiler/Cargo.toml --locked)
live=(go -C backend test -tags integration -race -count=1)

r VSF-B08-T09 package-manifest-freshness -- cargo test "${cm[@]}" -p vsf-registry
r VSF-B08-T09 promotion-freshness-and-signature -- cargo test "${cm[@]}" -p vsf-versioning --test promotion
r VSF-B08-T09 ir-1-2-0-freshness-rules -- cargo test "${cm[@]}" -p vsf-ir --test semantics --test fixtures
r VSF-B08-T01 lineage-graph -- cargo test "${cm[@]}" -p vsf-versioning --test lineage
r VSF-B08-T01 version-fixtures-drift -- cargo test "${cm[@]}" -p vsf-versioning --test fixtures
r VSF-B08-T02 semantic-diff-classes -- cargo test "${cm[@]}" -p vsf-versioning --test diff
r VSF-B08-T03 impact-and-test-selection -- cargo test "${cm[@]}" -p vsf-versioning --test impact
r VSF-B08-T04 pin-and-compatibility-state -- cargo test "${cm[@]}" -p vsf-versioning --test pin
r VSF-B08-T05 native-promotion-verdicts -- cargo test "${cm[@]}" -p vsf-versioning --test api
r VSF-B08-T05 crash-consistent-store -- "${live[@]}" ./versions/
r VSF-B08-T06 revocation-store-and-execution -- "${live[@]}" -run 'Revoked|Revocation' ./versions/ ./execution/
r VSF-B08-T07 drift-rollback-and-newer-version -- cargo test "${cm[@]}" -p vsf-versioning --test scenarios
r VSF-B08-T08 diff-report-emitter -- cargo test "${cm[@]}" -p vsf-versioning --test report
r VSF-B08-T08 diff-report-schema-consumer -- pnpm --filter @vsf/cross-language-tests run test
r VSF-B08-T10 idl-compat-and-snapshots -- pnpm --filter @vsf/idlgen run test
r VSF-B08-T10 rust-layout-rules -- cargo test "${cm[@]}" -p vsf-ir --test layout
r VSF-B08-T10 ts-layout-unit -- pnpm --filter @vsf/ui-tests run test:unit
r VSF-B08-T10 chromium-layout-compositions -- pnpm --filter @vsf/ui-tests exec playwright test specs/layout.spec.ts
r VSF-B08-T10 renderer-regression-suite -- pnpm --filter @vsf/ui-tests run test:browser

r GATE cargo-test-workspace -- cargo test "${cm[@]}" --workspace
r GATE cargo-fmt -- cargo fmt --manifest-path compiler/Cargo.toml --all -- --check
r GATE cargo-clippy -- cargo clippy "${cm[@]}" --workspace --all-targets -- -D warnings
r GATE go-test-all -- go -C backend test -race -count=1 ./...
r GATE go-integration-all -- "${live[@]}" ./...
r GATE gofmt -- scripts/gofmt-check.sh
r GATE go-vet -- go -C backend vet -tags integration ./...
r GATE build-ts -- pnpm build:ts
r GATE typecheck -- pnpm typecheck
r GATE biome -- pnpm exec biome check .
r GATE comment-limit -- node tools/checks/src/cli.ts comments
r GATE secret-scan -- node tools/checks/src/cli.ts secrets
r GATE idl-drift -- node --conditions=source tools/idlgen/src/cli.ts --check
r GATE registry-manifest -- cargo run --quiet "${cm[@]}" -p vsf-registry -- manifest verify . --development
r GATE workflow-check -- node tools/checks/src/cli.ts workflow-check
r GATE vuln-scan -- node tools/checks/src/cli.ts vuln-scan
r GATE checks-tests -- pnpm --filter @vsf/checks run test
r GATE adversarial-b00 -- pnpm --filter @vsf/adversarial-tests run test
r GATE ir-ts -- pnpm --filter @vsf/ir run test
r GATE wasm-host -- pnpm --filter @vsf/wasm-host run test
for job in native go wasm-browser cross-language observation ui browser ts hygiene execution; do
  r GATE "ci-job-$job" -- node tools/checks/src/cli.ts ci-run "$job"
done
