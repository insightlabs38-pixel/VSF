package auth

import (
	"slices"

	"github.com/insightlabs38-pixel/vsf/backend/execution"
)

// Sessions overlays what the broker knows about a person's sign-in on the sessions the bridge reports, so the runtime refuses work for a sign-in that ended, paused or expired.
type Sessions struct {
	Broker *Broker
	// Inner describes the runtime, the application version and the environment; the broker adds the sign-in.
	Inner execution.Sessions
}

// Lookup returns the session with Authenticated, Revoked, expiry and source scopes taken from the caller's grants for that application. A grant for a different person than the runtime reports counts as no sign-in.
func (s Sessions) Lookup(id string) (execution.Session, bool) {
	sess, ok := s.Inner.Lookup(id)
	if !ok {
		return execution.Session{}, false
	}
	var live, ended int
	sess.Authenticated = false
	sess.SourceScopes = nil
	for _, g := range s.Broker.Store.List(sess.Owner) {
		if g.Source.AppID != sess.AppID || g.Method != MethodSourceBrowser {
			continue
		}
		switch {
		case g.State == StateRevoked:
			ended++
		case g.State == StateActive && s.Broker.Now().Before(g.ExpiresAt) && (sess.PrincipalRef == "" || sess.PrincipalRef == g.PrincipalRef):
			live++
			sess.Authenticated = true
			sess.ExpiresAt = g.ExpiresAt
			sess.SourceScopes = slices.Clone(g.Scopes)
		}
	}
	if live == 0 && ended > 0 {
		sess.Revoked = true
	}
	return sess, true
}
