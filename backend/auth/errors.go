// Package auth brokers a person's sign-in to the applications VSF acts on: it binds each grant to one source and one caller, checks audiences, keeps secrets in a vault and revokes everything that depends on a grant.
package auth

import "errors"

// Callers learn only that a grant is unusable; the reason is recorded in the audit log, never returned, so a probe learns nothing about other callers' grants.
var (
	ErrDenied    = errors.New("auth.denied")
	ErrExpired   = errors.New("auth.expired")
	ErrReauth    = errors.New("auth.reauth_required")
	ErrInvalid   = errors.New("auth.invalid")
	ErrUntrusted = errors.New("auth.untrusted_token")
	// ErrStale rejects evidence that is old, replayed or out of order; ErrAudit reports a mandatory security record that is not on the log.
	ErrStale = errors.New("auth.stale")
	ErrAudit = errors.New("auth.audit_unavailable")
	// ErrWipePending means a grant is revoked and unusable but some secret could not yet be physically deleted; FlushWipes retries.
	ErrWipePending = errors.New("auth.wipe_pending")
)

// Reason is the closed set of causes the audit log records.
type Reason string

const (
	ReasonUnknownGrant Reason = "unknown_grant"
	ReasonOwner        Reason = "owner_mismatch"
	ReasonSource       Reason = "source_mismatch"
	ReasonAudience     Reason = "audience_mismatch"
	ReasonScope        Reason = "scope_missing"
	ReasonExpired      Reason = "expired"
	ReasonRevoked      Reason = "revoked"
	ReasonPaused       Reason = "reauth_required"
	ReasonOK           Reason = "ok"
	ReasonEpoch        Reason = "signin_changed"
	ReasonIdentity     Reason = "identity_unverified"
	ReasonRuntime      Reason = "runtime_unproven"
	ReasonReplay       Reason = "replayed_observation"
	ReasonWipe         Reason = "secret_not_deleted"
	ReasonProvider     Reason = "provider_mismatch"
	ReasonAmbiguous    Reason = "ambiguous_grant"
)
