// Package pairing authenticates one native companion to the browser extension that asked to use it.
package pairing

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"sync"
	"time"
)

// Error carries a stable code; the codes are the contract with callers and tests.
type Error struct{ Code string }

func (e *Error) Error() string { return e.Code }

func fail(code string) error { return &Error{Code: code} }

// Code returns the stable code of an error from this package, or "" for any other error.
func Code(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

type Config struct {
	// Dir holds the pairing records; empty keeps them in memory only.
	Dir string
	// Peers are the exact browser origins that may ask to pair, for example chrome-extension://<id>.
	Peers []string
	Now   func() time.Time
	Rand  io.Reader

	CodeTTL        time.Duration
	AccessTTL      time.Duration
	RefreshTTL     time.Duration
	MaxPending     int
	MaxAttempts    int
	BeginPerMinute int
}

const (
	nonceHexLen = 32
	usedKeep    = 8
	proofPrefix = "vsf-pair-v1"
)

// Alphabet is Crockford base32 without the letters people misread.
const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

type pending struct {
	id       string
	peer     string
	nonceC   string
	nonceS   string
	expires  time.Time
	approved bool
	code     string
	attempts int
}

// Pending is what an operator sees when deciding whether to allow a pairing.
type Pending struct {
	ID       string    `json:"pair_id"`
	Peer     string    `json:"peer"`
	Expires  time.Time `json:"expires_at"`
	Approved bool      `json:"approved"`
}

type Begin struct {
	PairID  string    `json:"pair_id"`
	NonceS  string    `json:"nonce_s"`
	Expires time.Time `json:"expires_at"`
}

type Credentials struct {
	PairingID      string    `json:"pairing_id"`
	Access         string    `json:"access"`
	AccessExpires  time.Time `json:"access_expires_at"`
	Refresh        string    `json:"refresh"`
	RefreshExpires time.Time `json:"refresh_expires_at"`
}

type Principal struct {
	PairingID string
	Peer      string
}

type Service struct {
	cfg     Config
	mu      sync.Mutex
	peers   map[string]bool
	pending map[string]*pending
	begins  map[string][]time.Time
	store   *store
}

func New(cfg Config) (*Service, error) {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Rand == nil {
		cfg.Rand = rand.Reader
	}
	if cfg.CodeTTL == 0 {
		cfg.CodeTTL = 2 * time.Minute
	}
	if cfg.AccessTTL == 0 {
		cfg.AccessTTL = 15 * time.Minute
	}
	if cfg.RefreshTTL == 0 {
		cfg.RefreshTTL = 30 * 24 * time.Hour
	}
	if cfg.MaxPending == 0 {
		cfg.MaxPending = 4
	}
	if cfg.MaxAttempts == 0 {
		cfg.MaxAttempts = 3
	}
	if cfg.BeginPerMinute == 0 {
		cfg.BeginPerMinute = 6
	}
	s := &Service{cfg: cfg, peers: map[string]bool{}, pending: map[string]*pending{}, begins: map[string][]time.Time{}}
	for _, p := range cfg.Peers {
		if p == "" || p == "null" || strings.Contains(p, "*") {
			return nil, errors.New("pairing: invalid peer origin " + p)
		}
		s.peers[p] = true
	}
	st, err := openStore(cfg.Dir)
	if err != nil {
		return nil, err
	}
	s.store = st
	return s, nil
}

func (s *Service) random(n int) (string, error) {
	b := make([]byte, n)
	if _, err := io.ReadFull(s.cfg.Rand, b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (s *Service) code() (string, error) {
	b := make([]byte, 10)
	if _, err := io.ReadFull(s.cfg.Rand, b); err != nil {
		return "", err
	}
	out := make([]byte, 10)
	for i, v := range b {
		out[i] = alphabet[int(v)&31]
	}
	return string(out), nil
}

// NormalizeCode upper-cases a typed code and drops separators; it does not guess at misread letters.
func NormalizeCode(code string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(code) {
		if r == '-' || r == ' ' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Proof is what the extension sends to show it was given the code, bound to this exchange and this peer.
func Proof(code, pairID, peer, nonceC, nonceS string) string {
	m := hmac.New(sha256.New, []byte(NormalizeCode(code)))
	for _, part := range []string{proofPrefix, pairID, peer, nonceC, nonceS} {
		m.Write([]byte(part))
		m.Write([]byte{0})
	}
	return hex.EncodeToString(m.Sum(nil))
}

func validNonce(n string) bool {
	if len(n) != nonceHexLen {
		return false
	}
	_, err := hex.DecodeString(n)
	return err == nil
}

func hash(secret string) string {
	h := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(h[:])
}

func (s *Service) expire(now time.Time) {
	for id, p := range s.pending {
		if !now.Before(p.expires) {
			delete(s.pending, id)
		}
	}
}

// Begin opens a pairing for a peer that is on the allow-list; nothing is granted until an operator approves it.
func (s *Service) Begin(peer, nonceC string) (Begin, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.cfg.Now()
	s.expire(now)
	if !s.peers[peer] {
		return Begin{}, fail("pairing.peer_not_allowed")
	}
	if !validNonce(nonceC) {
		return Begin{}, fail("pairing.bad_request")
	}
	recent := s.begins[peer][:0]
	for _, t := range s.begins[peer] {
		if now.Sub(t) < time.Minute {
			recent = append(recent, t)
		}
	}
	if len(recent) >= s.cfg.BeginPerMinute {
		s.begins[peer] = recent
		return Begin{}, fail("pairing.rate_limited")
	}
	s.begins[peer] = append(recent, now)
	if len(s.pending) >= s.cfg.MaxPending {
		return Begin{}, fail("pairing.too_many_pending")
	}
	id, err := s.random(16)
	if err != nil {
		return Begin{}, err
	}
	ns, err := s.random(16)
	if err != nil {
		return Begin{}, err
	}
	p := &pending{id: id, peer: peer, nonceC: nonceC, nonceS: ns, expires: now.Add(s.cfg.CodeTTL)}
	s.pending[id] = p
	return Begin{PairID: id, NonceS: ns, Expires: p.expires}, nil
}

// Pending lists the requests an operator can still decide on.
func (s *Service) Pending() []Pending {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expire(s.cfg.Now())
	out := make([]Pending, 0, len(s.pending))
	for _, p := range s.pending {
		out = append(out, Pending{ID: p.id, Peer: p.peer, Expires: p.expires, Approved: p.approved})
	}
	return out
}

// Approve is the operator's explicit authorization; it returns the one-time code to be typed into the extension.
func (s *Service) Approve(pairID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expire(s.cfg.Now())
	p, ok := s.pending[pairID]
	if !ok {
		return "", fail("pairing.unknown")
	}
	if p.approved {
		return "", fail("pairing.already_approved")
	}
	code, err := s.code()
	if err != nil {
		return "", err
	}
	p.code, p.approved = code, true
	return code, nil
}

func (s *Service) Deny(pairID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.pending, pairID)
}

func (s *Service) issue(p *record, now time.Time) (Credentials, error) {
	access, err := s.random(32)
	if err != nil {
		return Credentials{}, err
	}
	refresh, err := s.random(32)
	if err != nil {
		return Credentials{}, err
	}
	p.AccessHash, p.AccessExpires = hash("a_"+access), now.Add(s.cfg.AccessTTL)
	if p.RefreshHash != "" {
		p.Used = append(p.Used, p.RefreshHash)
		if len(p.Used) > usedKeep {
			p.Used = p.Used[len(p.Used)-usedKeep:]
		}
	}
	p.RefreshHash, p.RefreshExpires = hash("r_"+refresh), now.Add(s.cfg.RefreshTTL)
	return Credentials{
		PairingID:      p.ID,
		Access:         "vsf_a_" + access,
		AccessExpires:  p.AccessExpires,
		Refresh:        "vsf_r_" + refresh,
		RefreshExpires: p.RefreshExpires,
	}, nil
}

// Finish turns an approved pairing into credentials once, for the peer that began it and only with the right code.
func (s *Service) Finish(pairID, peer, proof string) (Credentials, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.cfg.Now()
	s.expire(now)
	p, ok := s.pending[pairID]
	if !ok {
		return Credentials{}, fail("pairing.unknown")
	}
	if p.peer != peer {
		return Credentials{}, fail("pairing.peer_mismatch")
	}
	if !p.approved {
		return Credentials{}, fail("pairing.not_approved")
	}
	want := Proof(p.code, p.id, p.peer, p.nonceC, p.nonceS)
	if subtle.ConstantTimeCompare([]byte(want), []byte(strings.ToLower(proof))) != 1 {
		p.attempts++
		if p.attempts >= s.cfg.MaxAttempts {
			delete(s.pending, pairID)
			return Credentials{}, fail("pairing.attempts_exceeded")
		}
		return Credentials{}, fail("pairing.bad_proof")
	}
	delete(s.pending, pairID)
	id, err := s.random(16)
	if err != nil {
		return Credentials{}, err
	}
	rec := &record{ID: id, Peer: peer, CreatedAt: now}
	creds, err := s.issue(rec, now)
	if err != nil {
		return Credentials{}, err
	}
	if err := s.store.add(rec, now); err != nil {
		return Credentials{}, err
	}
	return creds, nil
}

// Renew rotates a refresh token. Presenting one that was already rotated means it leaked, so the pairing is revoked.
func (s *Service) Renew(peer, refresh string) (Credentials, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.cfg.Now()
	h, ok := strings.CutPrefix(refresh, "vsf_r_")
	if !ok || len(h) != 64 {
		return Credentials{}, fail("auth.invalid")
	}
	rec, stale := s.store.byRefresh(hash("r_" + h))
	if rec == nil {
		return Credentials{}, fail("auth.invalid")
	}
	if rec.Revoked {
		return Credentials{}, fail("auth.revoked")
	}
	if rec.Peer != peer {
		return Credentials{}, fail("auth.peer_mismatch")
	}
	if stale {
		if err := s.store.revoke(rec.ID, "refresh_reused"); err != nil {
			return Credentials{}, err
		}
		return Credentials{}, fail("auth.refresh_reused")
	}
	if !now.Before(rec.RefreshExpires) {
		return Credentials{}, fail("auth.expired")
	}
	next := *rec
	creds, err := s.issue(&next, now)
	if err != nil {
		return Credentials{}, err
	}
	if err := s.store.replace(&next); err != nil {
		return Credentials{}, err
	}
	return creds, nil
}

// Authenticate checks an access token for the peer that presents it; a token stolen by another origin is useless.
func (s *Service) Authenticate(peer, access string) (Principal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	h, ok := strings.CutPrefix(access, "vsf_a_")
	if !ok || len(h) != 64 {
		return Principal{}, fail("auth.invalid")
	}
	rec := s.store.byAccess(hash("a_" + h))
	if rec == nil {
		return Principal{}, fail("auth.invalid")
	}
	if rec.Revoked {
		return Principal{}, fail("auth.revoked")
	}
	if rec.Peer != peer {
		return Principal{}, fail("auth.peer_mismatch")
	}
	if !s.cfg.Now().Before(rec.AccessExpires) {
		return Principal{}, fail("auth.expired")
	}
	return Principal{PairingID: rec.ID, Peer: rec.Peer}, nil
}

// Revoke ends a pairing for good; its tokens stop working immediately.
func (s *Service) Revoke(pairingID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.store.revoke(pairingID, "revoked")
}

// Info is the safe view of a pairing: no secret material.
type Info struct {
	ID        string    `json:"pairing_id"`
	Peer      string    `json:"peer"`
	CreatedAt time.Time `json:"created_at"`
	Revoked   bool      `json:"revoked"`
	Reason    string    `json:"reason,omitempty"`
}

func (s *Service) List() []Info {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.store.list()
}
