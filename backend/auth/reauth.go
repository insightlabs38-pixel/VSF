package auth

import (
	"crypto/subtle"
	"errors"
	"time"
)

// Challenge is a step-up the source demands of the person. VSF can only wait for it: nothing here accepts a code, a passkey assertion or a password.
type Challenge string

const (
	ChallengeMFA     Challenge = "mfa"
	ChallengePasskey Challenge = "passkey"
	ChallengeReauth  Challenge = "reauth"
)

const pauseTTL = 15 * time.Minute

var challenges = map[Challenge]bool{ChallengeMFA: true, ChallengePasskey: true, ChallengeReauth: true}

func init() { addReasons(ReasonSwitched, ReasonPauseExpired, ReasonStaleObs) }

const (
	ReasonSwitched     Reason = "principal_changed"
	ReasonPauseExpired Reason = "pause_expired"
	ReasonStaleObs     Reason = "observation_not_after_pause"
)

// Pause holds a grant while the person completes a challenge at the source; dependent work waits, and any use of the grant is refused as needing reauthentication. Pausing a revoked grant fails; pausing a paused grant keeps its first deadline.
func (b *Broker) Pause(grantID, owner string, kind Challenge) (Grant, error) {
	if !challenges[kind] {
		return Grant{}, ErrDenied
	}
	now := b.Now()
	g, changed, err := b.update(grantID, func(g *Grant) error {
		switch {
		case g.Owner != owner || g.State == StateRevoked:
			return ErrDenied
		case g.State == StatePaused:
			return errUnchanged
		}
		g.State, g.PausedAt, g.PauseUntil = StatePaused, now, now.Add(pauseTTL)
		return nil
	})
	if err != nil {
		return Grant{}, err
	}
	if changed {
		if err := b.mustNote(EventPaused, g, owner, ReasonPaused); err != nil {
			return g, err
		}
	}
	return g, nil
}

// Paused reports whether a grant is waiting for the person.
func (b *Broker) Paused(grantID string) bool {
	g, ok := b.Store.Get(grantID)
	return ok && g.State == StatePaused
}

// Resume lifts a pause, and only on a fresh observation that the runtime authority verified: the same person signed in with nothing left to answer, observed after the pause began. The sign-in epoch rises, so plans and work from before the challenge must be rebound.
func (b *Broker) Resume(grantID, owner string, o Observation) (Grant, error) {
	g, ok := b.Store.Get(grantID)
	if !ok || !o.verified || g.Owner != owner || g.State != StatePaused || o.Owner != owner || o.Source != g.Source {
		return Grant{}, ErrDenied
	}
	now := b.Now()
	switch {
	case !now.Before(g.PauseUntil):
		_, _ = b.endGrant(g.ID, owner, ReasonPauseExpired)
		return Grant{}, ErrExpired
	case !o.Authenticated:
		b.note(EventRefused, g, owner, ReasonPaused)
		return Grant{}, ErrReauth
	case subtle.ConstantTimeCompare([]byte(o.PrincipalRef), []byte(g.PrincipalRef)) != 1:
		_, _ = b.endGrant(g.ID, owner, ReasonSwitched)
		return Grant{}, ErrDenied
	case o.Challenge != "" || o.ObservedAt.IsZero() || !o.ObservedAt.After(g.PausedAt):
		b.note(EventRefused, g, owner, ReasonStaleObs)
		return Grant{}, ErrReauth
	}
	seen := g.Epoch
	next, changed, err := b.update(g.ID, func(x *Grant) error {
		if x.State != StatePaused || x.Epoch != seen {
			return ErrDenied
		}
		x.State, x.PausedAt, x.PauseUntil = StateActive, zeroTime, zeroTime
		x.Epoch++
		x.RuntimeID, x.SessionID, x.ObservedAt = o.RuntimeID, o.SessionID, o.ObservedAt
		if !o.Expires.IsZero() && o.Expires.After(now) && o.Expires.Before(x.ExpiresAt) {
			x.ExpiresAt = o.Expires
		}
		return nil
	})
	if err != nil || !changed {
		return Grant{}, errors.Join(ErrDenied, err)
	}
	b.cancelDependents(next, owner, next.Epoch)
	if err := b.mustNote(EventResumed, next, owner, ReasonOK); err != nil {
		_, _, _ = b.update(next.ID, func(x *Grant) error {
			if x.State != StateActive || x.Epoch != next.Epoch {
				return errUnchanged
			}
			x.State, x.PausedAt, x.PauseUntil = StatePaused, now, now.Add(pauseTTL)
			return nil
		})
		return Grant{}, err
	}
	return next, nil
}

// Sweep ends grants whose time is up: expired ones, and paused ones the person never came back to. Whatever depended on them is cancelled, and queued cleanup and audit writes are retried.
func (b *Broker) Sweep() int {
	now := b.Now()
	n := 0
	for _, g := range b.Store.All() {
		if g.State == StateRevoked {
			continue
		}
		var why Reason
		switch {
		case !now.Before(g.ExpiresAt):
			why = ReasonExpired
		case g.State == StatePaused && !now.Before(g.PauseUntil):
			why = ReasonPauseExpired
		default:
			continue
		}
		if _, err := b.endGrant(g.ID, g.Owner, why); err == nil || errors.Is(err, ErrWipePending) || errors.Is(err, ErrAudit) {
			n++
		}
	}
	b.FlushWipes()
	_, _ = b.FlushAudit()
	return n
}
