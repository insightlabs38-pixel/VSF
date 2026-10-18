#!/usr/bin/env bash
# Clones the committed HEAD into a fresh directory, builds with locked inputs and compares artifact digests.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
git -C "$root" diff --quiet HEAD -- . ':!docs/deep/evidence' || { echo "working tree has uncommitted changes; commit first" >&2; exit 1; }
git clone --quiet --no-hardlinks "$root" "$work/clone"
cd "$work/clone"
pnpm install --frozen-lockfile
node tools/checks/src/cli.ts toolchain
scripts/build-wasm.sh >/dev/null
scripts/build-native.sh >/dev/null
pnpm build:ts >/dev/null
node tools/checks/src/cli.ts ci-run cross-language
node tools/checks/src/cli.ts digest artifacts/wasm artifacts/bin > "$work/clone-digest.json"
cd "$root"
scripts/build-wasm.sh >/dev/null
scripts/build-native.sh >/dev/null
node tools/checks/src/cli.ts digest artifacts/wasm artifacts/bin > "$work/origin-digest.json"
a="$(node -e "console.log(JSON.parse(require('fs').readFileSync('$work/clone-digest.json','utf8')).rollup)")"
b="$(node -e "console.log(JSON.parse(require('fs').readFileSync('$work/origin-digest.json','utf8')).rollup)")"
echo "clone rollup:  $a"
echo "origin rollup: $b"
[ "$a" = "$b" ] || { echo "artifact digests differ between clone and origin" >&2; diff "$work/clone-digest.json" "$work/origin-digest.json" >&2 || true; exit 1; }
echo "clean-clone reproducible"
