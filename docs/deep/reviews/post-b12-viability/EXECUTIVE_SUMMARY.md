# Executive summary

**Verdict: STOP / SHELVE the broad transformation project as specified.** Confidence: moderate in the verdict, high that the current architecture and the planned batches B15 to B24 do not deliver the thesis. Audited commit `829ce9f`; full report `VIABILITY_REPORT.md`.

**What was measured.** B11's numbers were re-run and match exactly (118/142, 38/42, 8/15; 81 of 81 captures; B12's 46 browser and 34 unit tests pass). Then 48 frozen workflows on 7 unmodified open-source applications from 7 families that B11 never used (Gitea, PocketBase, JupyterLab, Datasette, Swagger UI, Kanboard, Gradio), with source-state oracles, all reference-confirmed.

| result on the new applications | value |
|---|---|
| complete through the shipped product (generated UI), native | **0 of 48** |
| complete through an emitted, authorized operation (API, CLI, MCP) | **0 of 48**; 0 actions available in 63 transform runs |
| shipped extension can transform the start state | 15 of 48 (it throws on inline SVG icons; reproduction in `evidence/svg-repro/`) |
| complete through the B11-style recognition path | **20 of 48 = 41.7 percent** (Wilson 28.8 to 55.7; applications 12.5 to 66.7 percent) |
| recognition path plus hybrid | 29 of 48 = 60.4 percent |
| B11's own aggregate | 82.4 percent (77.7 percent by family) |
| incorrect effects, misbound targets | 0, 0 |
| rules 3 against the rules 2 that ship, on the new set | 20 against 19 workflows (B11: +25 of 142 on its tuned partitions) |

**Why stop.**
1. The product has never worked end to end on a page its authors did not write. Besides the SVG crash, every action is disabled because the contract requires a behavioural witness and nothing produces one.
2. B11's figures measure a harness that bypasses the contract, renderer, executor and authorization, over operations that are 88 percent single-step and 13 percent commit-like; they do not transfer (one workflow here).
3. Effect evidence has no general deterministic source: a request-shape classifier is right on the 4 REST-shaped applications (28 of 28) and wrong on ordinary ones (a GET that closes a task, POSTs that only read), once in the unsafe direction.
4. The cheapest generalized fix, name reconciliation, recovers 3 of 33 failing steps; most of the rest need widget, scope, handler and specialist modelling or are credential and information limits.
5. B15 to B24 are hosting, desktop, billing, team, private and launch work. None addresses the decisive risk.

**What is sound.** The witness-gated safety design, unknown-commit and session machinery, versioning and storage, and a recognizer that finds a unit at 96 percent of the controls it is asked about, with no wrong binding ever observed. The audit found no unauthorized write or false VERIFIED effect; with no writes executable, that is weak evidence.

**What would change the verdict.** A fresh independent set of at least 60 workflows over at least 8 families, run through the shipped path, with at least 70 percent native or programmatic completion (half of the writes), zero incorrect or unauthorized effects including GET-write, POST-read and WebSocket-write counterexamples, a stated evidence mechanism whose per-application burden is measured, and a rules 3 gain of at least 8 points. Full conditions in section 12.

**Limits.** 48 workflows cannot establish a 95 to 99 percent claim either way. One agent wrote workflows, harness and analysis; no second reviewer (recommended). Authentication was restored from seeded state. Market demand and economics were not examined and nothing is asserted about them.
