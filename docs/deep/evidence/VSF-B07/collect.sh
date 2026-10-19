#!/usr/bin/env bash
# Regenerates the B07 evidence bundle from the current tree; every run is recorded with a checksummed log.
set -uo pipefail
cd "$(git rev-parse --show-toplevel)"
export VSF_CHROMIUM="${VSF_CHROMIUM:-$(node -e "console.log(require('@playwright/test').chromium.executablePath())")}"
export VSF_FABRIC="${VSF_FABRIC:-$PWD/artifacts/bin/vsf-fabric}"
rm -rf docs/deep/evidence/VSF-B07/logs docs/deep/evidence/VSF-B07/manifest.json
node tools/queue/src/cli.ts evidence-init B07 >/dev/null
r() { node tools/queue/src/cli.ts evidence-run B07 "$@"; }
unit=(go -C backend test -race -count=1)
live=(go -C backend test -tags integration -race -count=1)
cm=(--manifest-path compiler/Cargo.toml --locked)

r VSF-B07-T01 lifecycle-journal-and-false-success -- "${unit[@]}" -run 'TestHappyPath|TestEveryIllegal|TestAcknowledgement|TestReadBack|TestSourceRejection|TestJournal|TestTampered|TestRestartSettles|TestBrokenJournal' ./execution/
r VSF-B07-T02 caller-session-environment-contract-binding -- "${unit[@]}" -run 'TestBound|TestEveryBinding|TestMalformed|TestParameters|TestARoleRevoked|TestSessionLost|TestAuthorizationFromAnother|TestAZeroAuthorized' ./execution/
r VSF-B07-T03 freshness-and-confirmation-preconditions -- "${unit[@]}" -run 'TestStaleRead|TestRevalidated|TestAWriteWithout|TestObservationWithin|TestSourceRequired|TestConfirmationIs|TestPolicyThat|TestAmbiguousEffects|TestReadOnly|TestAConfirmationToken' ./execution/
r VSF-B07-T04 browser-and-api-share-one-taxonomy -- "${live[@]}" -run 'Taxonomy|ReadTheSameSnapshot|TestABrowsersOwnResend|TestStaleBrowserScreen' ./execution/
r VSF-B07-T04 transport-hazards-and-contradicted-claims -- "${unit[@]}" -run 'TestTheHTTPTransport|TestRedirects|TestAConnectionRefused|TestANoEffectClaim|TestAnUnreadable' ./execution/
r VSF-B07-T05 safe-retry-policy -- "${unit[@]}" -run 'TestThrottled|TestNoDedup|TestRetriesAreBounded|TestUncertain|TestTransportErrors|TestUnsent|TestDefinitive|TestThrottlingSlows' ./execution/
r VSF-B07-T05 api-throttle-retry-with-source-hint -- "${unit[@]}" -run 'TestSourceThrottlingIsRetried' ./execution/
r VSF-B07-T06 handles-progress-and-cancellation -- "${unit[@]}" -run 'TestLongTasks|TestCancelling' ./execution/
r VSF-B07-T07 locks-and-rate-limit-adaptation -- "${unit[@]}" -run 'TestLocks|TestAWaiter|TestReleasing|TestConcurrencyScopes|TestRecordScope|TestResourceBudget|TestLimiter' ./execution/
r VSF-B07-T08 unknown-commit-injection -- "${unit[@]}" -run 'TestDisconnect|TestReconciliation|TestTimeoutWhile|TestARestartAfter|TestLosingTheJournal|TestConcurrentDouble|TestReusing|TestAPIExecutor|TestAnUnreachable|TestStaleVersion' ./execution/
r VSF-B07-T08 ambiguous-write-and-denied-caller-live -- "${live[@]}" -run 'TestAmbiguousWriteNeverRetries|TestADeniedCaller' ./execution/
r VSF-B07-T10 fixture-server-module-graph -- pnpm --filter @vsf/fixture-apps run test
r VSF-B07-T10 browser-e2e-reports-load-failures -- pnpm --filter @vsf/e2e run test
r VSF-B07-T11 exclusion-matrix -- "${unit[@]}" -run 'TestADifferentRequest|TestManyDistinct|TestUnresolvedWritesBlock|TestAnUnresolvedWriteBlocks|TestAWriteWithoutAScope|TestACancelledWaiter|TestCancellingDuring|TestAJournalFailure|TestExclusionSurvives|TestScopeKeys|TestLocksExclude|TestAWaiterBehind|TestStartRefuses|TestARecordWithoutAFootprint' ./execution/
r VSF-B07-T11 exclusion-matrix-repeated-under-race -- "${unit[@]}" -count=20 -run 'TestADifferentRequest|TestManyDistinct|TestCancellingDuring|TestACancelledWaiter' ./execution/
r VSF-B07-T12 reconciliation-evidence-levels -- "${unit[@]}" -run 'TestAnotherActor|TestAnUnchanged|TestAStaleRead|TestAFence|TestWithoutAFence|TestAReceipt|TestAnExecutorCannot|TestNonMonotonic|TestReconciliationRevalidates|TestAFreshSession|TestEveryReconciliation|TestTwoReconciliations|TestOnlyReconciliation|TestReconciliationChecks|TestReconciliationRefuses' ./execution/
r VSF-B07-T13 late-authorization-and-non-authoritative-sources -- "${unit[@]}" -run 'TestAuthorizationThatLapses|TestANonAuthoritative|TestThrottlingFromANon' ./execution/
r VSF-B07-T13 journal-torn-tail-and-oversized-detail -- "${unit[@]}" -run 'TestATornFinal|TestACorruptMiddle|TestAnOversized' ./execution/
r VSF-B07-T13 browser-presses-only-usable-controls -- "${live[@]}" -run 'TestTheBrowserOnlyPresses' ./execution/

r VSF-B07-T09 rust-authorize-report -- cargo test "${cm[@]}" -p vsf-ir --test authorize
r VSF-B07-T09 worker-serves-authorize -- cargo test "${cm[@]}" -p vsf-native
r VSF-B07-T09 go-authority-cannot-be-forged -- "${unit[@]}" -run 'TestCodeOutside|TestDecodingOnly' ./execution/
r VSF-B07-T09 rust-refuses-what-go-decodes -- "${live[@]}" -run 'TestRust|TestOnlyTheWorker|TestBothStrategies' ./execution/
r VSF-B07-T09 ts-validated-gate -- pnpm --filter @vsf/wasm-host run test
r VSF-B07-T09 ts-unvalidated-decoder -- pnpm --filter @vsf/ir run test

r GATE go-test-all -- go -C backend test -race -count=1 ./...
r GATE go-integration-all -- "${live[@]}" ./execution/...
r GATE cargo-test-workspace -- cargo test "${cm[@]}" --workspace
r GATE cargo-fmt -- cargo fmt --manifest-path compiler/Cargo.toml --all -- --check
r GATE cargo-clippy -- cargo clippy "${cm[@]}" --workspace --all-targets -- -D warnings
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
r GATE cross-language -- pnpm --filter @vsf/cross-language-tests run test
r GATE ui-all -- pnpm --filter @vsf/ui-tests run test
r GATE wasm-browser-job -- node tools/checks/src/cli.ts ci-run wasm-browser
r GATE observation-all -- pnpm --filter @vsf/observation-tests run test
