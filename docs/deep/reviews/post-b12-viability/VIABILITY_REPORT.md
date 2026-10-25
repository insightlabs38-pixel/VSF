# Varifold Systems Foundry: independent post-B12 viability audit

Audited commit `829ce9f` (`main`, B12 recorded). Review branch `vsf/review-post-b12-viability`, documentation and harness only. Sandbox date 2026-10-25. One agent performed three separable roles (reference evaluator, VSF evaluator, technical auditor); no subagents, no paid services, no external models. Methodology, frozen before the VSF run: `METHODOLOGY.md`; applications: `APPLICATION_CORPUS.md`; reproduction: `REPRODUCIBILITY.md`.

Wording used throughout: **observation** is something measured here or read in the code; **inference** is drawn from observations; **assumption** and **speculation** are labelled. "Historical" means the project's own recorded claim; "reproduced" means rerun here.

## 1. Executive verdict

**STOP / SHELVE the broad transformation project as specified (Verdict C).** Confidence in the verdict: moderate. Confidence that the current architecture and the planned batches B15 to B24 do not deliver the thesis: high.

The reasons, each with its evidence:

1. **The shipped product does nothing on third-party applications.** Of 48 workflows on 7 unmodified open-source applications, 0 complete natively and 0 programmatically. The shipped extension fails to transform 33 of the 48 start states (69 percent) because it throws on inline SVG icons; with that one defect patched in an isolated copy, all 48 transform and 0 actions are available, because the contract requires a behavioural witness and nothing produces one. (sections 2, 4.4)
2. **B11's numbers reproduce exactly and measure something else.** 118/142, 38/42 and 8/15 came out identically. They measure how well rules 3 name controls inside a Playwright harness that bypasses contracts, authorization, the renderer and the executor; 88 percent of the verified operations are one step and 13 percent contain a commit-like step. (section 3)
3. **They do not transfer.** On new applications the same recognition path completes **20 of 48 workflows (41.7 percent, Wilson 95% 28.8 to 55.7)**, 29 of 48 (60.4 percent) with hybrid, against B11's 82.4 percent aggregate (77.7 percent weighted by family). B11's rules 3 versus the rules 2 that ship: +1 workflow here against +25 of 142 on the development partitions. (sections 4.2, 5)
4. **Effect evidence has no general deterministic source.** A request-shape classifier is exact on the four REST-shaped applications (28 of 28) and wrong on ordinary applications that are not (Kanboard closes a task with a GET; Gradio posts every call, including pure reads). That is one write classified read, the unsafe direction. (section 7, `EXPERIMENT_RESULTS.md`)
5. **The cheap fix is small.** Name reconciliation, the largest Category 1 cluster, recovers 3 of 33 failing steps. The failure mass is in widget, scope, handler and specialist modelling (Category 2) and in credentials and unnamed controls (Category 4). (sections 6, 10)
6. **B15 to B24 do not address any of this.** They are hosted runtime, desktop, billing, team, private deployment and launch validation. The risk that decides viability, executable effects on pages the authors did not write, sits in none of them.

What is good and should not be lost: the safety design is coherent and was never contradicted (0 wrong effects, 0 misbound targets in my 147 target steps and in B11's 199, the full Go suite and the recognizer and assemble Rust suites pass, B12's tests pass); a recognizer that finds a unit at 96 percent of the controls it is asked about; a read-only structured mirror that works today on pages without inline SVG.

What the evidence does not show: that the idea is impossible. It shows that the work that remains is not engineering polish and that the one component that decides the thesis has not been attempted. Reopening conditions are in section 12.

## 2. What was actually implemented (B00 to B14)

141 of 192 queue tasks are done and 15 gates accepted (B00 to B14). About 33,000 lines of Rust, 32,000 lines of non-test Go (19,000 test), 39,000 lines of TypeScript including tests; the recognizer grew from 5,743 to 7,316 lines across B11 and B12. 203 commits over eight days.

