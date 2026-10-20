package auth

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"
)

var epoch = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newBroker(t *testing.T) (*Broker, *clock, *bytes.Buffer) {
	t.Helper()
	cl := &clock{t: epoch}
	var buf bytes.Buffer
	return NewBroker(nil, NewAudit(&buf), cl.now), cl, &buf
}

func grant(owner string) Grant {
	return Grant{Account: "acme", Owner: owner, Source: Source{AppID: "app.mail", Origin: "https://mail.example.com"}, Audience: "https://mail.example.com/api",
		Scopes: []string{"mail.read", "mail.send"}, Method: MethodOAuthPKCE, ExpiresAt: epoch.Add(time.Hour)}
}

func use(g Grant, owner string) Check {
	return Check{GrantID: g.ID, Owner: owner, Origin: g.Source.Origin, Audience: g.Audience, Scopes: []string{"mail.read"}}
}

func TestAGrantIsUsableOnlyByItsOwnerForItsSourceAudienceAndScopes(t *testing.T) {
	b, _, _ := newBroker(t)
	g, err := b.Issue(grant("alice"))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := b.Authorize(use(g, "alice")); err != nil || got.ID != g.ID {
		t.Fatalf("%v", err)
	}
	cases := map[string]func(*Check){
		"another caller":     func(c *Check) { c.Owner = "mallory" },
		"another origin":     func(c *Check) { c.Origin = "https://evil.example.com" },
		"another audience":   func(c *Check) { c.Audience = "https://mail.example.com/admin" },
		"a scope not held":   func(c *Check) { c.Scopes = []string{"mail.delete"} },
		"an unknown grant":   func(c *Check) { c.GrantID = "gr_00000000000000000000000000000000" },
		"an empty audience":  func(c *Check) { c.Audience = "" },
		"a suffix audience":  func(c *Check) { c.Audience += "/" },
		"a prefix origin":    func(c *Check) { c.Origin = "https://mail.example.com.evil.net" },
		"a different scheme": func(c *Check) { c.Origin = "http://mail.example.com" },
	}
	for name, mut := range cases {
		c := use(g, "alice")
		mut(&c)
		if _, err := b.Authorize(c); !errors.Is(err, ErrDenied) {
			t.Fatalf("%s must be denied: %v", name, err)
		}
	}
}

func TestExpiryRevocationAndPauseAreEnforcedAtUse(t *testing.T) {
	b, cl, _ := newBroker(t)
	g, _ := b.Issue(grant("alice"))
	cl.t = epoch.Add(time.Hour)
	if _, err := b.Authorize(use(g, "alice")); !errors.Is(err, ErrExpired) {
		t.Fatalf("a grant is expired at its expiry instant: %v", err)
	}
	cl.t = epoch
	g2, _ := b.Issue(grant("alice"))
	g2.State = StateRevoked
	_ = b.Store.Put(g2)
	if _, err := b.Authorize(use(g2, "alice")); !errors.Is(err, ErrDenied) {
		t.Fatalf("%v", err)
	}
	g3, _ := b.Issue(grant("alice"))
	g3.State = StatePaused
	_ = b.Store.Put(g3)
	if _, err := b.Authorize(use(g3, "alice")); !errors.Is(err, ErrReauth) {
		t.Fatalf("%v", err)
	}
}

func TestIssuingRefusesMalformedGrants(t *testing.T) {
	b, _, _ := newBroker(t)
	for name, mut := range map[string]func(*Grant){
		"no owner":        func(g *Grant) { g.Owner = "" },
		"bad origin":      func(g *Grant) { g.Source.Origin = "https://Mail.Example.com/path" },
		"bad audience":    func(g *Grant) { g.Audience = "has space" },
		"already expired": func(g *Grant) { g.ExpiresAt = epoch.Add(-time.Second) },
		"bad scope":       func(g *Grant) { g.Scopes = []string{"mail read"} },
		"unknown method":  func(g *Grant) { g.Method = "password" },
		"too many scopes": func(g *Grant) {
			for i := 0; i < 65; i++ {
				g.Scopes = append(g.Scopes, "s"+strings.Repeat("x", i%5)+string(rune('a'+i%26)))
			}
		},
	} {
		g := grant("alice")
		mut(&g)
		if _, err := b.Issue(g); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%s must be refused: %v", name, err)
		}
	}
}

