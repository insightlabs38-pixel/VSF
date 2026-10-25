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

Both experiments ran on the machine and states of the frozen benchmark. Raw data: `evidence/exp1-results.json`, `evidence/exp2-results.jsonl`, `evidence/exp2-summary.json`. Code: `experiments/`. Neither changes any figure in the benchmark results.

### Experiment 1: evidence-reconciled binding. Criterion not met; the hypothesis is refuted as stated

147 target steps from 48 workflows, 141 of them with a recognized unit at the control. The frozen resolver (reimplemented offline; it agrees with the recorded layer R result on 146 of 147 steps) resolves 108. The 33 failures with a unit at the control were the population the claim was about.

| resolver | correct | ambiguous | none | wrong |
|---|---:|---:|---:|---:|
| V0 frozen name equality | 108 | 6 | 33 | 0 |
| V1 tiered exact match on all evidence strings | 111 | 6 | 29 | 1 |
| V2 V1 plus token containment, unique best, kind constraint | 111 | 8 | 28 | 0 |

V2 recovers **3 of the 33** failures (9 percent; the registered pass was 90 percent): `GITEA-05` "Color" (read from its caption, not its placeholder), `JUP-02` "Text File" and `JUP-03` "Terminal" (matched by the card text instead of its title). One workflow, `GITEA-05`, would complete as a result; the other two still stop at their editor and terminal. V2 has no wrong binding and no regression on V0's correct steps; V1 binds one control wrongly through the caption tier, which is why the tiering matters.

The remaining 30 failures are not name-evidence problems, by cause:

| cause of the 30 | steps | evidence in the snapshot |
|---|---:|---|
| custom select, dropdown, file-chooser button modelled as the wrong kind (`C-WIDGET`) | 7 | the control is recognized, as an action or an unbound duplicate; it needs an operation model, not a better name |
| same name repeated across columns, panels, dialogs, tiles (`C-SCOPE`) | 7 | genuinely ambiguous without the owning composite; the correct resolver result is "ambiguous" |
| credential or secret field withheld by policy (`C-CRED`, 3) and a popover item whose visibility had not yet updated (`C-TEMPORAL`, 1) | 4 | the policy is working as designed; the popover resolves after a longer wait |
| editor, terminal, notebook surfaces (`C-SPECIAL`) | 4 | recognized as generic text fields with a wrong label |
| row checkboxes with no name (`C-ROWCTX`) | 3 | identity is the row, not a string |
| unnamed native controls (`C-UNNAMED`) | 2 | no name or caption exists in the observation |
| placeholder, description or header not captured by the observer (`C-NAME`) | 3 | the observer snapshot has no placeholder field, so the evidence is not there |

Six more steps have no recognized unit at the control at all (clickable table cells, a list item, a submenu parent): the handler is invisible to a DOM observer.

Adversarial probes on V2: a name that appears nowhere bound 0 of 147 times; a near-miss name (the first token of the target, or the target with a word added) bound 22 of 147 times, 3 of them to a different control, so the fuzziness has a real misbinding cost. Self-resolution over every named usable unit on all 147 pages (7,675 queries) gave 5,272 correct, 2,403 ambiguous and 0 wrong for V0 and V2 alike.

**Reading.** Name reconciliation is a real but small fix (one workflow here, two more steps). The largest part of the gap between structural recognition (96 percent of steps have a unit) and name resolution (73 percent) is unrecoverable from the same observation without modelling widgets, scoping by composites, or new observations such as placeholder text and event handlers. That moves most of the failure from Category 1 to Category 2 and, for naming and credentials, Category 4.

### Experiment 2: effect evidence from browser-visible behaviour. Criterion not met; the safety criterion fails on ordinary applications

Overall 42 of 48 workflows classified correctly (87.5 percent; registered pass 90 percent). Per application: Gitea 8/8, PocketBase 8/8, Datasette 6/6, Swagger UI 6/6, Kanboard 5/6, Gradio 1/6, JupyterLab 8/8. The four REST-shaped applications (28 of 28) are classified perfectly; that is the case where the method looks sound.

| error | workflows | mechanism |
|---|---|---|
| write classified as read (the unsafe direction) | `KB-04` | Kanboard closes a task with a confirmed GET navigation; no non-GET request, no state-changing verb |
| read classified as write | `GR-01` to `GR-05` | Gradio posts every call, including a pure calculator, to the queue endpoint |

So one of the two ordinary non-REST applications defeats the classifier in the dangerous direction and the other in the needless-prompt direction. The criterion "no state change classified read" fails. JupyterLab scored 8/8 only because the set holds no read-only JupyterLab workflow: background `PUT` requests (workspace autosave, checkpoints) accompany every action, so it would label any action a write, and the terminal's actual effect is carried by WebSocket frames whose meaning is invisible. That success is uninformative and is not counted as evidence for the method.

**Reading.** Request shape is evidence about transport, not about effect. Where an application follows REST verb semantics the classifier works and a witness could be produced mechanically; where it does not (GET mutations, POST-for-read RPC, WebSocket protocols), the only generic classifier gives wrong answers in both directions, including the unsafe one. Safe witnesses for such applications need the application's own read-back or documented schema, which is per-application or per-vendor information. This is the finding that most affects the verdict.

### Not done

No third experiment, no expansion beyond the seven applications, and no attempt to build a better classifier after seeing the errors; the registered criteria were applied as written.
