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

// Lookup binds a session to exactly one grant: same caller, application, exact origin, source principal and runtime, a verified identity and a source-managed sign-in. With any of those missing, or more than one live grant fitting, the session counts as not signed in; scopes come from that one grant only. A session whose matching grants have all ended is reported revoked.
func (s Sessions) Lookup(id string) (execution.Session, bool) {
	sess, ok := s.Inner.Lookup(id)
	if !ok {
		return execution.Session{}, false
	}
	sess.Authenticated, sess.SourceScopes, sess.Generation = false, nil, ""
	if sess.Owner == "" || sess.AppID == "" || sess.Origin == "" || sess.PrincipalRef == "" || sess.RuntimeID == "" {
		return sess, true
	}
	now := s.Broker.Now()
	var live []Grant
	ended := 0
	for _, g := range s.Broker.Store.List(sess.Owner) {
		if g.Method != MethodSourceBrowser || !g.IdentityVerified || g.Source.AppID != sess.AppID || g.Source.Origin != sess.Origin ||
			g.PrincipalRef != sess.PrincipalRef || g.RuntimeID != sess.RuntimeID {
			continue
		}
		switch {
		case g.State == StateRevoked:
			ended++
		case g.State == StateActive && now.Before(g.ExpiresAt):
			live = append(live, g)
		}
	}
	switch {
	case len(live) == 1:
		g := live[0]
		sess.Authenticated, sess.ExpiresAt, sess.Generation = true, g.ExpiresAt, g.Generation()
		sess.SourceScopes = slices.Clone(g.Scopes)
	case len(live) > 1:
		s.Broker.note(EventRefused, live[0], safeActor(sess.Owner), ReasonAmbiguous)
	case ended > 0:
		sess.Revoked = true
	}
	return sess, true
}
