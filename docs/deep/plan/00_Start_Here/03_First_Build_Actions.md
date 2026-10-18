# What to implement first

The plan is ready to hand to an agent/team, but the first code change should be **B00** rather than an unbounded request to build VSF. This document is the immediate actionable starting checklist.

**Execution entry point:** `07_Development_Sequence/03_Executable_Task_Queue/TASKS.md` lists 152 planned tasks across B00–B24. Start with `10_Agent_Prompts/01_Bootstrap_B00_Batch_Prompt.md` and use `02_Keep_Going_Prompt.md` for later invocations. Bootstrap the queue into the repository without losing progress.

## B00 branch

1. Create mono-repository and pinned language toolchains (Rust, Node/TypeScript, Go) and licenses/docs.
2. Scaffold typed schema, native/WASM compiler stub, Go CLI/control-plane stub and TS browser extension/renderer stub.
3. Build synthetic app fixtures: server-rendered form, React-like virtualized table, embedded terminal placeholder, sequence editor fallback.
4. Generate a reproducible CI run with test commands, signed/hashed test artifact manifests, dependency/security checks and a known-failing regression.
5. Import the 924 catalog as an unimplemented candidate registry and the 41-gap register as a risk baseline. No automatic 'implemented' status.
6. Record schema and extension policy risks in ADRs, then start B01 browser boundary experiments.

## Developer workflow

Each batch should be its own branch/PR with coherent checkpoint commits and a **24-line maximum** routine report under `docs/deep/reports/`; detailed test artifacts belong under `docs/deep/evidence/`. Commit/PR messages are under 10 lines and code comments are one line only when necessary. Git identity: `insightlabs38-pixel` / `insightlabs38@gmail.com`. For challenging stages, divide into sub-batches but maintain the stable B00–B24 identifiers and their gates.

## Stop before large-scale primitive work

Do not scale parallel development of the catalog until B02 (IR+versioning contracts), B03 (registry/conformance), and B05 (recognition evidence) are sufficiently stable. Doing otherwise invites incompatibilities and wasted work despite high apparent output volume.