| batch | delivered | connected to a third-party page? |
|---|---|---|
| B00 to B03 | toolchain pins, observation pack, IR 1.x, primitive registry (2 of 924 candidates implemented at B03; B11 and B12 report no primitive change) | n/a |
| B04 | DOM observer with replica and epochs | yes; no accessibility tree, network, canvas, placeholder or handler data |
| B05 | recognizer, evidence ledger, witness-gated availability | in a harness; the product assembly runs rules 2 |
| B06 | renderer with preservation fallback | yes, read-only |
| B07 | verified execution, unknown-commit, journal | fixture applications only; takes hand-written selector plans |
| B08 | versions, pins, diff, revocation | yes, as infrastructure; drift not measured |
| B09 | MV3 extension, local bridge | shipped path; **crashes on inline SVG**; "write operations stay unavailable" |
| B10 | REST, CLI, MCP | fixture source only; lists only emitted operations, none for third-party pages |
| B11 | corpus, composites, file transfers, bulk plans, rules 3 | corpus and recognizer: research harness only; Go file and bulk executors: libraries |
| B12 | terminal, editor, notebook, chart, transports, stream hub | libraries only; no recognition maps a page to them |
| B13, B14 | storage, authentication, managed sessions | infrastructure |

Does it form a functioning product pipeline? **No, not on any application the authors did not write.** Observation to action are connected in the fixture world and the pieces are individually well made. The links that would make it a product are missing: rules 3 are not in the assembly, no witness producer exists, no plan generator exists between recognition and the executor, the B12 adapters have no recognition input, and the shipped extension had never run on an application with SVG icons. Section 1 of `ARCHITECTURAL_FINDINGS.md` has the stage table.

## 3. Evidence integrity (Phase A)

### 3.1 What was reproduced

Same pinned Chromium (141.0.7390.37) and fetched sources as B11.

| historical claim | result here | status |
|---|---|---|
| 81 of 81 capture digests reproduce | 81 of 81 | independently reproduced |
| development and validation 118/142 verified, wrong effect 0, misbound 0 | 118/142, identical on all 146 rows | independently reproduced |
| AdminLTE and TodoMVC 38/42, File Browser 8/15 | 38/42 and 8/15 | independently reproduced |
| aggregate 164/199 = 82.4 percent | 164/199 | reproduced arithmetic; invalid as a generalization statistic (3.3) |
| composites 93/110, action-effect agreement 74.7 percent, held-out about 39 and 38 percent | 93/110, 0.747, 0.386 aggregate | independently reproduced |
| hint false claims rose 4 to 17 on one held-out group | 24 total on the 23 held-out pages, consistent | reproduced in aggregate; the group split was not re-derived |
| recognizer median about 45 ms | reproduced as measured (42 ms) | reproduced, but misattributed: the figure includes spawning a Python wrapper; direct spawn is 2 to 10 ms (4.3) |
| B12: 46 Playwright scenarios, 34 unit, 14 Go stream tests, all pass | 46, 34 and the stream tests under `-race` pass | independently reproduced |
| B12: components not yet recognized from applications, hub not exposed | confirmed by code search and by the benchmark (section 8) | confirmed |
| evidence logs (82 B11 runs, 32 B12 runs) | every log matches its recorded sha256, all PASS | reproduced |
| full Go suite, Rust recognizer and assemble suites | all pass | independently reproduced |

Nothing was contradicted. Some interpretations are contradicted: see 3.2 and 3.3.

### 3.2 Test quality