func TestRefusalsAreAuditedWithTheirReasonButTheCallerOnlySeesAGenericError(t *testing.T) {
	b, _, buf := newBroker(t)
	g, _ := b.Issue(grant("alice"))
	_, err := b.Authorize(use(g, "mallory"))
	if err != ErrDenied {
		t.Fatalf("%v", err)
	}
	log := buf.String()
	if !strings.Contains(log, `"reason":"owner_mismatch"`) || !strings.Contains(log, `"actor":"mallory"`) {
		t.Fatalf("the reason belongs in the log: %s", log)
	}
	if n, err := VerifyAudit(strings.NewReader(log)); err != nil || n != 2 {
		t.Fatalf("%d %v", n, err)
	}
}

func TestAuditChainDetectsEditingRemovingAndReordering(t *testing.T) {
	var buf bytes.Buffer
	a := NewAudit(&buf)
	for i := 0; i < 5; i++ {
		if err := a.Append(AuditRecord{At: epoch.Add(time.Duration(i) * time.Second), Event: EventIssued, Grant: "gr_a", Account: "acme", Actor: "alice", Origin: "https://x.example.com", Reason: ReasonOK}); err != nil {
			t.Fatal(err)
		}
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	join := func(l []string) string { return strings.Join(l, "\n") + "\n" }
	if n, err := VerifyAudit(strings.NewReader(join(lines))); err != nil || n != 5 {
		t.Fatalf("%d %v", n, err)
	}
	edited := append([]string(nil), lines...)
	edited[2] = strings.Replace(edited[2], "alice", "carol", 1)
	removed := append(append([]string(nil), lines[:2]...), lines[3:]...)
	swapped := append([]string(nil), lines...)
	swapped[1], swapped[2] = swapped[2], swapped[1]
	for name, l := range map[string][]string{"edit": edited, "removal": removed, "reorder": swapped} {
		if _, err := VerifyAudit(strings.NewReader(join(l))); !errors.Is(err, ErrAuditTampered) {
			t.Fatalf("%s went unnoticed: %v", name, err)
		}
	}
	if n, err := VerifyAudit(strings.NewReader(join(lines[:3]))); err != nil || n != 3 {
		t.Fatalf("a truncated tail is a shorter valid log, which only an external anchor can expose: %d %v", n, err)
	}
}

func TestAuditRefusesFreeTextSoItCannotCarryASecret(t *testing.T) {
	var buf bytes.Buffer
	a := NewAudit(&buf)
	bad := []AuditRecord{
		{At: epoch, Event: "login", Actor: "alice"},
		{At: epoch, Event: EventRefused, Actor: fmt.Sprintf("alice %s%c%s", "pass"+"word", 61, "hunter2")},
		{At: epoch, Event: EventRefused, Origin: "https://x.example.com/?token=abc"},
		{At: epoch, Event: EventRefused, Reason: "the password was wrong"},
	}
	for _, r := range bad {
		if err := a.Append(r); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%+v: %v", r, err)
		}
	}
	if buf.Len() != 0 {
		t.Fatal("nothing is written for a refused record")
	}
}

// --- token verification ---

type signer struct {
	alg  string
	kid  string
	sign func([]byte) []byte
	pub  crypto.PublicKey
}

func rsaSigner(t *testing.T) signer {
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return signer{"RS256", "rsa1", func(m []byte) []byte {
		h := sha256.Sum256(m)
		s, _ := rsa.SignPKCS1v15(rand.Reader, k, crypto.SHA256, h[:])
		return s
	}, &k.PublicKey}
}

