package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

type codeInfo struct{ challenge, nonce, redirect, client string }

type idp struct {
	t       *testing.T
	srv     *httptest.Server
	sign    signer
	client  string
	mu      sync.Mutex
	codes   map[string]codeInfo
	n       int
	refresh map[string]bool
	// per-test misbehaviour
	claims        func(map[string]any)
	reply         func(map[string]any)
	status        int
	rawBody       string
	redirect      bool
	tokenSeq      int
	rejectRefresh bool
	now           func() time.Time
}

func newIdP(t *testing.T, cl *clock) *idp {
	i := &idp{t: t, sign: edSigner(t), client: "vsf-client", codes: map[string]codeInfo{}, refresh: map[string]bool{}, now: cl.now}
	i.srv = httptest.NewServer(http.HandlerFunc(i.token))
	t.Cleanup(i.srv.Close)
	return i
}

func (i *idp) provider() *Provider {
	return &Provider{Issuer: "https://id.example.com", AuthorizeURL: i.srv.URL + "/authorize", TokenURL: i.srv.URL + "/token", ClientID: i.client,
		RedirectURI: "http://127.0.0.1:9999/callback", Scopes: []string{"openid", "mail.read"}, Keys: keysOf(i.sign), Audience: "https://mail.example.com/api",
		Source: Source{AppID: "app.mail", Origin: "https://mail.example.com"}, HTTP: i.srv.Client()}
}

// authorize plays the person approving the request at the authorization server.
func (i *idp) authorize(authURL string) (state, code string) {
	u, err := url.Parse(authURL)
	if err != nil {
		i.t.Fatal(err)
	}
	q := u.Query()
	if q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" || q.Get("state") == "" || q.Get("nonce") == "" {
		i.t.Fatalf("authorization request is not a PKCE code request: %v", q)
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	i.n++
	code = "code-" + string(rune('a'+i.n))
	i.codes[code] = codeInfo{q.Get("code_challenge"), q.Get("nonce"), q.Get("redirect_uri"), q.Get("client_id")}
	return q.Get("state"), code
}

func (i *idp) token(w http.ResponseWriter, r *http.Request) {
	if i.redirect {
		http.Redirect(w, r, "http://127.0.0.1:1/steal", http.StatusFound)
		return
	}
	_ = r.ParseForm()
	if i.rawBody != "" {
		_, _ = w.Write([]byte(i.rawBody))
		return
	}
	if i.status != 0 {
		w.WriteHeader(i.status)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
		return
	}
	reply := map[string]any{"token_type": "Bearer", "expires_in": 3600}
	i.mu.Lock()
	i.tokenSeq++
	seq := i.tokenSeq
	i.mu.Unlock()
	reply["access_token"] = "at-" + string(rune('0'+seq))
	switch r.Form.Get("grant_type") {
	case "authorization_code":
		i.mu.Lock()
		ci, ok := i.codes[r.Form.Get("code")]
		delete(i.codes, r.Form.Get("code"))
		i.mu.Unlock()
		sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		if !ok || ci.redirect != r.Form.Get("redirect_uri") || ci.client != r.Form.Get("client_id") || ci.challenge != base64.RawURLEncoding.EncodeToString(sum[:]) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
			return
		}
		c := good()
		c["iss"], c["aud"], c["nonce"] = "https://id.example.com", i.client, ci.nonce
		c["iat"], c["exp"] = i.now().Unix(), i.now().Add(time.Hour).Unix()
		if i.claims != nil {
			i.claims(c)
		}
		reply["id_token"] = i.sign.token(c)
		reply["refresh_token"] = "rt-1"
		reply["scope"] = "openid mail.read"
	case "refresh_token":
		if i.rejectRefresh || r.Form.Get("refresh_token") != "rt-1" && r.Form.Get("refresh_token") != "rt-2" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
			return
		}
		reply["refresh_token"] = "rt-2"
	}
	if i.reply != nil {
		i.reply(reply)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(reply)
}

