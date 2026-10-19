# ADR-032: Foundation stabilization before the presentation renderer closes

Status: accepted (B06, gate G6). The G5 PROCEED decision stays historically accurate; it described the B05 code as merged, and the findings below were found afterwards. Corrections are queue tasks `VSF-B06-T09`..`T18`, all required for G6.

## Context
Reviewing B00-B05 interfaces before the renderer consumed them found defects that would have cost more to fix once B07+ depended on them: credentials classified by page-controlled hints, protocol documents trusted as written, claim ids that ignored their source, witnesses that were untyped strings, lexical verbs reachable as read-only authority, a CI gate that did not wait for all jobs, and no revocation or version invalidation.

## Decisions
1. **Correction lane.** `lane: "correction"` tasks run ahead of the sequential lane and the gate refuses to close while any is open. Earlier partial tasks are not marked done to satisfy the selector.
2. **Sensitive data (F1).** The scope floor comes from the control's intrinsic type and labels; the strictest ancestor marker wins; page markers can only raise it. Shared evidence is structure-only unless a validated share policy permits text. Redaction runs before clipping.
3. **Protocol evidence (F2).** Documents are fully validated and carry a standing: authoritative, advisory or rejected. Only a promotion bound to the trust context makes evidence authoritative; unsigned or unbound evidence can only raise unknown.
4. **Ledger (F3).** Claim ids are content addressed over source, producer, unit and sensitivity. Local and shared exports differ; a tampered ledger fails to load.
5. **Witnesses (F4).** A behavioural witness is typed and bound to the node digest and trust context, with freshness, revocation and issuer trust. Synthetic witnesses prove nothing outside tests.
6. **Availability (F5).** Intent, declared effect and demonstrated effect are separate. Lexical hints never authorize an action; availability has Local, Read, Write and Invoke levels.
7. **CI (F6).** The gate job waits on every job and a checker compares it with the workflow. Vulnerability checking queries OSV in bounded batches; a network or scanner failure is `unavailable` (exit 3), never clean. Ignores expire. The failure canary is unchanged.
8. **IR 1.1.0 (F7).** Additive only: trust context, revocation, version manifest, operation witness and environment binding. Readers decode every released version through the `ACCEPTED` table; a document is digested at its own version. The registry manifest must be rebuilt for 1.1.0 and verifies under an explicit trust set or `--development`.
9. **Renderer.** Presentations are refused for another semantic digest; the shadow root is closed by default; script-made clicks are ignored; names come from the semantic. Source styles are adopted in a layer behind a guard and validated by a tokenizer.

## Migrations and compatibility
- IR 1.0.0 documents decode unchanged; 1.1.0 additions are optional or gated by `since`. Released snapshots live in `schemas/ir/released/`, and the compatibility checker passes additive against 1.0.0.
- Evidence ledger v2 and the contract document change shape; B05 fixtures were regenerated and drift-checked. Rules V1 and V2 outputs on the regression corpus are unchanged.
- Rollback: revert the correction commits; 1.0.0 readers still accept 1.0.0 documents.

## Deferred, with reasons
- `VSF-B07-T09`: execution consumes only `Validated<T>` minted by Rust validation; no executor exists yet.
- `VSF-B08-T09`: signed manifests need sequence and expiry; no manifest publisher exists yet.
- Source styles guard the host only: a source rule hiding `html` or `body` is outside what a content script can prevent.
- English-only verb lexicon and untrusted page markers remain hints.

## Consequences
Later batches inherit stricter contracts: unknown or unbound evidence withholds, and every authority path needs a bound witness or promotion.