- **False positives are controlled.** A null executor that reports every step done scored 3 of 142 verified (2.1 percent; two are a negative control and an expectation that already held). A wrong-target executor scored 18 of 142 verified and 102 `misbound`; the 18 are operations whose page has no second control of that type, so the substituted control equals the intended one (inference from the control's patch, not checked row by row). Patches: `evidence/phase-a/control-*.patch`.
- **What a verified operation is.** 144 of 164 verified operations (88 percent) have one step; 5 verified operations are negative variants whose expectation is that nothing changes; only 27 of 204 non-negative operations (13 percent) contain a commit-like click or Enter; 1 of 212 operations is destructive; every expectation is read from the page (text 125, value 49, checked 35, absent 33, fragment 17, rows 14), none from source state. Steps name controls by the browser's accessible name, written by an annotator from that same accessibility tree, and the resolver matches them to recognized units by normalized name equality.
- **What it bypasses.** Contract availability, the renderer, the execution layer and authorization. A destructive or write step that the harness completes would be `unknown: effect_unverified` and disabled in the product.
- **Double counting.** Four TodoMVC implementations of one template contribute 24 verified operations (14.6 percent of the verified), all passing. Family-weighted over the 12 families the aggregate falls from 82.4 to 77.7 percent.

### 3.3 Independence of the historical partitions

| partition | families | status |
|---|---|---|
| development and validation (58 pages, 142 operations) | APG, jQuery UI, Choices.js, DataTables, jQuery File Upload, Tabulator, Django admin, SlickGrid, FullCalendar | used to design rules 2 and 3 and rank clusters; not independent |
| AdminLTE and TodoMVC (17 pages, 42 operations) | Bootstrap admin, TodoMVC x4 | opened for diagnosis; contaminated; one template counted four times |
| File Browser (6 pages, 15 operations) | File Browser | held out through the last measurement, looked at once afterwards; the only family not tuned against; **8/15 = 53 percent and unchanged by B11** |
| annotation | model sessions from an accessibility-tree dossier | no human or second-annotator agreement measured; ground truth is the accessibility tree |

The only never-tuned family scored 53 percent and did not move; B11's improvement is concentrated where it was diagnosed (B11's own limitation; the new benchmark confirms it, section 5). Historical results were not discarded; they are relabelled "reproduced, tuned, harness-level".

## 4. The independent benchmark

### 4.1 Design

Seven unmodified open-source applications from seven implementation families, none in B11, started from sealed disposable seeds before every workflow (`APPLICATION_CORPUS.md`): Gitea 28.1.0, PocketBase 0.40.4, JupyterLab 4.6.4, Datasette 0.65.5, Swagger UI 5.32.15 with the Petstore description, Kanboard 1.2.55, Gradio 6.30.0 with four demos. 48 workflows, 6 to 8 per application (no application above 17 percent), 163 steps, source-state oracles, three difficulty levels (11, 24, 13), 19 read, 24 write, 5 destructive. All 48 are reference-confirmed (47 in the first pass, `GR-04` after a timing defect in its own spec was fixed before the freeze; 13 re-run for stability, all stable); none is vacuous. Pre-registration was committed before the VSF run (`385689d`).

Limits stated up front: 48 workflows over 7 applications cannot establish a universal 95 to 99 percent compatibility and no such extrapolation is made; one agent wrote workflows, harness and analysis (mitigated by source-state oracles, negative controls and registered criteria); authentication was restored from seeded state so login is not a workflow; no true virtualized grid, no multi-tab or role-change workflows; one version per application so drift is unmeasured.

### 4.2 Results by layer

| measure | result |
|---|---|
| **P, shipped product path** | extension transform succeeds on 15 of 48 start states (PocketBase 8/8, Kanboard 6/6, Datasette 1/6); 33 fail with `inject_failed` |
| native workflow success (through the generated UI) | **0 of 48** |
| **G, programmatic** | 0 of 48; 0 actions `available` in 63 transform runs; 0 emitted operations |
| preserved original functionality | by construction: "Show original" restores the page; B09's fixture tests cover it, not re-measured on these applications |
| **R, recognition path (B11-equivalent)**, complete | **20 of 48 = 41.7 percent**, Wilson 95% 28.8 to 55.7; application-weighted 44.1 percent; cluster bootstrap over the 7 applications 27.8 to 56.8 |
| R, hybrid (some steps in VSF, rest in the source) | 9 |
| R, preserved source (VSF resolves no step) | 19 |
| R, complete or hybrid | 29 of 48 = 60.4 percent, Wilson 46.3 to 73.0 |
| over reference-confirmed workflows | the same (all 48 are confirmed) |
| incorrect effect, misbound target | **0 and 0** |
| steps finished in the source after VSF stopped | 86 of 163 (52.8 percent) |
| workflows whose failure includes an information or authorization limit | 3 (6 percent): `GITEA-06` and `SW-05` (credential fields), `DS-01` (controls with neither name nor caption) |

Layer R means: each step names the control as the browser names it, the frozen B11 resolver binds it to a recognized unit, the harness drives that element, the oracle reads source state. It is a measure of recognition and binding, not a capability of the product. Every layer R write workflow it completes would be disabled in the product. Corrected and original runs agree on all outcome classes (amendment A1 changed the recorded cause of 3 failures, not any outcome).

