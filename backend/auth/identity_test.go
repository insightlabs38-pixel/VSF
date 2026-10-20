package auth

import (
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestAnIdentityTokenWithoutAUsableSubjectMintsNoGrant(t *testing.T) {
	for name, claims := range map[string]func(map[string]any){
		"empty subject":   func(c map[string]any) { c["sub"] = "" },
		"missing subject": func(c map[string]any) { delete(c, "sub") },
		"huge subject":    func(c map[string]any) { c["sub"] = string(make([]byte, 300)) },
		"numeric subject": func(c map[string]any) { c["sub"] = 7 },
	} {
		r := newRig(t)
		r.idp.claims = claims
		if g, err := r.run(t, "alice"); err == nil {
			t.Fatalf("%s: %+v", name, g)
		}
		if len(r.b.List("alice")) != 0 || len(r.vault.m) != 0 {
			t.Fatalf("%s left state behind", name)
		}
	}
}

func nonOIDC(r *rig) *Provider {
	r.idp.reply = func(m map[string]any) { delete(m, "id_token"); m["scope"] = "mail.read" }
	p := r.idp.provider()
	p.Scopes = []string{"mail.read"}
	return p
}

func TestANonOIDCGrantIsExplicitlyIdentityUnknownAndSatisfiesNothingThatNeedsAPerson(t *testing.T) {
	r := newRig(t)
	p := nonOIDC(r)
	u, err := r.o.Begin("alice", "acme", p)
	if err != nil {
		t.Fatal(err)
	}
	state, code := r.idp.authorize(u)
	g, err := r.o.Complete("alice", state, code, "")
	if err != nil {
		t.Fatalf("a standards-compliant OAuth server need not issue an identity token: %v", err)
	}
	if g.IdentityVerified || g.PrincipalRef != "" {
		t.Fatalf("no identity is invented from nothing: %+v", g)
	}
	chk := Check{GrantID: g.ID, Owner: "alice", Origin: g.Source.Origin, Audience: g.Audience, Scopes: []string{"mail.read"}}
	if _, err := r.b.Authorize(chk); err != nil {
		t.Fatalf("a bearer-only use still works: %v", err)
	}
	chk.VerifiedIdentity = true
	if _, err := r.b.Authorize(chk); !errors.Is(err, ErrDenied) {
		t.Fatalf("an operation that needs a proven person refuses it: %v", err)
	}
	if g2, err := r.o.Refresh("alice", g.ID, p); err != nil || g2.IdentityVerified {
		t.Fatalf("refresh does not upgrade it: %+v %v", g2, err)
	}
	bad := g
	bad.PrincipalRef = "pr_made_up"
	if err := bad.validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a principal without verification is not a valid grant: %v", err)
	}
	bad = g
	bad.IdentityVerified = true
	if err := bad.validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("nor is a verified one without a principal: %v", err)
	}
}

func TestRefreshCannotSubstituteAProviderOrWidenWhatWasGranted(t *testing.T) {
	r := newRig(t)
	g, err := r.run(t, "alice")
	if err != nil {
		t.Fatal(err)
	}
	for name, mut := range map[string]func(*Provider){
		"token endpoint": func(p *Provider) { p.TokenURL = "http://127.0.0.1:1/token" },
		"client":         func(p *Provider) { p.ClientID = "another-client" },
		"redirect":       func(p *Provider) { p.RedirectURI = "http://127.0.0.1:9998/callback" },
		"issuer":         func(p *Provider) { p.Issuer = "https://id.evil.example.com" },
		"audience":       func(p *Provider) { p.Audience = "https://mail.example.com/admin" },
		"source":         func(p *Provider) { p.Source.Origin = "https://evil.example.com" },
		"scopes":         func(p *Provider) { p.Scopes = []string{"openid", "mail.read", "mail.admin"} },
	} {
		p := r.idp.provider()
		mut(p)
		if _, err := r.o.Refresh("alice", g.ID, p); !errors.Is(err, ErrDenied) {
			t.Fatalf("a different %s is a different provider: %v", name, err)
		}
	}
	if r.idp.refreshCalls != 0 {
		t.Fatal("nothing was sent to a substituted provider")
	}
	r.idp.reply = func(m map[string]any) { m["scope"] = "mail.read mail.admin" }
	if _, err := r.o.Refresh("alice", g.ID, r.idp.provider()); !errors.Is(err, ErrUntrusted) {
		t.Fatalf("refreshed scopes cannot exceed the grant: %v", err)
	}
	cur, _ := r.b.Store.Get(g.ID)
	if len(cur.Scopes) != 1 || cur.State != StateActive || cur.SecretGen != g.SecretGen {
		t.Fatalf("and the refused reply changed nothing: %+v", cur)
	}
	if rt, _ := r.vault.Get("acme", g.ID+"/refresh"); string(rt) != "rt-1" {
		t.Fatalf("or the stored credential: %q", rt)
	}
}

