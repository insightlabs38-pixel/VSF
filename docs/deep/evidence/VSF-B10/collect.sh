#!/usr/bin/env bash
# Regenerates the B10 evidence bundle from the current tree; every run is recorded with a checksummed log.
set -uo pipefail
cd "$(git rev-parse --show-toplevel)"
export VSF_CHROMIUM="${VSF_CHROMIUM:-$(node -e "console.log(require('@playwright/test').chromium.executablePath())")}"
export VSF_FABRIC="${VSF_FABRIC:-$PWD/artifacts/bin/vsf-fabric}"
rm -rf docs/deep/evidence/VSF-B10/logs docs/deep/evidence/VSF-B10/manifest.json docs/deep/evidence/VSF-B10/overhead.txt
node tools/queue/src/cli.ts evidence-init B10 >/dev/null
r() { node tools/queue/src/cli.ts evidence-run B10 "$@"; }
cm=(--manifest-path compiler/Cargo.toml --locked)
unit=(go -C backend test -race -count=1)
live=(go -C backend test -tags integration -race -count=1)

r VSF-B10-T01 taxonomy-spec-envelope-units -- "${unit[@]}" -run 'Code|Reason|Spec|Envelope|Violations|Strict|Large|Names|ResultSchema|ToolNames|Errors' ./api/ ./api/jsonschema/
r VSF-B10-T01 rest-routes-openapi-and-refusals -- "${live[@]}" -run 'REST|OpenAPI' ./interfaces/
r VSF-B10-T02 cli-grammar-and-exit-semantics -- "${unit[@]}" ./cli/
r VSF-B10-T02 cli-over-the-wire -- "${live[@]}" -run 'CLI' ./interfaces/
r VSF-B10-T03 mcp-protocol-and-conformance-units -- "${unit[@]}" ./mcp/
r VSF-B10-T03 mcp-against-the-real-service -- "${live[@]}" -run 'MCP' ./interfaces/
r VSF-B10-T04 credential-store-and-binding -- "${unit[@]}" -run 'Credential|Token|Expiry|Issuing|Store|Listing' ./api/
r VSF-B10-T04 callers-cannot-borrow-across-surfaces -- "${live[@]}" -run 'EverySurfaceAccepts|Borrow|UseAnother|Revocation|NoSurfaceEchoes|Confirmation' ./interfaces/
r VSF-B10-T04 caller-bound-confirmation-in-runtime -- "${live[@]}" -run 'Confirm|Precond|Memoized' ./execution/
r VSF-B10-T05 catalog-discovery-units -- "${unit[@]}" -run 'Listing|PageSize|Namespace|OnePage|Adversarial|Scopes|Cursors|Emitted|Pins|SpecCache|Clamped' ./api/
r VSF-B10-T05 mcp-discovery-tools -- "${unit[@]}" -run 'Tools|Listing|Namespaces|Fixed|Resources' ./mcp/
r VSF-B10-T05 unverified-operations-are-not-emitted -- "${live[@]}" -run 'NoSurfaceEmits' ./interfaces/
r VSF-B10-T05 catalog-benchmarks -- go -C backend test -run xxx -bench 'CatalogIndex|FirstPage' -benchmem -benchtime=20x ./api/
r VSF-B10-T06 same-outcome-on-every-surface -- "${live[@]}" -run 'VerifiedWriteHas|UnknownCommitMeans|UnverifiedEffect|Refusals|UnknownCommitOnOne|CancellingAQueued' ./interfaces/
r VSF-B10-T07 old-pins-bad-arguments-revoked-roles-long-runs -- "${live[@]}" -run 'OlderClient|Malformed|RoleRevoked|LongRun|CancellingARun|EveryError' ./interfaces/
r VSF-B10-T08 dispatch-overhead-report -- bash -c 'VSF_OVERHEAD_REPORT="$PWD/docs/deep/evidence/VSF-B10/overhead.txt" go -C backend test -tags integration -count=1 -v -run DispatchOverhead ./interfaces/'
r VSF-B10-T08 dispatch-overhead-under-race -- "${live[@]}" -run DispatchOverhead ./interfaces/

r GATE matrix-mcp-schema-collision -- "${live[@]}" -run 'MCPDistinctApplications|MCPAnOlderContract|FixedToolsComeFirst|ToolsListConforms|NamesRoundTrip' ./interfaces/ ./mcp/ ./api/
r GATE matrix-old-client-pin -- "${live[@]}" -run 'OlderClient|MCPAnOlderContract|CLIPins' ./interfaces/
r GATE matrix-unauthorized-caller -- "${live[@]}" -run 'EverySurfaceAccepts|Borrow|UseAnother|Revocation|NoSurfaceEchoes|Confirmation|NeedsACredential' ./interfaces/
r GATE matrix-unknown-commit -- "${live[@]}" -run 'UnknownCommit' ./interfaces/
r GATE matrix-unsupported-operation-emission -- "${live[@]}" -run 'NoSurfaceEmits|OnlyWhatMayBeEmitted' ./interfaces/ ./api/

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
