package api

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
	"unicode"

	"github.com/insightlabs38-pixel/vsf/backend/execution"
)

const (
	MaxTokenTTL         = 90 * 24 * time.Hour
	MaxTokensPerCaller  = 64
	maxTokenLabel       = 64
	tokenPrefix         = "vsf_"
	tokenSecretBytes    = 32
	maxCredentialLength = 512
)

// TokenInfo is everything known about a credential except the credential itself.
type TokenInfo struct {
	ID        string  `json:"id"`
	Caller    string  `json:"caller"`
	Surface   Surface `json:"surface"`
	Label     string  `json:"label,omitempty"`
	IssuedAt  int64   `json:"issued_at"`
	ExpiresAt int64   `json:"expires_at"`
	Revoked   bool    `json:"revoked,omitempty"`
}

type tokenRecord struct {
	TokenInfo
	Hash string `json:"hash"`
}

// TokenStore persists credential records; they hold only digests, never a credential.
type TokenStore interface {
	Load() ([]tokenRecord, error)
	Save([]tokenRecord) error
}

// FileTokenStore keeps the records in one private file, replaced atomically.
type FileTokenStore struct{ Path string }

func (f FileTokenStore) Load() ([]tokenRecord, error) {
	info, err := os.Stat(f.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("token store %s must not be accessible by group or others", f.Path)
	}
	b, err := os.ReadFile(f.Path)
	if err != nil {
		return nil, err
	}
	var recs []tokenRecord
	if err := json.Unmarshal(b, &recs); err != nil {
		return nil, fmt.Errorf("token store is corrupt")
	}
	return recs, nil
}

func (f FileTokenStore) Save(recs []tokenRecord) error {
	b, err := json.Marshal(recs)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(f.Path), ".tokens-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), f.Path)
}

// TokenAuth issues and checks credentials, each bound to one caller and one surface.
type TokenAuth struct {
	mu      sync.Mutex
	now     func() time.Time
	callers execution.Callers
	byHash  map[[32]byte]*tokenRecord
	store   TokenStore
}

func NewTokenAuth(callers execution.Callers, now func() time.Time, store TokenStore) (*TokenAuth, error) {
	if callers == nil {
		return nil, errors.New("a caller directory is required")
	}
	if now == nil {
		now = time.Now
	}
	a := &TokenAuth{now: now, callers: callers, byHash: map[[32]byte]*tokenRecord{}, store: store}
	if store != nil {
		recs, err := store.Load()
		if err != nil {
			return nil, err
		}
		for i := range recs {
			raw, err := hex.DecodeString(recs[i].Hash)
			if err != nil || len(raw) != 32 {
				return nil, errors.New("token store is corrupt")
			}
			var k [32]byte
			copy(k[:], raw)
			a.byHash[k] = &recs[i]
		}
	}
	return a, nil
}

func validSurface(s Surface) bool {
	switch s {
	case SurfaceREST, SurfaceCLI, SurfaceMCP, SurfaceUI:
		return true
	}
	return false
}

func digest(token string) [32]byte { return sha256.Sum256([]byte(token)) }

func idOf(h [32]byte) string { return "tok_" + hex.EncodeToString(h[:6]) }

func cleanLabel(s string) (string, error) {
	if len(s) > maxTokenLabel {
		return "", errors.New("label too long")
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return "", errors.New("label has control characters")
		}
	}
	return s, nil
}

func (a *TokenAuth) saveLocked() error {
	if a.store == nil {
		return nil
	}
	recs := make([]tokenRecord, 0, len(a.byHash))
	for _, r := range a.byHash {
		recs = append(recs, *r)
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].ID < recs[j].ID })
	return a.store.Save(recs)
}

// Issue mints a credential that works on one surface for one caller; the secret is returned once and never stored.
func (a *TokenAuth) Issue(caller string, surface Surface, ttl time.Duration, label string) (string, TokenInfo, error) {
	if !validSurface(surface) {
		return "", TokenInfo{}, errors.New("unknown surface")
	}
	if ttl <= 0 || ttl > MaxTokenTTL {
		return "", TokenInfo{}, errors.New("ttl out of range")
	}
	label, err := cleanLabel(label)
	if err != nil {
		return "", TokenInfo{}, err
	}
	if c, ok := a.callers.Lookup(caller); !ok || c.ID != caller {
		return "", TokenInfo{}, errors.New("unknown caller")
	}
	var raw [tokenSecretBytes]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", TokenInfo{}, err
	}
	token := tokenPrefix + string(surface) + "_" + base64.RawURLEncoding.EncodeToString(raw[:])
	h := digest(token)
	now := a.now()
	rec := &tokenRecord{TokenInfo: TokenInfo{ID: idOf(h), Caller: caller, Surface: surface, Label: label,
		IssuedAt: now.UnixMilli(), ExpiresAt: now.Add(ttl).UnixMilli()}, Hash: hex.EncodeToString(h[:])}
	a.mu.Lock()
	defer a.mu.Unlock()
	active := 0
	for _, r := range a.byHash {
		if r.Caller == caller && !r.Revoked && r.ExpiresAt > now.UnixMilli() {
			active++
		}
	}
	if active >= MaxTokensPerCaller {
		return "", TokenInfo{}, errors.New("too many active credentials for this caller")
	}
	a.byHash[h] = rec
	if err := a.saveLocked(); err != nil {
		delete(a.byHash, h)
		return "", TokenInfo{}, err
	}
	return token, rec.TokenInfo, nil
}

// Authenticate accepts a credential only on the surface it was issued for, and only while its caller still exists.
func (a *TokenAuth) Authenticate(surface Surface, credential string) (Principal, *Error) {
	unauth := func(reason string) (Principal, *Error) { return Principal{}, newError(CodeUnauthenticated, reason) }
	if credential == "" || len(credential) > maxCredentialLength {
		return unauth("")
	}
	h := digest(credential)
	a.mu.Lock()
	rec, ok := a.byHash[h]
	var r tokenRecord
	if ok {
		r = *rec
	}
	a.mu.Unlock()
	if !ok {
		return unauth("")
	}
	want, _ := hex.DecodeString(r.Hash)
	if subtle.ConstantTimeCompare(want, h[:]) != 1 {
		return unauth("")
	}
	if r.Revoked || a.now().UnixMilli() >= r.ExpiresAt {
		return unauth("credential_inactive")
	}
	if r.Surface != surface {
		return Principal{}, newError(CodeWrongSurface, "")
	}
	if c, ok := a.callers.Lookup(r.Caller); !ok || c.ID != r.Caller {
		return unauth("caller_revoked")
	}
	return Principal{Caller: r.Caller, Surface: surface, Credential: r.ID}, nil
}

// Revoke ends one credential; it stays revoked.
func (a *TokenAuth) Revoke(id string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, r := range a.byHash {
		if r.ID == id && !r.Revoked {
			r.Revoked = true
			_ = a.saveLocked()
			return true
		}
	}
	return false
}

// RevokeCaller ends every credential of one caller on every surface.
func (a *TokenAuth) RevokeCaller(caller string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	n := 0
	for _, r := range a.byHash {
		if r.Caller == caller && !r.Revoked {
			r.Revoked = true
			n++
		}
	}
	if n > 0 {
		_ = a.saveLocked()
	}
	return n
}

// List describes a caller's credentials without any secret.
func (a *TokenAuth) List(caller string) []TokenInfo {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []TokenInfo
	for _, r := range a.byHash {
		if r.Caller == caller {
			out = append(out, r.TokenInfo)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

var _ Authenticator = (*TokenAuth)(nil)
