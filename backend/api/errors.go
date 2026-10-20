package api

import (
	"errors"
	"fmt"
	"strings"
)

// Code is a stable, machine-readable failure class. The same code reaches every surface unchanged.
type Code string

const (
	CodeInvalidRequest             Code = "invalid_request"
	CodeInvalidArgument            Code = "invalid_argument"
	CodeUnauthenticated            Code = "unauthenticated"
	CodePermissionDenied           Code = "permission_denied"
	CodeWrongSurface               Code = "wrong_surface"
	CodeOperationNotFound          Code = "operation_not_found"
	CodeContractVersionUnsupported Code = "contract_version_unsupported"
	CodeContractPinMismatch        Code = "contract_pin_mismatch"
	CodeContractRevoked            Code = "contract_revoked"
	CodeOperationUnverified        Code = "operation_unverified"
	CodeSessionInvalid             Code = "source_session_invalid"
	CodeSourceVersionUnsupported   Code = "source_version_unsupported"
	CodeConfirmationRequired       Code = "confirmation_required"
	CodePreconditionRequired       Code = "precondition_required"
	CodePreconditionFailed         Code = "precondition_failed"
	CodeBlockedByUnknownCommit     Code = "blocked_by_unknown_commit"
	CodeRequestIDReused            Code = "request_id_reused"
	CodeEnvironmentChanged         Code = "environment_changed"
	CodeUnsupported                Code = "unsupported"
	CodeCancelled                  Code = "cancelled"
	CodeSourceRejected             Code = "source_rejected"
	CodeSourceConflict             Code = "source_conflict"
	CodeSourceThrottled            Code = "source_throttled"
	CodeSourceUnavailable          Code = "source_unavailable"
	CodeUnknownCommit              Code = "unknown_commit"
	CodeUnverifiedEffect           Code = "unverified_effect"
	CodeInvocationNotFound         Code = "invocation_not_found"
	CodeNotReconcilable            Code = "not_reconcilable"
	CodeReconcileInProgress        Code = "reconcile_in_progress"
	CodeStillUnknown               Code = "still_unknown"
	CodeReconciledNotCommitted     Code = "reconciled_not_committed"
	CodeAbandoned                  Code = "abandoned"
	CodeTooLarge                   Code = "request_too_large"
	CodeRateLimited                Code = "rate_limited"
	CodeServiceUnreachable         Code = "service_unreachable"
	CodeInternal                   Code = "internal"
)

// Effect says whether the source may have changed.
type Effect string

const (
	EffectNone    Effect = "none"
	EffectApplied Effect = "applied"
	EffectUnknown Effect = "unknown"
)

// Retry says what a caller may do next. Nothing here ever invites a blind retry of an ambiguous write.
type Retry string

const (
	RetryNever        Retry = "never"
	RetryFixRequest   Retry = "fix_request"
	RetrySameRequest  Retry = "same_request"
	RetryReconcile    Retry = "reconcile_first"
	RetryNewRequestID Retry = "new_request_id"
)

// Category is the outcome class of an invocation, identical on every surface.
type Category string

const (
	CategorySucceeded        Category = "succeeded"
	CategoryRefused          Category = "refused"
	CategoryFailed           Category = "failed"
	CategoryUnknownCommit    Category = "unknown_commit"
	CategoryUnverifiedEffect Category = "unverified_effect"
	CategoryInProgress       Category = "in_progress"
)

// Info is the single table that fixes what each code means on every surface.
type Info struct {
	HTTP     int
	Exit     int
	Category Category
	Effect   Effect
	Retry    Retry
	Summary  string
}

// Exit codes of the CLI; 0 is success and nothing else may reuse it.
const (
	ExitOK          = 0
	ExitError       = 1
	ExitUsage       = 2
	ExitAuth        = 3
	ExitRefused     = 4
	ExitFailed      = 5
	ExitUnknown     = 10
	ExitUnverified  = 11
	ExitInProgress  = 12
	ExitUnreachable = 6
)

