# Targeted research between ZIP review passes

**Date:** October 18, 2026. **Reason:** The first archive audit found assumptions requiring current primary-source confirmation. This pass intentionally occurs after review of the draft archive and before second archive construction.

## Verified findings and implementation changes

| Research item | Primary source | What the source establishes | Plan change |
|---|---|---|---|
| MV3 remotely hosted code | https://developer.chrome.com/docs/webstore/program-policies/mv3-requirements | Chrome Web Store restricts downloaded executable logic and even complex command interpreters fetched as remote data, with limited documented exceptions | Add `RemoteArtifactPolicy` decision table; restrict extension cloud inputs to declarative configuration with packaged local logic; test store policy review early |
| MV3 background lifecycle | https://developer.chrome.com/docs/extensions/develop/concepts/service-workers/lifecycle | Service workers commonly terminate after 30s idle; globals are lost; long tasks need durable state/recovery | Add recovery fixture and persistent event sequence checkpoint in B01/B09; require long-running work in companion/hosted process as needed |
| Native Messaging | https://developer.chrome.com/docs/extensions/develop/concepts/native-messaging | Registered host/allowed extension origins, JSON length-prefixed native messages; max native→browser 1MiB, browser→native 64MiB | Add protocol framing limits, chunked manifest/artifact transfer or out-of-band local authenticated store and version handshake |
| Content script trust | https://developer.chrome.com/docs/extensions/develop/concepts/messaging | Content scripts are less trustworthy; origin/sender validation and input sanitization required before privileged operations | Add sender-origin validation, pair-bound token and adversarial malicious-page/native-bridge tests |
| MCP release profile | https://blog.modelcontextprotocol.io/posts/2026-07-28/ | Core `2026-07-28` is released, uses stateless core and cacheable list responses; authorization, negotiated extension behavior changed | Use actual stable core profile; do not claim extension support for every client; pin conformance to spec version |
| MCP Tasks | https://tasks.extensions.modelcontextprotocol.io/specification/2026-07-28/tasks | Tasks belong to a separately negotiated extension and may still be marked draft | Serve plain synchronous/pollable VSF job handle fallback; only use Tasks with clients advertising support; test drafts independently |
| MCP tool schemas and names | https://modelcontextprotocol.io/specification/2026-07-28/server/tools | JSON Schema 2020-12 and tool naming, uniqueness/scope rules apply | Add stable namespaced contract grammar, schema-validation and collision tests |
| OAuth browser apps | https://www.rfc-editor.org/rfc/rfc10017.html | August 2026 IETF BCP describes secure OAuth patterns and threat models | Keep token audience/scope/issuer semantics, PKCE, BFF where appropriate and reauth per environment |
| Benchling V3 contracts | https://docs.benchling.com/docs/v3-api-overview | V3 preferred for new integrations; stable API guarantees >=90 days breaking-change notice, beta may change without notice | Add connector contract `stability_tier` and planned deprecation-window handling; never equate vendor V3 with perpetual stability |
| Chrome extension permission and user data | https://developer.chrome.com/docs/webstore/program-policies/policies | Narrowest required permissions, disclosure and consent, source data handling requirements | Add store disclosure/permission review and default local-only data-policy integration tests |

## Decision table: what may run in an extension?

| Input | Treatment | Strategy |
|---|---|---|
| User preferences, colors, layout configuration | Declarative data | Safe only if packaged engine behavior and policy are reviewable; validate schema |
| Remote source app observation captured in authorized session | Untrusted observed data | Normalize and sanitize; never evaluate as code |
| Remote compiled list of arbitrary executable browser actions | **Policy-sensitive, likely remote code interpretation concern** | Avoid in extension; use packaged bounded interpreter or native/hosted execution after formal policy audit |
| New version of compiler/WASM module | Executable package | Distribute as extension update or signed native installation, not downloaded executable into extension |
| Provider-documented API schemas | Data/evidence | Can be ingested as typed contracts under user authorization and runtime policy |
| Signed immutable semantic manifest | Data + execution-adjacent artifact | Signing alone does not make policy compliant; ensure schema cannot express forbidden remote logic |

This is an engineering risk framework, **not official store approval**. Validate a real packaged prototype and store submission requirements.

## Native message transport detail

Because a native host may send at most 1MiB in one response message, the local companion cannot blindly send a complete 20MB compiled application blob through one native message. Choose compact manifest with small responses, chunk with bounded framing and sequence/hash checking, or use a mutually authenticated loopback channel after explicit pairing. Do not expose loopback endpoints to arbitrary websites; validate `Origin`, host and authentication. Include exact source trace in B09 fixtures.

## Updated acceptance requirements

- **B01:** Time/CPU split, MV3 lifecycle survival, policy evaluation, native framing size benchmark.
- **B08:** Pinned contract + downgrade/entitlement expiry/source incompatibility and security revocation scenarios.
- **B09:** Content-script-origin mediation + protocol versions + >1MiB artifact transfer.
- **B10:** Current MCP core conformance and conditional Task extension; full JSON Schema 2020-12 and namespaced collision tests.
- **B14:** External OAuth tokens remain audience/issuer bound and session transfer explicitly reauths.
- **B17/B19:** Expired paid retention and tier downgrade cannot delete legal or user-pinned data without documented policy, grace, export path and verified GC.

## Research limitations

Official documentation is not proof of cross-browser feature availability, a guaranteed Chrome Web Store acceptance, compatibility with a specific Benchling tenant, or lawful third-party automation. Commerce-provider options and actual unit economics remain selection workstreams, not researched vendor recommendations. These findings validate architectural constraints, not commercial success.
