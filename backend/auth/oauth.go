package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	pendingTTL    = 10 * time.Minute
	maxPending    = 64
	maxTokenReply = 1 << 20
	maxAccessLife = 24 * time.Hour
	nameMax       = 64
)

var nameRe = regexp.MustCompile(`^[A-Za-z0-9._:/-]{1,128}$`)

// Provider describes one authorization server and the one source its grants are for.
type Provider struct {
	Issuer       string
	AuthorizeURL string
	TokenURL     string
	ClientID     string
	RedirectURI  string
	Scopes       []string
	Keys         *KeySet
	// Audience is the resource the resulting grant is for; it is not taken from anything the server returns.
	Audience string
	Source   Source
	// IssParam marks a server that returns its issuer with the authorization response, which then must match.
	IssParam bool
	HTTP     *http.Client
}

type pending struct {
	owner, account string
	p              *Provider
	state          string
	verifier       string
	nonce          string
	expires        time.Time
}

// OAuth runs the authorization-code flow with PKCE (S256 only) for a caller and turns a completed flow into a grant plus vaulted secrets.
type OAuth struct {
	Broker *Broker

	mu      sync.Mutex
	pending map[string]*pending
}

func NewOAuth(b *Broker) *OAuth { return &OAuth{Broker: b, pending: map[string]*pending{}} }

func randToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("no entropy: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func loopback(host string) bool {
	h, _, err := net.SplitHostPort(host)
	if err != nil {
		h = host
	}
	ip := net.ParseIP(h)
	return h == "localhost" || (ip != nil && ip.IsLoopback())
}

// secureURL accepts https, or http only for loopback (a local redirect or a test server).
func secureURL(raw string) (*url.URL, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" {
		return nil, false
	}
	return u, u.Scheme == "https" || (u.Scheme == "http" && loopback(u.Host))
}

func (p *Provider) check() error {
	_, a := secureURL(p.AuthorizeURL)
	_, t := secureURL(p.TokenURL)
	_, r := secureURL(p.RedirectURI)
	if !a || !t || !r || p.Issuer == "" || p.ClientID == "" || p.Audience == "" || len(p.Scopes) == 0 || !originRe.MatchString(p.Source.Origin) {
		return ErrInvalid
	}
	return nil
}

// Begin starts a flow for a caller and returns the address the person must open; the verifier never leaves this process.
func (o *OAuth) Begin(owner, account string, p *Provider) (string, error) {
	if !idRe.MatchString(owner) || !idRe.MatchString(account) || p == nil {
		return "", ErrInvalid
	}
	if err := p.check(); err != nil {
		return "", err
	}
	now := o.Broker.Now()
	o.mu.Lock()
	defer o.mu.Unlock()
	live := 0
	for s, x := range o.pending {
		if !now.Before(x.expires) {
			delete(o.pending, s)
		} else if x.owner == owner {
			live++
		}
	}
	if live >= maxPending {
		return "", ErrDenied
	}
	snap := *p
	snap.Scopes = slices.Clone(p.Scopes)
	p = &snap
	pd := &pending{owner: owner, account: account, p: p, state: randToken(32), verifier: randToken(32), nonce: randToken(24), expires: now.Add(pendingTTL)}
	o.pending[pd.state] = pd
	sum := sha256.Sum256([]byte(pd.verifier))
	u, _ := url.Parse(p.AuthorizeURL)
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", p.ClientID)
	q.Set("redirect_uri", p.RedirectURI)
	q.Set("scope", strings.Join(p.Scopes, " "))
	q.Set("state", pd.state)
	q.Set("nonce", pd.nonce)
	q.Set("code_challenge", base64.RawURLEncoding.EncodeToString(sum[:]))
	q.Set("code_challenge_method", "S256")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

type tokenReply struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	Scope        string `json:"scope"`
	Error        string `json:"error"`
}

func (p *Provider) post(form url.Values) (tokenReply, int, error) {
	hc := p.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}
	c := *hc
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	req, err := http.NewRequest(http.MethodPost, p.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return tokenReply{}, 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		return tokenReply{}, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxTokenReply+1))
	if err != nil || len(body) > maxTokenReply {
		return tokenReply{}, resp.StatusCode, ErrUntrusted
	}
	var tr tokenReply
	if json.Unmarshal(body, &tr) != nil || !noDuplicateKeys(body) {
		return tokenReply{}, resp.StatusCode, ErrUntrusted
	}
	return tr, resp.StatusCode, nil
}

// digest identifies the provider configuration a grant was created under: who issues, where tokens come from, which client and redirect, for which resource and source, with which scopes. Keys are not part of it, so a key rotation does not change a provider's identity.
func (p *Provider) digest() string {
	sc := scopeSet(strings.Join(p.Scopes, " "))
	b, _ := json.Marshal([]any{p.Issuer, p.AuthorizeURL, p.TokenURL, p.ClientID, p.RedirectURI, p.Audience, p.Source, sc})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:16])
}

