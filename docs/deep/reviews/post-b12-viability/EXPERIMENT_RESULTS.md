# Bounded feasibility experiments

At most two, isolated from production code, run only after the failure taxonomy showed which hypotheses could change the verdict. Both are research probes on the frozen workflows and states; neither is added to the measured results and neither alters the audited implementation. Success criteria below were written before either was run.

## Why these two

The frozen measurements (see `VIABILITY_REPORT.md`) point at two separable questions:

1. Most layer R failures are steps where a recognized unit already exists at the control but the contract's name does not equal the name a person sees (unnamed controls, captions that the binder ignores, placeholder or description text bound as the label). Is that a straightforward generalized fix, or is the name genuinely underdetermined by what the observer sees?
2. No state-changing action can ever be offered, because the contract needs a *behavioural witness* and nothing produces one. Can a deterministic procedure obtain effect evidence from what a browser extension can observe, in a disposable instance, without application-specific rules?

If the first holds, the recognition gap is engineering. If the second fails on ordinary applications, the original thesis (deterministic, general, no per-application integration) has no route to executable writes.

## Experiment 1: evidence-reconciled binding

**Claim.** Binding a step's target to a control using all evidence the observer snapshot already carries (explicit name, label relations, element text, title, placeholder, state fields, nearest preceding caption, ancestor heading), with a uniqueness constraint and a kind constraint, resolves at least 90 percent of the steps for which the frozen resolver fails although a unit exists at the control, with zero wrong bindings, without any application-specific rule, on at least five of the seven applications.

**Method.** `experiments/exp1_collect.ts` replays each workflow with the reference driver and saves, for every target step, the exact observer snapshot the recognizer sees (control awaited first, as in amendment A1) and the node id of the control the reference driver acts on. `experiments/exp1_eval.py` evaluates three resolvers offline on those snapshots: V0, the frozen B11 resolver's name equality; V1, equality against any evidence string; V2, token containment over all evidence strings with a unique-best constraint and the kind constraint. No recognizer or observer code is modified.

**Negative and adversarial checks.** Steps whose target is genuinely repeated on the page (the four "Add a new task" links, the two "Clear" buttons, the duplicated repository-name field) must stay unresolved. Names that appear nowhere on the page and near-miss names (a substring of another control's name) must not bind.

**Pass.** V2 correct on at least 90 percent of the V0 failures that have a unit at the control; wrong bindings 0 over all steps and all adversarial probes; V0's correct steps stay correct; the genuinely ambiguous steps stay ambiguous.

**Why it matters.** Passing makes the largest failure cluster a Category 1 fix. Failing, with wrong bindings or unrecoverable steps, shows that the contract's naming cannot be completed from the observation and needs either an authored name or a different evidence source.

## Experiment 2: effect evidence from browser-visible behaviour

**Claim.** In a disposable instance, whether a workflow changes source state can be derived from what an extension-level observer can see (same-origin request methods, WebSocket frames sent, storage changes) with no write classified as read and at least 90 percent accuracy, on at least two application families, without application-specific rules.

**Method.** `experiments/exp2_effects.ts` runs each of the 48 workflows with request, WebSocket and storage tracing, and measures ground truth independently by hashing the source state before and after (application API listing, database file, directory tree, mock server store). The classifier is fixed in advance: `write` if any same-origin request with a method other than GET or HEAD, or any WebSocket frame, follows the first workflow step; `read` if only GET or HEAD requests occur; `none` if nothing is observed. Ground truth is `changed` or `unchanged` from the state hashes.

**Adversarial examples built into the set.** The Gradio calculator posts to the server but changes nothing (a read through a POST); the JupyterLab terminal writes a file through a WebSocket only; Swagger UI "Execute" sends whatever method the operation declares; Datasette is GET only.

**Pass.** No workflow with changed state classified `read` or `none`, accuracy at least 90 percent overall and per family for the two families with most workflows, and none of the adversarial examples misclassified.

**Prediction recorded before running.** The request-shape classifier fails on the adversarial examples: POST-as-read in Gradio gives false writes, the terminal gives a false none. Request shape is evidence about transport, not about effect.

**Why it matters.** A pass would give the deterministic route to behavioural witnesses that the plan assumes. A failure on ordinary applications would mean witnesses need server-side read-back or documented schemas, that is, per-application or per-vendor integration, which the owner's thesis excludes.

## Results

Written after the runs; see below.
