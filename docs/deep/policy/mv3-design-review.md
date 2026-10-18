# Chrome MV3 policy design review (B01, gate G1)

Status: design review against the plan's constraints (D6, ADR-011) and measured browser behavior. It is **not** a Chrome Web Store determination; submission-time review is a B24 item and may be stricter.

## Rule applied

Anything that changes what the extension *does* must ship inside the reviewed package. Remote content may only be data that packaged code reads through fixed, non-Turing-complete rules. When logic must vary per source application, it runs in a companion or hosted worker, never in the extension.

## Artifact format classification

| Artifact | Where it runs | Classification | Rule |
|---|---|---|---|
| Extension JS (background, content, panel) | extension | Packaged code | Bundled by `apps/extension/build.ts`; digests in `artifacts/extension/*.build.json` |
| Fabric WASM (`vsf_wasm.wasm`) | extension worker | Packaged code | Shipped in the package, never fetched; CSP `wasm-unsafe-eval` for extension pages only |
| Observation pack (`VSFS` binary) | page to worker | Data produced locally | Strictly validated, canonical, size and node limits |
| Command messages | panel to worker | Data | Closed command set, validated origin strings, no code or selectors |
| Remote observation configuration | store to extension | Data, restricted | Allowed only as schema-validated JSON with fixed vocabulary: enums, bounded integers, literal strings. No expressions, conditionals, loops, user functions or string-to-code paths |
| Compiled app artifacts (Semantic IR, Presentation IR, Operation Contracts) | extension renders, never executes | Data | The extension may render Presentation IR and forward an operation name plus arguments; it never interprets operation plans |
| Operation plans (ordered source-app steps) | companion or hosted worker | Not allowed in extension | Hard-stop rule from B01: restricted data contract or move the interpreter to the companion. Decision: the interpreter lives outside the extension |
| Remote JavaScript or WASM | any | Prohibited | Never fetched for execution; no `eval`, `new Function`, or remote script tags |
| Native companion binary | OS process | Out of store | Signed and distributed separately; reached by native messaging or the authenticated loopback bridge |

## Measured facts that shape the design

1. **Host permissions ignore the port.** With `http://127.0.0.1/*` granted, `scripting.executeScript` also succeeds on `http://127.0.0.1:<other port>`. The browser permission is host-level, so the per-origin (scheme, host, port) grant list kept by VSF is the real authorization boundary. Both layers are exercised in `tests/browser/specs/extension-permissions.spec.ts`.
2. **Hosts outside the granted pattern are blocked by the browser** (`localhost` versus `127.0.0.1` in the fixture), so a VSF bug cannot widen access beyond what the user granted at the browser layer.
3. **Command surface.** Only the packaged panel page in the top frame may send commands. A content-script context, a web page, or a foreign extension is refused with `sender_not_allowed`. Web pages cannot reach `chrome.runtime` because `externally_connectable` is absent, and `panel.html` is not web accessible.
4. **Messaging is JSON.** Structured data over `chrome.runtime`/`executeScript` is JSON-serialized: a number array costs about 10x a base64 string at 1 MB (see evidence `bench/capture.json`). Transport uses base64 strings.
5. **Worker lifecycle.** Natural idle termination was observed at about 30 s with a browser-level DevTools socket only. Attaching Playwright to the worker keeps it alive, so lifecycle tests use `ServiceWorker.stopWorker`. State that must survive lives in `storage.local` (grants) and `storage.session` (counters, pending-capture checkpoints); a restart marks running captures `interrupted` and never retries them.
6. **Native messaging.** Browser to host messages up to 32 MB succeeded. Host to browser failed at 1,048,576 bytes and succeeded at 999,900, consistent with the documented 1 MiB cap. Large results must use the loopback bridge or chunking.
7. **Secrets.** Form control values are never read; text and labels pass through the shared redaction patterns before packing. The packaged scripts contain no logging calls and the worker's globals, storage and replies were checked for planted secrets.

## Open items carried forward

- The product build requests host access at runtime (`optional_host_permissions`). The panel authorizes via `permissions.request`, which needs a real user gesture; it is exercised manually in B09, not in automation.
- Store policy interpretation of declarative remote configuration must be confirmed at submission (B24). Until then no remote configuration is consumed at all.
- Firefox and Safari behavior (structured-clone messaging, different lifecycle) is out of scope until B21.