func principalRef(iss, sub string) string {
	sum := sha256.Sum256([]byte(iss + "\x00" + sub))
	return "pr_" + hex.EncodeToString(sum[:16])
}

func scopeSet(s string) []string {
	f := strings.Fields(s)
	slices.Sort(f)
	return slices.Compact(f)
}

func secretNames(grant string) (access, refresh string) { return grant + "/access", grant + "/refresh" }

// Cancel drops a flow the caller abandoned; its state can no longer be completed.
func (o *OAuth) Cancel(owner, state string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if pd, ok := o.pending[state]; ok && pd.owner == owner {
		delete(o.pending, state)
	}
}

// Complete finishes a flow. The state is spent whether or not it succeeds, so a code cannot be tried twice.
func (o *OAuth) Complete(owner, state, code, iss string) (Grant, error) {
	now := o.Broker.Now()
	o.mu.Lock()
	pd, ok := o.pending[state]
	delete(o.pending, state)
	o.mu.Unlock()
	if !ok || pd.owner != owner || !now.Before(pd.expires) || code == "" || len(code) > 2048 {
		return Grant{}, ErrDenied
	}
	p := pd.p
	if (p.IssParam && iss != p.Issuer) || (iss != "" && iss != p.Issuer) {
		return Grant{}, ErrDenied
	}
	tr, status, err := p.post(url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {p.RedirectURI}, "client_id": {p.ClientID}, "code_verifier": {pd.verifier}})
	if err != nil || status != http.StatusOK || tr.Error != "" || tr.AccessToken == "" || !strings.EqualFold(tr.TokenType, "bearer") {
		return Grant{}, ErrUntrusted
	}
	life := time.Duration(tr.ExpiresIn) * time.Second
	if life <= 0 || life > maxAccessLife {
		return Grant{}, ErrUntrusted
	}
	granted := scopeSet(tr.Scope)
	if tr.Scope == "" {
		granted = scopeSet(strings.Join(p.Scopes, " "))
	}
	asked := scopeSet(strings.Join(p.Scopes, " "))
	for _, s := range granted {
		if !slices.Contains(asked, s) {
			return Grant{}, ErrUntrusted
		}
	}
	var sub string
	if slices.Contains(asked, "openid") {
		c, err := VerifyToken(tr.IDToken, p.Keys, Expect{Issuer: p.Issuer, Audience: p.ClientID, Nonce: pd.nonce, Now: now, MaxAge: pendingTTL})
		if err != nil {
			return Grant{}, ErrUntrusted
		}
		sub = c.Subject
	}
	if o.Broker.Vault == nil {
		return Grant{}, ErrInvalid
	}
	appScopes := slices.DeleteFunc(slices.Clone(granted), func(s string) bool { return s == "openid" })
	ng := Grant{Account: pd.account, Owner: owner, Source: p.Source, Audience: p.Audience, Issuer: p.Issuer, Scopes: appScopes, Provider: p.digest(),
		Method: MethodOAuthPKCE, ExpiresAt: now.Add(life)}
	if sub != "" {
		ng.PrincipalRef, ng.IdentityVerified = principalRef(p.Issuer, sub), true
	}
	g, err := o.Broker.Issue(ng)
	if err != nil {
		return Grant{}, err
	}
	an, rn := secretNames(g.ID)
	fail := func(err error) (Grant, error) {
		_, _ = o.Broker.endGrant(g.ID, owner, ReasonRevoked)
		return Grant{}, err
	}
	if tr.RefreshToken != "" {
		if err := o.Broker.Vault.Put(g.Account, rn, []byte(tr.RefreshToken)); err != nil {
			return fail(err)
		}
	}
	if err := o.Broker.Vault.Put(g.Account, an, []byte(tr.AccessToken)); err != nil {
		return fail(err)
	}
	o.Broker.note(EventSignedIn, g, owner, ReasonOK)
	return g, nil
}

type flight struct {
	done chan struct{}
	g    Grant
	err  error
}

