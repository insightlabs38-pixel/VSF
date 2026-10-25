# Reproducibility

Everything below was run in the sandbox that produced the evidence: Linux x86-64, 4 cores, 15 GB, Node 22.22.0, pnpm 10.28.0, Go 1.24.7, Rust 1.97.0, Python 3.13, PHP 8.3, Chromium 141.0.7390.37 (Playwright 1.56.1 build 1194). Working directory is the repository root. The audited commit is `829ce9f` (`origin/main`); the review branch adds only `docs/deep/reviews/post-b12-viability/`.

## 0. Environment

```sh
pnpm install --frozen-lockfile
scripts/build-wasm.sh && scripts/build-native.sh && pnpm build:ts
cargo build --manifest-path compiler/Cargo.toml --release -p vsf-recognizer
# the pinned browser, not the sandbox default (a newer Chromium changes capture digests)
PLAYWRIGHT_BROWSERS_PATH=$HOME/pwb node_modules/.bin/playwright install chromium
export PLAYWRIGHT_BROWSERS_PATH=$HOME/pwb
```

Application cache (not committed, `.audit-cache/`, ignored through `.git/info/exclude`): Gitea binary, PocketBase binary, Kanboard and Gradio demo checkouts at the revisions in `evidence/environment.json`, `swagger-ui-dist@5.32.15` and the Petstore description, two Python virtual environments from `evidence/pip-freeze-main.txt` and `evidence/pip-freeze-gradio.txt`, and the sealed seeds (`seed/<app>`, plus `seed/<app>.storage.json` for the pre-authenticated applications). Seeds were produced once: users and sample data through each application's own API or command line, then the process was stopped and the state directory copied. `harness/apps.ts` restores a seed before every workflow and starts the application on a fixed port.

## 1. Phase A: historical B11 and B12 claims

```sh
cd tests/corpus
node --conditions=source src/cli.ts fetch --partition dev,validation,heldout        # pinned and hash-verified sources
node --conditions=source src/cli.ts capture --partition dev,validation,heldout --verify   # 81 of 81 digests
node --conditions=source src/cli.ts operations --partition dev,validation --jobs 3 --out ops-devval.json   # 118/142
node --conditions=source src/cli.ts operations --partition heldout --unseal --jobs 3 --out ops-heldout.json # 38/42 and 8/15
node --conditions=source src/cli.ts evaluate --partition dev,validation --out eval-devval.json
node --conditions=source src/cli.ts evaluate --partition heldout --unseal --out eval-heldout.json
```

Negative controls on the B11 harness: copy `tests/corpus` to `tests/_audit_ctl_null` and `tests/_audit_ctl_shuffle`, apply `evidence/phase-a/control-null.patch` and `control-shuffle.patch` to the copy's `src/execute.ts`, and run the same `operations` command in each copy. The copies were deleted after the runs.

Derived figures: `python3 -I harness/phase_a_stats.py` writes `evidence/phase-a/b11-harness-audit.json`.

Other reruns: `cd backend && go test ./execution/... -count=1 -race`, `cargo test --manifest-path compiler/Cargo.toml -p vsf-recognizer -p vsf-assemble --release`, the B12 suites below, and a hash check of all 82 plus 32 recorded evidence logs against their manifests (`evidence/phase-a/`).

Product-path probe on historical captures: `vsf-fabric assemble <snapshot.json> --origin http://corpus.invalid --variant var_00000000000000000000000000000000 --epoch 1 --observed-at 1700000000000` for ten pages of `.corpus-cache/captures`; every action in every output is `unknown`, `conflicted` or `unusable`.

## 2. Phase B: the new benchmark

```sh
H=docs/deep/reviews/post-b12-viability/harness
node $H/refrun.ts [--out=file] [ids or app names]        # reference driver and source-state oracle
node $H/vsfrun.ts [--out=file]                           # layer R, recognition path
node $H/product.ts [--out=file]                          # layer P and G, the shipped extension
node $H/observerun.ts [--out=file]                       # observation-only structural pass
node $H/perf.ts [--out=file]                             # latency and memory
node $H/freeze.ts                                        # workflow listing, harness digests, environment
python3 -I $H/analyze.py && python3 -I $H/analyze_steps.py
```

`--calib` selects the three TodoMVC calibration workflows. `AUDIT_CONTROL=null|shuffle` turns `vsfrun.ts` into the null or wrong-target control. Results land in `evidence/*.jsonl`; `evidence/harness.sha256` fixes the harness as it was before the VSF run.

## 3. What is not reproducible byte for byte

- Timing figures depend on the machine. Counts and classes are deterministic except where the application is timing sensitive (JupyterLab kernel start, Gradio example loading); the reference pass and the observation pass agree on every workflow's source-state result, and 13 workflows were re-run for stability.
- Gitea issue creation times and PocketBase record ids differ between seeds; oracles do not use them.
- The date in the sandbox (2026-10-25) is the only clock used.
