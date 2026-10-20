package bridge

import (
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"
)

const (
	pairCodeBytes     = 10
	pairIterations    = 100_000
	DefaultOfferTTL   = 2 * time.Minute
	MaxOfferAttempts  = 3
	DefaultAccessTTL  = 15 * time.Minute
	DefaultRefreshTTL = 30 * 24 * time.Hour
	maxClients        = 16
	pairRatePerMin    = 30
)

const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

var b64 = base64.RawURLEncoding

// PairConfig fixes who may pair and for how long credentials live.
type PairConfig struct {
	// Origins are the exact extension origins that may pair; a peer from anywhere else is refused before any secret is checked.
	Origins    []string
	OfferTTL   time.Duration
	AccessTTL  time.Duration
	RefreshTTL time.Duration
	Store      Store
	Now        func() time.Time
	// OnRevoke runs after a client is revoked, so state keyed by the client can be dropped with it.
	OnRevoke func(clientID string)
}

// Client is one paired extension installation. Only hashes of its secrets are kept.
type Client struct {
	ID          string    `json:"id"`
	Origin      string    `json:"origin"`
	Name        string    `json:"name"`
	RefreshHash string    `json:"refresh_hash"`
	PrevHash    string    `json:"prev_hash,omitempty"`
	Created     time.Time `json:"created"`
	RefreshEnd  time.Time `json:"refresh_expires"`
	Revoked     bool      `json:"revoked"`
}

type offer struct {
	id       string
	origin   string
	salt     []byte
	key      []byte
	expires  time.Time
	attempts int
	nonceC   []byte
	nonceS   []byte
}

type accessEntry struct {
	client  string
	expires time.Time
}

// Pairing authorizes extension installations to use the bridge. It never sees the source application's credentials.
type Pairing struct {
	mu      sync.Mutex
	cfg     PairConfig
	origins map[string]bool
	offer   *offer
	clients map[string]*Client
	access  map[[32]byte]accessEntry
	window  []time.Time
}

var (
	ErrDenied        = errors.New("pair.denied")
	ErrRateLimited   = errors.New("pair.rate_limited")
	ErrRefreshReused = errors.New("pair.refresh_reused")
	ErrExpired       = errors.New("pair.expired")
)

func NewPairing(cfg PairConfig) (*Pairing, error) {
	if len(cfg.Origins) == 0 {
		return nil, errors.New("pairing: at least one extension origin is required")
	}
	if cfg.OfferTTL == 0 {
		cfg.OfferTTL = DefaultOfferTTL
	}
	if cfg.AccessTTL == 0 {
		cfg.AccessTTL = DefaultAccessTTL
	}
	if cfg.RefreshTTL == 0 {
		cfg.RefreshTTL = DefaultRefreshTTL
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Store == nil {
		cfg.Store = memoryStore{}
	}
	p := &Pairing{cfg: cfg, origins: map[string]bool{}, clients: map[string]*Client{}, access: map[[32]byte]accessEntry{}}
	for _, o := range cfg.Origins {
		if !strings.HasPrefix(o, "chrome-extension://") || strings.ContainsAny(o[len("chrome-extension://"):], "/*?# ") {
			return nil, errors.New("pairing: origin must be an exact chrome-extension:// origin")
		}
		p.origins[o] = true
	}
	loaded, err := cfg.Store.Load()
	if err != nil {
		return nil, err
	}
	for i := range loaded {
		c := loaded[i]
		p.clients[c.ID] = &c
	}
	return p, nil
}

func randBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("pairing: no entropy: " + err.Error())
	}
	return b
}

func sum(b []byte) [32]byte { return sha256.Sum256(b) }

// NormalizeCode folds the display form of a pairing code to the characters it encodes.
func NormalizeCode(code string) string {
	var out strings.Builder
	for _, r := range strings.ToUpper(code) {
		switch r {
		case '-', ' ':
			continue
		case 'O':
			r = '0'
		case 'I', 'L':
			r = '1'
		}
		out.WriteRune(r)
	}
	return out.String()
}

func deriveKey(code string, salt []byte) ([]byte, error) {
	return pbkdf2.Key(sha256.New, NormalizeCode(code), salt, pairIterations, 32)
}

