package bridge

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/insightlabs38-pixel/vsf/backend/pairing"
)

const nonceC = "00112233445566778899aabbccddeeff"

type paired struct {
	*fixture
	svc *pairing.Service
}

func startPaired(t *testing.T) *paired {
	t.Helper()
	var svc *pairing.Service
	f := start(t, func(c *Config) {
		c.Token = ""
		var err error
		svc, err = pairing.New(pairing.Config{Peers: []string{extOrigin}, Now: c.Now})
		if err != nil {
			t.Fatal(err)
		}
		c.Pairing = svc
	})
	return &paired{fixture: f, svc: svc}
}

func (p *paired) post(t *testing.T, path string, v any, o opts) (int, map[string]any) {
	t.Helper()
	b, _ := json.Marshal(v)
	if o.origin == "" {
		o.origin = extOrigin
	}
	o.ctype, o.body = "application/json", b
	if o.headers == nil {
		o.headers = map[string]string{csrfHeader: "1"}
	}
	status, body, _ := p.do(t, "POST", path, o)
	var out map[string]any
	_ = json.Unmarshal([]byte(body), &out)
	return status, out
}

func (p *paired) creds(t *testing.T) pairing.Credentials {
	t.Helper()
	status, b := p.post(t, "/v1/pair/begin", map[string]string{"nonce_c": nonceC}, opts{})
	if status != 200 {
		t.Fatalf("begin %d %v", status, b)
	}
	id, ns := b["pair_id"].(string), b["nonce_s"].(string)
	code, err := p.svc.Approve(id)
	if err != nil {
		t.Fatal(err)
	}
	status, b = p.post(t, "/v1/pair/finish", map[string]string{"pair_id": id, "proof": pairing.Proof(code, id, extOrigin, nonceC, ns)}, opts{})
	if status != 200 {
		t.Fatalf("finish %d %v", status, b)
	}
	raw, _ := json.Marshal(b)
	var c pairing.Credentials
	_ = json.Unmarshal(raw, &c)
	return c
}

func (p *paired) ping(t *testing.T, token, origin string) (int, string) {
	t.Helper()
	status, body, _ := p.do(t, "GET", "/v1/ping", opts{origin: origin, auth: "Bearer " + token})
	return status, body
}

func TestPairedExtensionReachesTheBridgeAndNobodyElseDoes(t *testing.T) {
	p := startPaired(t)
	if status, _ := p.ping(t, "anything", extOrigin); status != 401 {
		t.Fatalf("before pairing: %d", status)
	}
	c := p.creds(t)
	if status, body := p.ping(t, c.Access, extOrigin); status != 200 || !strings.Contains(body, `"ok":true`) {
		t.Fatalf("paired: %d %s", status, body)
	}
	if status, body, _ := p.do(t, "GET", "/v1/ping", opts{auth: "Bearer " + c.Access}); status != 401 {
		t.Fatalf("no origin: %d %s", status, body)
	}
	if status, _ := p.ping(t, c.Access, "https://evil.test"); status != 403 {
		t.Fatalf("stolen token from a web origin: %d", status)
	}
	if status, _ := p.ping(t, c.Refresh, extOrigin); status != 401 {
		t.Fatalf("a refresh token must not authenticate calls: %d", status)
	}
}

func TestPairingCallsAreHeldToTheSameRulesAsEveryOtherCall(t *testing.T) {
	p := startPaired(t)
	body := map[string]string{"nonce_c": nonceC}
	if status, b := p.post(t, "/v1/pair/begin", body, opts{origin: "https://evil.test"}); status != 403 || b["error"] != "bridge.origin_not_allowed" {
		t.Fatalf("web origin: %d %v", status, b)
	}
	if status, b, _ := p.do(t, "POST", "/v1/pair/begin", opts{ctype: "application/json", headers: map[string]string{csrfHeader: "1"}, body: []byte(`{"nonce_c":"` + nonceC + `"}`)}); status != 403 {
		t.Fatalf("no origin: %d %v", status, b)
	}
	if status, b := p.post(t, "/v1/pair/begin", body, opts{host: "evil.test:80"}); status != 403 || b["error"] != "bridge.host_not_allowed" {
		t.Fatalf("rebinding: %d %v", status, b)
	}
	if status, b := p.post(t, "/v1/pair/begin", body, opts{headers: map[string]string{}}); status != 403 || b["error"] != "bridge.csrf_header_missing" {
		t.Fatalf("csrf header: %d %v", status, b)
	}
	status, b, _ := p.do(t, "POST", "/v1/pair/begin", opts{origin: extOrigin, ctype: "application/x-www-form-urlencoded", headers: map[string]string{csrfHeader: "1"}, body: []byte("nonce_c=" + nonceC)})
	if status != 415 {
		t.Fatalf("form post: %d %s", status, b)
	}
	if status, b := p.post(t, "/v1/pair/begin", map[string]any{"nonce_c": nonceC, "extra": 1}, opts{}); status != 400 {
		t.Fatalf("unknown field: %d %v", status, b)
	}
	if status, b := p.post(t, "/v1/pair/begin", map[string]string{"nonce_c": "nope"}, opts{}); status != 400 || b["error"] != "bridge.pairing.bad_request" {
		t.Fatalf("bad nonce: %d %v", status, b)
	}
	if status, _, _ := p.do(t, "GET", "/v1/pair/begin", opts{origin: extOrigin, auth: "Bearer x"}); status != 401 {
		t.Fatalf("GET without a token: %d", status)
	}
}

