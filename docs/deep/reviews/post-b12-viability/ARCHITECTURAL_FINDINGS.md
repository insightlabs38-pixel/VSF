# Architectural findings

Observation means measured here or read in the audited code. Inference means a conclusion drawn from observations. Assumption and speculation are labelled. Figures come from `evidence/`; layer names are those of `METHODOLOGY.md` (R recognition path, P shipped product path, G programmatic).

## 1. The pipeline as it stands

| stage | what exists | connected to a third-party page? |
|---|---|---|
| Observation (B01, B04) | DOM mutation observer, replica, epochs, shadow roots, redaction | yes; the page snapshot carries tag, role, explicit name, text, label relations and a few states. It has no placeholder, no event handlers, no computed styles, no accessibility tree, no network, no canvas |
| Recognition (B05, B11) | rule-based units, ledger, composites, structural effect hints (rules 3) | rules 3 only in the research corpus harness. The product assembly calls `recognize(&obs, Rules::V2)` (`vsf-assemble/src/lib.rs:32`) and never reads composites |
| Contract (B05) | availability per action; a write tool needs a behavioural witness | `assemble` passes `AcceptedWitnesses::none()`, so no action can ever be `available` |
| Presentation (B06, B08, B09) | renderer, layouts, extension overlay, "Show original" | works on pages it can observe; every action is disabled with "Unavailable, unknown: effect_unverified" |
| Execution (B07, B13, B14) | Go executor with journal, unknown-commit, reconciliation, auth, storage | takes hand-written plans (`BrowserPlan` holds CSS selectors) or documented API operations; nothing produces a plan from recognition; only tests construct one |
| Interfaces (B10) | REST, CLI, MCP, error taxonomy, catalogs | fixture source only; list only operations the contract emits, which for third-party pages is none |
| Files and bulk (B11) | Go `FileExecutor`, `BulkExecutor` | libraries; not reachable from recognition |
| Specialist (B12) | terminal, editor, notebook, chart, transport models; Go stream hub | libraries; `@vsf/specialized` is imported only by its own tests, the `Hub` only by its own tests |

Observation: what B11 calls a verified operation (`tests/corpus/src/execute.ts`) resolves a step by annotated accessible name against recognized units and then drives that DOM element with Playwright. It does not touch the contract, the renderer, the executor or any authorization. Inference: the B11 coverage figures measure how well rules 3 find and name controls; the product capability they are read as is a different quantity and, on third-party applications, is currently zero.

## 2. The two defects that stop the product path

**Inline SVG crashes observation in the extension.** `apps/extension/src/observe.ts:26-28` forwards every element, SVG included, to `chrome.dom.openOrClosedShadowRoot`, which accepts only HTML elements and throws. `transform` then reports `inject_failed: no result`. 33 of the 48 start states (69 percent) fail; every application that draws icons with inline SVG fails (Gitea, JupyterLab, Swagger UI, Gradio, most Datasette pages). Minimal reproduction: `evidence/svg-repro/`. Category 1; it escaped because B09's browser tests use fixture pages without SVG and B11's corpus harness does not install the capability. Also an indicator: no test ever ran the shipped extension on an application the authors did not write.

**No witness producer.** The contract, the renderer and the executor all require evidence of effect before an action may be offered, which is the correct safety design. But nothing in the repository creates evidence for a real page: B09 states it ("write operations stay unavailable: no operation has a behavioural witness yet"), `assemble` hard-codes none, and the plan defers it to "later batches". Result: of 63 transform runs (15 as shipped, 48 with the SVG guard in an isolated worktree), 0 actions were `available`. Observation.

With the SVG guard (experimental, not counted) all 48 start states transform and the generated interface exposes the source's fields as editable inputs and every action disabled. Five workflows have all their named controls present and enabled; they are read-only (a filter, "Load more", a facet, a sort, a label dropdown). Attempting them through the overlay gave 0 successes: the controls were disabled, absent under that name, or did not change what the generated interface shows (`evidence/native-attempt-svgguard.jsonl`). Example of fidelity: the generated interface for the Kanboard board is a single-row table whose cells repeat card text and whose buttons are named by concatenations such as "Update docs: ~1h Task age in days ..." (`evidence/overlay-samples/KB-06.txt`).

