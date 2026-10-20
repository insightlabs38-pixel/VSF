package bridge

import (
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const otherExt = "chrome-extension://aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

type pairRig struct {
	*fixture
	p *Pairing
}

func startPairing(t *testing.T, mutate func(*PairConfig)) *pairRig {
	t.Helper()
	clock := &fakeClock{t: time.Unix(1_800_000_000, 0)}
	cfg := PairConfig{Origins: []string{extOrigin, otherExt}, Now: clock.now}
	if mutate != nil {
		mutate(&cfg)
	}
	p, err := NewPairing(cfg)
	if err != nil {
		t.Fatal(err)
	}
	f := start(t, func(c *Config) {
		c.Token = ""
		c.Pairing = p
		c.Now = clock.now
	})
	f.clock = clock
	return &pairRig{fixture: f, p: p}
}

func (r *pairRig) post(t *testing.T, path, origin string, v any) (int, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(v)
	status, body, _ := r.do(t, "POST", path, opts{origin: origin, ctype: "application/json", headers: map[string]string{csrfHeader: "1"}, body: raw})
	out := map[string]any{}
	_ = json.Unmarshal([]byte(body), &out)
	return status, out
}

// pairWith is the extension's half of the protocol, written independently of the server's.
func (r *pairRig) pairWith(t *testing.T, origin, code string) (int, map[string]any) {
	t.Helper()
	nonceC := []byte("0123456789abcdef-client-nonce")
	status, begin := r.post(t, "/v1/pair/begin", origin, map[string]string{"nonce": b64.EncodeToString(nonceC)})
	if status != 200 {
		return status, begin
	}
	salt, _ := b64.DecodeString(begin["salt"].(string))
	nonceS, _ := b64.DecodeString(begin["nonce_s"].(string))
	key, err := pbkdf2.Key(sha256.New, NormalizeCode(code), salt, int(begin["iterations"].(float64)), 32)
	if err != nil {
		t.Fatal(err)
	}
	proof := macOf(key, "vsf-pair-c", nonceC, nonceS, []byte(origin))
	status, fin := r.post(t, "/v1/pair/finish", origin, map[string]string{"offer_id": begin["offer_id"].(string), "proof": b64.EncodeToString(proof), "name": "test browser"})
	if status == 200 {
		want := macOf(key, "vsf-pair-s", nonceC, nonceS, []byte(fin["client_id"].(string)))
		got, _ := b64.DecodeString(fin["proof"].(string))
		if !hmac.Equal(want, got) {
			t.Fatalf("the server could not prove it knew the code")
		}
	}
	return status, fin
}

func macOf(key []byte, label string, parts ...[]byte) []byte { return mac(key, label, parts...) }

func (r *pairRig) offer(t *testing.T, origin string) string {
	t.Helper()
	code, _, err := r.p.NewOffer(origin)
	if err != nil {
		t.Fatal(err)
	}
	return code
}

func (r *pairRig) ping(t *testing.T, origin, access string) int {
	t.Helper()
	status, _, _ := r.do(t, "GET", "/v1/ping", opts{origin: origin, auth: "Bearer " + access})
	return status
}

func TestPairingIssuesCredentialsThatAuthenticateOnlyForTheirOrigin(t *testing.T) {
	r := startPairing(t, nil)
	status, creds := r.pairWith(t, extOrigin, r.offer(t, extOrigin))
	if status != 200 {
		t.Fatalf("pairing failed: %d %v", status, creds)
	}
	access := creds["access"].(string)
	if got := r.ping(t, extOrigin, access); got != 200 {
		t.Fatalf("paired call: %d", got)
	}
	if got := r.ping(t, otherExt, access); got != 401 {
		t.Fatalf("another extension origin must not reuse the credential: %d", got)
	}
	if got := r.ping(t, "", access); got != 401 {
		t.Fatalf("a call without an origin must not reuse the credential: %d", got)
	}
	if got := r.ping(t, extOrigin, strings.Repeat("A", 43)); got != 401 {
		t.Fatalf("a guessed token: %d", got)
	}
}

func TestPairingNeedsAnOfferTheUserStarted(t *testing.T) {
	r := startPairing(t, nil)
	status, _ := r.post(t, "/v1/pair/begin", extOrigin, map[string]string{"nonce": b64.EncodeToString(make([]byte, 16))})
	if status != 403 {
		t.Fatalf("begin without an offer: %d", status)
	}
	if _, _, err := r.p.NewOffer("chrome-extension://bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"); err == nil {
		t.Fatal("an offer for an unlisted origin must not exist")
	}
}

func TestWrongCodesBurnTheOfferAndTheRightCodeThenFails(t *testing.T) {
	r := startPairing(t, nil)
	good := r.offer(t, extOrigin)
	for i := 0; i < MaxOfferAttempts; i++ {
		if status, _ := r.pairWith(t, extOrigin, "ZZZZ-ZZZZ-ZZZZ-ZZZZ"); status != 403 {
			t.Fatalf("wrong code %d: %d", i, status)
		}
	}
	if status, _ := r.pairWith(t, extOrigin, good); status != 403 {
		t.Fatalf("the offer must be gone after too many wrong proofs: %d", status)
	}
	if n := len(r.p.Clients()); n != 0 {
		t.Fatalf("no client may exist: %d", n)
	}
}

func TestOfferExpiresAndCodeWorksOnce(t *testing.T) {
	r := startPairing(t, nil)
	code := r.offer(t, extOrigin)
	r.clock.advance(DefaultOfferTTL + time.Second)
	if status, _ := r.pairWith(t, extOrigin, code); status != 403 {
		t.Fatalf("expired offer: %d", status)
	}
	code = r.offer(t, extOrigin)
	if status, _ := r.pairWith(t, extOrigin, code); status != 200 {
		t.Fatalf("fresh offer: %d", status)
	}
	if status, _ := r.pairWith(t, extOrigin, code); status != 403 {
		t.Fatalf("a used code must not pair again: %d", status)
	}
}

func TestOfferIsBoundToTheOriginItWasIssuedFor(t *testing.T) {
	r := startPairing(t, nil)
	code := r.offer(t, extOrigin)
	if status, _ := r.pairWith(t, otherExt, code); status != 403 {
		t.Fatalf("another listed extension must not take the offer: %d", status)
	}
	if status, _ := r.pairWith(t, "https://evil.example", code); status != 403 {
		t.Fatalf("a web origin must not take the offer: %d", status)
	}
	raw, _ := json.Marshal(map[string]string{"nonce": b64.EncodeToString(make([]byte, 16))})
	status, _, _ := r.do(t, "POST", "/v1/pair/begin", opts{ctype: "application/json", headers: map[string]string{csrfHeader: "1"}, body: raw})
	if status != 403 {
		t.Fatalf("no origin: %d", status)
	}
	if status, _ := r.pairWith(t, extOrigin, code); status != 200 {
		t.Fatalf("the intended extension still pairs: %d", status)
	}
}

func TestPairingEndpointsKeepTheBridgeProtections(t *testing.T) {
	r := startPairing(t, nil)
	raw := []byte(`{"nonce":"AAAAAAAAAAAAAAAAAAAAAA"}`)
	cases := []struct {
		name string
		o    opts
		want int
	}{
		{"rebinding host", opts{host: "attacker.example:80", origin: extOrigin, ctype: "application/json", headers: map[string]string{csrfHeader: "1"}, body: raw}, 403},
		{"no csrf header", opts{origin: extOrigin, ctype: "application/json", body: raw}, 403},
		{"form content type", opts{origin: extOrigin, ctype: "application/x-www-form-urlencoded", headers: map[string]string{csrfHeader: "1"}, body: raw}, 415},
		{"cross site fetch", opts{origin: "https://evil.example", ctype: "application/json", headers: map[string]string{csrfHeader: "1"}, body: raw}, 403},
	}
	for _, c := range cases {
		if status, _, _ := r.do(t, "POST", "/v1/pair/begin", c.o); status != c.want {
			t.Errorf("%s: %d", c.name, status)
		}
	}
	if status, _, _ := r.do(t, "GET", "/v1/pair/begin", opts{origin: extOrigin}); status != 405 {
		t.Errorf("GET: %d", status)
	}
	bad := [][]byte{[]byte(`{"nonce":"x","extra":1}`), []byte(`not json`), []byte(`{"nonce":"!!!"}`), []byte(strings.Repeat("a", 5000))}
	for _, b := range bad {
		status, _, _ := r.do(t, "POST", "/v1/pair/begin", opts{origin: extOrigin, ctype: "application/json", headers: map[string]string{csrfHeader: "1"}, body: b})
		if status != 400 && status != 413 {
			t.Errorf("malformed body %q: %d", b[:min(len(b), 20)], status)
		}
	}
}

func TestAccessExpiresRefreshRotatesAndAReusedSecretRevokesTheClient(t *testing.T) {
	r := startPairing(t, nil)
	_, first := r.pairWith(t, extOrigin, r.offer(t, extOrigin))
	r.clock.advance(DefaultAccessTTL + time.Minute)
	if got := r.ping(t, extOrigin, first["access"].(string)); got != 401 {
		t.Fatalf("expired access token: %d", got)
	}
	status, second := r.post(t, "/v1/token/refresh", extOrigin, map[string]string{"client_id": first["client_id"].(string), "refresh": first["refresh"].(string)})
	if status != 200 {
		t.Fatalf("refresh: %d %v", status, second)
	}
	if r.ping(t, extOrigin, second["access"].(string)) != 200 {
		t.Fatal("renewed access token must work")
	}
	if second["refresh"] == first["refresh"] {
		t.Fatal("the refresh secret must rotate")
	}
	status, third := r.post(t, "/v1/token/refresh", extOrigin, map[string]string{"client_id": first["client_id"].(string), "refresh": first["refresh"].(string)})
	if status != 401 || third["error"] != "pair.refresh_reused" {
		t.Fatalf("a rotated-away secret means a copy exists: %d %v", status, third)
	}
	if got := r.ping(t, extOrigin, second["access"].(string)); got != 401 {
		t.Fatalf("reuse revokes every token of the client: %d", got)
	}
	if status, _ := r.post(t, "/v1/token/refresh", extOrigin, map[string]string{"client_id": first["client_id"].(string), "refresh": second["refresh"].(string)}); status != 403 {
		t.Fatalf("the legitimate holder is locked out too, and must pair again: %d", status)
	}
}

func TestRefreshChecksOriginSecretAndLifetime(t *testing.T) {
	r := startPairing(t, nil)
	_, c := r.pairWith(t, extOrigin, r.offer(t, extOrigin))
	id, refresh := c["client_id"].(string), c["refresh"].(string)
	if status, _ := r.post(t, "/v1/token/refresh", otherExt, map[string]string{"client_id": id, "refresh": refresh}); status != 403 {
		t.Fatalf("another origin: %d", status)
	}
	if status, _ := r.post(t, "/v1/token/refresh", extOrigin, map[string]string{"client_id": id, "refresh": b64.EncodeToString(make([]byte, 32))}); status != 403 {
		t.Fatalf("wrong secret: %d", status)
	}
	if status, _ := r.post(t, "/v1/token/refresh", extOrigin, map[string]string{"client_id": "cli_nope", "refresh": refresh}); status != 403 {
		t.Fatalf("unknown client: %d", status)
	}
	r.clock.advance(DefaultRefreshTTL + time.Hour)
	status, body := r.post(t, "/v1/token/refresh", extOrigin, map[string]string{"client_id": id, "refresh": refresh})
	if status != 401 || body["error"] != "pair.expired" {
		t.Fatalf("expired refresh: %d %v", status, body)
	}
}

func TestRevokeEndsAccessFromEitherSide(t *testing.T) {
	r := startPairing(t, nil)
	_, a := r.pairWith(t, extOrigin, r.offer(t, extOrigin))
	status, _, _ := r.do(t, "POST", "/v1/pair/revoke", opts{origin: extOrigin, auth: "Bearer " + a["access"].(string), ctype: "application/json", headers: map[string]string{csrfHeader: "1"}, body: []byte("{}")})
	if status != 200 {
		t.Fatalf("self revoke: %d", status)
	}
	if r.ping(t, extOrigin, a["access"].(string)) != 401 {
		t.Fatal("revoked access token must stop working")
	}
	if status, _ := r.post(t, "/v1/token/refresh", extOrigin, map[string]string{"client_id": a["client_id"].(string), "refresh": a["refresh"].(string)}); status != 403 {
		t.Fatalf("revoked refresh: %d", status)
	}
	_, b := r.pairWith(t, extOrigin, r.offer(t, extOrigin))
	if !r.p.Revoke(b["client_id"].(string)) {
		t.Fatal("companion-side revoke")
	}
	if r.ping(t, extOrigin, b["access"].(string)) != 401 {
		t.Fatal("companion-side revoke must end access immediately")
	}
	if r.p.Revoke(b["client_id"].(string)) {
		t.Fatal("revoking twice reports nothing to do")
	}
}

func TestRegistrySurvivesRestartAndStoresNoSecret(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "pairing.json")
	r := startPairing(t, func(c *PairConfig) { c.Store = FileStore{Path: path} })
	code := r.offer(t, extOrigin)
	_, c := r.pairWith(t, extOrigin, code)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{c["refresh"].(string), c["access"].(string), strings.ReplaceAll(code, "-", "")} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("the registry holds a secret: %s", secret[:6])
		}
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Fatalf("registry permissions: %v", info.Mode().Perm())
	}
	if info, _ := os.Stat(filepath.Dir(path)); info.Mode().Perm() != 0o700 {
		t.Fatalf("state directory permissions: %v", info.Mode().Perm())
	}
	again, err := NewPairing(PairConfig{Origins: []string{extOrigin}, Store: FileStore{Path: path}, Now: r.clock.now})
	if err != nil {
		t.Fatal(err)
	}
	creds, err := again.Refresh(extOrigin, c["client_id"].(string), mustDecode(t, c["refresh"].(string)))
	if err != nil || creds.ClientID != c["client_id"].(string) {
		t.Fatalf("refresh after restart: %v", err)
	}
	if _, ok := again.Authenticate(c["access"].(string), extOrigin); ok {
		t.Fatal("access tokens do not survive a restart")
	}
	if err := os.WriteFile(path, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewPairing(PairConfig{Origins: []string{extOrigin}, Store: FileStore{Path: path}}); err == nil {
		t.Fatal("a damaged registry must not be silently replaced")
	}
}