### 4.3 Per application, per family

| application (family) | n | R complete | R hybrid | preserved source | complete rate | complete or hybrid |
|---|---:|---:|---:|---:|---:|---:|
| Gitea (Fomantic server-rendered) | 8 | 3 | 3 | 2 | 37.5% | 75.0% |
| PocketBase (custom Svelte admin) | 8 | 1 | 2 | 5 | 12.5% | 37.5% |
| JupyterLab (Lumino) | 8 | 2 | 1 | 5 | 25.0% | 37.5% |
| Datasette (server-rendered) | 6 | 4 | 0 | 2 | 66.7% | 66.7% |
| Swagger UI (React) | 6 | 3 | 3 | 0 | 50.0% | 100% |
| Kanboard (PHP, jQuery) | 6 | 3 | 0 | 3 | 50.0% | 50.0% |
| Gradio (Svelte components) | 6 | 4 | 0 | 2 | 66.7% | 66.7% |

Median application 50 percent; lowest and, with seven applications, lowest decile: PocketBase 12.5 percent; worst family is the same (each application is its own family). By effect: read 10/19, write 6/24, destructive 2/5. By difficulty: level 1 6/11, level 2 9/24, level 3 3/13. By category (three or more workflows): form 8/17, table 4/10, crud 4/10, dialog 2/12, file 2/9, upload 0/4, filter 0/4, specialist 2/6, developer 3/6, research 4/7. Reading: the harder and the more consequential the workflow, the lower the rate; the workflows that change state are a quarter.

Step level (observation-only pass over all 147 target steps): a recognized unit exists at the control in 141 (95.9 percent), is usable in 134 (91.2 percent), and the frozen resolver binds the right one in 107 (72.8 percent), never a wrong one. Structural recognition is not the bottleneck; naming and operation semantics are.

### 4.4 Product path and the generated interface

The extension throws `TypeError: Error in invocation of dom.openOrClosedShadowRoot(HTMLElement element)` on any observed SVG element (`apps/extension/src/observe.ts:26-28`); minimal reproduction in `evidence/svg-repro/` (two lines of HTML apart). It affected 33 of 48 start states: all of Gitea, JupyterLab, Swagger UI, Gradio and 5 of 6 Datasette pages. It escaped because B09's browser tests use fixture pages without SVG and B11's harness does not install that capability.

With a one-line guard in an isolated worktree (experimental, never counted as a capability), the 48 start states transform in a median 142 ms (p95 229 ms: observe 48, assemble 74, render 21 ms; median 498 nodes, up to 1,411), with 1,538 actions `unknown`, 11 `conflicted`, 2 `unusable`, **0 available**, and 2,216 items withheld. Five workflows have every named control present and enabled in the generated UI; all are read-only, and **0 of 5 completed through the overlay** (controls disabled, absent under that name, or not reflected in what the interface shows). The generated interface exposes the source's fields but not the actions that make the form useful, and misreads some structure: the Kanboard board becomes a one-row table whose buttons are named by concatenated card text (`evidence/overlay-samples/`). "A cleaner interface that loses essential workflows is worse than the original interface" describes today's overlay on every state-changing workflow in this set.

Observation on effect hints, 43 committing controls (the last click of each workflow): of 22 in write workflows, 14 hinted write, 5 gave no hint, 1 had no unit, and 2 hinted navigation (`GITEA-03`, "Close with Comment", is a real error because *close* means dismiss; the other is the Gradio archive download link, which is a read); 6 of 16 in read workflows hinted write ("Submit" on a calculator, "Apply" on a filter form); of 5 destructive controls 2 hinted destroy, and the two confirm buttons and "Execute" gave no hint. Hints never authorize, but they cannot be witnesses.

## 5. Generalization

