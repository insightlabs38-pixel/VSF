# ADR-036: Extension integration, session binding and the local bridge

Status: accepted (B09, gate G9). Code: `apps/extension`, `compiler/crates/vsf-assemble`, `backend/bridge`. Review design: `docs/deep/policy/mv3-design-review.md`, checklist `docs/deep/policy/mv3-distribution-checklist.md`.

## Context
B04 to B08 produce observations, recognition, contracts, execution and a renderer, each tested alone. B09 joins them in a real browser: the page the user is already signed in to is read, rebuilt and shown, and every step can be undone. Nothing here may widen what the source account allows.

## Decisions
1. **One pipeline, Rust in the middle.** Packaged observer -> recognizer snapshot (JSON) -> WASM `vsf_assemble` (ABI 4) -> `vsf-assembly-v1` -> TypeScript presentation compiler and checker -> renderer. `vsf-assemble` builds a real `SemanticIR` document (validated by `vsf-ir` before it leaves), typed row data and the availability of every action taken from the recognizer's own contract. Without a behavioural witness every action is `unknown: effect_unverified`; the renderer disables it and says why. Shape inferred from values is only ever `inferred`; credentials and hidden controls are withheld, not drawn.
2. **Shape digest.** The document digest includes when the page was observed, so it changes on every read. `shape_digest` covers entities, fields and actions only; an unchanged shape updates rows in place, a changed one re-renders.
3. **Message protocol `vsf-ext-v1`.** Commands come only from the packaged panel. Page messages are envelopes (`kind`, unique `id`, session, strictly increasing `seq`, bounded body) from the extension's own script in the same top-level document. The worker checks sender id, tab, frame, document id and origin against the session, then reserves the request id as `sent` before doing anything. A repeated id returns the recorded outcome; an older `seq` is a replay.
4. **Worker death is expected.** Sessions live in `storage.session`. After a restart a half-built session is undone, a request that was `sent` becomes `unknown` and is never resent, and the page keeps its sequence. Nothing in the worker's memory is needed to continue.
5. **Replacement, not injection.** The source body is made `inert` and an overlay with a closed shadow root is shown on top. Nothing in the source document is rewritten. Restore removes the overlay and the two attributes it set. The element is registered in a set held in the extension's isolated world and skipped by the observer; an attribute the page could copy would let a hostile page hide content from the user.
6. **Emergency restore.** The page restores itself on a crash in the extension's code, on the escape chord, when the worker cannot be reached or no longer knows the session, when the overlay is removed, and when the session was written by a newer version. The restore is local; telling the worker is best effort.
7. **Pairing is not source permission.** The companion pairs an extension origin with a one-time 80-bit code that never crosses the wire: PBKDF2 (100,000 rounds) and HMAC proofs in both directions bind the code to both nonces and the origin. Credentials are an expiring access token and a rotating refresh secret; a refresh secret presented twice revokes the client. Only hashes are stored, in a 0600 file. A pairing authorizes calls to the companion, nothing else.
8. **Sessions are bound, not inferred.** A runtime registers an `EnvironmentCapabilityManifest` (validated by the Rust worker) for one origin and one document. The binding records client, principal, runtime, origin, document, manifest digest and scopes; a different client, origin or document fails `Check`, and revoking the pairing ends the binding. One client keeps one principal.
9. **The bridge treats every web origin as hostile.** Host allowlist (rebinding), exact origin allowlist, no-Origin cross-site refusal (elements, forms), CSRF header and content type, private-network preflight answered only to the extension, bearer tokens bound to the extension origin, no credential in URLs, request log without credentials. Hostile traffic never reaches the pairing rate limit.
10. **Settings and credentials are sealed.** AES-GCM with a non-extractable key in IndexedDB; the record name is authenticated data. Only the pairing credentials and preferences are sealed today.

## Consequences
- Write operations stay unavailable: no operation has a behavioural witness yet and the companion has no executor for a user's own tab. The path exists (dispatch -> companion) and refuses with the reason.
- The extension needs `activeTab`, `scripting` and `storage`; hosts are optional and per-origin. Nothing is loaded from the network.
- ABI 4 is not compatible with ABI 3 hosts; the host checks before the first call.

## Limits
- Pairing resists a process that cannot read the code; a process that can read the screen where the code is shown can pair. The code expires in two minutes and works once.
- If the worker dies between receiving a rotated refresh secret and storing it, the next renewal looks like theft and the user pairs again.
- The principal is a per-installation pseudonym until accounts exist.
- Firefox and Safari are out of scope (B21).

## Rollback
Remove the `vsf_assemble` export and the `assemble` command; the panel keeps `capture`. Bridge pairing is additive: a static development token still works.