var taxonomy = map[Code]Info{
	CodeInvalidRequest:             {400, ExitUsage, CategoryRefused, EffectNone, RetryFixRequest, "The request is malformed."},
	CodeInvalidArgument:            {400, ExitUsage, CategoryRefused, EffectNone, RetryFixRequest, "An argument is missing, unknown or has the wrong type or value."},
	CodeUnauthenticated:            {401, ExitAuth, CategoryRefused, EffectNone, RetryFixRequest, "The credential is missing, invalid, expired or revoked."},
	CodePermissionDenied:           {403, ExitAuth, CategoryRefused, EffectNone, RetryNever, "The caller is not allowed to run this operation here."},
	CodeWrongSurface:               {403, ExitAuth, CategoryRefused, EffectNone, RetryNever, "The credential was issued for a different interface."},
	CodeOperationNotFound:          {404, ExitRefused, CategoryRefused, EffectNone, RetryFixRequest, "No such operation is available to this caller."},
	CodeContractVersionUnsupported: {404, ExitRefused, CategoryRefused, EffectNone, RetryFixRequest, "This contract version is not served; see the supported versions."},
	CodeContractPinMismatch:        {409, ExitRefused, CategoryRefused, EffectNone, RetryFixRequest, "The pinned contract digest does not match the contract served."},
	CodeContractRevoked:            {410, ExitRefused, CategoryRefused, EffectNone, RetryNever, "This contract version was revoked and will not run."},
	CodeOperationUnverified:        {422, ExitRefused, CategoryRefused, EffectNone, RetryNever, "Nothing verified for this source context allows the operation to run."},
	CodeSessionInvalid:             {409, ExitRefused, CategoryRefused, EffectNone, RetryFixRequest, "The source session is unknown, expired, revoked, unauthenticated or for another source."},
	CodeSourceVersionUnsupported:   {409, ExitRefused, CategoryRefused, EffectNone, RetryNever, "The source version is outside the range this contract covers."},
	CodeConfirmationRequired:       {428, ExitRefused, CategoryRefused, EffectNone, RetryFixRequest, "A person's confirmation for this exact request is required."},
	CodePreconditionRequired:       {428, ExitRefused, CategoryRefused, EffectNone, RetryFixRequest, "The operation needs a freshness precondition that was not supplied."},
	CodePreconditionFailed:         {412, ExitRefused, CategoryRefused, EffectNone, RetryFixRequest, "The record changed or the observation is too old; read it again."},
	CodeBlockedByUnknownCommit:     {409, ExitRefused, CategoryRefused, EffectNone, RetryReconcile, "An earlier write to this scope has an unknown outcome; reconcile it first."},
	CodeRequestIDReused:            {409, ExitRefused, CategoryRefused, EffectNone, RetryNewRequestID, "This request id was used for a different request."},
	CodeEnvironmentChanged:         {409, ExitRefused, CategoryRefused, EffectNone, RetryFixRequest, "The execution environment changed after admission."},
	CodeUnsupported:                {422, ExitRefused, CategoryRefused, EffectNone, RetryNever, "The requested execution strategy is not available."},
	CodeCancelled:                  {409, ExitFailed, CategoryFailed, EffectNone, RetrySameRequest, "The run was cancelled before it reached the source."},
	CodeSourceRejected:             {422, ExitFailed, CategoryFailed, EffectNone, RetryFixRequest, "The source rejected the change; nothing was applied."},
	CodeSourceConflict:             {409, ExitFailed, CategoryFailed, EffectNone, RetryFixRequest, "The source reported a conflict; nothing was applied."},
	CodeSourceThrottled:            {429, ExitFailed, CategoryFailed, EffectNone, RetrySameRequest, "The source is rate limiting; nothing was applied."},
	CodeSourceUnavailable:          {503, ExitFailed, CategoryFailed, EffectNone, RetrySameRequest, "The source could not be reached before anything was sent."},
	CodeUnknownCommit:              {504, ExitUnknown, CategoryUnknownCommit, EffectUnknown, RetryReconcile, "The source may or may not have applied the change; do not retry blindly."},
	CodeUnverifiedEffect:           {502, ExitUnverified, CategoryUnverifiedEffect, EffectUnknown, RetryReconcile, "The source acknowledged the change but the result could not be verified."},
	CodeInvocationNotFound:         {404, ExitRefused, CategoryRefused, EffectNone, RetryFixRequest, "No such invocation for this caller."},
	CodeNotReconcilable:            {409, ExitRefused, CategoryRefused, EffectNone, RetryNever, "Only an invocation with an unknown outcome can be reconciled."},
	CodeReconcileInProgress:        {409, ExitRefused, CategoryRefused, EffectUnknown, RetrySameRequest, "Another reconciliation of this invocation is running."},
	CodeStillUnknown:               {409, ExitUnknown, CategoryUnknownCommit, EffectUnknown, RetryReconcile, "The source showed nothing that settles whether the change was applied."},
	CodeReconciledNotCommitted:     {409, ExitFailed, CategoryFailed, EffectNone, RetryNewRequestID, "The source proved this request was never applied and never will be."},
	CodeAbandoned:                  {409, ExitFailed, CategoryFailed, EffectNone, RetryNewRequestID, "The service restarted before the request reached the source."},
	CodeTooLarge:                   {413, ExitUsage, CategoryRefused, EffectNone, RetryFixRequest, "The request is larger than the interface accepts."},
	CodeRateLimited:                {429, ExitError, CategoryRefused, EffectNone, RetrySameRequest, "Too many requests from this caller."},
	CodeServiceUnreachable:         {503, ExitUnreachable, CategoryFailed, EffectUnknown, RetrySameRequest, "The service did not answer; the request may have been received. Repeat it with the same request id."},
	CodeInternal:                   {500, ExitError, CategoryRefused, EffectNone, RetryNever, "The interface failed before reaching a decision."},
}

