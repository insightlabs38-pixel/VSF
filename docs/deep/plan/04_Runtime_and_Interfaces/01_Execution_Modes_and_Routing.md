# Execution environments, connections and routing

## Principle

Connection, execution and persistence are independent axes. A browser extension, desktop client, localhost service, hosted sandbox, headless/on-demand worker or private server uses the same verified operation contracts but **not** necessarily the same physical capabilities, data residency, session or cost structure. A saved compiled plan is portable only when its required capabilities are satisfied.

## Matrix

| Mode | Observation | Execution | State | Authentication | Commercial placement |
|---|---|---|---|---|---|
| Extension | in-tab DOM, accessibility with limits | trusted local browser input | tab and limited device store | existing user session | free/individual |
| Desktop | extension-controlled browser and/or embedded webview | native companion + browser | profile/keychain + local SQL | interactive or OAuth | individual/pro |
| Localhost | browser/companion adapter | long-running local gateway to source | durable local store | paired caller + user source session | premium Pro |
| Hosted sandbox | managed browser and documented provider APIs | isolated cloud worker | tenant vault + controlled runtime store | managed session authorized per app | subscription + PAYG |
| Headless/on-demand | server worker/protocols | asynchronous jobs, no display necessary | execution handle, checkpoint | delegated auth or managed browser | Pro/Team + PAYG/BYO |
| Private/VPC | customer-authorized connectors, browser workers | private runners and policy | customer-controlled storage/vault | enterprise identity, policy | enterprise license/service |

## Capability negotiation contract

Worker advertises `capture_dom`, `capture_accessibility`, `open_frame`, `real_pointer_input`, `native_terminal`, `filesystem_access`, `document_download`, `interactive_mfa`, `network_policy`, `background_duration`, `secure_keychain`, `sse/ws/webrtc`, `persisted_session`, `supported_ir_version`, `primitive_set_digest`, `policy_version`, `source_origin_allowlist`, and `artifact_format_version`.

Compiler emits required capabilities for each operation. Router selects a suitable executor only from user/admin **authorized** environments; nonmatching environment returns `UNSUPPORTED_ENVIRONMENT` with requirements, never implicit cloud fallback.

## Extension details

MV3 packaged code, narrowly scoped site grants, content script/isolated world, controlled bridge when needed, browser lifecycle/restart recovery, service worker state reconstruction and explicit consent for debugger API. Remote configuration data cannot become forbidden remotely hosted execution logic. Store review and browser extension distribution are discrete release gates.

## Desktop details

Early launch should prefer lightweight native companion + existing browser, not bundle a second Chromium until measured need. Desktop shell choice (Tauri/Wry or other) is conditional after OS compatibility, installer/updater behavior, WebView features and code-signing tests. Support Windows first with portable design for macOS/Linux; avoid promising same supported feature set across WebViews.

## Localhost details

Persistent HTTP/MCP/API server bound loopback by default, origin/CSRF/DNS-rebinding defenses, authenticated clients/pairing, per-source capability tokens, TLS when not loopback, idle and quota controls, separate mode entitlement. Existing extension local compilation need not require premium localhost server; persistent service does.

## Hosted & headless details

Zero-trust sandbox: isolated worker identity, no unrestricted private network or metadata service access; DNS resolution and redirect egress controls, browser process limits, per-org quotas, job queue fairness, workspace stop/resume, audit and budget accounting. Long-lived session idle suspension with explicit state restore semantics. Never claim identical behavior with interactive browser where MFA/UX constraints differ.

## Private deployment

Customer-managed region, BYO object store/database/vault where feasible, offline update bundles, signed releases, rollback compatibility, org approval for external communications, policy logs, license entitlement with grace/resilience. Support `no external telemetry` mode and private-network connector allowlists.

## Cross-mode migration

Portable: compiled non-sensitive structural artifact, compatible contract version, UI layout and primitive pack manifest. Conditioned: user-derived state and version snapshots under authorization. Not assumed portable: cookies, passkeys, device-bound authenticators, transient browser state, source permissions. Reauthenticate in destination environment when needed.

## Routing algorithm

Filter allowed environments by org policy -> caller entitlement -> data residency -> required capabilities -> authenticated source scope -> health -> user priority -> projected latency and cost. Prefer deterministic stable route; offer explicit failover approval for cost/residency boundary change. Version provenance records actual executor.

## Native Messaging transport limit (verified)

Chrome native messaging registers allowed extension IDs and uses JSON length-prefixed messages. Native-host-to-browser single messages are limited to **1MiB**, while browser-to-host messages allow up to **64MiB** under current Chrome documentation. Do not ship large compiled IR as a single native response; use compact manifests, bounded chunked transfer with digest checks, or a paired/authorized loopback artifact channel. Extension service worker can terminate after ~30 seconds of inactivity, so restore state from durable checkpoints.