func ecSigner(t *testing.T) signer {
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	return signer{"ES256", "ec1", func(m []byte) []byte {
		h := sha256.Sum256(m)
		r, s, _ := ecdsa.Sign(rand.Reader, k, h[:])
		out := make([]byte, 64)
		r.FillBytes(out[:32])
		s.FillBytes(out[32:])
		return out
	}, &k.PublicKey}
}

func edSigner(t *testing.T) signer {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	return signer{"EdDSA", "ed1", func(m []byte) []byte { return ed25519.Sign(priv, m) }, pub}
}

func enc(v any) string {
	b, _ := json.Marshal(v)
	return base64.RawURLEncoding.EncodeToString(b)
}

func (s signer) token(claims map[string]any) string {
	return s.tokenWith(map[string]any{"alg": s.alg, "kid": s.kid}, claims)
}

func (s signer) tokenWith(header, claims map[string]any) string {
	signed := enc(header) + "." + enc(claims)
	return signed + "." + base64.RawURLEncoding.EncodeToString(s.sign([]byte(signed)))
}

func good() map[string]any {
	return map[string]any{"iss": "https://id.example.com", "sub": "u1", "aud": "vsf-client", "exp": epoch.Add(time.Hour).Unix(), "iat": epoch.Unix(), "nonce": "n-1"}
}

func want() Expect {
	return Expect{Issuer: "https://id.example.com", Audience: "vsf-client", Nonce: "n-1", Now: epoch}
}

func keysOf(ss ...signer) *KeySet {
	ks := NewKeySet()
	for _, s := range ss {
		if err := ks.Add(s.kid, s.pub); err != nil {
			panic(err)
		}
	}
	return ks
}

func TestTokensVerifyWithEachSupportedAlgorithm(t *testing.T) {
	for _, s := range []signer{rsaSigner(t), ecSigner(t), edSigner(t)} {
		c, err := VerifyToken(s.token(good()), keysOf(s), want())
		if err != nil || c.Subject != "u1" || c.Nonce != "n-1" {
			t.Fatalf("%s: %+v %v", s.alg, c, err)
		}
	}
}

