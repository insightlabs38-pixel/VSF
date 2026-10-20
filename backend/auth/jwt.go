package auth

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"slices"
	"strings"
	"time"
)

const (
	maxTokenBytes = 16 << 10
	clockSkew     = 60 * time.Second
	minRSABits    = 2048
)

// KeySet holds the public keys a token issuer publishes, by key id.
type KeySet struct{ keys map[string]crypto.PublicKey }

func NewKeySet() *KeySet { return &KeySet{keys: map[string]crypto.PublicKey{}} }

// Add registers a key; only RSA of at least 2048 bits, ECDSA P-256 and Ed25519 are accepted.
func (k *KeySet) Add(kid string, pub crypto.PublicKey) error {
	switch p := pub.(type) {
	case *rsa.PublicKey:
		if p.N.BitLen() < minRSABits {
			return ErrInvalid
		}
	case *ecdsa.PublicKey:
		if p.Curve != elliptic.P256() {
			return ErrInvalid
		}
	case ed25519.PublicKey:
	default:
		return ErrInvalid
	}
	if kid == "" {
		return ErrInvalid
	}
	k.keys[kid] = pub
	return nil
}

type jwk struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Crv string `json:"crv"`
	N   string `json:"n"`
	E   string `json:"e"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

func b64(s string) ([]byte, error) { return base64.RawURLEncoding.DecodeString(s) }

// ParseJWKS reads a published key set, skipping keys of kinds VSF does not accept instead of failing the whole set.
func ParseJWKS(raw []byte) (*KeySet, error) {
	var doc struct {
		Keys []jwk `json:"keys"`
	}
	if len(raw) > 256<<10 || json.Unmarshal(raw, &doc) != nil {
		return nil, ErrInvalid
	}
	ks := NewKeySet()
	for _, k := range doc.Keys {
		switch k.Kty {
		case "RSA":
			n, e := new(big.Int), new(big.Int)
			nb, err1 := b64(k.N)
			eb, err2 := b64(k.E)
			if err1 != nil || err2 != nil || len(eb) > 4 {
				continue
			}
			n.SetBytes(nb)
			e.SetBytes(eb)
			_ = ks.Add(k.Kid, &rsa.PublicKey{N: n, E: int(e.Int64())})
		case "EC":
			xb, err1 := b64(k.X)
			yb, err2 := b64(k.Y)
			if k.Crv != "P-256" || err1 != nil || err2 != nil {
				continue
			}
			x, y := new(big.Int).SetBytes(xb), new(big.Int).SetBytes(yb)
			if !elliptic.P256().IsOnCurve(x, y) {
				continue
			}
			_ = ks.Add(k.Kid, &ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y})
		case "OKP":
			xb, err := b64(k.X)
			if k.Crv != "Ed25519" || err != nil || len(xb) != ed25519.PublicKeySize {
				continue
			}
			_ = ks.Add(k.Kid, ed25519.PublicKey(xb))
		}
	}
	return ks, nil
}

// Claims are the registered claims VSF checks.
type Claims struct {
	Issuer     string
	Subject    string
	Audience   []string
	Authorized string
	Expires    time.Time
	NotBefore  time.Time
	IssuedAt   time.Time
	Nonce      string
}

// Expect is what a token must say to be accepted here.
type Expect struct {
	Issuer   string
	Audience string
	Nonce    string
	Now      time.Time
	// MaxAge, when set, rejects tokens issued longer ago than this.
	MaxAge time.Duration
}

type rawClaims struct {
	Iss   string          `json:"iss"`
	Sub   string          `json:"sub"`
	Aud   json.RawMessage `json:"aud"`
	Azp   string          `json:"azp"`
	Exp   *json.Number    `json:"exp"`
	Nbf   *json.Number    `json:"nbf"`
	Iat   *json.Number    `json:"iat"`
	Nonce string          `json:"nonce"`
}

func unixOf(n *json.Number) (time.Time, bool) {
	if n == nil {
		return time.Time{}, false
	}
	f, err := n.Float64()
	if err != nil || f < 0 || f > 1e11 {
		return time.Time{}, false
	}
	return time.Unix(int64(f), 0), true
}

func audiences(raw json.RawMessage) ([]string, bool) {
	var one string
	if json.Unmarshal(raw, &one) == nil {
		return []string{one}, one != ""
	}
	var many []string
	if json.Unmarshal(raw, &many) == nil && len(many) > 0 {
		return many, true
	}
	return nil, false
}

// noDuplicateKeys rejects an object that repeats a member name, since parsers disagree on which one wins.
func noDuplicateKeys(b []byte) bool {
	dec := json.NewDecoder(bytes.NewReader(b))
	tok, err := dec.Token()
	if d, ok := tok.(json.Delim); err != nil || !ok || d != '{' {
		return false
	}
	seen := map[string]bool{}
	for dec.More() {
		kt, err := dec.Token()
		k, ok := kt.(string)
		if err != nil || !ok || seen[k] {
			return false
		}
		seen[k] = true
		var skip json.RawMessage
		if dec.Decode(&skip) != nil {
			return false
		}
	}
	return true
}

type jwsHeader struct {
	Alg  string          `json:"alg"`
	Kid  string          `json:"kid"`
	Crit json.RawMessage `json:"crit"`
}

// VerifyToken checks a signed token's signature against the issuer's keys and its claims against what this caller expects. It never trusts the token's own claim about which algorithm or key to use beyond a fixed allow-list.
func VerifyToken(raw string, keys *KeySet, want Expect) (Claims, error) {
	if len(raw) == 0 || len(raw) > maxTokenBytes || keys == nil {
		return Claims{}, ErrUntrusted
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return Claims{}, ErrUntrusted
	}
	hb, err1 := b64(parts[0])
	pb, err2 := b64(parts[1])
	sig, err3 := b64(parts[2])
	if err1 != nil || err2 != nil || err3 != nil || !noDuplicateKeys(hb) || !noDuplicateKeys(pb) {
		return Claims{}, ErrUntrusted
	}
	var h jwsHeader
	if json.Unmarshal(hb, &h) != nil || len(h.Crit) != 0 {
		return Claims{}, ErrUntrusted
	}
	pub, ok := keys.keys[h.Kid]
	if !ok {
		return Claims{}, ErrUntrusted
	}
	signed := []byte(parts[0] + "." + parts[1])
	if !verifySignature(h.Alg, pub, signed, sig) {
		return Claims{}, ErrUntrusted
	}
	var rc rawClaims
	if json.Unmarshal(pb, &rc) != nil {
		return Claims{}, ErrUntrusted
	}
	c := Claims{Issuer: rc.Iss, Subject: rc.Sub, Authorized: rc.Azp, Nonce: rc.Nonce}
	var okAud, okExp bool
	c.Audience, okAud = audiences(rc.Aud)
	c.Expires, okExp = unixOf(rc.Exp)
	if !okAud || !okExp {
		return Claims{}, ErrUntrusted
	}
	c.NotBefore, _ = unixOf(rc.Nbf)
	c.IssuedAt, _ = unixOf(rc.Iat)
	now := want.Now
	switch {
	case c.Issuer == "" || c.Issuer != want.Issuer:
		return Claims{}, ErrUntrusted
	case !slices.Contains(c.Audience, want.Audience) || (len(c.Audience) > 1 && c.Authorized != want.Audience):
		return Claims{}, ErrUntrusted
	case !now.Before(c.Expires.Add(clockSkew)):
		return Claims{}, ErrExpired
	case !c.NotBefore.IsZero() && now.Add(clockSkew).Before(c.NotBefore):
		return Claims{}, ErrUntrusted
	case !c.IssuedAt.IsZero() && now.Add(clockSkew).Before(c.IssuedAt):
		return Claims{}, ErrUntrusted
	case want.MaxAge > 0 && (c.IssuedAt.IsZero() || now.Sub(c.IssuedAt) > want.MaxAge+clockSkew):
		return Claims{}, ErrExpired
	case want.Nonce != "" && c.Nonce != want.Nonce:
		return Claims{}, ErrUntrusted
	}
	return c, nil
}

// verifySignature accepts a fixed set of algorithms and only with the key type that algorithm needs; "none" and shared-secret algorithms are never accepted.
func verifySignature(alg string, pub crypto.PublicKey, signed, sig []byte) bool {
	sum := sha256.Sum256(signed)
	switch alg {
	case "RS256":
		k, ok := pub.(*rsa.PublicKey)
		return ok && rsa.VerifyPKCS1v15(k, crypto.SHA256, sum[:], sig) == nil
	case "ES256":
		k, ok := pub.(*ecdsa.PublicKey)
		if !ok || len(sig) != 64 {
			return false
		}
		return ecdsa.Verify(k, sum[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:]))
	case "EdDSA":
		k, ok := pub.(ed25519.PublicKey)
		return ok && ed25519.Verify(k, signed, sig)
	}
	return false
}
