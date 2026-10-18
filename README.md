# Varifold Systems Foundry (VSF)

Deterministic, primitive-first compiler from authorized web applications to accessible custom UI plus generated API, CLI and MCP contracts. The source application stays the system of record; no LLM inference is required to compile or execute.

## Layout

| Path | Purpose |
|---|---|
| `compiler/` | Rust workspace: canonical JSON, schema envelope, redaction, Fabric stub, native CLI, WASM bridge |
| `backend/` | Go module: CLI and control-plane stubs sharing the canonical profile |
| `frontend/packages/` | TypeScript packages: `@vsf/schema`, `@vsf/wasm-host` |
| `fixtures/` | Synthetic source apps and deterministic golden outputs |
| `schemas/` | Single-source contracts and shared test vectors |
| `registry/` | Imported candidate capability inventory and gap baseline (unimplemented) |
| `tests/` | Cross-language chain tests and Playwright end-to-end tests |
| `tools/` | Repository checks, redaction and the queue/evidence tooling |
| `docs/deep/` | Plan archive, task queue, ADRs, batch reports and evidence |

## Reproduce

Toolchains are pinned in `toolchain/versions.json` (Rust, Go, Node, pnpm, Playwright).

```sh
pnpm install --frozen-lockfile
scripts/build-wasm.sh && scripts/build-native.sh && pnpm build:ts
node tools/checks/src/cli.ts ci-run hygiene   # also: native go ts wasm-browser cross-language
```

`scripts/clean-clone-check.sh` clones the committed tree elsewhere, builds it and compares artifact digests.

## Rules enforced by tooling

Commit messages, PR titles and bodies: fewer than 10 lines, no attribution footers or links. Source comments: one line. Reports: at most 24 lines. Progress lives in `docs/deep/queue/state.json`; `TASKS.md` is derived from it.
