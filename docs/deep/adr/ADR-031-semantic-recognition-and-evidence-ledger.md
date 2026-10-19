# ADR-031: Deterministic semantic recognition, evidence ledger and availability

Status: accepted (B05, gate G5). Code: `compiler/crates/vsf-recognizer`; fixtures: `fixtures/recognition/`; export: `Replica.toRecognizerInput()`.

## Context
B04 produces a faithful, incremental observation. Compilation needs fields, actions and collections with the evidence behind each claim, and it must say "unknown" before it guesses.

## Decisions
1. **Input.** `vsf-observation-snapshot-v1`: a preorder node list with stable ids, role, name, text, flags, relations and scope. It has no epoch, sequence or clock, so equal pages give equal bytes and an equal digest.
2. **Index.** Each node yields features (`tag:`, `role:`, `flag:`, `tok:`, `scope:`). Postings are sparse sorted lists or dense bitsets (dense above 1/32 of the nodes). A query is evaluated on postings only and must equal a reference full scan; the scan is kept as the oracle in tests.
3. **Units.** A recognition unit is a field, an action or a collection rooted at one node. Discovery uses the index; each unit is computed through a read-logging `Reader`, so its dependency set is exactly the nodes it looked at.
4. **Labels.** Order of authority: label relations and `aria-labelledby`, then the control's own name, then proximity, then (rules v2 only) delegated sources: column header, wrapper sibling, enclosing text. A label shared by several controls, or two competing nearby labels, binds nothing (`Ambiguous`). Duplicated labels are told apart by the nearest named region, not by choosing one.
5. **Candidates, not picks.** Role/tag overlap (a control that is also a button) and wording that names several verbs stay as separate candidates in a group with resolution `single`, `ambiguous` or `conflicted`. Verbs come from a fixed lexicon and are only hints (`effect_hint`); no effect is demonstrated by recognition.
6. **Ledger.** Every claim is content-addressed over subject, predicate, object, certainty and the digests of the nodes it cites, and cites a recorded source (an observation digest, a protocol document id or a decision id). Certainty is `Inferred`, `Unknown` or `Conflicted`; `Verified` needs behavioural witnesses and recognition cannot produce it. Claims go stale when a cited node digest changes.
7. **Incremental.** A reverse index from node id to the units that read it limits recomputation to affected units; creation and removal are found by discovery on the changed ids. The result must equal a from-scratch run (checked by randomized edit sequences and by mutation of the logging).
8. **Availability.** UI and tool contract derive from claims: `Unknown` and `Conflicted` withhold with a reason and show `needs_review` or `conflict_badge`; unusable (hidden, disabled, credential) stays disabled. A write tool is emitted only for a node that has a behavioural witness and no open ambiguity or conflict; recognition alone emits reads.
9. **Protocol reconciliation.** `vsf-protocol-evidence-v1` request fields are matched to UI labels by tokens (camelCase and snake_case split), scoped by region when labels repeat. Type disagreement and a required field absent from the UI become `Conflicted` claims citing both sides; unsupported or stale evidence can only raise `Unknown`. A conflict closes only through a recorded decision (identified principal, reason, side); the conflict claim itself is never rewritten.
10. **Rules versions.** V1 (direct evidence) and V2 (adds delegation) coexist. A change is accepted only if it fixes held-out fixtures from unrelated frameworks without altering any output on the regression corpus. No vendor or framework name appears in the recognizer (checked by test).
11. **No models.** The crate depends only on `vsf-canon`, and tests reject network, process and model references.

## Consequences
- Rust owns recognition. TypeScript only exports the snapshot. A Rust consumer of live deltas is left to the batch that needs it.
- Without behavioural witnesses no write tool exists; later batches supply witnesses.
- Column-header labels name the column, not the row; row identity stays with the collection.

## Rollback
The crate is additive and referenced only by its tests and CI. Removing it from the workspace and dropping the `recognition-snapshots` CI step restores the B04 state.
