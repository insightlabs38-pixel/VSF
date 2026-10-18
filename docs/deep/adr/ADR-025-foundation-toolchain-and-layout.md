# ADR-025: Foundation toolchain, layout, canonical profile and dependencies

Status: accepted (B00, gate G0)

## Context
B00 needs a reproducible polyglot foundation before any IR work. Rust, Go and TypeScript must exchange byte-identical data, and WASM must match native output.

## Decision
- Layout follows the queue's planned paths: `compiler/` (Rust), `backend/` (Go), `frontend/` (TS), plus `fixtures/ schemas/ registry/ tests/ tools/ docs/deep/`.
- Pins live in `toolchain/versions.json` and are cross-checked against `rust-toolchain.toml`, `backend/go.mod`, `.node-version`, `package.json` and exact devDependency versions. Rust 1.97.0, Go 1.24.7, Node 22.22.0, pnpm 10.28.0, Playwright 1.56.1 (matches the preinstalled Chromium r1194).
- Canonical JSON profile (`vsf-canon`, `@vsf/schema`, `backend/canon`): UTF-16 code-unit key order, minimal escapes, integers only within +/-(2^53-1), duplicate keys, floats, lone surrogates, depth over 64 and input over 16 MiB rejected. No Unicode normalization is applied. Decimal and normalization semantics are deferred to B02 and must be settled there before any IR field uses them.
- Constants (schema name, version, digest) are generated from `schemas/smoke/envelope.schema.json` into all three languages; CI fails on drift.
- WASM uses a plain C ABI with an explicit `vsf_abi_version` export instead of wasm-bindgen: no JS glue, no imports, and loader-side fail-closed ABI checks. A feature flag builds a deliberately incompatible module for the negative test.
- Dependencies (each justified): Rust `sha2`, `regex`; TS `typescript`, `@biomejs/biome`, `@playwright/test`, `ajv`, `@types/node`; Go standard library only. Node runs repository scripts directly via type stripping, so TypeScript is restricted to erasable syntax.
- Redaction patterns are one data file consumed by Rust and TypeScript; parity is tested.

## Alternatives
wasm-bindgen: larger glue surface and implicit ABI. serde_json: cannot enforce UTF-16 ordering or duplicate-key rejection without extra code. Workspace-wide ESLint/Prettier: more dependencies than Biome for no extra coverage.

## Consequences
- Measured: `vsf_wasm.wasm` is about 988 KB because `regex` carries Unicode tables; B18 must evaluate trimming it.
- CI actions are pinned by major tag, not commit SHA; SHA pinning is a B24 release-hardening item.
- Open owner decision: repository license. `LICENSE` is currently all-rights-reserved.

## Rollback
Revert the B00 merge; no external system or data depends on it.

## Revisit when
B02 defines real IR schemas, or profiling shows regex size or canonicalization cost matters.
