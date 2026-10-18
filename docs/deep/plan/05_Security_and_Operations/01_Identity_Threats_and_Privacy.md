# Identity, threats, privacy and regulated use

## Identity model

Separate: **VSF person/organization**, **API/CLI/MCP client principal**, **device/browser**, **execution worker**, **source-app account**, **source-app permission/role**, **IdP/OAuth client authorization** and **artifact owner**. These identities are not interchangeable.

## Supported authentication

1. Browser existing-session mode, with per-origin permission and no cookie export for normal transformation.
2. OAuth/OIDC connections, correct client/audience/scope binding, PKCE, discovery validation and token lifecycle; Google/GitHub tokens cannot generically log into arbitrary sites using those providers.
3. Explicit source-app managed browser login, with MFA/passkeys handled through supported user flows and environment restrictions.
4. Credential vault injection for authorized legacy login, using OS keychain or managed secrets service; narrow origin binding and redaction.
5. Enterprise SSO when institution/customer authorizes it; separate organization policy, user permissions and source role.

Do not attempt to bypass MFA, authorization checks, anti-bot controls or application policy. Credential/session portability is conditional, not assumed.

## Threat register

| Threat | Mitigation | Adversarial test |
|---|---|---|
| Third-party page controls local API | Loopback origin validation, pairing, CSRF/DNS rebinding protection, scoped capability tokens | Malicious web origin attempts API/MCP invocation |
| Extension JS injection/cross-origin confusion | Packaged code, strict schema parser, per-origin grants, content/page isolation | Remote compiled 'data' tries to execute arbitrary code |
| Source app exposes hostile text/tool descriptions | Treat source text as untrusted; namespaced sanitized contracts; provenance | Malicious page content tries to change execution policy |
| Hosted browser requests internal endpoints | Egress filtering at network layer, metadata deny, DNS rebinding defense, redirect enforcement | SSRF allowlist/denylist bypass simulation |
| Cross-tenant compiled artifact leakage | Private artifact scope, data classification, keyed IDs when necessary, signed manifests | One tenant requests other's digest |
| Cross-user source permissions | Check bound user source auth at invocation; never authorize from shared IR | Admin-compiled operation invoked by read-only user |
| Credential exfiltration through debug log | Data minimization, secret detection/redaction, encrypted diagnostics, short retention | Fault causes auth header in trace |
| Source write double-submission | Effect classification, no blind retries, unknown-commit state, reconciliation | Kill worker after remote commit |
| Version rollback to vulnerable plan | Revocation list, signing and policy minimum, provenance checks | Security-revoked pinned artifact refuses to execute |
| Generated MCP tool misuse | Independent client principal, consent for high-risk actions, source confirmation | Tool used outside user's authorized scope |

## Security architecture checklist

- Secure session storage; token TTL/refresh/revoke; rotation; key management; KMS/HSM where enterprise constraints require.
- Minimum permissions in browser extension and cloud scopes; no blanket origin grant by default.
- Signed releases, SBOM, dependency audits, reproducible build hashes and patch policy.
- HTTPS/TLS endpoints outside loopback, authenticated and authorized native bridge, secret zeroization where feasible and no plaintext disk backups.
- Private/local mode prohibits unexpected outbound diagnostic uploads; user controls error report contents and retention.
- Data classification for observation snapshots before dedup/sharing; audit and deletion lifecycles; protected backups.
- Regular threat-model updates, independent penetration test before sensitive regulated enterprise use, incident response and disclosure procedures.

## Compliance decision tree

Do not claim blanket HIPAA/FERPA/FDA compliance. Determine if VSF receives, hosts or processes regulated records. U.S. healthcare ePHI can require business-associate agreements; student records can implicate FERPA contractual rules; regulated lab electronic records may need audit, electronic signatures and computer validation. Keep specific industry support behind verifiable requirements and counsel/customer security review. Local processing reduces some data transfer risk but does not negate all regulatory obligations.

## Privacy acceptance

Source DOM can include PII, tokens or sensitive material. Default telemetry is metadata/counts/latencies and redacted structural failures, not full contents. Structured privacy export, retention windows, erase/delete procedures, breach response and control-plane access logs are part of paid and non-paid core security. Cross-user cache proof must inspect raw snapshots for secret contamination.

## Localhost access control and native bridge acceptance

`http://127.0.0.1` is not a trusted authorization boundary. Persistent premium **localhost** hosting must authenticate each CLI/API/MCP caller, bind the source-app account and capabilities, prevent DNS rebinding and CSRF, constrain WebSocket origins, and keep all listeners loopback-only unless the user deliberately enables a hardened remote mode. Native Messaging host allowlists plus pairing authorization, explicit protocol versions and replay resistance must be tested. Revoking a client must not automatically log out the original user's source application unless that is the user's selected policy.
