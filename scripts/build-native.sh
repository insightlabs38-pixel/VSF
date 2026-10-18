#!/usr/bin/env bash
# Builds the native Fabric CLI and Go CLI into artifacts/bin with path-independent flags.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
out="$root/artifacts/bin"
mkdir -p "$out"
export SOURCE_DATE_EPOCH=0
export CARGO_ENCODED_RUSTFLAGS="--remap-path-prefix=${root}=/vsf"$'\x1f'"--remap-path-prefix=${CARGO_HOME:-$HOME/.cargo}=/cargo"$'\x1f'"--remap-path-prefix=${RUSTUP_HOME:-$HOME/.rustup}=/rustup"
target="${VSF_NATIVE_TARGET_DIR:-$root/compiler/target}"
CARGO_TARGET_DIR="$target" cargo build --manifest-path "$root/compiler/Cargo.toml" --locked --release -p vsf-native
cp "$target/release/vsf-fabric" "$out/vsf-fabric"
(cd "$root/backend" && CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags="-s -w -buildid=" -o "$out/vsfctl" ./cmd/vsfctl)
sha256sum "$out"/*