**Do B11's fixes transfer?** Post-hoc, not pre-registered. The same 48 workflows with the rules 2 that the shipped assembly uses give 19 complete, 10 hybrid, 19 preserved. Rules 3 give 20, 9, 19. Rules 3 changed **one** workflow (`GR-03`, a custom dropdown resolved through a recognized combobox popup). B11 reported rules 3 raising the development and validation partitions by 25 of 142 operations (65.5 to 83.1 percent), AdminLTE and TodoMVC by 4 of 42, File Browser by 0 of 15. This is the plan's own stop rule: "Revise core abstraction if held-out primitive fixes do not transfer across unrelated applications" (`02_Risks_and_Decision_Tree.md`). Caveat: B11's composites (tabs, pagination, bulk selection, trees) were barely exercised by these workflows (one popup), so what transfers is the part of rules 3 that independent workflows touch; the transfer of the untested composites is unknown.

**Across families.** Rates range from 12.5 to 66.7 percent with no pattern a single fix addresses: the apps with the best results are plain server-rendered HTML (Datasette) and a framework with strong ARIA (Gradio); the worst are a custom admin SPA (PocketBase) and a widget toolkit (JupyterLab). Poor accessibility semantics were not an excuse here; they are part of the result.

**Scale of the long tail.** The 28 incomplete workflows break into 11 clusters; six recur in two or more applications and cover 24 of the 28 first failures; five occur in one application (section 6). Each new application of seven contributed at least one cluster the others lacked.

## 6. Failure mechanisms

Earliest causal failure per incomplete workflow (`FAILURE_TAXONOMY.jsonl`, assigned after reading unit, DOM and recognizer output for each; `evidence/taxonomy-summary.json`). Category: 1 straightforward generalized fix, 2 significant but plausible algorithmic work, 3 application-specific or high-cost, 4 information or authorization barrier.

| cluster | class | first failures | steps | apps | cat. | mechanism |
|---|---|---:|---:|---:|:-:|---|
| C-NAME | incorrect structural matching | 6 | 6 | 4 | 1 | unit exists but its name is the title, placeholder or a description or header, not the caption a person reads (Jupyter launcher cards, Gitea colour field, Swagger body, Swagger select, PocketBase filter) |
| C-SCOPE | missing composite relationships | 6 | 7 | 5 | 2 | the same name on several controls (four "Add a new task", two "Clear", repository name in a modal and on the page); ambiguity is the correct answer without scope |
| C-HANDLER | observation / inaccessible state | 5 | 5 | 3 | 2 | clickable table cells, list items, example cells carry no role or name evidence; the handler is invisible to a DOM observer |
| C-WIDGET | incorrect operation semantics | 4 | 7 | 4 | 2 | custom select as an action, button-triggered file chooser, hidden file input, duplicated dropdown wrapper |
| C-CRED | authorization or session limitation | 2 | 3 | 2 | 4 | credential and API-key fields withheld by design; the workflow needs a person-supplied secret |
| C-SPECIAL | specialist adapter gap | 1 | 4 | 2 | 2 | CodeMirror, terminal and cell editors appear as generic text fields with the wrong label |
| C-ROWCTX, C-MENU, C-UNNAMED, C-DND | various | 4 | 7 | 4 | 1 to 4 | unnamed row checkboxes; submenu parent as a list item; controls with neither name nor caption; drag and drop |
| C-TEMPORAL | state-transition model | 0 | 1 | 1 | 2 | a menu item recorded not visible just after its menu opened |

Representative traces (`harness/inspect.ts` prints these):

- `GITEA-04`: the label dropdown yields three units, one bound "Label" but unusable, one inner unit unbound and usable; the resolver finds none. It is the "duplicated enhanced input" B11 listed as a follow-up.
- `JUP-02`: the launcher card is recognized and usable, named by its title attribute "Create a new text file"; the person and the accessibility tree call it "Text File". Reconciliation fixes this step; the next step, the editor, appears as a text field labelled "9 1" (the cursor position).
- `PB-02`: the table row opens a panel when its cell is clicked; the cell has no role. Only a click in a disposable instance, or the event listeners, would say so.
- `SW-03`: the parameter select is bound to the column header "Description", because the cell it sits in is under that header.
- `KB-01`: "Add a new task" occurs four times, one per column; correct output is "ambiguous".

Leverage (derivation: a fix counts only for workflows whose every failing step is in its clusters; an upper bound on recognition-path completion): all Category 1 clusters would unblock 5 workflows (20 to 25 of 48), adding scope and widget modelling 14 (34 of 48, 71 percent), everything except the Category 4 clusters 25 (45 of 48, 94 percent). Experiment 1 found the Category 1 estimate optimistic: name reconciliation fixed 1 workflow. Leverage on layer R is not product capability; see section 7.

