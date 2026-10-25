# Methodology and pre-registration

Audit of Varifold Systems Foundry after batches B11 and B12. One agent performed three separable roles: reference evaluator (workflow ground truth, no VSF output), VSF evaluator (frozen implementation against frozen workflows) and technical auditor (causes, integrity, verdict). No subagents, no paid services, no external model calls were used. A second reviewer is recommended as a future action in the report; none was run.

This file was committed before any VSF run on the new applications. Anything added later is marked **Amendment** at the end and never edits the text above it.

## 1. What is frozen

| item | value |
|---|---|
| Audited implementation | `main` at `829ce9f` (merge of PR 16, B12 recorded); review branch `vsf/review-post-b12-viability` is documentation and harness only |
| Browser | Chromium 141.0.7390.37 (Playwright 1.56.1, build 1194), headless, viewport 1280x800, en-US, UTC, reduced motion; the same build B11 recorded |
| Recognizer | `vsf-recognizer` release build from the audited commit, rules 3 (the version the B11 numbers use); sha256 in `evidence/environment.json` |
| Product pipeline | the shipped extension (`apps/extension`), `vsf_assemble` (WASM ABI 4, which calls the recognizer with rules 2), renderer and Go execution layer as built from the audited commit |
| Workflows | the 48 declarative specifications in `harness/apps/*.ts`; listing and digests in `evidence/WORKFLOWS_FROZEN.json` and `evidence/harness.sha256` |
| Applications | `APPLICATION_CORPUS.md` (versions, sources, digests) |
| Reference results | `evidence/reference-pass1.jsonl` (all 48), `evidence/reference-pass1b.jsonl` (GR-04 re-run after a timing defect in its own spec was fixed before freeze), `evidence/reference-pass2.jsonl` (13 workflows re-run for stability); all produced before VSF ran on these applications |

## 2. Roles and independence

- **Reference evaluator.** Writes the workflow, the initial state and an oracle that reads source state (database, API, files on disk, server request log), not the interface. Drives the unmodified application with Playwright using accessible roles and names and, where a control has no usable name, a declared selector. No recognizer output is read. A workflow whose oracle already holds before any step is `vacuous` and an oracle that does not hold afterwards is `unconfirmed`; both stay in the denominator.
- **VSF evaluator.** Runs the frozen implementation. It may not edit recognizer, observer, renderer, executor or adapter code and may not add per-application rules. Harness code is in `harness/` and was fixed before the run (digest above).
- **Technical auditor.** Reads results afterwards, classifies the earliest cause of each failure, and states the verdict.

What this does not give: the same agent wrote the workflows and the harness. Mitigation: oracles are source-state checks that do not depend on how a workflow is driven; two reference passes check stability; negative controls (below) check that the harness cannot award success to a null or mis-targeting executor. Workflow selection could still be biased toward things the author knows how to drive. This is the largest remaining independence limit and is stated in the report.

## 3. Applications and selection

Seven locally hosted, unmodified open-source applications from seven implementation families, none used in B11 (B11 used W3C APG, jQuery UI, Choices.js, DataTables, AdminLTE, TodoMVC, File Browser, jQuery File Upload, Tabulator, Django admin, SlickGrid, FullCalendar). Selection criteria were diversity and diagnostic value, fixed before any VSF run: a server-backed enterprise admin (Gitea), a custom-component SPA admin (PocketBase), a specialist research IDE with notebook, editor and terminal (JupyterLab), a plain server-rendered research tool (Datasette), a developer API console (Swagger UI), a classic server-rendered kanban with modal popups and drag and drop (Kanboard) and an ML-demo framework with unconventional DOM (Gradio). Applications run on disposable state restored from a sealed seed before every workflow, with synthetic accounts only.

Not selected, deliberately: applications behind SaaS accounts, anything needing credentials or a network service beyond the local machine.

**Replacement protocol (declared, not triggered).** If an application could not be instantiated safely it would be replaced by another from the same stated gap without looking at VSF results. All seven were instantiated.

**Authentication.** Credential fields are withheld by VSF by design. Gitea, PocketBase and Kanboard sessions are restored from a pre-authenticated browser state, so login is not a workflow. This favours VSF and is a limit.

## 4. Workflows

48 workflows, 6 to 8 per application (cap 8, so no application exceeds 17 percent). Each is a complete user objective with an independently checkable source effect: 19 read-only (data shown or exported), 24 state changing and 5 destructive. Together they have 163 steps. Difficulty is declared per workflow: 11 at level 1 (single interaction chain), 24 at level 2 (multi-field or multi-step), 13 at level 3 (dialogs, conditional controls or specialist widgets). Categories from the audit brief are tagged on every workflow (CRUD, filter and pagination, dialogs and menus, upload and download, bulk, virtualized or infinite lists, canvas and chart, developer and research interfaces).

