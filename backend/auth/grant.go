package auth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"sync"
	"time"
)

// Method says how a person signed in; none of them lets VSF answer a challenge or mint a session the source did not.
type Method string

const (
	MethodOAuthPKCE     Method = "oauth_pkce"
	MethodSourceBrowser Method = "source_browser"
	MethodVault         Method = "vault"
)

type State string

const (
	StateActive  State = "active"
	StatePaused  State = "paused"
	StateRevoked State = "revoked"
)

// Source names the application and the one origin a grant may act on.
type Source struct {
	AppID  string `json:"app_id"`
	Origin string `json:"origin"`
}

// Grant is what the broker knows about one sign-in: whose it is, where it works, for which audience and until when. It never holds a secret.
type Grant struct {
	ID           string    `json:"id"`
	Account      string    `json:"account"`
	Owner        string    `json:"owner"`
	Source       Source    `json:"source"`
	Audience     string    `json:"audience"`
	Issuer       string    `json:"issuer,omitempty"`
	Scopes       []string  `json:"scopes"`
	Method       Method    `json:"method"`
	PrincipalRef string    `json:"principal_ref,omitempty"`
	IssuedAt     time.Time `json:"issued_at"`
	ExpiresAt    time.Time `json:"expires_at"`
	State        State     `json:"state"`
	// Epoch rises each time the person signs in again, so work started under an earlier sign-in can be told apart.
	Epoch int `json:"epoch"`
}

var (
	idRe       = regexp.MustCompile(`^[A-Za-z0-9._:@-]{1,128}$`)
	originRe   = regexp.MustCompile(`^https?://[a-z0-9.-]{1,253}(:[0-9]{1,5})?$`)
	audienceRe = regexp.MustCompile(`^[\x21-\x7e]{1,512}$`)
	scopeRe    = regexp.MustCompile(`^[A-Za-z0-9._:/-]{1,128}$`)
)

const maxScopes = 64

func (g Grant) validate() error {
	if !idRe.MatchString(g.Account) || !idRe.MatchString(g.Owner) || !idRe.MatchString(g.Source.AppID) || !originRe.MatchString(g.Source.Origin) ||
		!audienceRe.MatchString(g.Audience) || len(g.Scopes) > maxScopes || g.ExpiresAt.IsZero() || !g.ExpiresAt.After(g.IssuedAt) {
		return ErrInvalid
	}
	for _, s := range g.Scopes {
		if !scopeRe.MatchString(s) {
			return ErrInvalid
		}
	}
	switch g.Method {
	case MethodOAuthPKCE, MethodSourceBrowser, MethodVault:
	default:
		return ErrInvalid
	}
	return nil
}

// Store keeps grant records; they hold no secrets.
type Store interface {
	Put(g Grant) error
	Get(id string) (Grant, bool)
	List(owner string) []Grant
	All() []Grant
}

// MemStore is the in-memory store.
type MemStore struct {
	mu sync.Mutex
	m  map[string]Grant
}

func NewMemStore() *MemStore { return &MemStore{m: map[string]Grant{}} }

func (s *MemStore) Put(g Grant) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[g.ID] = g
	return nil
}

func (s *MemStore) Get(id string) (Grant, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.m[id]
	return g, ok
}

func (s *MemStore) List(owner string) []Grant {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Grant
	for _, g := range s.m {
		if g.Owner == owner {
			out = append(out, g)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (s *MemStore) All() []Grant {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Grant, 0, len(s.m))
	for _, g := range s.m {
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Broker issues, checks and revokes grants. Every decision it makes is audited.
type Broker struct {
	Store Store
	Audit *Audit
	// Vault, when set, is wiped of a grant's secrets whenever the grant ends.
	Vault Vault
	Now   func() time.Time

	mu         sync.Mutex
	dependents map[string]map[string]func()
	pauses     map[string]pause
}

func NewBroker(store Store, audit *Audit, now func() time.Time) *Broker {
	if store == nil {
		store = NewMemStore()
	}
	if now == nil {
		now = time.Now
	}
	return &Broker{Store: store, Audit: audit, Now: now, dependents: map[string]map[string]func(){}, pauses: map[string]pause{}}
}

func (b *Broker) note(ev Event, g Grant, actor string, r Reason) {
	if b.Audit != nil {
		_ = b.Audit.Append(AuditRecord{At: b.Now(), Event: ev, Grant: g.ID, Account: g.Account, Actor: actor, Origin: g.Source.Origin, Reason: r})
	}
}

// Issue records a new active grant. The id is random and carries no meaning.
func (b *Broker) Issue(g Grant) (Grant, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return Grant{}, err
	}
	g.ID, g.State, g.Epoch = "gr_"+hex.EncodeToString(raw), StateActive, 1
	if g.IssuedAt.IsZero() {
		g.IssuedAt = b.Now()
	}
	g.Scopes = slices.Clone(g.Scopes)
	sort.Strings(g.Scopes)
	if err := g.validate(); err != nil {
		return Grant{}, err
	}
	if err := b.Store.Put(g); err != nil {
		return Grant{}, err
	}
	b.note(EventIssued, g, g.Owner, ReasonOK)
	return g, nil
}

// Check is one attempt to use a grant: who is asking, for which source and audience, with which scopes.
type Check struct {
	GrantID  string
	Owner    string
	Origin   string
	Audience string
	Scopes   []string
}

// Authorize returns the grant only for its owner, exact source and audience, covered scopes and a live state; callers see only ErrDenied, ErrExpired or ErrReauth.
func (b *Broker) Authorize(c Check) (Grant, error) {
	g, ok := b.Store.Get(c.GrantID)
	if !ok {
		b.note(EventRefused, Grant{ID: c.GrantID, Source: Source{Origin: safeOrigin(c.Origin)}}, safeActor(c.Owner), ReasonUnknownGrant)
		return Grant{}, ErrDenied
	}
	refuse := func(r Reason, err error) (Grant, error) {
		b.note(EventRefused, g, safeActor(c.Owner), r)
		return Grant{}, err
	}
	switch {
	case g.Owner != c.Owner:
		return refuse(ReasonOwner, ErrDenied)
	case g.Source.Origin != c.Origin:
		return refuse(ReasonSource, ErrDenied)
	case g.Audience != c.Audience:
		return refuse(ReasonAudience, ErrDenied)
	case g.State == StateRevoked:
		return refuse(ReasonRevoked, ErrDenied)
	case !b.Now().Before(g.ExpiresAt):
		return refuse(ReasonExpired, ErrExpired)
	case g.State == StatePaused:
		return refuse(ReasonPaused, ErrReauth)
	}
	for _, s := range c.Scopes {
		if !slices.Contains(g.Scopes, s) {
			return refuse(ReasonScope, ErrDenied)
		}
	}
	return g, nil
}

// List returns a caller's own grants.
func (b *Broker) List(owner string) []Grant { return b.Store.List(owner) }

func safeOrigin(o string) string {
	if originRe.MatchString(o) {
		return o
	}
	return ""
}

func safeActor(a string) string {
	if idRe.MatchString(a) {
		return a
	}
	return ""
}

func (b *Broker) must(id string) (Grant, error) {
	g, ok := b.Store.Get(id)
	if !ok {
		return Grant{}, fmt.Errorf("%w: grant", ErrDenied)
	}
	return g, nil
}
