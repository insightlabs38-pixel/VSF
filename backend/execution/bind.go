package execution

import (
	"slices"
	"time"

	"github.com/insightlabs38-pixel/vsf/backend/ir"
	"github.com/insightlabs38-pixel/vsf/backend/ir/rt"
)

type Caller struct {
	ID           string
	Scopes       []string
	Entitlements []string
}

// Callers is consulted at admission and again before the side effect, so a revoked role stops a queued run.
type Callers interface {
	Lookup(id string) (Caller, bool)
}

type Session struct {
	ID           string
	Owner        string
	AppID        string
	AppVersion   string
	VariantClass string
	VariantID    string
	PrincipalRef string
	RuntimeID    string
	// Origin is the exact source origin the session was registered for; Generation names the sign-in it runs under (empty when no broker is involved).
	Origin           string
	Generation       string
	SourceState      string
	CapabilityDigest string
	SourceScopes     []string
	Authenticated    bool
	ExpiresAt        time.Time
	Revoked          bool
}

type Sessions interface {
	Lookup(id string) (Session, bool)
}

func hasAll(have, need []string) bool {
	for _, n := range need {
		if !slices.Contains(have, n) {
			return false
		}
	}
	return true
}

// bind checks caller, source session and environment against the contract before any source side effect.
func bind(c ir.OperationContract, callers Callers, sessions Sessions, caller, sessionID string, now time.Time) (Env, error) {
	a := c.Authorization
	var who Caller
	if a.RequiresVsfCaller {
		got, ok := callers.Lookup(caller)
		if !ok || got.ID == "" {
			return Env{}, refuse("unknown_caller")
		}
		who = got
		if !hasAll(who.Scopes, scopeStrings(a.RequiredScopes)) {
			return Env{}, refuse("caller_scope_missing")
		}
		if a.Entitlement != nil && !slices.Contains(who.Entitlements, string(*a.Entitlement)) {
			return Env{}, refuse("entitlement_missing")
		}
	}
	s, ok := sessions.Lookup(sessionID)
	if !ok {
		return Env{}, refuse("unknown_session")
	}
	if s.Revoked {
		return Env{}, refuse("session_revoked")
	}
	if a.RequiresSourceSession {
		if !s.Authenticated {
			return Env{}, refuse("session_unauthenticated")
		}
		if !s.ExpiresAt.IsZero() && !s.ExpiresAt.After(now) {
			return Env{}, refuse("session_expired")
		}
		if s.Owner != caller {
			return Env{}, refuse("session_other_caller")
		}
		if !hasAll(s.SourceScopes, scopeStrings(a.RequiredScopes)) {
			return Env{}, refuse("source_scope_missing")
		}
	}
	if s.AppID != string(c.Source.AppID) || s.VariantClass != string(c.Source.VariantClass) {
		return Env{}, refuse("session_other_source")
	}
	if !versionInRange(s.AppVersion, c.Source.VersionRange) {
		return Env{}, refuse("source_version_out_of_range")
	}
	return Env{
		Now:              now.UnixMilli(),
		RuntimeID:        s.RuntimeID,
		SourceState:      s.SourceState,
		CapabilityDigest: s.CapabilityDigest,
		PrincipalRef:     s.PrincipalRef,
		VariantID:        s.VariantID,
	}, nil
}

func scopeStrings(in []ir.ScopeId) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = string(s)
	}
	return out
}

func versionInRange(have string, r *ir.VersionRange) bool {
	if r == nil || (r.Min == nil && r.Max == nil) {
		return true
	}
	v, ok := rt.ParseVersion(have)
	if !ok {
		return false
	}
	if r.Min != nil {
		if lo, ok := rt.ParseVersion(string(*r.Min)); !ok || v.Less(lo) {
			return false
		}
	}
	if r.Max != nil {
		if hi, ok := rt.ParseVersion(string(*r.Max)); !ok || hi.Less(v) {
			return false
		}
	}
	return true
}