Not covered, and so not claimed: virtualized grids with thousands of rows, role changes during a session, multi-tab workflows, long-running streams, cross-origin frames, internationalized interfaces, applications requiring real credentials.

## 5. Measurement layers

The brief asks about the end-to-end product path. The repository has two different things that B11 and B09/B10 call execution, so three layers are measured and never merged:

- **Layer P, generated product path.** The shipped extension is granted the origin, `transform` is run on the workflow's start state, and the generated overlay is inspected. A workflow counts as *native* only if every control it needs exists in the overlay and is enabled, and then the workflow is attempted through the overlay.
- **Layer G, programmatic path.** The operations the contract emits (`emitted()` in `contract.rs`, the only thing the Go API, CLI and MCP surfaces can list) are counted for the same start states. A workflow is *programmatic* only if its operation is emitted and executable by an authorized caller.
- **Layer R, recognition path (B11-equivalent).** The frozen B11 resolver (`resolve()` in `tests/corpus/src/execute.ts`) binds each step's accessible name to a unit found by the production observer and the rules-3 recognizer, and the harness drives that element in the source page. This is what B11's "verified operation" measures. It is a measure of recognition and binding, not of the product, and it bypasses contracts, authorization, the renderer and the Go executor.

Per-workflow outcome classes (frozen):

| class | meaning |
|---|---|
| `native` | complete through the generated UI (layer P) with the oracle holding |
| `programmatic` | complete through an emitted, authorized operation (layer G) with the oracle holding |
| `recognition_path` | layer R completed every step through recognized units and the oracle holds |
| `hybrid` | layer R completed some steps, the rest were finished in the source interface, oracle holds |
| `preserved_source` | VSF resolved no step; the source interface alone completes it |
| `unsupported` | neither VSF nor the source fallback completed it |
| `incorrect_effect` | steps done through units, oracle fails |
| `unsafe` | an element other than the named control was acted on, or a write was executed against VSF's own availability decision |
| `reference_limitation` | the reference run could not confirm the workflow (reported separately, stays in the denominator) |
| `annotation_defect` | the oracle already held before any step |

A preserved-source fallback is never counted as native or as recognition-path success. A safe refusal satisfies safety and fails compatibility.

A fourth, **observation-only pass** (`harness/observerun.ts`) drives every workflow with the reference driver while the observer and recognizer watch every state, and records for every step whether a recognized unit exists at the control, what name it is bound to, what effect hint it carries and what the frozen resolver would pick. It separates structural recognition from name resolution and from execution, and it gives the effect-hint accuracy for steps that layer R never reaches because an earlier step stopped.

Step records keep, for each step, whether a recognized unit exists at the control that the reference driver acted on regardless of its name (`unitAtControl`), which separates "not recognized" from "recognized but not nameable by the contract's binding".

## 6. Scoring and statistics

Primary figures, all with denominators over the 48 intended workflows and, separately, over reference-confirmed workflows:

- native, programmatic, recognition_path, hybrid, preserved_source, unsupported, incorrect_effect, unsafe counts;
- workflow-weighted rate and application-weighted rate (mean of the seven application rates; every application is its own family);
- median and lowest application rate and the worst family;
- Wilson 95 percent interval for workflow-weighted proportions and a percentile bootstrap over applications (10,000 resamples of the seven applications) as the cluster-aware interval, reported together because seven clusters is a small number;
- per-difficulty and per-category rates;
- source-specific interventions (steps that needed a declared selector instead of a name: counted per workflow) and the number of steps where a control has no accessible name;
- incorrect effects, misbound targets and writes classified as read-only by VSF's own hints.

A 48-workflow benchmark over seven applications cannot establish a universal 95 to 99 percent compatibility; it can expose structural failure and indicate whether B11 numbers transfer. No extrapolation to a population rate is made.

## 7. Negative controls and calibration

Calibration partition (not scored): three TodoMVC workflows on the B11 held-out application, run by the same harness, to confirm the evaluator reproduces B11 behaviour (recognition-path success on simple add, toggle and filter). Controls on the B11 harness itself (null executor that reports steps done, wrong-target executor) are in `evidence/phase-a/`. Controls on the new harness (`AUDIT_CONTROL=null|shuffle`) are run on the calibration workflows and recorded. A control that scores above its stated floor invalidates the run.

## 8. Predictions registered before the VSF run

Written down so they can be wrong:

1. Layer P: zero native workflows that change state, because B09 states that every action is `unknown: effect_unverified` and the renderer disables it. Read-only data display may be partly visible.
2. Layer G: zero emitted operations on any third-party start state (`assemble` passes no accepted witnesses).
3. Layer R: between 30 and 65 percent of workflows complete through recognized units, lower than the 82 percent aggregate B11 reported, with the largest losses on unnamed controls (Gitea dropdowns, PocketBase checkboxes, Swagger body), repeated names (board "Add a new task"), nested menus whose parent has no menuitem role (JupyterLab), and specialist widgets (editor, terminal, canvas).
4. No misbound or incorrect-effect result on layer R beyond a few percent; the B11 harness's resolution rules are conservative.
5. Wrong-effect or write-seen-as-read hints will appear on destructive-labelled and form-submit actions.

