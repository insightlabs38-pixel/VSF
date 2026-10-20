package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"sync"
	"sync/atomic"
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
	// Rev is the store's revision; every accepted change raises it by one and a change made from an older revision is refused.
	Rev uint64 `json:"rev"`
	// SecretGen rises each time the grant's stored tokens are replaced, which supersedes every lease taken before.
	SecretGen int `json:"secret_gen"`
	// Provider is the digest of the OAuth provider configuration the grant was made under; a refresh must present the same one.
	Provider string `json:"provider,omitempty"`
	// IdentityVerified is true only when the source principal was proven by a verified identity token or by trusted runtime evidence; a grant without it names no person.
	IdentityVerified bool `json:"identity_verified"`
	// RuntimeID and SessionID name the paired runtime and its bridge session that last observed the sign-in (source-managed sign-in only).
	RuntimeID  string    `json:"runtime_id,omitempty"`
	SessionID  string    `json:"session_id,omitempty"`
	ObservedAt time.Time `json:"observed_at,omitempty"`
	// PausedAt and PauseUntil are set while the source is asking the person for a challenge.
	PausedAt   time.Time `json:"paused_at,omitempty"`
	PauseUntil time.Time `json:"pause_until,omitempty"`
}

// Generation names this sign-in exactly: the grant and its epoch. Work and plans bound to it stop being valid when the epoch moves.
func (g Grant) Generation() string { return fmt.Sprintf("%s:%d", g.ID, g.Epoch) }

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
	if g.IdentityVerified != (g.PrincipalRef != "") || (g.PrincipalRef != "" && !idRe.MatchString(g.PrincipalRef)) {
		return ErrInvalid
	}
	switch g.Method {
	case MethodOAuthPKCE, MethodSourceBrowser, MethodVault:
	default:
		return ErrInvalid
	}
	return nil
}

// Store keeps grant records; they hold no secrets. Swap is the only way to change a record and must be atomic with respect to every other writer of the same store, in this process or another: it applies only when the stored revision equals prev, raises the revision by one, never lets a revoked grant leave that state, never lets the epoch fall, and never changes who or where the grant is for or widens its scopes.
type Store interface {
	Create(g Grant) error
	Get(id string) (Grant, bool)
	Swap(prev uint64, next Grant) (Grant, error)
	List(owner string) []Grant
	All() []Grant
}

var (
	// ErrConflict means the record changed since it was read; the caller re-reads and decides again.
	ErrConflict = errors.New("auth.conflict")
	// ErrEnded means the grant is revoked and can only stay that way.
	ErrEnded = errors.New("auth.grant_ended")
)

// MemStore is the in-memory store. Its atomicity covers this process only.
type MemStore struct {
	mu sync.Mutex
	m  map[string]Grant
}

func NewMemStore() *MemStore { return &MemStore{m: map[string]Grant{}} }

func (s *MemStore) Create(g Grant) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, dup := s.m[g.ID]; dup {
		return ErrConflict
	}
	g.Rev = 1
	s.m[g.ID] = g
	return nil
}

// checkSwap is the contract every Store applies inside its own atomic section.
func checkSwap(cur, next Grant, prev uint64) error {
	switch {
	case cur.Rev != prev:
		return ErrConflict
	case cur.State == StateRevoked && next.State != StateRevoked:
		return ErrEnded
	case next.ID != cur.ID || next.Account != cur.Account || next.Owner != cur.Owner || next.Source != cur.Source || next.Audience != cur.Audience ||
		next.Method != cur.Method || next.Issuer != cur.Issuer || next.PrincipalRef != cur.PrincipalRef || next.IdentityVerified != cur.IdentityVerified ||
		next.Provider != cur.Provider || next.Epoch < cur.Epoch || next.SecretGen < cur.SecretGen || !next.IssuedAt.Equal(cur.IssuedAt):
		return ErrInvalid
	}
	for _, sc := range next.Scopes {
		if !slices.Contains(cur.Scopes, sc) {
			return ErrInvalid
		}
	}
	return nil
}