## 3. Answers to the research questions

**Structural recognition.** Generalizes at the unit level and not at the semantic level. 141 of 147 target steps have a recognized unit at the control and 134 are usable (96 and 91 percent); the frozen resolver binds 107 (73 percent) and never binds a wrong control. The gap between 96 and 73 is naming, scoping and widget modelling, not detection.

**Behavioural inference.** Not implemented and not measured, except the effect probe below. The missing semantics are exactly the ones a DOM observer cannot see: clickable cells and list items with no role (6 steps), widget behaviour (7), repeated names that only a composite scope resolves (7), nested menus (1). Clicking and re-observing in a disposable instance could resolve them; it needs a disposable instance and never covers irreversible actions. Speculation: probing could lift recognition to the high 80s on read paths; there is no data on writes.

**Temporal state models.** Partly present as re-observation loops (the combobox popup in the B11 resolver) and not as an inferred state machine. One step (`PB-03`: a menu item recorded as not visible a moment after its menu opened and usable after a longer wait) shows the observer's visibility hint lagging the DOM, which B04 lists as a known limit. Scope, widget, handler, menu, temporal and row-context clusters, the ones that need state or structure models rather than names, are the only failing clusters in 17 of the 28 failed workflows.

**Composite inference.** B11's composites (tabs, pagination, bulk selection and so on) were hardly exercised by independent workflows: one step used a recognized combobox popup (`GR-03`, succeeded), none used a tab, tree, pagination or bulk composite. No virtualized grid with thousands of rows was in the set. Transfer of the composite work is therefore unmeasured by this benchmark; see section 4 for the rules 2 versus 3 comparison.

**Semantic ambiguity (lexical effect hints).** The hint lexicon holds about 75 base words plus 32 extended verbs, English only. On the 43 committing controls (the last click of each workflow): of 22 in write workflows, 14 hinted write, 5 gave no hint ("Commit Changes", "Run All Cells", "Execute" among them), 1 had no unit, and 2 hinted navigation ("Close with Comment", because *close* means dismiss, which is an error; the other is a download link, which is a read); of 16 in read workflows, 6 hinted write ("Submit" on a calculator, "Apply" on a filter form); of 5 destructive controls, 2 hinted destroy and 3 gave no hint (the two confirm buttons and "Execute"). Hints never authorize, so the observed errors do not yet cause a write; they are the reason an inferred hint cannot be a witness. Inference: any policy that lets a hint relax a prompt would be unsafe on this sample.

**Source-backed evidence.** The decision tree prefers documented protocols, and the Go API executor and contract model support that path. In this benchmark six of the seven applications (all but Swagger UI itself) ship a documented HTTP, JSON-RPC or OpenAPI interface. An operation model built from such a description is the proven way to get evidence-backed writes, and it is per-application or per-vendor information, which the owner's thesis excludes as a default.

**Deterministic synthesis (constraint reconciliation).** Experiment 1 tested a tiered evidence reconciliation for binding. It recovered 3 of the 33 failures that have a unit at the control, with no wrong binding, because the missing information is mostly not in the observation.

**Version drift.** Not measured: the benchmark uses one version per application. B08 builds the machinery (variant fingerprints, semantic diff, pins, revocation). Risk R05 in the plan stays open. Assumption: conservative contracts degrade to "unknown" under drift, which is safe and removes capability.

**Execution boundaries.** Authorization, source freshness, session ownership and unknown commits are implemented and tested for fixtures (full Go suite, `-race` on execution, passes). In this benchmark they never became the limiting factor because no workflow reached them; credential fields (2 workflows) are withheld by design and are a real limit on administrative forms.

## 4. Do B11's changes transfer?

Pre-registration did not include this; it is a post-hoc analysis and is labelled so. The same 48 workflows were run again with recognizer rules 2 (the version the shipped assembly uses) to see how much of the result depends on the rules B11 added. See `VIABILITY_REPORT.md` section 5 for the result and its interpretation.

## 5. Candidate fixes, ranked by measured leverage

Leverage is the number of the 28 not-completed workflows whose every failing step lies in the listed clusters (an upper bound, derived in `evidence/taxonomy-summary.json`). Confidence is in the evidence, not in the effort.

