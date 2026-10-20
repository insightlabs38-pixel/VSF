# ADR-037: One operation service behind the REST, CLI, MCP and interface-client surfaces

Status: accepted (B10, gate G10). Code: `backend/api`, `backend/cli`, `backend/mcp`, `backend/interfaces` (tests), `backend/internal/stacktest`. Budgets: `docs/deep/policy/dispatch-budget.md`.

## Context
B07 runs one verified operation; B08 and B09 give it versions, sessions and a bridge. B10 lets people and agents reach it. The rule is that no surface owns business logic: each is generated from the same verified contract and translates to one service.

## Decisions
1. **One service, thin adapters.** `api.Service` (the `Backend` interface) decodes the invocation envelope, resolves the served contract, checks scope and session ownership, and calls `execution.Runtime`. REST (`NewREST`), the CLI (through `api.Client`), MCP (`mcp.Server`) and the interface client are translations of `Backend`; none decides an outcome.
2. **Specs come from the contract and nothing else.** `BuildSpec` derives name, argument schema, result schema, annotations and an effect disclosure from an `OperationContract`. An operation is served only if it decodes, is `active`, passes the Rust validator and has a tool-safe name. Anything else is withheld with a reason and listed as such by the versions route. No operation is invented for behaviour nothing verified.
3. **Names and routing.** Tool name `op.<encoded app>.<operation id>`; the app encoding is reversible and collision-free; `vsf.` names are fixed tools. REST invocation routes always carry an exact contract version. The newest served version is the default for describe, and every call may pin a version and a digest; a pin to a revoked version is refused whatever its age, a pin to a deprecated one names what is served.
4. **One taxonomy.** `Code`, `Effect`, `Retry`, `Category`, HTTP status and CLI exit code are one table. `unknown_commit` and `unverified_effect` carry effect `unknown`, retry `reconcile_first` and distinct exit codes (10, 11); a test fails if a runtime reason reaches clients as `internal`. Error text is fixed per code and never carries source or caller values.
5. **Same outcome everywhere.** Category, effect, retry, code, reason and observed values are identical on every surface for the same inputs (`backend/interfaces/cross_test.go`). A lost answer on the client is `service_unreachable`, effect unknown, retry the same request id, which the runtime treats idempotently.
6. **Credentials per surface.** `TokenAuth` issues 256-bit credentials for one caller and one surface, stores only digests in a 0600 file, expires them, and re-checks the caller on every call. A credential is refused on any other surface; the caller directory, not the token, owns scopes. Session ownership is checked in the service in addition to the runtime, with one answer for a missing session and another caller's, so session ids are not an oracle.
7. **Confirmation belongs to a person, a caller and a session.** `ConfirmationDigest` now binds the executing caller and source session. Only a user-interface credential can obtain one (`Service.Confirm`); REST, CLI and MCP credentials cannot confirm for themselves.
8. **MCP 2026-07-28, stateless.** No handshake or session: every request carries `_meta` and mirrored headers (`MCP-Protocol-Version`, `Mcp-Method`, `Mcp-Name`), validated before dispatch. An unsupported version returns `-32022` with the supported list; `initialize` is answered the same way. Tools and resources are listed with deterministic order, bounded pages and `ttlMs` with `cacheScope: private` (the list varies with the caller's scopes). Results carry `structuredContent` that conforms to the published `outputSchema`; `isError` is true for every outcome that is not a success, a dry run or a run in progress. Fixed tools: status, cancel, reconcile, versions, namespaces, list, describe.
9. **Discovery is lazy and bounded.** The catalog indexes names and versions without decoding a document; a page evaluates at most four times its size; namespaces are answered from the index; the spec cache is bounded. `MaxListedOperations` can cap what `tools/list` advertises while every served operation stays callable and discoverable through the list and describe tools.
10. **Decoded documents are memoized by content** in the execution runtime (256 entries); it removed about half of the per-call cost.

## Consequences
- The interface adds about a millisecond over the runtime; see the budget document. The Rust authorization round trips are now the largest part.
- A client sees one honest meaning of "unknown": it must reconcile, and reconciliation settles only on source evidence.

## Limits
- No daemon composes these pieces yet (B16-T05); the surfaces are libraries tested against a fixture source.
- The browser shell does not call the interface client yet (B16-T06); the interface surface is exercised through the REST transport with a user-interface credential.
- A person cannot yet confirm a request on behalf of an agent caller (B14-T06); the agent receives `confirmation_required`.
- The MCP Tasks extension, stdio transport and progress notifications are not implemented (B16-T07); long runs use a handle and `vsf.invocation.status`. No MCP client or conformance suite implementing the 2026-07-28 revision existed when this was written (the TypeScript SDK speaks 2025-11-25, the conformance package targets a 2026 draft), so conformance is checked by an independent test client written from the specification text.
- Rate limiting is per caller in memory, not shared across processes.

## Rollback
Remove the adapters (`rest.go`, `client.go`, `cli`, `mcp`); `Service` and `Backend` remain. The caller-bound confirmation digest is a deliberate incompatibility with confirmations issued before B10, which none exist for.
