package bridge

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestHostHeaderVariantsOutsideTheBoundAddressAreDenied(t *testing.T) {
	f := start(t, nil)
	port := f.addr[strings.LastIndex(f.addr, ":")+1:]
	hosts := []string{
		"evil.example:" + port,
		"127.0.0.1.evil.example:" + port,
		"127.0.0.1:1",
		"127.0.0.1",
		"[::1]:" + port,
		"0.0.0.0:" + port,
		"LOCALHOST:" + port,
		"localhost.:" + port,
		"127.0.0.1.:" + port,
		"2130706433:" + port,
		"0x7f.0.0.1:" + port,
		" 127.0.0.1:" + port,
	}
	for _, h := range hosts {
		status, body, _ := f.do(t, "GET", "/v1/ping", opts{host: h, origin: extOrigin, auth: "Bearer " + f.token})
		if status != 403 && status != 400 {
			t.Errorf("host %q: %d %s", h, status, body)
		}
	}
}

func TestSecFetchMetadataFromOtherSitesNeverReachesAHandler(t *testing.T) {
	f := start(t, nil)
	for _, site := range []string{"cross-site", "same-site"} {
		status, body, _ := f.do(t, "GET", "/v1/ping", opts{auth: "Bearer " + f.token, headers: map[string]string{"Sec-Fetch-Site": site, "Sec-Fetch-Mode": "no-cors", "Sec-Fetch-Dest": "image"}})
		if status != 403 || !strings.Contains(body, "bridge.origin_not_allowed") {
			t.Errorf("%s: %d %s", site, status, body)
		}
	}
}

func TestPreflightFromAnUnlistedOriginGrantsNothing(t *testing.T) {
	f := start(t, nil)
	status, _, h := f.do(t, "OPTIONS", "/v1/envelope/verify", opts{origin: "https://evil.example", headers: map[string]string{
		"Access-Control-Request-Method":          "POST",
		"Access-Control-Request-Headers":         "authorization,content-type,x-vsf-bridge",
		"Access-Control-Request-Private-Network": "true",
	}})
	if status != 403 {
		t.Fatalf("preflight: %d", status)
	}
	for _, name := range []string{"Access-Control-Allow-Origin", "Access-Control-Allow-Private-Network", "Access-Control-Allow-Headers", "Access-Control-Allow-Methods"} {
		if v := h.Get(name); v != "" {
			t.Errorf("%s must not be granted to a foreign origin: %q", name, v)
		}
	}
}

func TestPreflightFromTheExtensionAsksForPrivateNetworkAccessAndGetsOnlyThat(t *testing.T) {
	f := start(t, nil)
	status, _, h := f.do(t, "OPTIONS", "/v1/envelope/verify", opts{origin: extOrigin, headers: map[string]string{
		"Access-Control-Request-Method":          "POST",
		"Access-Control-Request-Private-Network": "true",
	}})
	if status != 204 || h.Get("Access-Control-Allow-Private-Network") != "true" || h.Get("Access-Control-Allow-Origin") != extOrigin {
		t.Fatalf("extension preflight: %d %v", status, h)
	}
	if got := h.Get("Access-Control-Allow-Methods"); got != "GET, POST" {
		t.Fatalf("methods: %q", got)
	}
}

func TestEveryRequestIsRecordedWithItsOutcomeAndNoCredential(t *testing.T) {
	var recs []RequestRecord
	f := start(t, func(c *Config) { c.OnRequest = func(r RequestRecord) { recs = append(recs, r) } })
	f.do(t, "GET", "/v1/ping", opts{origin: "https://evil.example", auth: "Bearer " + f.token})
	f.do(t, "GET", "/v1/ping", opts{origin: extOrigin, auth: "Bearer " + f.token})
	raw, _ := json.Marshal(recs)
	if len(recs) != 2 || recs[0].Status != 403 || recs[1].Status != 200 {
		t.Fatalf("records: %s", raw)
	}
	if strings.Contains(string(raw), f.token) {
		t.Fatal("the log must never hold a credential")
	}
}

func TestPairingEndpointsDoNotCountForeignAttemptsAgainstTheLegitimateRateLimit(t *testing.T) {
	r := startPairing(t, nil)
	code := r.offer(t, extOrigin)
	for i := 0; i < pairRatePerMin*3; i++ {
		r.post(t, "/v1/pair/begin", "https://evil.example", map[string]string{"nonce": b64.EncodeToString(make([]byte, 16))})
		r.post(t, "/v1/pair/finish", "", map[string]string{"offer_id": "x", "proof": "AAAA"})
	}
	if status, body := r.pairWith(t, extOrigin, code); status != 200 {
		t.Fatalf("hostile traffic must not use up the user's pairing attempts: %d %v", status, body)
	}
}

func TestAPageThatHoldsAStolenAccessTokenStillCannotUseIt(t *testing.T) {
	r := startPairing(t, nil)
	_, c := r.pairWith(t, extOrigin, r.offer(t, extOrigin))
	access := c["access"].(string)
	for _, origin := range []string{"https://evil.example", "http://127.0.0.1:1", "null", ""} {
		status, _, _ := r.do(t, "GET", "/v1/ping", opts{origin: origin, auth: "Bearer " + access})
		if status == 200 {
			t.Errorf("origin %q reached the bridge with a stolen token", origin)
		}
	}
	status, _, _ := r.do(t, "GET", "/v1/ping?access="+access, opts{origin: extOrigin})
	if status != 401 {
		t.Errorf("a token in the query string must not authenticate: %d", status)
	}
}