// Refresh renews a grant's access token with its refresh token, under the provider configuration the grant was made with. A refusal by the server ends the grant; a network failure leaves it as it was. Concurrent refreshes of one grant in this process share one request, so a rotating refresh token is never spent twice; across processes the caller must arrange a single refresher per grant.
func (o *OAuth) Refresh(owner, grantID string, p *Provider) (Grant, error) {
	g, ok := o.Broker.Store.Get(grantID)
	if !ok || g.Owner != owner || g.Method != MethodOAuthPKCE || g.State == StateRevoked || p == nil {
		return Grant{}, ErrDenied
	}
	if g.State == StatePaused {
		return Grant{}, ErrReauth
	}
	if p.check() != nil || g.Provider == "" || p.digest() != g.Provider || g.Source != p.Source || g.Audience != p.Audience || g.Issuer != p.Issuer || o.Broker.Vault == nil {
		o.Broker.note(EventRefused, g, safeActor(owner), ReasonProvider)
		return Grant{}, ErrDenied
	}
	b := o.Broker
	b.mu.Lock()
	if f, running := b.flights[g.ID]; running {
		b.joins.Add(1)
		b.mu.Unlock()
		<-f.done
		return f.g, f.err
	}
	f := &flight{done: make(chan struct{})}
	b.flights[g.ID] = f
	b.mu.Unlock()
	f.g, f.err = o.refreshOnce(owner, g, p)
	b.mu.Lock()
	delete(b.flights, g.ID)
	b.mu.Unlock()
	close(f.done)
	return f.g, f.err
}

func (o *OAuth) refreshOnce(owner string, g Grant, p *Provider) (Grant, error) {
	b := o.Broker
	_, rn := secretNames(g.ID)
	rt, err := b.Vault.Get(g.Account, rn)
	if err != nil {
		return Grant{}, ErrReauth
	}
	tr, status, err := p.post(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {string(rt)}, "client_id": {p.ClientID}})
	zero(rt)
	if err != nil {
		return Grant{}, fmt.Errorf("%w: refresh unavailable", ErrUntrusted)
	}
	if status == http.StatusBadRequest || status == http.StatusUnauthorized {
		_, _ = b.endGrant(g.ID, owner, ReasonRevoked)
		return Grant{}, ErrReauth
	}
	life := time.Duration(tr.ExpiresIn) * time.Second
	if status != http.StatusOK || tr.Error != "" || tr.AccessToken == "" || !strings.EqualFold(tr.TokenType, "bearer") || life <= 0 || life > maxAccessLife {
		return Grant{}, ErrUntrusted
	}
	scopes := g.Scopes
	if tr.Scope != "" {
		granted := slices.DeleteFunc(scopeSet(tr.Scope), func(s string) bool { return s == "openid" })
		for _, s := range granted {
			if !slices.Contains(g.Scopes, s) {
				return Grant{}, ErrUntrusted
			}
		}
		scopes = granted
	}
	if tr.IDToken != "" && g.IdentityVerified {
		c, err := VerifyToken(tr.IDToken, p.Keys, Expect{Issuer: p.Issuer, Audience: p.ClientID, Now: b.Now()})
		if err != nil {
			return Grant{}, ErrUntrusted
		}
		if principalRef(p.Issuer, c.Subject) != g.PrincipalRef {
			_, _ = b.endGrant(g.ID, owner, ReasonSwitched)
			return Grant{}, ErrDenied
		}
	}
	next, lost, err := o.commitRefresh(owner, g, tr, scopes, life, p)
	if lost {
		_, _ = b.endGrant(g.ID, owner, ReasonRevoked)
		return Grant{}, fmt.Errorf("%w: rotated credential could not be stored", ErrReauth)
	}
	if err != nil {
		return Grant{}, err
	}
	b.note(EventRefreshed, next, owner, ReasonOK)
	return next, nil
}

// commitRefresh stores a refreshed token pair and updates the grant while holding the grant's secret lock, which revocation's cleanup also takes. lost reports a rotated refresh token that could not be stored, which leaves the grant without a usable credential.
func (o *OAuth) commitRefresh(owner string, g Grant, tr tokenReply, scopes []string, life time.Duration, p *Provider) (next Grant, lost bool, err error) {
	b := o.Broker
	an, rn := secretNames(g.ID)
	mu := b.stripe(g.ID)
	mu.Lock()
	defer mu.Unlock()
	cur, ok := b.Store.Get(g.ID)
	if !ok || cur.State == StateRevoked || cur.Epoch != g.Epoch {
		return Grant{}, false, ErrDenied
	}
	if tr.RefreshToken != "" {
		if err := b.Vault.Put(g.Account, rn, []byte(tr.RefreshToken)); err != nil {
			return Grant{}, true, err
		}
	}
	if err := b.Vault.Put(g.Account, an, []byte(tr.AccessToken)); err != nil {
		return Grant{}, false, err
	}
	exp := b.Now().Add(life)
	next, _, err = b.update(g.ID, func(x *Grant) error {
		if x.State == StateRevoked || x.Epoch != g.Epoch {
			return ErrDenied
		}
		x.ExpiresAt, x.Scopes = exp, slices.Clone(scopes)
		x.SecretGen++
		return nil
	})
	if err != nil && errors.Is(err, ErrDenied) {
		_ = b.Vault.Delete(g.Account, an)
		_ = b.Vault.Delete(g.Account, rn)
	}
	return next, false, err
}