func TestFinishNeedsApprovalAndTheRightProof(t *testing.T) {
	p := startPaired(t)
	_, b := p.post(t, "/v1/pair/begin", map[string]string{"nonce_c": nonceC}, opts{})
	id := b["pair_id"].(string)
	if status, e := p.post(t, "/v1/pair/finish", map[string]string{"pair_id": id, "proof": strings.Repeat("0", 64)}, opts{}); status != 409 || e["error"] != "bridge.pairing.not_approved" {
		t.Fatalf("unapproved: %d %v", status, e)
	}
	if _, err := p.svc.Approve(id); err != nil {
		t.Fatal(err)
	}
	for i, want := range []int{403, 403, 403} {
		if status, e := p.post(t, "/v1/pair/finish", map[string]string{"pair_id": id, "proof": strings.Repeat("0", 64)}, opts{}); status != want {
			t.Fatalf("attempt %d: %d %v", i, status, e)
		}
	}
	if status, e := p.post(t, "/v1/pair/finish", map[string]string{"pair_id": id, "proof": strings.Repeat("0", 64)}, opts{}); status != 404 {
		t.Fatalf("burned: %d %v", status, e)
	}
}

func TestExpiryRenewalRotationAndReuseOverHTTP(t *testing.T) {
	p := startPaired(t)
	c := p.creds(t)
	p.clock.advance(16 * time.Minute)
	status, body := p.ping(t, c.Access, extOrigin)
	if status != 401 || !strings.Contains(body, "bridge.token_expired") {
		t.Fatalf("expired: %d %s", status, body)
	}
	status, b := p.post(t, "/v1/pair/renew", map[string]string{"refresh": c.Refresh}, opts{})
	if status != 200 {
		t.Fatalf("renew: %d %v", status, b)
	}
	next := b["access"].(string)
	if status, _ := p.ping(t, next, extOrigin); status != 200 {
		t.Fatalf("renewed token: %d", status)
	}
	if status, e := p.post(t, "/v1/pair/renew", map[string]string{"refresh": c.Refresh}, opts{}); status != 401 || e["error"] != "bridge.auth.refresh_reused" {
		t.Fatalf("reuse: %d %v", status, e)
	}
	status, body = p.ping(t, next, extOrigin)
	if status != 401 || !strings.Contains(body, "bridge.revoked") {
		t.Fatalf("after reuse: %d %s", status, body)
	}
}

func TestAPairedPeerCanRevokeItselfAndWhoamiReportsIt(t *testing.T) {
	p := startPaired(t)
	c := p.creds(t)
	status, body, _ := p.do(t, "GET", "/v1/pair/whoami", opts{origin: extOrigin, auth: "Bearer " + c.Access})
	if status != 200 || !strings.Contains(body, c.PairingID) || !strings.Contains(body, extOrigin) {
		t.Fatalf("whoami: %d %s", status, body)
	}
	if status, e := p.post(t, "/v1/pair/revoke", map[string]string{}, opts{headers: map[string]string{csrfHeader: "1"}, auth: "Bearer " + c.Access}); status != 200 {
		t.Fatalf("revoke: %d %v", status, e)
	}
	status, body = p.ping(t, c.Access, extOrigin)
	if status != 401 || !strings.Contains(body, "bridge.revoked") {
		t.Fatalf("after revoke: %d %s", status, body)
	}
	if status, _ := p.post(t, "/v1/pair/revoke", map[string]string{}, opts{}); status != 401 {
		t.Fatalf("revoke without a token: %d", status)
	}
}

func TestStaticTokenModeIsUnchangedAndHasNoPairingRoutes(t *testing.T) {
	f := start(t, nil)
	if status, _ := f.good(t, "GET", "/v1/ping", nil, ""); status != 200 {
		t.Fatalf("static token: %d", status)
	}
	status, _, _ := f.do(t, "POST", "/v1/pair/begin", opts{origin: extOrigin, auth: "Bearer " + f.token, ctype: "application/json", headers: map[string]string{csrfHeader: "1"}, body: []byte(`{}`)})
	if status != 404 {
		t.Fatalf("pairing routes without a pairing service: %d", status)
	}
}

func TestWhoamiAcceptsPostBecauseBrowsersDoNotAlwaysSendOriginOnGet(t *testing.T) {
	p := startPaired(t)
	c := p.creds(t)
	status, e := p.post(t, "/v1/pair/whoami", map[string]string{}, opts{auth: "Bearer " + c.Access})
	if status != 200 || e["peer"] != extOrigin {
		t.Fatalf("whoami by POST: %d %v", status, e)
	}
	if status, _ := p.post(t, "/v1/pair/whoami", map[string]string{}, opts{}); status != 401 {
		t.Fatalf("whoami without a token: %d", status)
	}
}
