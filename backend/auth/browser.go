package auth

import (
	"slices"
	"sort"
	"sync"
	"time"
)

const (
	intentTTL      = 15 * time.Minute
	maxSourceLife  = 12 * time.Hour
	maxIntentsEach = 16
)

// Observation is what the paired browser runtime reports about a source page. Only trusted bridge code may build one, and Owner must be the caller that runtime is paired with.
type Observation struct {
	Owner         string
	Source        Source
	PrincipalRef  string
	Authenticated bool
	Expires       time.Time
	Scopes        []string
}

type intent struct {
	account string
	scopes  []string
	expires time.Time
}

// Browser tracks sign-ins the source application manages itself. VSF never sees or sends credentials: the person signs in on the page, the runtime reports that it happened, and a grant records it without any secret.
type Browser struct {
	Broker *Broker

	mu      sync.Mutex
	intents map[string]intent
}

func NewBrowser(b *Broker) *Browser { return &Browser{Broker: b, intents: map[string]intent{}} }

func ikey(owner string, s Source) string { return owner + "\x00" + s.AppID + "\x00" + s.Origin }

// Expect records that a caller is about to sign in to a source; an observation of a sign-in that was not expected creates nothing.
func (br *Browser) Expect(owner, account string, src Source, scopes []string) error {
	if !idRe.MatchString(owner) || !idRe.MatchString(account) || !idRe.MatchString(src.AppID) || !originRe.MatchString(src.Origin) || len(scopes) > maxScopes {
		return ErrInvalid
	}
	for _, s := range scopes {
		if !scopeRe.MatchString(s) {
			return ErrInvalid
		}
	}
	sc := slices.Clone(scopes)
	sort.Strings(sc)
	now := br.Broker.Now()
	br.mu.Lock()
	defer br.mu.Unlock()
	count := 0
	for k, x := range br.intents {
		if !now.Before(x.expires) {
			delete(br.intents, k)
		} else {
			count++
		}
	}
	if count >= maxIntentsEach*8 {
		return ErrDenied
	}
	br.intents[ikey(owner, src)] = intent{account: account, scopes: slices.Compact(sc), expires: now.Add(intentTTL)}
	return nil
}

func (br *Browser) active(owner string, src Source) []Grant {
	var out []Grant
	for _, g := range br.Broker.Store.List(owner) {
		if g.Method == MethodSourceBrowser && g.Source == src && g.State != StateRevoked {
			out = append(out, g)
		}
	}
	return out
}

// Observe applies what the runtime saw. A sign-out ends the grants for that source; a sign-in as someone else ends the old person's grant before anything new is recorded.
func (br *Browser) Observe(o Observation) (Grant, error) {
	if !idRe.MatchString(o.Owner) || !idRe.MatchString(o.Source.AppID) || !originRe.MatchString(o.Source.Origin) {
		return Grant{}, ErrInvalid
	}
	now := br.Broker.Now()
	if !o.Authenticated {
		for _, g := range br.active(o.Owner, o.Source) {
			br.Broker.note(EventSignedOut, g, o.Owner, ReasonRevoked)
			if err := br.Broker.end(g, o.Owner); err != nil {
				return Grant{}, err
			}
		}
		return Grant{}, nil
	}
	if o.PrincipalRef == "" || !idRe.MatchString(o.PrincipalRef) {
		return Grant{}, ErrInvalid
	}
	expires := now.Add(maxSourceLife)
	if !o.Expires.IsZero() && o.Expires.Before(expires) {
		expires = o.Expires
	}
	if !expires.After(now) {
		return Grant{}, ErrExpired
	}
	for _, g := range br.active(o.Owner, o.Source) {
		if g.PrincipalRef == o.PrincipalRef {
			g.ExpiresAt, g.State = expires, StateActive
			return g, br.Broker.Store.Put(g)
		}
		if err := br.Broker.end(g, o.Owner); err != nil {
			return Grant{}, err
		}
	}
	k := ikey(o.Owner, o.Source)
	br.mu.Lock()
	in, ok := br.intents[k]
	delete(br.intents, k)
	br.mu.Unlock()
	if !ok || !now.Before(in.expires) {
		return Grant{}, ErrDenied
	}
	var scopes []string
	for _, s := range in.scopes {
		if slices.Contains(o.Scopes, s) {
			scopes = append(scopes, s)
		}
	}
	g, err := br.Broker.Issue(Grant{Account: in.account, Owner: o.Owner, Source: o.Source, Audience: o.Source.Origin, Scopes: scopes,
		Method: MethodSourceBrowser, PrincipalRef: o.PrincipalRef, ExpiresAt: expires})
	if err != nil {
		return Grant{}, err
	}
	br.Broker.note(EventSignedIn, g, o.Owner, ReasonOK)
	return g, nil
}

// Authenticated reports whether the source itself has a signed-in session for the caller. A grant made another way, such as OAuth, does not make it true.
func (br *Browser) Authenticated(owner string, src Source) bool {
	now := br.Broker.Now()
	for _, g := range br.active(owner, src) {
		if g.State == StateActive && now.Before(g.ExpiresAt) {
			return true
		}
	}
	return false
}