// Codes lists every code in a fixed order, for documents and exhaustiveness tests.
func Codes() []Code {
	out := make([]Code, 0, len(taxonomy))
	for c := range taxonomy {
		out = append(out, c)
	}
	sortCodes(out)
	return out
}

func sortCodes(cs []Code) {
	for i := 1; i < len(cs); i++ {
		for j := i; j > 0 && cs[j] < cs[j-1]; j-- {
			cs[j], cs[j-1] = cs[j-1], cs[j]
		}
	}
}

// InfoOf returns the fixed meaning of a code; an unknown code is treated as internal.
func InfoOf(c Code) Info {
	if i, ok := taxonomy[c]; ok {
		return i
	}
	return taxonomy[CodeInternal]
}

// Error is the one typed failure every surface renders. Messages come from fixed text, never from source content.
type Error struct {
	Code         Code           `json:"code"`
	Message      string         `json:"message"`
	Reason       string         `json:"reason,omitempty"`
	Effect       Effect         `json:"effect"`
	Retry        Retry          `json:"retry"`
	RetryAfterMs int64          `json:"retry_after_ms,omitempty"`
	Details      map[string]any `json:"details,omitempty"`
}

func (e *Error) Error() string {
	if e.Reason != "" {
		return fmt.Sprintf("%s: %s (%s)", e.Code, e.Message, e.Reason)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Newf builds a typed error with the fixed message of its code.
func newError(c Code, reason string) *Error {
	i := InfoOf(c)
	return &Error{Code: c, Message: i.Summary, Reason: reason, Effect: i.Effect, Retry: i.Retry}
}

// Errf builds a typed error and attaches structured details.
func errWith(c Code, reason string, details map[string]any) *Error {
	e := newError(c, reason)
	e.Details = details
	return e
}

// AsError returns the typed error inside err, or an internal error that hides the original text.
func AsError(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return newError(CodeInternal, "")
}

// ClassifyReason maps what the execution runtime recorded to the public taxonomy.
func ClassifyReason(reason string) Code {
	switch {
	case reason == "request_malformed":
		return CodeInvalidRequest
	case strings.HasPrefix(reason, "param_"), reason == "expect_required", reason == "record_required":
		return CodeInvalidArgument
	case reason == "seen_required":
		return CodePreconditionRequired
	case reason == "stale_read", reason == "stale_observation":
		return CodePreconditionFailed
	case reason == "unknown_caller", reason == "caller_scope_missing", reason == "entitlement_missing",
		reason == "source_scope_missing", reason == "write_scope_insufficient", reason == "session_other_caller":
		return CodePermissionDenied
	case reason == "unknown_session", reason == "session_revoked", reason == "session_unauthenticated",
		reason == "session_expired", reason == "session_other_source", reason == "other_source":
		return CodeSessionInvalid
	case reason == "source_version_out_of_range":
		return CodeSourceVersionUnsupported
	case reason == "contract_pin_mismatch":
		return CodeContractPinMismatch
	case strings.HasPrefix(reason, "contract_revoked"):
		return CodeContractRevoked
	case reason == "confirmation_required", reason == "confirmation_spent":
		return CodeConfirmationRequired
	case reason == "strategy_unavailable", reason == "no_executor", reason == "concurrency_unknown":
		return CodeUnsupported
	case reason == "authorization_invalid", reason == "invalid_contract", reason == "not_executable",
		reason == "no_passing_witness", reason == "synthetic_witness", reason == "env_malformed",
		strings.HasPrefix(reason, "witness_"):
		return CodeOperationUnverified
	case reason == "environment_changed":
		return CodeEnvironmentChanged
	case reason == "unresolved_unknown_commit":
		return CodeBlockedByUnknownCommit
	case reason == "request_id_reused":
		return CodeRequestIDReused
	case reason == "cancelled_before_run", reason == "cancelled_before_effect":
		return CodeCancelled
	case reason == "rejected":
		return CodeSourceRejected
	case reason == "conflict":
		return CodeSourceConflict
	case reason == "throttled":
		return CodeSourceThrottled
	case reason == "unavailable", reason == "read_failed":
		return CodeSourceUnavailable
	case reason == "not_unknown":
		return CodeNotReconcilable
	case reason == "reconciled_not_committed":
		return CodeReconciledNotCommitted
	case strings.HasPrefix(reason, "abandoned_"):
		return CodeAbandoned
	case reason == "reconcile_in_progress":
		return CodeReconcileInProgress
	case reason == "expect_mismatch":
		return CodeInvalidArgument
	case reason == "other_caller":
		return CodeInvocationNotFound
	}
	return CodeInternal
}