func (s *MemStore) Swap(prev uint64, next Grant) (Grant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.m[next.ID]
	if !ok {
		return Grant{}, ErrConflict
	}
	if err := checkSwap(cur, next, prev); err != nil {
		return Grant{}, err
	}
	next.Rev = cur.Rev + 1
	s.m[next.ID] = next
	return next, nil
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

// Broker issues, checks and revokes grants. Every change goes through Store.Swap, so a stale writer can never undo a newer decision, and revocation wins over every other transition. Its in-memory parts (locks, single-flight, dependents) serialize one process only; cross-process safety rests on the Store's own atomicity.
type Broker struct {
	Store Store
	Audit *Audit
	// Vault, when set, is wiped of a grant's secrets whenever the grant ends.
	Vault Vault
	Now   func() time.Time

	mu         sync.Mutex
	dependents map[string]map[string]dependent
	flights    map[string]*flight
	wipes      map[string]Grant
	unaudited  []AuditRecord
	stripes    [64]sync.Mutex
	dropped    atomic.Uint64
	// requireAudit makes the mandatory security records a precondition or a reported failure instead of a best-effort note.
	requireAudit bool
}

type dependent struct {
	cancel func()
	epoch  int
}

func NewBroker(store Store, audit *Audit, now func() time.Time) *Broker {
	if store == nil {
		store = NewMemStore()
	}
	if now == nil {
		now = time.Now
	}
	return &Broker{Store: store, Audit: audit, Now: now, requireAudit: audit != nil, dependents: map[string]map[string]dependent{},
		flights: map[string]*flight{}, wipes: map[string]Grant{}}
}

// AuditRequired reports whether security records are mandatory here. A broker built without an audit log has chosen to run without one; one built with a log treats a failed write as a failure of the operation.
func (b *Broker) AuditRequired() bool { return b.requireAudit }

// DroppedDiagnostics counts best-effort records (refusals, profile notes) that could not be written.
func (b *Broker) DroppedDiagnostics() uint64 { return b.dropped.Load() }

// stripe serializes the short commit phase of one grant's secrets in this process.
func (b *Broker) stripe(id string) *sync.Mutex {
	h := sha256.Sum256([]byte(id))
	return &b.stripes[int(h[0])%len(b.stripes)]
}

// note is a best-effort diagnostic: a failed write is counted and never changes a decision.
func (b *Broker) note(ev Event, g Grant, actor string, r Reason) {
	if b.Audit == nil {
		return
	}
	if err := b.Audit.Append(AuditRecord{At: b.Now(), Event: ev, Grant: g.ID, Account: g.Account, Actor: actor, Origin: g.Source.Origin, Reason: r}); err != nil {
		b.dropped.Add(1)
	}
}

const maxUnaudited = 1024

// mustNote is a mandatory security record. When it cannot be written it is kept for FlushAudit and the caller is told with ErrAudit, which no caller may treat as success.
func (b *Broker) mustNote(ev Event, g Grant, actor string, r Reason) error {
	if !b.requireAudit {
		return nil
	}
	rec := AuditRecord{At: b.Now(), Event: ev, Grant: g.ID, Account: g.Account, Actor: actor, Origin: g.Source.Origin, Reason: r}
	if err := b.flushLocked(); err == nil {
		if err := b.Audit.Append(rec); err == nil {
			return nil
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.unaudited) < maxUnaudited {
		b.unaudited = append(b.unaudited, rec)
	}
	return ErrAudit
}

func (b *Broker) flushLocked() error {
	b.mu.Lock()
	pending := b.unaudited
	b.unaudited = nil
	b.mu.Unlock()
	for i, rec := range pending {
		if err := b.Audit.Append(rec); err != nil {
			b.mu.Lock()
			b.unaudited = append(slices.Clone(pending[i:]), b.unaudited...)
			b.mu.Unlock()
			return ErrAudit
		}
	}
	return nil
}

// FlushAudit retries mandatory records that failed to write and returns how many are still unwritten.
func (b *Broker) FlushAudit() (int, error) {
	if !b.requireAudit {
		return 0, nil
	}
	err := b.flushLocked()
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.unaudited), err
}

// Issue records a new active grant. The id is random and carries no meaning. The grant is returned only after its issuance is on the audit log; if that fails the grant is ended.
func (b *Broker) Issue(g Grant) (Grant, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return Grant{}, err
	}
	g.ID, g.State, g.Epoch, g.Rev = "gr_"+hex.EncodeToString(raw), StateActive, 1, 0
	g.PausedAt, g.PauseUntil = time.Time{}, time.Time{}
	if g.IssuedAt.IsZero() {
		g.IssuedAt = b.Now()
	}
	g.Scopes = slices.Compact(slices.Sorted(slices.Values(g.Scopes)))
	if err := g.validate(); err != nil {
		return Grant{}, err
	}
	if err := b.Store.Create(g); err != nil {
		return Grant{}, err
	}
	g, _ = b.Store.Get(g.ID)
	if err := b.mustNote(EventIssued, g, g.Owner, ReasonOK); err != nil {
		_, _ = b.endGrant(g.ID, g.Owner, ReasonRevoked)
		return Grant{}, err
	}
	return g, nil
}

// Check is one attempt to use a grant: who is asking, for which source and audience, with which scopes.
type Check struct {
	GrantID  string
	Owner    string
	Origin   string
	Audience string
	Scopes   []string
	// Generation, when set, must be the grant's current sign-in (see Grant.Generation); work bound to an earlier one is refused.
	Generation string
	// VerifiedIdentity demands that the grant names a proven person, for operations that depend on who is signed in.
	VerifiedIdentity bool
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
	case c.Generation != "" && c.Generation != g.Generation():
		return refuse(ReasonEpoch, ErrReauth)
	case c.VerifiedIdentity && !g.IdentityVerified:
		return refuse(ReasonIdentity, ErrDenied)
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
