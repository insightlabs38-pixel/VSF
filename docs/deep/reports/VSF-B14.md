Batch: B14 | Gate: G14 | Decision: PROCEED
Branch: vsf/b14-auth-managed-sessions | Base: becebe5 | Commit: 69ef2a8 (code and receipts; CI run on this head)
PR: https://github.com/insightlabs38-pixel/VSF/pull/13 ; merge commit and final CI recorded in state.json and evidence/remote-ci.md after merge
Closed tasks: VSF-B14-T01 VSF-B14-T02 VSF-B14-T03 VSF-B14-T04 VSF-B14-T05 VSF-B14-T06 VSF-B14-T07 VSF-B14-T08 VSF-B14-T09 VSF-B14-T10 VSF-B14-T11 VSF-B14-T12 VSF-B14-T13 VSF-B14-T14 VSF-B14-T15
Contracts/primitive IDs: no IR or primitive change; Go-only: auth.Store.Swap, auth.Evidence/Observation, PlanRef.Epoch, execution.Session.Origin/Generation, ConfirmationDigestAt, secret.Keyring.Retire(ctx)
Changed modules: backend/auth (grant, lifecycle, browser authority, oauth, jwt, profile, audit, vault, lease), backend/bridge (app id), backend/execution and api (sign-in generation), backend/storage/{secret,artifact}, ADR-039
Tests: 39 evidence runs PASS (15 tasks x positive and negative, 9 gate runs): full Go suite and integration suite with -race, vet, gofmt, comments, secrets, biome, queue tests; all 12 remote CI jobs on the head
Negative/adversarial: refresh racing revoke and refresh, stale resume vs newer pause, forged/replayed/out-of-order evidence, provider substitution, widened scopes, disk-full and torn audit/vault writes, lease after revoke, confirmation across reauthentication, key retirement with live data
Performance: no measured regression; audit costs one fsync per mandatory record; refresh holds no lock during network I/O; ordering state bounded (8192 marks, 4096 profiles)
Security/session scope: sessions bind to exactly one verified grant (caller, app, origin, principal, runtime); evidence needs a bridge-vouched session; revocation wins and is immediate even when deletion is retried; mandatory audit fails closed
Source compatibility: synthetic fixture sources only; no third-party application involved (corpus work is B11)
Version/pin/retention: grant epoch and secret generation bind plans, leases and confirmations; no artifact version or retention change
Known limitations: coordination is per process (a shared Store must implement Swap atomically); no JWKS fetcher; audit tail needs an external head anchor; extension-to-Evidence transport is not wired; native macOS/Windows stores are VSF-B16-T08
Follow-ups queued: VSF-B15-T06 (contract cache), VSF-B16-T08, VSF-B18-T07 (ZEROS3)
Evidence: docs/deep/evidence/VSF-B14/ (manifest.json, logs/)
Next eligible: VSF-B11-T07 (independent corpus bootstrap), then VSF-B11-T01