## 7. Architectural viability

What remains plausible: deterministic structural recognition without model inference at run time; the witness-gated contract; recognition at interactive cost; a read-only structured mirror with the original one click away.

What needs major change: the connection between recognition and the product (rules 3 into assembly, plan generation, specialist wiring, SVG); naming and scope evidence; widget and composite models; observation of placeholder and handler evidence.

What meets a fundamental limit under the owner's constraints:

- **Evidence of effect.** The contract is right to require it. The only run-time source available to a deterministic, annotation-free extension is behaviour visible to the browser. Experiment 2 (48 workflows, state hashed before and after): overall 42 of 48 (87.5 percent). The four REST-shaped applications are 28 of 28; Kanboard's confirmed GET close is a write classified read; Gradio's five pure reads are classified write; JupyterLab's 8 of 8 is uninformative because every Jupyter request is a write by background autosave. Two ordinary applications of seven contain counterexamples; the transport carries no semantics (a POST can be a read, a GET can be a write, a WebSocket frame can be anything). Safe witnesses for those applications come from a documented schema, server read-back, or a person's assertion: per-application or per-vendor information, or a human step. The plan lists `user_assertion`, `documented_schema` and `protocol_trace` evidence types; none is implemented, and each is the "application-specific integration" the thesis excludes by default. Speculation: user assertion per action with post-hoc UI read-back may be a bounded burden for CRUD pages; it has not been tried and does not cover notebooks, terminals, exports or jobs.
- **Credentials.** Credential and secret fields are withheld by design, which removes user creation, key entry and login from any workflow set.
- **Hints.** English lexicon, `close` as dismiss, `commit` unknown.

The 95 to 99 percent ambition: the measured recognition-path ceiling from the clusters is about 94 percent (upper bound, not predicted) and the product-path ceiling is 0 until evidence exists; there is no measurement that links the two.

## 8. Specialized interface viability (B12)

B12's components are good libraries and are not integrable today. Observation: the benchmark's JupyterLab notebook, terminal and editor workflows (`JUP-01` to `JUP-03`) and the Datasette SQL editor all stop at the surface: the editor appears as a generic text field with a misleading label, the terminal as a field named by its input, the notebook cells as fields named by their prompt. Nothing in the recognizer, assembly, renderer or extension references a terminal, editor or notebook; `@vsf/specialized` is imported only by its tests; the Go `Hub` is referenced only by its tests; the reconnect client is test code; there is no live Jupyter or shell server in any test. B12's own report says so ("nothing here is recognized from a page yet").

Real remaining barriers, in order: recognizing the surface (a rule per library: xterm, CodeMirror 5 and 6, Lumino cells); a runtime channel into the live process (JupyterLab: kernel and terminal WebSockets, which Experiment 2 shows are opaque to request-shape evidence); the notebook model's kernel identity; authorization for code execution. Passing synthetic scenarios (46 browser, 34 unit, 14 stream tests) proves component behaviour and nothing about support for Jupyter, a shell or a chart on a source application. Chart workflows were barely covered (`GR-05` read the regression table; the plot itself is an image).

## 9. Security, performance and operational complexity

**Security (observation, bounded).** No unauthorized source write and no false VERIFIED effect was found: through the shipped path nothing can write, and in layer R the 0 misbound and 0 incorrect-effect results hold. This is absence of evidence in a bounded suite, not a proof, and it is cheap in a system that refuses to act. Findings that matter: (1) layer R, which B11 reports, completed 8 state-changing workflows (6 write, 2 destructive) that the product would have refused, so a reader must not take "verified" as authorized; (2) hint errors in the unsafe direction exist (`Close with Comment`) and the three confirm-or-execute controls have no hint at all; (3) the probing route to witnesses is unsafe on unknown writes by construction and needs disposable instances that production SaaS does not offer; (4) the bridge, pairing, tenant, session and unknown-commit machinery passes its suites and was not attacked further here. Credential, session and cross-tenant tests are B14's and were not repeated beyond running them.

