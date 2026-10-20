package bridge

import (
	"encoding/hex"
	"errors"
	"regexp"
	"sync"
	"time"

	"github.com/insightlabs38-pixel/vsf/backend/ir"
)

const (
	DefaultBindingTTL = 15 * time.Minute
	maxBindings       = 8
	manifestSkew      = 5 * time.Minute
	maxBindingAge     = 8 * time.Hour
)

// Validation is what the native validator says about one document.
type Validation struct {
	Valid      bool
	Family     string
	Digest     string
	Violations []string
}

// Validator is the Rust validator; the bridge keeps no copy of the invariants it enforces.
type Validator interface {
	Validate(doc []byte) (Validation, error)
}

var (
	ErrBindingDenied  = errors.New("session.denied")
	ErrBindingMissing = errors.New("session.unknown")
	ErrBindingStale   = errors.New("session.stale")

	documentID  = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)
	canonOrigin = regexp.MustCompile(`^https?://[a-z0-9.-]{1,253}(:[0-9]{1,5})?$`)
)

// Binding ties one capability manifest to one observed origin, one document and one paired client.
type Binding struct {
	ID        string    `json:"session_id"`
	Client    string    `json:"-"`
	Origin    string    `json:"origin"`
	AppID     string    `json:"app_id,omitempty"`
	Document  string    `json:"document"`
	Principal string    `json:"principal"`
	Runtime   string    `json:"runtime_id"`
	Manifest  string    `json:"manifest_digest"`
	Scopes    []string  `json:"scopes"`
	Created   time.Time `json:"-"`
	Expires   time.Time `json:"-"`
	ExpiresAt int64     `json:"expires_at"`
}

type identity struct{ principal, runtime string }

// Bindings records which runtime may act for which source, under which principal. Pairing alone grants none of it.
type Bindings struct {
	mu      sync.Mutex
	v       Validator
	now     func() time.Time
	ttl     time.Duration
	byID    map[string]*Binding
	byKey   map[string]string
	clients map[string]identity
}

func NewBindings(v Validator, now func() time.Time) *Bindings {
	if now == nil {
		now = time.Now
	}
	return &Bindings{v: v, now: now, ttl: DefaultBindingTTL, byID: map[string]*Binding{}, byKey: map[string]string{}, clients: map[string]identity{}}
}

func key(client, origin, document string) string { return client + "\x00" + origin + "\x00" + document }

// Register validates the manifest with Rust and binds it; the same manifest for the same document renews the binding.
func (b *Bindings) Register(client, origin, document string, manifest []byte) (Binding, error) {
	if client == "" || !documentID.MatchString(document) || !canonOrigin.MatchString(origin) {
		return Binding{}, ErrBindingDenied
	}
	res, err := b.v.Validate(manifest)
	if err != nil {
		return Binding{}, err
	}
	if !res.Valid || res.Family != "environment" || res.Digest == "" {
		return Binding{}, ErrBindingDenied
	}
	doc, err := ir.DecodeDocument(manifest)
	if err != nil || doc.Digest() != res.Digest {
		return Binding{}, ErrBindingDenied
	}
	m := doc.Unsafe().Environment
	if m == nil || m.Mode != ir.ExecutionMode("extension") {
		return Binding{}, ErrBindingDenied
	}
	now := b.now()
	if d := now.Sub(time.UnixMilli(int64(m.GeneratedAt))); d > manifestSkew || d < -manifestSkew {
		return Binding{}, ErrBindingStale
	}
	var scopes []string
	for _, g := range m.Permissions {
		if string(g.Origin) == origin {
			for _, s := range g.Scopes {
				scopes = append(scopes, string(s))
			}
		}
	}
	if len(scopes) == 0 {
		return Binding{}, ErrBindingDenied
	}
	id := identity{principal: string(m.PrincipalRef), runtime: string(m.RuntimeID)}
	app := ""
	if m.AppID != nil {
		app = string(*m.AppID)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if prior, ok := b.clients[client]; ok && prior != id {
		return Binding{}, ErrBindingDenied
	}
	k := key(client, origin, document)
	if old, ok := b.byID[b.byKey[k]]; ok {
		if old.Manifest == res.Digest && now.Before(old.Expires) {
			old.Expires = now.Add(b.ttl)
			old.ExpiresAt = old.Expires.Unix()
			return *old, nil
		}
		b.dropLocked(old)
	}
	live := 0
	for _, x := range b.byID {
		if x.Client == client && now.Before(x.Expires) {
			live++
		}
	}
	if live >= maxBindings {
		return Binding{}, ErrBindingDenied
	}
	raw := randBytes(12)
	bd := &Binding{
		ID: "bs_" + hex.EncodeToString(raw), Client: client, Origin: origin, AppID: app, Document: document,
		Principal: id.principal, Runtime: id.runtime, Manifest: res.Digest, Scopes: scopes,
		Created: now, Expires: now.Add(b.ttl),
	}
	bd.ExpiresAt = bd.Expires.Unix()
	b.clients[client] = id
	b.byID[bd.ID] = bd
	b.byKey[k] = bd.ID
	return *bd, nil
}

func (b *Bindings) dropLocked(x *Binding) {
	delete(b.byID, x.ID)
	delete(b.byKey, key(x.Client, x.Origin, x.Document))
}

// Renew extends a live binding for its owner; a binding is never older than maxBindingAge.
func (b *Bindings) Renew(id, client string) (Binding, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	x, ok := b.byID[id]
	if !ok || x.Client != client {
		return Binding{}, ErrBindingMissing
	}
	now := b.now()
	if !now.Before(x.Expires) || now.Sub(x.Created) > maxBindingAge {
		b.dropLocked(x)
		return Binding{}, ErrBindingStale
	}
	x.Expires = now.Add(b.ttl)
	x.ExpiresAt = x.Expires.Unix()
	return *x, nil
}

// Check is what anything acting for a source must pass: same client, same origin, same document, not expired, not ended.
func (b *Bindings) Check(id, client, origin, document string) (Binding, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	x, ok := b.byID[id]
	if !ok || x.Client != client {
		return Binding{}, ErrBindingMissing
	}
	if !b.now().Before(x.Expires) {
		b.dropLocked(x)
		return Binding{}, ErrBindingStale
	}
	if x.Origin != origin || x.Document != document {
		return Binding{}, ErrBindingDenied
	}
	return *x, nil
}

// Get returns a binding to its owner only; for anyone else it does not exist.
func (b *Bindings) Get(id, client string) (Binding, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	x, ok := b.byID[id]
	if !ok || x.Client != client {
		return Binding{}, ErrBindingMissing
	}
	if !b.now().Before(x.Expires) {
		b.dropLocked(x)
		return Binding{}, ErrBindingStale
	}
	return *x, nil
}

func (b *Bindings) End(id, client string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	x, ok := b.byID[id]
	if !ok || x.Client != client {
		return false
	}
	b.dropLocked(x)
	return true
}

// RevokeClient ends every binding a client holds, for use when its pairing is revoked.
func (b *Bindings) RevokeClient(client string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, x := range b.byID {
		if x.Client == client {
			b.dropLocked(x)
		}
	}
	delete(b.clients, client)
}
