#!/usr/bin/env bash
# Builds the production WASM and the ABI-injected negative variant into artifacts/wasm.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
out="$root/artifacts/wasm"
mkdir -p "$out"
export SOURCE_DATE_EPOCH=0
export CARGO_ENCODED_RUSTFLAGS="--remap-path-prefix=${root}=/vsf"$'\x1f'"--remap-path-prefix=${CARGO_HOME:-$HOME/.cargo}=/cargo"$'\x1f'"--remap-path-prefix=${RUSTUP_HOME:-$HOME/.rustup}=/rustup"
build() { # target-dir features dest
  CARGO_TARGET_DIR="$1" cargo build --manifest-path "$root/compiler/Cargo.toml" --locked --release \
    --target wasm32-unknown-unknown -p vsf-wasm ${2:+--features "$2"}
  cp "$1/wasm32-unknown-unknown/release/vsf_wasm.wasm" "$3"
}
build "${VSF_WASM_TARGET_DIR:-$root/compiler/target}" "" "$out/vsf_wasm.wasm"
build "${VSF_WASM_TARGET_DIR_INJECT:-$root/compiler/target-abi-inject}" "inject-incompatible-abi" "$out/vsf_wasm.abi-injected.wasm"
sha256sum "$out"/*.wasm
