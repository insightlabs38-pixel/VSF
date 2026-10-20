# ADR-039: Authentication broker and managed sessions

Status: in progress (B14). Code: `backend/auth`. Extends ADR-036 (local bridge) and ADR-037 (interfaces); uses ADR-038 (sealing, vault).

## Context
VSF acts for a person inside applications that person signs in to. B09 binds an extension runtime to an origin and B10 binds a caller to a source session, but nothing yet records how the person signed in, for which audience, until when, or what depends on it.

## Decisions
1. **A grant is a record, not a secret.** `auth.Grant` names the account, the owning caller, one source (application and exact origin), one audience, scopes, method, expiry, state and an epoch that rises on every sign-in. Secrets live only in a vault and are reached through a scoped lease. Use of a grant is checked against all of owner, origin, audience, scopes, expiry, revocation and pause; the caller sees only a generic denial, expiry or reauthentication error and the audit log records the true reason, so a probe learns nothing about other callers' grants.
2. **No assumed cross-context reuse.** Audience and origin are matched exactly (no prefix, suffix or scheme rewriting), a grant never moves to another caller or source, and a token is never passed through: VSF verifies tokens it receives against the audience it expects and never forwards a provider token to a different audience.
3. **Token verification is a fixed allow-list.** Only RS256 (2048 bits or more), ES256 (P-256) and EdDSA verify; `none`, shared-secret algorithms, critical headers, duplicate members, unknown key ids and algorithm/key mismatches are refused; issuer, audience (with `azp` when several), expiry, not-before, issued-at, nonce and maximum age are checked.
4. **The audit log is closed-vocabulary and hash-chained.** Records contain only identifiers, an origin and members of fixed sets, so they cannot carry a secret or a person's data; each line commits to the previous one. A truncated tail is a shorter valid log and needs an external anchor to detect.
