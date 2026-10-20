#!/usr/bin/env bash
# Regenerates the B13 evidence bundle from the current tree; every run is recorded with a checksummed log.
set -uo pipefail
cd "$(git rev-parse --show-toplevel)"
export VSF_CHROMIUM="${VSF_CHROMIUM:-$(node -e "console.log(require('@playwright/test').chromium.executablePath())")}"
export VSF_FABRIC="${VSF_FABRIC:-$PWD/artifacts/bin/vsf-fabric}"
rm -rf docs/deep/evidence/VSF-B13/logs docs/deep/evidence/VSF-B13/manifest.json
node tools/queue/src/cli.ts evidence-init B13 >/dev/null
r() { node tools/queue/src/cli.ts evidence-run B13 "$@"; }
cm=(--manifest-path compiler/Cargo.toml --locked)
unit=(go -C backend test -race -count=1)
live=(go -C backend test -tags integration -race -count=1)

r VSF-B13-T01 metadata-store-units -- "${unit[@]}" ./storage/meta/
r VSF-B13-T01 postgres-schema-against-a-real-cluster -- "${live[@]}" -run Postgres ./storage/meta/
r VSF-B13-T02 artifact-store-and-signed-manifests -- "${unit[@]}" ./storage/artifact/
r VSF-B13-T02 object-backends-conformance -- "${unit[@]}" ./storage/blob/
r VSF-B13-T03 workload-harness-and-crash-runs -- "${unit[@]}" ./storage/bench/
r VSF-B13-T04 garbage-collection-and-recovery -- "${unit[@]}" ./storage/gc/
r VSF-B13-T04 export-import-bundles -- "${unit[@]}" ./storage/bundle/
r VSF-B13-T05 sealing-keys-and-disk-scans -- "${unit[@]}" ./storage/secret/
r VSF-B13-T05 tenant-isolated-cache -- "${unit[@]}" ./storage/tcache/

r GATE matrix-gc-pinned-object -- "${unit[@]}" -run 'Pins|Pinned|Ancestors|Shared|Dies|Racing|DryRun|Orphan' ./storage/gc/
r GATE matrix-small-object-amplification -- "${unit[@]}" -run 'Corpus|Versions|Chunk|Summarize|WholeRun' ./storage/bench/
r GATE matrix-torn-write -- "${unit[@]}" -run 'Torn|Crash|Durable|BitRot|HalfBuilt|Restart|OnlyOneProcess' ./storage/blob/ ./storage/artifact/ ./storage/meta/ ./storage/bench/
r GATE matrix-tenant-cache-probing -- "${unit[@]}" ./storage/tcache/ ./storage/secret/
r GATE matrix-corrupt-exported-bundle -- "${unit[@]}" -run 'Damaged|Tampering|Dropped|AnotherTenant|Dies|SizeLimit|Sealed' ./storage/bundle/ ./storage/secret/

r GATE cargo-test-workspace -- cargo test "${cm[@]}" --workspace
r GATE cargo-fmt -- cargo fmt --manifest-path compiler/Cargo.toml --all -- --check
r GATE cargo-clippy -- cargo clippy "${cm[@]}" --workspace --all-targets -- -D warnings
r GATE go-test-all -- "${unit[@]}" ./...
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