func TestARefreshThatNamesAnotherPersonEndsTheGrant(t *testing.T) {
	r := newRig(t)
	g, _ := r.run(t, "alice")
	cancelled := 0
	r.b.Track(g.ID, "run", func() { cancelled++ })
	c := good()
	c["sub"], c["aud"] = "u2", r.idp.client
	c["iat"], c["exp"] = epoch.Unix(), epoch.Add(time.Hour).Unix()
	delete(c, "nonce")
	r.idp.reply = func(m map[string]any) { m["id_token"] = r.idp.sign.token(c) }
	if _, err := r.o.Refresh("alice", g.ID, r.idp.provider()); !errors.Is(err, ErrDenied) {
		t.Fatalf("%v", err)
	}
	if cur, _ := r.b.Store.Get(g.ID); cur.State != StateRevoked || cancelled != 1 {
		t.Fatal("the earlier person's grant must not continue as someone else")
	}
	r2 := newRig(t)
	g2, _ := r2.run(t, "alice")
	c["sub"], c["aud"] = "u1", "someone-else"
	r2.idp.reply = func(m map[string]any) { m["id_token"] = r2.idp.sign.token(c) }
	if _, err := r2.o.Refresh("alice", g2.ID, r2.idp.provider()); !errors.Is(err, ErrUntrusted) {
		t.Fatalf("an identity token for another client is not accepted on refresh either: %v", err)
	}
}

func TestStateIsSingleUseAndBoundToItsCallerAndIssuer(t *testing.T) {
	r := newRig(t)
	u, _ := r.o.Begin("alice", "acme", r.idp.provider())
	state, code := r.idp.authorize(u)
	if _, err := r.o.Complete("mallory", state, code, ""); !errors.Is(err, ErrDenied) {
		t.Fatalf("a state belongs to the caller who began it: %v", err)
	}
	if _, err := r.o.Complete("alice", state, code, ""); !errors.Is(err, ErrDenied) {
		t.Fatalf("and is spent by any attempt: %v", err)
	}
	p := r.idp.provider()
	p.IssParam = true
	u, _ = r.o.Begin("alice", "acme", p)
	state, code = r.idp.authorize(u)
	if _, err := r.o.Complete("alice", state, code, "https://id.evil.example.com"); !errors.Is(err, ErrDenied) {
		t.Fatalf("a mixed-up issuer is refused: %v", err)
	}
	u, _ = r.o.Begin("alice", "acme", p)
	state, code = r.idp.authorize(u)
	if _, err := r.o.Complete("alice", state, code, ""); !errors.Is(err, ErrDenied) {
		t.Fatalf("a server that promised to return its issuer must: %v", err)
	}
}

func TestAFlowKeepsTheProviderItBeganWith(t *testing.T) {
	r := newRig(t)
	p := r.idp.provider()
	u, _ := r.o.Begin("alice", "acme", p)
	state, code := r.idp.authorize(u)
	p.TokenURL, p.Audience = "http://127.0.0.1:1/evil", "https://evil.example.com"
	g, err := r.o.Complete("alice", state, code, "")
	if err != nil || g.Audience != "https://mail.example.com/api" {
		t.Fatalf("later changes to the caller's struct do not redirect the flow: %+v %v", g, err)
	}
}

func TestProviderKeysAreValidatedAndRotateWithoutChangingTheProvider(t *testing.T) {
	a, b := edSigner(t), edSigner(t)
	b.kid = "ed2"
	ks := keysOf(a)
	if err := ks.Add("ed1", a.pub); err != nil {
		t.Fatalf("re-adding the same key is harmless: %v", err)
	}
	if err := ks.Add("ed1", b.pub); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a key id cannot be silently rebound to a different key: %v", err)
	}
	for i := 0; i < maxKeys+2; i++ {
		_ = ks.Add("k"+string(rune('a'+i%26))+string(rune('a'+i/26)), b.pub)
	}
	if len(ks.keys) > maxKeys {
		t.Fatalf("key sets are bounded: %d", len(ks.keys))
	}
	if _, err := ParseJWKS([]byte(`{"keys":[{"kty":"OKP","crv":"Ed25519","kid":"x","x":"11qYAYKxCrfVS_7TyWQHOg7hcvPapiMlrwIaaPcHURo"},{"kty":"OKP","crv":"Ed25519","kid":"x","x":"11qYAYKxCrfVS_7TyWQHOg7hcvPapiMlrwIaaPcHURo"}]}`)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("duplicate key ids in a published set are ambiguous: %v", err)
	}
	r := newRig(t)
	r.idp.sign = b
	p := r.idp.provider()
	p.Keys = keysOf(a)
	u, _ := r.o.Begin("alice", "acme", p)
	state, code := r.idp.authorize(u)
	if _, err := r.o.Complete("alice", state, code, ""); !errors.Is(err, ErrUntrusted) {
		t.Fatalf("a token signed by a key the provider has not published is refused: %v", err)
	}
	p.Keys = keysOf(a, b)
	u, _ = r.o.Begin("alice", "acme", p)
	state, code = r.idp.authorize(u)
	g, err := r.o.Complete("alice", state, code, "")
	if err != nil {
		t.Fatalf("after rotation the new key verifies: %v", err)
	}
	rotated := r.idp.provider()
	rotated.Keys = keysOf(b)
	if _, err := r.o.Refresh("alice", g.ID, rotated); err != nil {
		t.Fatalf("key rotation does not change the provider's identity: %v", err)
	}
}

func TestARefusedRefreshTokenEndsTheGrantAndAServerErrorDoesNot(t *testing.T) {
	r := newRig(t)
	g, _ := r.run(t, "alice")
	r.idp.status = http.StatusInternalServerError
	if _, err := r.o.Refresh("alice", g.ID, r.idp.provider()); err == nil || errors.Is(err, ErrReauth) {
		t.Fatalf("a server error is not a verdict on the credential: %v", err)
	}
	if cur, _ := r.b.Store.Get(g.ID); cur.State != StateActive {
		t.Fatal("still active")
	}
}