func mac(key []byte, label string, parts ...[]byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(label))
	for _, p := range parts {
		h.Write([]byte{0})
		h.Write(p)
	}
	return h.Sum(nil)
}

// NewOffer starts one pairing for one extension origin and returns the code the user must carry to it.
func (p *Pairing) NewOffer(origin string) (code string, expires time.Time, err error) {
	if !p.origins[origin] {
		return "", time.Time{}, ErrDenied
	}
	raw := randBytes(pairCodeBytes)
	var chars strings.Builder
	var acc, bits uint
	for _, b := range raw {
		acc = acc<<8 | uint(b)
		bits += 8
		for bits >= 5 {
			bits -= 5
			chars.WriteByte(crockford[(acc>>bits)&31])
		}
	}
	plain := chars.String()
	salt := randBytes(16)
	key, err := deriveKey(plain, salt)
	if err != nil {
		return "", time.Time{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.cfg.Now()
	p.offer = &offer{id: hex.EncodeToString(randBytes(8)), origin: origin, salt: salt, key: key, expires: now.Add(p.cfg.OfferTTL)}
	return plain[0:4] + "-" + plain[4:8] + "-" + plain[8:12] + "-" + plain[12:16], p.offer.expires, nil
}

func (p *Pairing) limited(now time.Time) bool {
	cut := now.Add(-time.Minute)
	kept := p.window[:0]
	for _, t := range p.window {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	p.window = kept
	if len(p.window) >= pairRatePerMin {
		return true
	}
	p.window = append(p.window, now)
	return false
}

type BeginReply struct {
	OfferID    string `json:"offer_id"`
	Salt       string `json:"salt"`
	NonceS     string `json:"nonce_s"`
	Iterations int    `json:"iterations"`
	ExpiresAt  int64  `json:"expires_at"`
}

// Begin binds the extension's nonce to the offer and returns the server's. Failures all look alike.
func (p *Pairing) Begin(origin string, nonceC []byte) (BeginReply, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.cfg.Now()
	if p.limited(now) {
		return BeginReply{}, ErrRateLimited
	}
	o := p.offer
	if o == nil || !now.Before(o.expires) || o.origin != origin || !p.origins[origin] || len(nonceC) < 16 || len(nonceC) > 64 {
		return BeginReply{}, ErrDenied
	}
	o.nonceC = nonceC
	o.nonceS = randBytes(16)
	return BeginReply{OfferID: o.id, Salt: b64.EncodeToString(o.salt), NonceS: b64.EncodeToString(o.nonceS), Iterations: pairIterations, ExpiresAt: o.expires.Unix()}, nil
}

type Credentials struct {
	ClientID       string `json:"client_id"`
	Refresh        string `json:"refresh"`
	RefreshExpires int64  `json:"refresh_expires"`
	Access         string `json:"access"`
	AccessExpires  int64  `json:"access_expires"`
	Proof          string `json:"proof,omitempty"`
}

func (p *Pairing) issue(c *Client, now time.Time) Credentials {
	refresh := randBytes(32)
	if c.RefreshHash != "" {
		c.PrevHash = c.RefreshHash
	}
	h := sum(refresh)
	c.RefreshHash = hex.EncodeToString(h[:])
	c.RefreshEnd = now.Add(p.cfg.RefreshTTL)
	token := randBytes(32)
	p.access[sum(token)] = accessEntry{client: c.ID, expires: now.Add(p.cfg.AccessTTL)}
	return Credentials{
		ClientID: c.ID, Refresh: b64.EncodeToString(refresh), RefreshExpires: c.RefreshEnd.Unix(),
		Access: b64.EncodeToString(token), AccessExpires: now.Add(p.cfg.AccessTTL).Unix(),
	}
}

func (p *Pairing) saveLocked() error {
	list := make([]Client, 0, len(p.clients))
	for _, c := range p.clients {
		list = append(list, *c)
	}
	return p.cfg.Store.Save(list)
}

// Finish checks the extension's proof of the code and, once, hands out credentials with the server's own proof.
func (p *Pairing) Finish(origin, offerID, name string, proof []byte) (Credentials, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.cfg.Now()
	if p.limited(now) {
		return Credentials{}, ErrRateLimited
	}
	o := p.offer
	if o == nil || !now.Before(o.expires) || o.id != offerID || o.origin != origin || o.nonceC == nil {
		return Credentials{}, ErrDenied
	}
	want := mac(o.key, "vsf-pair-c", o.nonceC, o.nonceS, []byte(origin))
	if subtle.ConstantTimeCompare(want, proof) != 1 {
		o.attempts++
		o.nonceC = nil
		if o.attempts >= MaxOfferAttempts {
			p.offer = nil
		}
		return Credentials{}, ErrDenied
	}
	live := 0
	for _, c := range p.clients {
		if !c.Revoked {
			live++
		}
	}
	if live >= maxClients {
		return Credentials{}, ErrDenied
	}
	p.offer = nil
	c := &Client{ID: "cli_" + hex.EncodeToString(randBytes(12)), Origin: origin, Name: clean(name, 64), Created: now}
	creds := p.issue(c, now)
	p.clients[c.ID] = c
	if err := p.saveLocked(); err != nil {
		delete(p.clients, c.ID)
		return Credentials{}, err
	}
	creds.Proof = b64.EncodeToString(mac(o.key, "vsf-pair-s", o.nonceC, o.nonceS, []byte(c.ID)))
	return creds, nil
}

func clean(s string, max int) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 0x20 && r != 0x7f && b.Len() < max {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Refresh rotates the refresh secret. A secret that was already rotated away means a copy exists, so the client is revoked.
func (p *Pairing) Refresh(origin, id string, refresh []byte) (Credentials, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.cfg.Now()
	if p.limited(now) {
		return Credentials{}, ErrRateLimited
	}
	c := p.clients[id]
	if c == nil || c.Revoked || c.Origin != origin {
		return Credentials{}, ErrDenied
	}
	h := sum(refresh)
	got := hex.EncodeToString(h[:])
	if c.PrevHash != "" && subtle.ConstantTimeCompare([]byte(got), []byte(c.PrevHash)) == 1 {
		p.revokeLocked(c)
		return Credentials{}, ErrRefreshReused
	}
	if subtle.ConstantTimeCompare([]byte(got), []byte(c.RefreshHash)) != 1 {
		return Credentials{}, ErrDenied
	}
	if !now.Before(c.RefreshEnd) {
		return Credentials{}, ErrExpired
	}
	creds := p.issue(c, now)
	if err := p.saveLocked(); err != nil {
		return Credentials{}, err
	}
	return creds, nil
}

func (p *Pairing) revokeLocked(c *Client) {
	c.Revoked = true
	if p.cfg.OnRevoke != nil {
		defer p.cfg.OnRevoke(c.ID)
	}
	for k, e := range p.access {
		if e.client == c.ID {
			delete(p.access, k)
		}
	}
	_ = p.saveLocked()
}

// Revoke ends a client's access at once, including every access token it holds.
func (p *Pairing) Revoke(id string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	c := p.clients[id]
	if c == nil || c.Revoked {
		return false
	}
	p.revokeLocked(c)
	return true
}

// Authenticate maps a bearer access token to the paired client, or reports that there is none.
func (p *Pairing) Authenticate(token, origin string) (string, bool) {
	raw, err := b64.DecodeString(token)
	if err != nil || len(raw) != 32 {
		return "", false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	k := sum(raw)
	e, ok := p.access[k]
	if !ok {
		return "", false
	}
	now := p.cfg.Now()
	c := p.clients[e.client]
	if !now.Before(e.expires) || c == nil || c.Revoked || c.Origin != origin {
		if !now.Before(e.expires) {
			delete(p.access, k)
		}
		return "", false
	}
	return c.ID, true
}

// Clients lists paired installations without any secret material.
func (p *Pairing) Clients() []Client {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]Client, 0, len(p.clients))
	for _, c := range p.clients {
		cp := *c
		cp.RefreshHash, cp.PrevHash = "", ""
		out = append(out, cp)
	}
	return out
}
