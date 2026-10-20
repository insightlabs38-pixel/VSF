# MV3 distribution checklist (B09, task `VSF-B09-T08`)

This is a self-review against the rules in `mv3-design-review.md`, not a store determination; store review at submission is a B24 item and may be stricter. Every row names the automated rule that enforces it. `node tools/checks/src/cli.ts ext-audit` runs all of them over the product and test packages and writes `artifacts/extension/audit.json`, which also lists every packaged file with its digest and its class.

## 1. What is packaged: code versus data
| Class | Files | Why it is acceptable |
|---|---|---|
| Executable, packaged | `background.js`, `content.js`, `observe.js`, `render.js`, `panel.js`, `panel.html`, `vsf_wasm.wasm` | Built from this repository by `apps/extension/build.ts`; reviewed with the package; no inline script, no remote URL, no dynamic evaluation. |
| Data | `manifest.json` | Declares permissions and entry points only. |
| Data received at run time | observation snapshots, assembly documents (`vsf-assembly-v1`), semantic and presentation documents | Parsed by fixed code; never evaluated. The renderer draws them and forwards an operation name plus arguments; it never interprets an operation plan. |
| Not in the extension | operation plans, source-specific logic, executors | Live in the companion or a hosted worker (see the design review). |

## 2. Rules and their enforcement
| # | Requirement | Rule id | Test |
|---|---|---|---|
| 1 | Manifest V3 with a packaged module service worker | `manifest-mv3` | `tools/checks/test/extaudit.test.ts` (mv2 and missing worker) |
| 2 | Permissions limited to `activeTab`, `scripting`, `storage` | `manifest-permissions` | extaudit test (`tabs`, `debugger`); `tests/browser/specs/extension-install.spec.ts` reads the installed set |
| 3 | No install-time host access in the product build; optional hosts are `http(s)://*/*` only, requested per site from a user click | `manifest-hosts` | extaudit test; install spec (`origins` is empty, grant refused without the browser permission) |
| 4 | No static content scripts, no `externally_connectable`, no web-accessible resources, no `commands`, no declarative network rules | `manifest-surface` | extaudit test; install spec (a page cannot fetch the extension's files) |
| 5 | Content security policy is exactly `script-src 'self' 'wasm-unsafe-eval'; object-src 'self'` | `manifest-csp` | extaudit test |
| 6 | No remote code: no `importScripts`, dynamic `import()`, script elements, `src` assignment, workers, streaming WASM compile | `no-remote-code` | extaudit test (script scan) |
| 7 | No dynamic code: no `eval`, `new Function`, string timers | `no-dynamic-code` | extaudit test |
| 8 | No inline script, inline handler, `javascript:` URL or remote reference in packaged pages | `no-inline-script`, `no-remote-code` | extaudit test (HTML scan) |
| 9 | Network sinks (`fetch`, `XMLHttpRequest`, `WebSocket`, `EventSource`, `sendBeacon`) appear only where listed in the policy; loading a packaged file with `getURL` is the only unlisted use | `network-sink` | extaudit test; the worker's one listed sink is the companion client |
| 10 | The WASM module imports nothing from the host | `wasm-imports` | extaudit test (module with an import is refused) |
| 11 | Only `.js`, `.html`, `.wasm` and `.json` ship; package under 8 MiB | `file-types`, `package-size` | extaudit test |
| 12 | The package is reproducible | digest list in `audit.json` | `tests/browser/specs/extension-package.spec.ts` builds twice and compares |
| 13 | Worker commands come only from the packaged panel; page messages are bounded, versioned and bound to a session | n/a | `apps/extension/test/*.test.ts`, `tests/browser/specs/extension-transform.spec.ts` |
| 14 | The source page is restorable at any time | n/a | `extension-transform.spec.ts`, `extension-fallback.spec.ts` |

## 3. Single purpose and data handling
- **Purpose**: shows a page the user is already viewing in a clearer, accessible form, keeping the original one click away.
- **Reads**: the accessible structure of a page on an origin the user authorized. Form control values are never read; text passes the shared redaction patterns first; credentials and hidden controls are withheld.
- **Stores**: authorized origins, counters and open sessions (`storage.local`/`storage.session`); display preferences and companion credentials, sealed with AES-GCM under a non-extractable key.
- **Sends**: nothing, unless the user pairs a local companion; then requests go to `127.0.0.1` with the paired credentials. There is no analytics and no remote configuration.
- **Removes**: revoking an origin restores its pages, drops the grant and removes the browser permission; unpairing revokes the credentials on the companion and forgets them here.

## 4. Reviewer's reading order
1. `apps/extension/manifest.template.json` and `docs/deep/adr/ADR-036-extension-integration-and-local-bridge.md`.
2. `apps/extension/src/core.ts` (all decisions), `protocol.ts`, `envelope.ts`, `session.ts` (what may be asked of the worker).
3. `apps/extension/src/render.ts` (what is injected into a page) and `observe.ts`.
4. `tools/checks/src/extaudit.ts` (what the package must not contain).

## 5. Open items
- Store policy interpretation of the optional-host request flow and of the localhost companion is confirmed at submission (B24).
- The user-gesture permission prompt is exercised by hand; automation cannot accept it.