type rig struct {
	b     *Broker
	cl    *clock
	vault *MemVault
	o     *OAuth
	idp   *idp
	buf   *strings.Builder
}

func newRig(t *testing.T) *rig {
	b, cl, _ := newBroker(t)
	v := NewMemVault()
	b.Vault = v
	return &rig{b: b, cl: cl, vault: v, o: NewOAuth(b), idp: newIdP(t, cl)}
}

func (r *rig) run(t *testing.T, owner string) (Grant, error) {
	t.Helper()
	u, err := r.o.Begin(owner, "acme", r.idp.provider())
	if err != nil {
		t.Fatal(err)
	}
	state, code := r.idp.authorize(u)
	return r.o.Complete(owner, state, code, "")
}

func TestAnAuthorizationCodeFlowWithPKCEYieldsAGrantAndVaultedSecrets(t *testing.T) {
	r := newRig(t)
	g, err := r.run(t, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if g.Owner != "alice" || g.Method != MethodOAuthPKCE || g.Audience != "https://mail.example.com/api" || g.Source.Origin != "https://mail.example.com" ||
		len(g.Scopes) != 1 || g.Scopes[0] != "mail.read" || !g.ExpiresAt.Equal(epoch.Add(time.Hour)) || g.Epoch != 1 {
		t.Fatalf("%+v", g)
	}
	if !strings.HasPrefix(g.PrincipalRef, "pr_") || strings.Contains(g.PrincipalRef, "u1") {
		t.Fatalf("the source principal is an opaque reference: %s", g.PrincipalRef)
	}
	at, _ := r.vault.Get("acme", g.ID+"/access")
	rt, _ := r.vault.Get("acme", g.ID+"/refresh")
	if string(at) != "at-1" || string(rt) != "rt-1" {
		t.Fatalf("%q %q", at, rt)
	}
	enc, _ := json.Marshal(g)
	if strings.Contains(string(enc), "at-1") || strings.Contains(string(enc), "rt-1") || strings.Contains(string(enc), "token") {
		t.Fatalf("a grant carries no token: %s", enc)
	}
	if _, err := r.b.Authorize(Check{GrantID: g.ID, Owner: "alice", Origin: g.Source.Origin, Audience: g.Audience, Scopes: []string{"mail.read"}}); err != nil {
		t.Fatal(err)
	}
}

func TestTheFlowRefusesAnythingButPKCEAndSecureEndpoints(t *testing.T) {
	r := newRig(t)
	u, err := r.o.Begin("alice", "acme", r.idp.provider())
	if err != nil {
		t.Fatal(err)
	}
	q, _ := url.Parse(u)
	if q.Query().Get("code_challenge_method") != "S256" || len(q.Query().Get("code_challenge")) != 43 {
		t.Fatalf("%s", u)
	}
	if strings.Contains(u, "code_verifier") {
		t.Fatal("the verifier never leaves the process")
	}
	for name, mut := range map[string]func(*Provider){
		"plain http token endpoint": func(p *Provider) { p.TokenURL = "http://idp.example.com/token" },
		"credentials in url":        func(p *Provider) { p.AuthorizeURL = "https://" + "user" + string(rune(58)) + "pw" + "@idp.example.com/authorize" },
		"no audience":               func(p *Provider) { p.Audience = "" },
		"no scopes":                 func(p *Provider) { p.Scopes = nil },
		"bad redirect":              func(p *Provider) { p.RedirectURI = "javascript:alert(1)" },
		"fragment":                  func(p *Provider) { p.AuthorizeURL = "https://idp.example.com/authorize#x" },
	} {
		p := r.idp.provider()
		mut(p)
		if _, err := r.o.Begin("alice", "acme", p); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestStateIsBoundSingleUseAndExpiring(t *testing.T) {
	r := newRig(t)
	u, _ := r.o.Begin("alice", "acme", r.idp.provider())
	state, code := r.idp.authorize(u)
	if _, err := r.o.Complete("mallory", state, code, ""); !errors.Is(err, ErrDenied) {
		t.Fatalf("another caller cannot finish alice's flow: %v", err)
	}
	if _, err := r.o.Complete("alice", state, code, ""); !errors.Is(err, ErrDenied) {
		t.Fatalf("a refused attempt spends the state: %v", err)
	}
	u2, _ := r.o.Begin("alice", "acme", r.idp.provider())
	s2, c2 := r.idp.authorize(u2)
	if _, err := r.o.Complete("alice", s2, c2, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := r.o.Complete("alice", s2, c2, ""); !errors.Is(err, ErrDenied) {
		t.Fatalf("a completed flow cannot be replayed: %v", err)
	}
	if _, err := r.o.Complete("alice", "forged", "x", ""); !errors.Is(err, ErrDenied) {
		t.Fatalf("%v", err)
	}
	u3, _ := r.o.Begin("alice", "acme", r.idp.provider())
	s3, c3 := r.idp.authorize(u3)
	r.cl.t = r.cl.t.Add(pendingTTL)
	if _, err := r.o.Complete("alice", s3, c3, ""); !errors.Is(err, ErrDenied) {
		t.Fatalf("a flow older than ten minutes is refused: %v", err)
	}
}

func TestACodeStolenWithoutTheVerifierIsUseless(t *testing.T) {
	r := newRig(t)
	u, _ := r.o.Begin("alice", "acme", r.idp.provider())
	state, _ := r.idp.authorize(u)
	// The attacker obtains a code the server issued against their own challenge.
	other := strings.Replace(u, "code_challenge=", "code_challenge=x", 1)
	_, stolen := r.idp.authorize(other)
	if _, err := r.o.Complete("alice", state, stolen, ""); !errors.Is(err, ErrUntrusted) {
		t.Fatalf("the server refuses a code whose challenge does not match the verifier: %v", err)
	}
	if got := r.b.List("alice"); len(got) != 0 {
		t.Fatalf("no grant results: %v", got)
	}
}

func TestMixUpAndIssuerChecks(t *testing.T) {
	r := newRig(t)
	p := r.idp.provider()
	for _, c := range []struct {
		name     string
		issParam bool
		iss      string
		ok       bool
	}{
		{"matching iss", true, "https://id.example.com", true},
		{"missing iss when the server promises one", true, "", false},
		{"another server's iss", true, "https://evil.example.com", false},
		{"another iss when not promised", false, "https://evil.example.com", false},
		{"no iss when not promised", false, "", true},
	} {
		p.IssParam = c.issParam
		u, _ := r.o.Begin("alice", "acme", p)
		state, code := r.idp.authorize(u)
		_, err := r.o.Complete("alice", state, code, c.iss)
		if (err == nil) != c.ok {
			t.Fatalf("%s: %v", c.name, err)
		}
	}
}

func TestAHostileOrBuggyServerCannotMintAGrant(t *testing.T) {
	cases := map[string]func(*idp){
		"id token for another client":      func(i *idp) { i.claims = func(c map[string]any) { c["aud"] = "someone-else" } },
		"id token from another issuer":     func(i *idp) { i.claims = func(c map[string]any) { c["iss"] = "https://evil.example.com" } },
		"replayed nonce":                   func(i *idp) { i.claims = func(c map[string]any) { c["nonce"] = "old" } },
		"expired id token":                 func(i *idp) { i.claims = func(c map[string]any) { c["exp"] = epoch.Add(-time.Hour).Unix() } },
		"no id token":                      func(i *idp) { i.reply = func(m map[string]any) { delete(m, "id_token") } },
		"mac token type":                   func(i *idp) { i.reply = func(m map[string]any) { m["token_type"] = "mac" } },
		"no access token":                  func(i *idp) { i.reply = func(m map[string]any) { m["access_token"] = "" } },
		"zero lifetime":                    func(i *idp) { i.reply = func(m map[string]any) { m["expires_in"] = 0 } },
		"negative lifetime":                func(i *idp) { i.reply = func(m map[string]any) { m["expires_in"] = -5 } },
		"endless lifetime":                 func(i *idp) { i.reply = func(m map[string]any) { m["expires_in"] = 1 << 40 } },
		"widened scope":                    func(i *idp) { i.reply = func(m map[string]any) { m["scope"] = "openid mail.read mail.admin" } },
		"redirect from the token endpoint": func(i *idp) { i.redirect = true },
		"error status":                     func(i *idp) { i.status = http.StatusInternalServerError },
		"duplicate members": func(i *idp) {
			i.rawBody = `{"access_token":"a","access_token":"b","token_type":"Bearer","expires_in":60}`
		},
		"oversize body": func(i *idp) { i.rawBody = `{"access_token":"` + strings.Repeat("a", maxTokenReply) + `"}` },
		"not json":      func(i *idp) { i.rawBody = `<html>` },
	}
	for name, mut := range cases {
		r := newRig(t)
		mut(r.idp)
		if g, err := r.run(t, "alice"); err == nil {
			t.Fatalf("%s produced a grant: %+v", name, g)
		}
		if len(r.b.List("alice")) != 0 {
			t.Fatalf("%s left a grant behind", name)
		}
		if len(r.vault.m) != 0 {
			t.Fatalf("%s left secrets behind", name)
		}
	}
}

func TestRefreshRenewsAGrantAndAServerRefusalEndsIt(t *testing.T) {
	r := newRig(t)
	g, _ := r.run(t, "alice")
	p := r.idp.provider()
	r.cl.t = r.cl.t.Add(50 * time.Minute)
	g2, err := r.o.Refresh("alice", g.ID, p)
	if err != nil || !g2.ExpiresAt.Equal(r.cl.t.Add(time.Hour)) {
		t.Fatalf("%+v %v", g2, err)
	}
	if at, _ := r.vault.Get("acme", g.ID+"/access"); string(at) == "at-1" {
		t.Fatal("the access token was replaced")
	}
	if rt, _ := r.vault.Get("acme", g.ID+"/refresh"); string(rt) != "rt-2" {
		t.Fatalf("a rotated refresh token is kept: %q", rt)
	}
	if _, err := r.o.Refresh("mallory", g.ID, p); !errors.Is(err, ErrDenied) {
		t.Fatalf("%v", err)
	}
	wrong := *p
	wrong.Audience = "https://other.example.com"
	if _, err := r.o.Refresh("alice", g.ID, &wrong); !errors.Is(err, ErrDenied) {
		t.Fatalf("a grant is refreshed only for its own audience: %v", err)
	}
	r.idp.srv.Close()
	if _, err := r.o.Refresh("alice", g.ID, p); err == nil {
		t.Fatal("an unreachable server cannot renew")
	}
	if cur, _ := r.b.Store.Get(g.ID); cur.State != StateActive {
		t.Fatal("a network failure does not end the grant")
	}
	r2 := newRig(t)
	g3, _ := r2.run(t, "alice")
	r2.idp.rejectRefresh = true
	if _, err := r2.o.Refresh("alice", g3.ID, r2.idp.provider()); !errors.Is(err, ErrReauth) {
		t.Fatalf("%v", err)
	}
	if cur, _ := r2.b.Store.Get(g3.ID); cur.State != StateRevoked {
		t.Fatal("a refused refresh token ends the grant")
	}
	if _, err := r2.vault.Get("acme", g3.ID+"/access"); !errors.Is(err, ErrNotFound) {
		t.Fatal("and its secrets are wiped")
	}
}

func browserRig(t *testing.T) (*Browser, *Broker, *clock) {
	b, cl, _ := newBroker(t)
	b.Vault = NewMemVault()
	return NewBrowser(b), b, cl
}

var mail = Source{AppID: "app.mail", Origin: "https://mail.example.com"}

func TestSourceManagedSignInRecordsAGrantWithNoSecret(t *testing.T) {
	br, b, _ := browserRig(t)
	if err := br.Expect("alice", "acme", mail, []string{"mail.read", "mail.send"}); err != nil {
		t.Fatal(err)
	}
	g, err := br.Observe(Observation{Owner: "alice", Source: mail, PrincipalRef: "pr_abc", Authenticated: true, Scopes: []string{"mail.read"}})
	if err != nil {
		t.Fatal(err)
	}
	if g.Method != MethodSourceBrowser || g.Audience != mail.Origin || len(g.Scopes) != 1 || g.Scopes[0] != "mail.read" || !br.Authenticated("alice", mail) {
		t.Fatalf("%+v", g)
	}
	if !g.ExpiresAt.Equal(epoch.Add(maxSourceLife)) {
		t.Fatalf("%v", g.ExpiresAt)
	}
	short := epoch.Add(time.Hour)
	g2, err := br.Observe(Observation{Owner: "alice", Source: mail, PrincipalRef: "pr_abc", Authenticated: true, Expires: short, Scopes: []string{"mail.read"}})
	if err != nil || g2.ID != g.ID || !g2.ExpiresAt.Equal(short) {
		t.Fatalf("a repeat observation of the same person renews the same grant to the source's own expiry: %+v %v", g2, err)
	}
	if len(b.List("alice")) != 1 {
		t.Fatal("no duplicate grants")
	}
}

func TestAnUnexpectedSignInOrAnotherCallersObservationChangesNothing(t *testing.T) {
	br, b, _ := browserRig(t)
	if _, err := br.Observe(Observation{Owner: "alice", Source: mail, PrincipalRef: "pr_abc", Authenticated: true}); !errors.Is(err, ErrDenied) {
		t.Fatalf("a sign-in nobody asked for creates no grant: %v", err)
	}
	_ = br.Expect("alice", "acme", mail, []string{"mail.read"})
	if _, err := br.Observe(Observation{Owner: "bob", Source: mail, PrincipalRef: "pr_abc", Authenticated: true}); !errors.Is(err, ErrDenied) {
		t.Fatalf("%v", err)
	}
	if br.Authenticated("alice", mail) || len(b.List("alice")) != 0 || len(b.List("bob")) != 0 {
		t.Fatal("nothing was recorded")
	}
	if _, err := br.Observe(Observation{Owner: "alice", Source: mail, Authenticated: true}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("an authenticated observation names a principal: %v", err)
	}
}

func TestSigningOutOrSwitchingPersonEndsTheEarlierGrant(t *testing.T) {
	br, b, _ := browserRig(t)
	_ = br.Expect("alice", "acme", mail, []string{"mail.read"})
	g, _ := br.Observe(Observation{Owner: "alice", Source: mail, PrincipalRef: "pr_one", Authenticated: true, Scopes: []string{"mail.read"}})
	cancelled := 0
	b.Track(g.ID, "run-1", func() { cancelled++ })
	_ = br.Expect("alice", "acme", mail, []string{"mail.read"})
	g2, err := br.Observe(Observation{Owner: "alice", Source: mail, PrincipalRef: "pr_two", Authenticated: true, Scopes: []string{"mail.read"}})
	if err != nil || g2.ID == g.ID {
		t.Fatalf("%+v %v", g2, err)
	}
	if old, _ := b.Store.Get(g.ID); old.State != StateRevoked || cancelled != 1 {
		t.Fatalf("a different person on the same page ends the earlier sign-in and what depended on it: %v %d", old.State, cancelled)
	}
	if _, err := br.Observe(Observation{Owner: "alice", Source: mail, Authenticated: false}); err != nil {
		t.Fatal(err)
	}
	if cur, _ := b.Store.Get(g2.ID); cur.State != StateRevoked || br.Authenticated("alice", mail) {
		t.Fatal("signing out ends the grant")
	}
}

func TestAnOAuthGrantDoesNotMakeTheSourceSessionAuthenticated(t *testing.T) {
	r := newRig(t)
	br := NewBrowser(r.b)
	if _, err := r.run(t, "alice"); err != nil {
		t.Fatal(err)
	}
	if br.Authenticated("alice", r.idp.provider().Source) {
		t.Fatal("authorizing an API does not sign the person in to the application's own pages")
	}
}
