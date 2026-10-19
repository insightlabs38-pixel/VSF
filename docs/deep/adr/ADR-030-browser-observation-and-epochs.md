# ADR-030: Browser observation, epochs and incremental events

Status: accepted (B04, gate G4). Code: `frontend/packages/observer`; tests: `tests/observation`, `tests/browser/specs/extension-observe.spec.ts`; schema: `schemas/protocol-evidence/`.

## Context
B01 proved a one-shot, flat snapshot. Compilation needs observations that survive change: nested scopes, partial access, updates smaller than the page, and an honest statement of what each runtime cannot see.

## Decisions
1. **One tree, explicit boundaries.** Elements are nodes. Documents, shadow roots and inaccessible regions are pseudo-nodes (`#document`, `#shadow-root`, `#opaque`) carrying scope metadata. Cross-origin, sandboxed, policy-blocked frames, canvases, plugins and closed shadow roots without runtime help become `#opaque` placeholders with origin and reason and no children; nothing inside them is guessed. A custom element that may hide a closed root is counted, not invented.
2. **Grants first.** A session refuses to start (`origin_not_granted`) unless the policy allows the document origin, and every embedded document is checked again; an accessible frame outside the grant is a placeholder.
3. **Epochs, sequence, digest.** An epoch is one baseline. Epochs come from a per-tab, per-origin counter that never falls below the clock, so lost storage cannot reuse one. Deltas carry the epoch, a sequence that rises by one, and a digest (order-independent sum and count of the whole tree) so a consumer proves it equals the producer. A consumer applies a batch completely or not at all.
4. **Recovery is bounded.** A gap is repaired by replay from a ring of recent batches, else by one full resync in a new epoch; a subtree resync rebuilds one node's children inside the epoch. Frame navigation is a subtree change with a new frame generation; replacing the top document starts a new epoch; SPA route changes keep the epoch and publish a `nav` op.
5. **Work proportional to the change.** Records are grouped by parent. Plain adds and removes touch only those nodes; anything ambiguous (moves, reorders) reconciles that parent's children with a longest-increasing-subsequence diff. Names that depend on other nodes are tracked so a changed label updates its controls.
6. **Lazy evidence.** Names, roles, relations and visibility are computed per element on capture or change. Geometry is read only on `probe`, stamped with epoch and sequence. Stylesheet-only visibility changes are noticed at the next probe or related mutation.
7. **Advertised limits.** `captureCapabilities` lists what this runtime can and cannot do (closed shadow roots only through the extension API, cross-origin frames never, no browser accessibility tree, no network, late `attachShadow` only through `rescan`) in the shape of EnvironmentCapabilityManifest observation entries, and `missingSignals` yields the matching ObservationIR gaps.
8. **Environment.** Locale comes only from what the page declares (`lang`, `content-language`), canonicalised like the Rust identity layer; the browser's language is never used. Viewport is bucketed; flags come from configured probes. A cache overlay writes unknown inputs as `?` and is not shareable.
9. **Sensitive data.** Source scopes are `credential`, `account` or `public` (explicit marker, conventional credential controls, and personal-data text shapes). Credential regions yield no text, values are never read, secret-shaped text is redacted at capture and on every change. Account text stays local and flagged; `sharedEvidence` withholds it. Diagnostics accept only codes and numbers. The observer has no network surface.
10. **Protocol evidence** (`vsf-protocol-evidence-v1`) is a standalone schema with promotion rules: shapes and templates only, no sample values, authorization and provenance required, observed traffic always starts `unsupported` and `undocumented`, and `supported` needs vendor documentation or a reviewed promotion with witnesses. It is not yet an IR family; adding one is an additive IR minor and is left to the batch that first consumes it.

## Consequences
- The batch shape is `Batch`/`Op` in TypeScript. Rust and Go consume the VSFS pack derived from a replica (`toSnapshot`); a Rust delta applier is added when the compiler first needs incremental input.
- Visibility is the weakest signal; consumers must treat it as a hint unless probed.
- The page-context harness and the extension see different trees by design; the capability report is part of every observation.

## Rollback
The observer package additions are self-contained; the extension `observe` command is additive. Reverting them restores the B01 snapshot path unchanged.
