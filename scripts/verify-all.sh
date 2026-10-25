#!/usr/bin/env bash
# Runs every CI job locally in dependency-safe order and fails if any job fails.
set -uo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
status=0
for job in hygiene native go ts wasm-browser cross-language browser observation corpus canary; do
  node tools/checks/src/cli.ts ci-run "$job" || status=1
done
exit $status