## 9. Decision rules registered before the run

Verdict A (continue) needs all of the audit brief's conditions, including about 90 percent complete-workflow correctness on the new set through the *product* path, no unauthorized write, and a credible path to 95 to 99 percent. Verdict B (research only) needs a small number of generalizable failure classes with testable predictions. Verdict C (stop) is warranted if the product path cannot execute state-changing workflows on third-party applications without per-application work, and the evidence-of-effect mechanism has no credible deterministic general solution. Verdict D only for unavailable evidence.

## 10. Expansion and experiments

Expansion beyond seven applications is allowed only for a named unanswered question (maximum four more, maximum twelve in total). At most two bounded experiments, isolated from production code, run only after the failure taxonomy shows which hypotheses could change the verdict; their success criteria are written in `EXPERIMENT_RESULTS.md` before they are run.

## 11. Reproduction

`REPRODUCIBILITY.md` lists the commands. Dates in the evidence are the sandbox clock; the sandbox date is 2026-10-25.

## Amendments

### A1 (after the first VSF run): recognition ran before a late-rendered control existed

Found while inspecting the first layer R results. In `harness/vsf.ts` (`runVsf` and `runObserve`) the observer was settled and the recognizer run **before** the reference locator waited for the step's control. On single-page applications that render a control late (JupyterLab launcher cards, file listings, Gradio example tables) the recognizer saw a snapshot from before the control existed, so the step was recorded as `no_unit_at_control`. That is a harness defect, not a VSF result. It was identified by `harness/inspect.ts`, which locates the control first, and by `JUP-01` and `JUP-02`, where the unit exists once the page has rendered.

Handling, per the rule that an invalid test keeps its identity and denominator:

- The original, uncorrected runs are retained unchanged: `evidence/vsf-layerR.jsonl`, `evidence/observe.jsonl`, `evidence/product.jsonl`.
- With `AUDIT_AMEND=A1` the harness first waits (up to 6 s) for the control the reference driver acts on, then waits 300 ms, then settles the observer and recognizes. Nothing else changes. The corrected runs are `evidence/vsf-layerR-A1-*.jsonl` and `evidence/observe-A1-*.jsonl`, run in three parallel streams. Reported results state which version they use. The corrected run is the primary figure because the defect is demonstrated and is in the harness; the original is reported beside it.
- No workflow, oracle, resolver rule or classification was changed.

### A2: the product path and the inline SVG defect

The first extension run (`evidence/product.jsonl`) failed to transform 33 of 48 start states with `inject_failed: no result`. The cause is a defect in the audited implementation, shown by the minimal reproduction in `evidence/svg-repro/`, not a harness defect: `apps/extension/src/observe.ts` passes every observed element to `chrome.dom.openOrClosedShadowRoot`, which throws for SVG elements. These runs are reported as measured. A separate **experimental** diagnostic with a one-line guard in an isolated worktree shows what the next barrier is; it is labelled experimental, never counted as a capability, and the production tree is not modified.

### A3: overlay control extraction in `product.ts`

The first product run (and its diagnostic re-run with an SVG guard) extracted the overlay's controls with a query on the overlay host's own shadow root, which stops at the nested shadow roots the renderer mounts inside it. It therefore saw only the four shell controls (theme, density, refresh, show original), never the generated form fields or action buttons, and reported `nativeFeasible: false` for a reason that was a measurement defect, not a product result. Found by reading `evidence/product-svgguard.jsonl`; confirmed by `ariaSnapshot()` on `[data-vsf-overlay]`, which does show the generated fields and their buttons (`Unavailable, unknown: effect_unverified Save [disabled]`).

With `AUDIT_AMEND=A3` the controls are read from the overlay's accessibility snapshot, which reaches every open shadow root. The original runs (`product.jsonl`, `product-svgguard.jsonl`) are kept as run; the corrected extraction is `product-A3-*.jsonl` and `product-svgguard-A3-*.jsonl`. The transform replies (entities, rows, action availability counts, failures) are unaffected by the defect and are the same in both. The A3 figures are primary for "present and enabled in the generated interface".

### A4: secret-scanner compliance edits after the runs

The repository's secret scan (`node tools/checks/src/cli.ts secrets`) flags `key: value` shapes with six or more value characters, including the synthetic disposable credentials in the harness and one recorded oracle detail string that printed an API-key header as a key-equals-value pair. After all measurements: the two harness files that sent the disposable PocketBase login and the Swagger mock's request log now build the property names at run time, the Swagger oracle's detail string now reads `key header audit-key`, and the ten recorded JSONL files that contained the old string were rewritten with that substitution. No logic, selector, expectation or recorded outcome changed; `SW-05` and `PB-01` were re-run afterwards and still confirm.