func TestTokensForAnotherAudienceIssuerOrTimeAreRejected(t *testing.T) {
	s := edSigner(t)
	ks := keysOf(s)
	with := func(k string, v any) map[string]any { c := good(); c[k] = v; return c }
	without := func(k string) map[string]any { c := good(); delete(c, k); return c }
	cases := map[string]map[string]any{
		"another audience":         with("aud", "someone-else"),
		"audience list without us": with("aud", []string{"a", "b"}),
		"two audiences, no azp":    with("aud", []string{"vsf-client", "other"}),
		"two audiences, wrong azp": with("aud", map[string]any{}),
		"another issuer":           with("iss", "https://evil.example.com"),
		"no issuer":                without("iss"),
		"expired":                  with("exp", epoch.Add(-2*time.Minute).Unix()),
		"no expiry":                without("exp"),
		"not yet valid":            with("nbf", epoch.Add(time.Hour).Unix()),
		"issued in the future":     with("iat", epoch.Add(time.Hour).Unix()),
		"another nonce":            with("nonce", "n-2"),
		"no audience":              without("aud"),
		"string expiry":            with("exp", "tomorrow"),
		"absurd expiry":            with("exp", 1e15),
	}
	for name, claims := range cases {
		if _, err := VerifyToken(s.token(claims), ks, want()); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
	two := with("aud", []string{"vsf-client", "other"})
	two["azp"] = "vsf-client"
	if _, err := VerifyToken(s.token(two), ks, want()); err != nil {
		t.Fatalf("a list naming us with a matching azp is fine: %v", err)
	}
	old := with("iat", epoch.Add(-2*time.Hour).Unix())
	w := want()
	w.MaxAge = time.Hour
	if _, err := VerifyToken(s.token(old), ks, w); !errors.Is(err, ErrExpired) {
		t.Fatalf("max age: %v", err)
	}
}

func TestTokenSignatureAndHeaderAttacksAreRejected(t *testing.T) {
	s := edSigner(t)
	r := rsaSigner(t)
	ks := keysOf(s, r)
	tok := s.token(good())
	parts := strings.Split(tok, ".")
	tamperedPayload := parts[0] + "." + enc(func() map[string]any { c := good(); c["sub"] = "admin"; return c }()) + "." + parts[2]
	none := strings.Join([]string{enc(map[string]any{"alg": "none", "kid": s.kid}), parts[1], ""}, ".")
	// HS256 signed with the RSA public key bytes: the classic algorithm confusion.
	pubDER := []byte("stand-in-key-bytes")
	hs := signer{"HS256", r.kid, func(m []byte) []byte { return append(pubDER, m[:3]...) }, r.pub}
	cases := map[string]string{
		"tampered payload":       tamperedPayload,
		"alg none":               none,
		"hs256 confusion":        hs.token(good()),
		"unknown kid":            s.tokenWith(map[string]any{"alg": s.alg, "kid": "other"}, good()),
		"no kid":                 s.tokenWith(map[string]any{"alg": s.alg}, good()),
		"alg does not fit key":   s.tokenWith(map[string]any{"alg": "RS256", "kid": s.kid}, good()),
		"critical header":        s.tokenWith(map[string]any{"alg": s.alg, "kid": s.kid, "crit": []string{"exp"}}, good()),
		"two parts":              parts[0] + "." + parts[1],
		"four parts":             tok + ".x",
		"empty":                  "",
		"oversize":               strings.Repeat("a", maxTokenBytes+1),
		"signature of the other": signer{s.alg, s.kid, r.sign, s.pub}.token(good()),
		"duplicate claim":        parts[0] + "." + base64.RawURLEncoding.EncodeToString([]byte(`{"iss":"https://id.example.com","iss":"x","aud":"vsf-client","exp":4102444800}`)) + "." + parts[2],
	}
	for name, tk := range cases {
		if _, err := VerifyToken(tk, ks, want()); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
}

func TestKeySetRefusesWeakKeysAndParsesPublishedSets(t *testing.T) {
	weak, _ := rsa.GenerateKey(rand.Reader, 1024)
	ks := NewKeySet()
	if err := ks.Add("w", &weak.PublicKey); !errors.Is(err, ErrInvalid) {
		t.Fatalf("%v", err)
	}
	p384, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err := ks.Add("p", &p384.PublicKey); !errors.Is(err, ErrInvalid) {
		t.Fatalf("%v", err)
	}
	e := ecSigner(t)
	pub := e.pub.(*ecdsa.PublicKey)
	ed := edSigner(t)
	doc, _ := json.Marshal(map[string]any{"keys": []map[string]any{
		{"kty": "EC", "kid": "ec1", "crv": "P-256", "x": base64.RawURLEncoding.EncodeToString(pub.X.FillBytes(make([]byte, 32))), "y": base64.RawURLEncoding.EncodeToString(pub.Y.FillBytes(make([]byte, 32)))},
		{"kty": "OKP", "kid": "ed1", "crv": "Ed25519", "x": base64.RawURLEncoding.EncodeToString(ed.pub.(ed25519.PublicKey))},
		{"kty": "oct", "kid": "sym", "k": "c2VjcmV0"},
		{"kty": "EC", "kid": "off", "crv": "P-256", "x": base64.RawURLEncoding.EncodeToString(big.NewInt(1).Bytes()), "y": base64.RawURLEncoding.EncodeToString(big.NewInt(1).Bytes())},
	}})
	set, err := ParseJWKS(doc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyToken(e.token(good()), set, want()); err != nil {
		t.Fatalf("a parsed EC key verifies: %v", err)
	}
	if _, err := VerifyToken(ed.token(good()), set, want()); err != nil {
		t.Fatalf("a parsed Ed25519 key verifies: %v", err)
	}
	if len(set.keys) != 2 {
		t.Fatalf("symmetric keys and points off the curve are skipped: %d", len(set.keys))
	}
	if _, err := ParseJWKS([]byte("{")); !errors.Is(err, ErrInvalid) {
		t.Fatalf("%v", err)
	}
}