**Performance.** Observer cold capture 1 to 19 ms (95 to 1,341 nodes), page load with and without the observer within a few percent, recognizer rules 3 2 to 10 ms and assemble 4 to 14 ms by direct spawn (p95 up to 20 ms), recognizer RSS about 11.5 MB, whole transform median 142 ms and p95 229 ms (`evidence/perf*.jsonl`, `product-svgguard-A3.jsonl`). No persistent instrumentation beyond the observer and no repeated probing are needed. The B11 figure "recognizer median about 45 ms" is dominated by spawning an interpreter. Interaction-to-verified-result latency cannot be reported for the product because no workflow completes through it; in layer R the median is 4.6 s against 1.8 s for the source alone, mostly harness waits and a recognizer call per step, which is a harness cost and not a product measurement.

**Operational complexity.** The plan assumed coverage per primitive; the measured relation is the opposite. B11 and B12 added about 1,600 recognizer lines (+27 percent) and about 7,800 lines overall across the recognizer, corpus harness, execution and specialized packages (45 files), and the measured transfer of those rules to new applications is one workflow. Eleven failure clusters appeared across seven new applications and five of them occurred in one application only. Application-specific exceptions did not grow (no vendor names in the recognizer, as B05 checks); the cost is in modelling work per widget family and per composite, which is open-ended. Drift repair, per-application annotation effort and support burden are unmeasured; speculation: the evidence problem, not recognition, would dominate support.

## 10. Future compatibility path

- **Straightforward fixes** (Category 1): SVG guard, rules 3 in assembly, caption-over-placeholder and card-text matching, submenu parents, placeholder in the observation. Measured leverage on layer R: about +2 to +5 workflows of 48. Cheap and worth doing in any scenario; none changes the verdict.
- **Algorithmic work** (Category 2): scope by owning composite, widget operation models, clickable-element inference, specialist wiring. Upper bound on layer R about +25 workflows (to about 94 percent); needs experimental validation per item; B11 shows a rule family of this size transferring poorly.
- **Product path from layer R**: requires effect evidence, which is not deterministic or general (Experiment 2). No figure is offered for it.
- **Application-specific intervention** (Category 3): per-application operation models or documented-API import, which does give evidence (six of the seven applications here have a documented HTTP, JSON-RPC or OpenAPI interface) and is a different, existing product category.
- **Barriers** (Category 4): credential fields (3 workflows here), unnamed controls, writes through opaque transports.

Evidence for a future 95 to 99 percent compatibility: recognition finds units almost everywhere; no wrong binding was ever produced; six recurring clusters. Evidence against: transfer of B11's gains is about one tenth of the claimed size; the cheapest hypothesis recovered 9 percent; the product path is zero for writes and evidence is not general; the 95 to 99 target was never measurable (the plan's own "Narrow" rule).

## 11. Alternatives

| option | what it is | assessment |
|---|---|---|
| A. Continue and refine the current architecture | fix SVG and wiring, push rules and specialist adapters, B15 to B24 | does not address the evidence problem; B15 to B24 are hosting, desktop, billing, team, private and launch work on top of an unproven core; the next measurable gain is a few workflows for each batch of rules and the cost of validating each is another independent corpus. Not recommended |
| B. Major but bounded behavioural-inference revision | disposable-instance probing, state-machine inference, user-asserted witnesses | the only option that attacks the real problem. Experiment 2 shows the first signal (request shape) is insufficient and unsafe; a revision would have to rest on read-back or human assertion. A bounded cycle can test it, but its best case is a human-confirmed product, not the stated thesis. Recommended only as an owner-authorized, separate research decision (conditions in section 12), not as continuation |
| C. Stop the broad transformation project | shelve; keep the infrastructure that has independent value (secure session, journal and unknown-commit, versioned artifact store) | **Recommended** |
| D. (separate, not a success claim) narrower positioning | read-only structured mirror of tables and forms; or evidence-backed operations imported from documented APIs | both are real, differ from the thesis, and are reduced scopes; D1 works on a minority of today's pages until the SVG defect is fixed; D2 competes with API-client generation. Requires explicit owner authorization before being treated as the project |

## 12. Final decision

**Recommended: Option C, stop and shelve the broad transformation project; do not start B15.**

