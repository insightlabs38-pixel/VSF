package auth

import (
	"crypto/subtle"
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

type pause struct {
	grant   string
	owner   string
	epoch   int
	at      time.Time
	expires time.Time
}

func init() { addReasons(ReasonSwitched, ReasonPauseExpired, ReasonStaleObs) }

const (
	ReasonSwitched     Reason = "principal_changed"
	ReasonPauseExpired Reason = "pause_expired"
	ReasonStaleObs     Reason = "observation_not_after_pause"
)

// Pause holds a grant while the person completes a challenge at the source; dependent work waits, and any use of the grant is refused as needing reauthentication.
func (b *Broker) Pause(grantID, owner string, kind Challenge) (Grant, error) {
	g, ok := b.Store.Get(grantID)
	if !ok || g.Owner != owner || !challenges[kind] || g.State == StateRevoked {
		return Grant{}, ErrDenied
	}
	now := b.Now()
	b.mu.Lock()
	if _, held := b.pauses[g.ID]; !held {
		b.pauses[g.ID] = pause{grant: g.ID, owner: owner, epoch: g.Epoch, at: now, expires: now.Add(pauseTTL)}
	}
	b.mu.Unlock()
	if g.State != StatePaused {
		g.State = StatePaused
		if err := b.Store.Put(g); err != nil {
			return Grant{}, err
		}
		b.note(EventPaused, g, owner, ReasonPaused)
	}
	return g, nil
}

// Paused reports whether a grant is waiting for the person.
func (b *Broker) Paused(grantID string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, ok := b.pauses[grantID]
	return ok
}

// Resume lifts a pause, and only on a fresh observation from the source that shows the same person signed in with nothing left to answer. The caller supplying that observation cannot make it up: it comes from the paired page.
func (b *Broker) Resume(grantID, owner string, o Observation) (Grant, error) {
	g, ok := b.Store.Get(grantID)
	b.mu.Lock()
	p, held := b.pauses[grantID]
	b.mu.Unlock()
	if !ok || !held || g.Owner != owner || p.owner != owner || g.State != StatePaused || o.Owner != owner || o.Source != g.Source {
		return Grant{}, ErrDenied
	}
	now := b.Now()
	switch {
	case !now.Before(p.expires):
		_ = b.end(g, owner, ReasonPauseExpired)
		return Grant{}, ErrExpired
	case !o.Authenticated:
		b.note(EventRefused, g, owner, ReasonPaused)
		return Grant{}, ErrReauth
	case subtle.ConstantTimeCompare([]byte(o.PrincipalRef), []byte(g.PrincipalRef)) != 1:
		_ = b.end(g, owner, ReasonSwitched)
		return Grant{}, ErrDenied
	case o.Challenge != "" || o.ObservedAt.IsZero() || !o.ObservedAt.After(p.at):
		b.note(EventRefused, g, owner, ReasonStaleObs)
		return Grant{}, ErrReauth
	}
	g.State = StateActive
	g.Epoch++
	if !o.Expires.IsZero() && o.Expires.After(now) && o.Expires.Before(g.ExpiresAt) {
		g.ExpiresAt = o.Expires
	}
	if err := b.Store.Put(g); err != nil {
		return Grant{}, err
	}
	b.mu.Lock()
	delete(b.pauses, grantID)
	b.mu.Unlock()
	b.note(EventResumed, g, owner, ReasonOK)
	return g, nil
}

// Sweep ends grants whose time is up: expired ones, and paused ones the person never came back to. Whatever depended on them is cancelled.
func (b *Broker) Sweep() int {
	now := b.Now()
	n := 0
	for _, g := range b.Store.All() {
		if g.State == StateRevoked {
			continue
		}
		b.mu.Lock()
		p, held := b.pauses[g.ID]
		b.mu.Unlock()
		var why Reason
		switch {
		case !now.Before(g.ExpiresAt):
			why = ReasonExpired
		case held && !now.Before(p.expires):
			why = ReasonPauseExpired
		default:
			continue
		}
		if b.end(g, g.Owner, why) == nil {
			n++
		}
	}
	return n
}
