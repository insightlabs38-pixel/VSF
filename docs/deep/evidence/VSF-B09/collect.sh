#!/usr/bin/env bash
# Regenerates the B09 evidence bundle from the current tree; every run is recorded with a checksummed log.
set -uo pipefail
cd "$(git rev-parse --show-toplevel)"
export VSF_CHROMIUM="${VSF_CHROMIUM:-$(node -e "console.log(require('@playwright/test').chromium.executablePath())")}"
export VSF_FABRIC="${VSF_FABRIC:-$PWD/artifacts/bin/vsf-fabric}"
rm -rf docs/deep/evidence/VSF-B09/logs docs/deep/evidence/VSF-B09/manifest.json
node tools/queue/src/cli.ts evidence-init B09 >/dev/null
r() { node tools/queue/src/cli.ts evidence-run B09 "$@"; }
cm=(--manifest-path compiler/Cargo.toml --locked)
live=(go -C backend test -tags integration -race -count=1)
pw=(pnpm --filter @vsf/browser-tests exec playwright test)

r VSF-B09-T01 mv3-package-audit -- bash -c 'pnpm --filter @vsf/extension run package && node tools/checks/src/cli.ts ext-audit'
r VSF-B09-T01 mv3-audit-rules-and-defects -- pnpm --filter @vsf/checks run test
r VSF-B09-T01 install-permissions-and-package -- "${pw[@]}" specs/extension-install.spec.ts specs/extension-permissions.spec.ts specs/extension-package.spec.ts
r VSF-B09-T02 assemble-semantic-ir -- cargo test "${cm[@]}" -p vsf-assemble
r VSF-B09-T02 wasm-host-assembly -- pnpm --filter @vsf/wasm-host run test
r VSF-B09-T02 envelope-session-core-units -- pnpm --filter @vsf/extension run test
r VSF-B09-T02 observe-assemble-render-in-chromium -- "${pw[@]}" specs/extension-observe.spec.ts specs/extension-transform.spec.ts
r VSF-B09-T03 worker-death-and-idle -- "${pw[@]}" specs/extension-lifecycle.spec.ts specs/extension-idle.spec.ts
r VSF-B09-T03 fallback-to-source-ui -- "${pw[@]}" specs/extension-fallback.spec.ts
r VSF-B09-T04 pairing-protocol-and-store -- "${live[@]}" -run 'Pair|Offer|Access|Refresh|Revoke|Registry|Origins|StaticToken' ./bridge/
r VSF-B09-T04 pairing-cross-language -- pnpm --filter @vsf/cross-language-tests run test
r VSF-B09-T04 extension-pairs-with-companion -- "${pw[@]}" specs/extension-bridge.spec.ts specs/bridge.spec.ts
r VSF-B09-T05 hostile-origin-and-rebinding-go -- go -C backend test -race -count=1 -run 'DNSRebinding|Foreign|CSRF|Malformed|Oversized|Host|SecFetch|Preflight|Stolen|Recorded|Loopback|Unauthenticated' ./bridge/
r VSF-B09-T05 hostile-web-page-in-chromium -- "${pw[@]}" specs/bridge-attacks.spec.ts
r VSF-B09-T06 session-bindings -- "${live[@]}" -run 'Manifest|Registration|Principal|Binding|Revoking|Session|RustValidator' ./bridge/
r VSF-B09-T06 native-validator-chain -- "${live[@]}" ./bridge/...
r VSF-B09-T07 sealed-prefs-and-vault -- "${pw[@]}" specs/extension-settings.spec.ts
r VSF-B09-T08 distribution-checklist-rules -- node tools/checks/src/cli.ts ext-audit
r VSF-B09-T09 gallery-baselines-and-structure -- pnpm --filter @vsf/ui-tests exec playwright test specs/gallery.spec.ts specs/visual-defects.spec.ts
r VSF-B09-T09 renderer-regression-suite -- pnpm --filter @vsf/ui-tests run test:browser

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
r GATE adversarial-b00 -- pnpm --filter @vsf/adversarial-tests run test
r GATE ir-ts -- pnpm --filter @vsf/ir run test
for job in native go wasm-browser cross-language observation ui browser ts hygiene execution; do
  r GATE "ci-job-$job" -- node tools/checks/src/cli.ts ci-run "$job"
done