Decisive evidence: the product path completes 0 of 48 workflows on unmodified third-party applications and fails to start on 69 percent of their pages; B11's headline numbers reproduce but measure the harness, and rules 3 transfer as +1 of 48 against +25 of 142 on the tuned partitions; no deterministic, general, annotation-free source of effect evidence exists, tested on ordinary applications; the remaining batches do not address any of that.

Rejected: A (continuing) for the reasons above; B as a continuation, because its best case redefines the product as human-confirmed; D as a way of declaring success.

Remaining uncertainties (honest limits): 48 workflows in 7 applications from one author; some of my workflows are harder than B11's (dialogs, uploads, unnamed controls), which lowers layer R but is also what ordinary applications contain; recognition-path numbers are far from product numbers, and I could not measure the product on any write; transfer of untested B11 composites; drift; real SaaS; Firefox and Safari; users and economics.

**What would change the verdict.** All of the following on a fresh, independently selected set of at least 60 workflows over at least 8 families not seen by the project, run through the shipped extension, assembly, renderer and an authorized executor with source-state oracles:

1. at least 70 percent complete natively or programmatically, with at least half of the write workflows among them;
2. zero incorrect effects, zero misbound targets, zero unauthorized or false-verified writes, including counterexamples of the three kinds found here (GET write, POST read, WebSocket write);
3. the effect-evidence mechanism stated and shown to need no per-application information beyond a bounded human review per action, with that burden measured;
4. the gain of rules 3 over rules 2 on that set at least 8 points;
5. a second reviewer's verification of the disputed observations (this audit did none).

**Recommended even if the owner proceeds anyway:** fix the SVG defect and put rules 3 into the assembly (days, Category 1), add a test that runs the shipped extension against at least five third-party applications, and register this benchmark as the permanent held-out set before any further rule work.

## 13. Confidence

| item | confidence | basis |
|---|---|---|
| current compatibility measurement (layer R 41.7 percent, P and G 0) | medium-high for the numbers, medium for their generalization | exact reproduction of B11, source-state oracles, negative controls; n = 48, 7 applications, one author |
| architectural generalization assessment | medium | clusters read case by case; Experiment 1 and the rules comparison agree; untested composites |
| safety assessment | medium | strong design, full suites pass, no counterexample found; little exposure because nothing writes |
| estimated future coverage ceiling | low | recognition-path upper bound 94 percent is derived, product ceiling not measurable |
| development effort and cost projection | low | no per-cluster effort data |
| final viability verdict | medium | the decisive uncertainty is whether a bounded human-assertion route is acceptable to the owner |

## 14. Amendments, deviations and what was not done

- Amendment A1 (recognition ran before a late control existed): corrected runs primary, originals kept, no outcome changed. A2: the SVG defect. A3: overlay control extraction used the wrong shadow root; corrected figures primary. (`METHODOLOGY.md`)
- Post-hoc and not pre-registered: the rules 2 versus rules 3 comparison, and the native attempts on the five feasible workflows.
- The shipped Chromium of the sandbox (153) differs from the pinned 141; the pinned build was downloaded and used for everything.
- Not done: expansion beyond seven applications (none needed; the verdict does not depend on the Stage E candidate, Streamlit); a second reviewer (recommended); cross-browser; drift; user studies; economic analysis beyond what the measurements support. No customer demand or revenue figure is asserted. Technical feasibility, product usefulness, economic maintainability and market validation are four different questions: only the first two have measurements here, the third has none, the fourth is untested.
- One tool-permission denial occurred when a commit command included a hook-path override; it was not retried that way and hooks were never bypassed.

## 15. Index of artifacts

`EXECUTIVE_SUMMARY.md`, `METHODOLOGY.md`, `APPLICATION_CORPUS.md`, `WORKFLOW_RESULTS.jsonl`, `FAILURE_TAXONOMY.jsonl`, `ARCHITECTURAL_FINDINGS.md`, `EXPERIMENT_RESULTS.md`, `DECISION_SCORECARD.md`, `REPRODUCIBILITY.md`; `harness/` (frozen measurement and analysis code), `experiments/`, `evidence/` (reference passes, layer R original and A1, product original and A3, observation passes, exp 1 and 2, controls, perf, Phase A runs and logs, SVG reproduction, overlay samples, environment, digests).