func mustDecode(t *testing.T, s string) []byte {
	t.Helper()
	b, err := b64.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestPairingAttemptsAreRateLimited(t *testing.T) {
	r := startPairing(t, nil)
	r.offer(t, extOrigin)
	limited := 0
	for i := 0; i < pairRatePerMin+5; i++ {
		status, _ := r.post(t, "/v1/pair/begin", extOrigin, map[string]string{"nonce": b64.EncodeToString(make([]byte, 16))})
		if status == http.StatusTooManyRequests {
			limited++
		}
	}
	if limited != 5 {
		t.Fatalf("limited %d", limited)
	}
	r.clock.advance(2 * time.Minute)
	r.offer(t, extOrigin)
	if status, _ := r.post(t, "/v1/pair/begin", extOrigin, map[string]string{"nonce": b64.EncodeToString(make([]byte, 16))}); status != 200 {
		t.Fatalf("the window must reopen: %d", status)
	}
}

func TestConcurrentFinishPairsExactlyOnce(t *testing.T) {
	p, err := NewPairing(PairConfig{Origins: []string{extOrigin}})
	if err != nil {
		t.Fatal(err)
	}
	code, _, _ := p.NewOffer(extOrigin)
	nonceC := []byte("0123456789abcdef")
	begin, err := p.Begin(extOrigin, nonceC)
	if err != nil {
		t.Fatal(err)
	}
	salt, _ := b64.DecodeString(begin.Salt)
	nonceS, _ := b64.DecodeString(begin.NonceS)
	key, _ := pbkdf2.Key(sha256.New, NormalizeCode(code), salt, begin.Iterations, 32)
	proof := mac(key, "vsf-pair-c", nonceC, nonceS, []byte(extOrigin))
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok := 0
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := p.Finish(extOrigin, begin.OfferID, "x", proof); err == nil {
				mu.Lock()
				ok++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if ok != 1 || len(p.Clients()) != 1 {
		t.Fatalf("paired %d times, %d clients", ok, len(p.Clients()))
	}
}

func TestOriginsAndCodesAreValidated(t *testing.T) {
	for _, bad := range [][]string{nil, {"https://a.example"}, {"chrome-extension://abc/path"}, {"chrome-extension://*"}} {
		if _, err := NewPairing(PairConfig{Origins: bad}); err == nil {
			t.Errorf("origins %v must be refused", bad)
		}
	}
	if NormalizeCode("abcd-0o1i-lxyz") != "ABCD00111XYZ" {
		t.Fatalf("normalize: %s", NormalizeCode("abcd-0o1i-lxyz"))
	}
	p, _ := NewPairing(PairConfig{Origins: []string{extOrigin}})
	a, _, _ := p.NewOffer(extOrigin)
	b, _, _ := p.NewOffer(extOrigin)
	if a == b || len(a) != 19 {
		t.Fatalf("codes must be fresh and 16 characters in groups: %q %q", a, b)
	}
}

func TestStaticTokenCannotPairOrRevoke(t *testing.T) {
	r := startPairing(t, func(c *PairConfig) {})
	f := start(t, func(c *Config) {
		c.Pairing = r.p
	})
	status, _, _ := f.do(t, "POST", "/v1/pair/revoke", opts{origin: extOrigin, auth: "Bearer " + f.token, ctype: "application/json", headers: map[string]string{csrfHeader: "1"}, body: []byte("{}")})
	if status != 405 {
		t.Fatalf("the development token carries no client identity: %d", status)
	}
}
