#!/usr/bin/env bash
# Re-runs the B01 verification set and records checksummed logs in this bundle.
set -uo pipefail
cd "$(git rev-parse --show-toplevel)"
r() { node tools/queue/src/cli.ts evidence-run B01 "$@"; }
pw() { pnpm --filter @vsf/browser-tests exec playwright test "$@"; }
scripts/build-wasm.sh >/dev/null && scripts/build-native.sh >/dev/null && pnpm build:ts >/dev/null && pnpm --filter @vsf/extension run build >/dev/null

r VSF-B01-T01 extension-unit -- pnpm --filter @vsf/extension run test
r VSF-B01-T01 extension-observes-authorized-origin-only -- pw specs/extension-observe.spec.ts
r VSF-B01-T01 extension-package-digests -- cat artifacts/extension/test.build.json artifacts/extension/product.build.json

r VSF-B01-T02 grant-revoke-and-page-origin-misuse -- pw specs/extension-permissions.spec.ts

r VSF-B01-T03 worker-restart-interrupt-and-secret-free-state -- pw specs/extension-lifecycle.spec.ts
r VSF-B01-T03 natural-idle-termination-30s -- pw specs/extension-idle.spec.ts

r VSF-B01-T04 rust-observation-pack -- cargo test --manifest-path compiler/Cargo.toml --locked -p vsf-observation -p vsf-fixtures -p vsf-wasm -p vsf-native
r VSF-B01-T04 ts-observation-pack -- pnpm --filter @vsf/observer --filter @vsf/wasm-host run test
r VSF-B01-T04 rust-ts-wasm-pack-parity -- pnpm --filter @vsf/cross-language-tests run test

r VSF-B01-T05 capture-parse-transport-benchmark -- pnpm --filter @vsf/browser-tests exec node --conditions=source bench/capture.ts docs/deep/evidence/VSF-B01/bench/capture.json

r VSF-B01-T06 boundary-benchmark -- pnpm --filter @vsf/browser-tests exec node --conditions=source bench/boundary.ts docs/deep/evidence/VSF-B01/bench/boundary.json

r VSF-B01-T07 go-bridge-race-tests -- go -C backend test -race -count=1 -v ./bridge
r VSF-B01-T07 browser-bridge-cors-and-csrf -- pw specs/bridge.spec.ts
r VSF-B01-T07 native-messaging-limits -- pw specs/native-messaging.spec.ts

for job in hygiene native go ts wasm-browser cross-language browser canary; do
  r GATE "ci-$job" -- node tools/checks/src/cli.ts ci-run "$job"
done
r GATE clean-clone-reproducible -- scripts/clean-clone-check.sh