| candidate | clusters | workflows it can fully unblock on its own | applications | category | new observation needed | expected generalization | worst-case regression | complexity | security | overhead | still unsolved | confidence |
|---|---|---:|---:|:--:|---|---|---|---|---|---|---|---|
| name reconciliation (caption over placeholder, card text over title, aria-haspopup list items) | C-NAME, C-MENU | 5 (Experiment 1: 1 realistically) | 4 | 1 | placeholder, data-placeholder | high, rule-level | binds a near-miss to another control (3 of 147 probes) | low | none | none | unnamed controls, scoping | high |
| scoping by owning composite (dialog, panel, column, tile) | C-SCOPE | 4 | 5 | 2 | dialog and popover ownership, column headers | high, structural | wrong scope picks the wrong control: must stay ambiguous when unsure | medium | wrong-target writes if scope is wrong | small | handler-only elements | medium |
| widget operation models (custom select, file-chooser button, hidden file input, drag and drop) | C-WIDGET, C-DND | 4 | 5 | 2 | more states per widget; a probing step | medium; each widget family is its own model | mis-modelling a chooser as a toggle | medium to high | file transfer policy applies | small | specialist surfaces | medium |
| clickable-element inference (cells, rows, list items) | C-HANDLER, C-ROWCTX | 3 to 4 | 3 | 2 to 3 | event handlers or cursor and tabindex evidence; probing | low to medium without a handler channel | false clickables | high | probing executes handlers: unsafe on writes | needs repeated probing | row identity naming | low |
| specialist surfaces mapped to B12 adapters | C-SPECIAL | 1 on its own (the three Jupyter tasks also need the launcher fix) | 2 | 2 | adapter attachment points, kernel/session identity | per library; xterm and CodeMirror are common, notebooks are not | adapter owns input the source also handles | high | process ownership model exists, not wired | per library | live Jupyter or shell server needed | low |
| effect evidence (a witness) for writes | all write workflows | 0 on its own; it is a precondition for every write to be offered | 7 | 4 | read-back, schema, assertion or probe | not deterministic: Experiment 2 | a wrong witness is a false VERIFIED write | per application unless a person asserts | the critical risk | disposable instance | all of it, today | high that the problem is real |

## 6. Counterexamples that defeat the tempting general methods

Two kinds were found in ordinary applications, not contrived ones.

1. Matching DOM and accessibility semantics with opposite hidden behaviour: a "Submit" button that posts to the server and changes nothing (Gradio) versus a "Submit" that creates a record (Gitea). Request shape, name, role and position are identical; only server state tells them apart.
2. State changes through transports that carry no verb: Kanboard closes a task with a confirmed GET; JupyterLab's terminal writes through a WebSocket.

Inference: these are not rare. Six of the 48 workflows (12.5 percent, in 2 of 7 applications) are counterexamples to request-shape classification, and the plan's own risk register lists race, double write and ambiguous commits as top risks. How much they matter depends on the share of workflows that change state: 29 of the 48 here, 60 percent.

## 7. What still looks plausible

- A deterministic, no-LLM recognizer that finds and names the controls on most pages is plausible: the units are there on 96 percent of target steps, nothing was bound wrongly, and the recurring failure clusters are few (six clusters recur across two or more applications and account for 24 of the 28 first failures).
- The safety design (witness-gated availability, unknown-commit, at-most-once input, tenant and session binding) is coherent, well tested, and exactly what makes the current product refuse to act.
- A read-only structured mirror of tables and forms with the original one click away is a working product shape today on pages without inline SVG.

## 8. What looks implausible under the stated constraints

- Executable writes from third-party pages with deterministic, general, annotation-free effect evidence. Experiment 2 and the counterexamples above are the evidence; the only generic signal that the design can use at run time is request shape, which is wrong in both directions on ordinary applications.
- Reaching 95 to 99 percent complete-workflow compatibility by adding primitives. The recognition failures are not primitive gaps: 20 of 30 unrecovered naming-related failures need structural models, 6 are policy or information limits, and the measured transfer of B11's gains to new applications is far below B11's own figures.
