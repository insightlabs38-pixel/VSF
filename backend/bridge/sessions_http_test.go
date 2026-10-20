package bridge

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

type fullRig struct {
	*pairRig
	b *Bindings
}

func startFull(t *testing.T) *fullRig {
	t.Helper()
	clock := &fakeClock{t: time.Unix(1_800_000_000, 0)}
	b := NewBindings(decodeValidator{valid: true}, clock.now)
	p, err := NewPairing(PairConfig{Origins: []string{extOrigin, otherExt}, Now: clock.now, OnRevoke: b.RevokeClient})
	if err != nil {
		t.Fatal(err)
	}
	f := start(t, func(c *Config) {
		c.Token = ""
		c.Pairing = p
		c.Bindings = b
		c.Now = clock.now
	})
	f.clock = clock
	return &fullRig{pairRig: &pairRig{fixture: f, p: p}, b: b}
}

func (r *fullRig) call(t *testing.T, method, path, origin, access string, body any) (int, map[string]any) {
	t.Helper()
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	status, text, _ := r.do(t, method, path, opts{origin: origin, auth: "Bearer " + access, ctype: "application/json", headers: map[string]string{csrfHeader: "1"}, body: raw})
	out := map[string]any{}
	_ = json.Unmarshal([]byte(text), &out)
	return status, out
}

func TestSessionsAreBoundThroughThePairedClientOnly(t *testing.T) {
	r := startFull(t)
	_, a := r.pairWith(t, extOrigin, r.offer(t, extOrigin))
	access := a["access"].(string)
	body := map[string]any{"origin": srcOrigin, "document": "doc-0001-aaaa", "manifest": json.RawMessage(manifest(t, r.clock.now(), nil))}
	status, bd := r.call(t, "POST", "/v1/session", extOrigin, access, body)
	if status != 200 || bd["principal"] != principalA || bd["origin"] != srcOrigin {
		t.Fatalf("register: %d %v", status, bd)
	}
	id := bd["session_id"].(string)
	if status, _ := r.call(t, "GET", "/v1/session/"+id, extOrigin, access, nil); status != 200 {
		t.Fatalf("get: %d", status)
	}
	if status, _ := r.call(t, "POST", "/v1/session/renew", extOrigin, access, map[string]string{"session_id": id}); status != 200 {
		t.Fatalf("renew: %d", status)
	}

	_, c := r.pairWith(t, extOrigin, r.offer(t, extOrigin))
	if status, _ := r.call(t, "GET", "/v1/session/"+id, extOrigin, c["access"].(string), nil); status != 404 {
		t.Fatalf("another paired client must not see the binding: %d", status)
	}
	if status, _ := r.call(t, "POST", "/v1/session/end", extOrigin, c["access"].(string), map[string]string{"session_id": id}); status != 200 {
		t.Fatalf("end by a stranger answers politely: %d", status)
	}
	if status, _ := r.call(t, "GET", "/v1/session/"+id, extOrigin, access, nil); status != 200 {
		t.Fatal("a stranger's end must not end it")
	}

	if status, _ := r.call(t, "POST", "/v1/pair/revoke", extOrigin, access, map[string]string{}); status != 200 {
		t.Fatalf("revoke: %d", status)
	}
	if _, err := r.b.Get(id, a["client_id"].(string)); err == nil {
		t.Fatal("revoking the pairing ends its sessions")
	}
}

func TestStaticDevelopmentTokenCannotOwnASession(t *testing.T) {
	r := startFull(t)
	f := start(t, func(c *Config) {
		c.Pairing = r.p
		c.Bindings = r.b
	})
	body, _ := json.Marshal(map[string]any{"origin": srcOrigin, "document": "doc-0001-aaaa", "manifest": json.RawMessage(manifest(t, r.clock.now(), nil))})
	status, _, _ := f.do(t, "POST", "/v1/session", opts{origin: extOrigin, auth: "Bearer " + f.token, ctype: "application/json", headers: map[string]string{csrfHeader: "1"}, body: body})
	if status != http.StatusForbidden {
		t.Fatalf("no client identity, no session: %d", status)
	}
}

func TestSessionRoutesRejectMalformedAndUnknownInput(t *testing.T) {
	r := startFull(t)
	_, a := r.pairWith(t, extOrigin, r.offer(t, extOrigin))
	access := a["access"].(string)
	for _, c := range []struct {
		path string
		body any
		want int
	}{
		{"/v1/session", map[string]any{"origin": srcOrigin}, 400},
		{"/v1/session", map[string]any{"origin": srcOrigin, "document": "d", "manifest": map[string]any{}}, 403},
		{"/v1/session", map[string]any{"extra": 1, "origin": srcOrigin, "document": "doc-0001-aaaa", "manifest": map[string]any{}}, 400},
		{"/v1/session/end", map[string]any{"session_id": 5}, 400},
		{"/v1/session/renew", map[string]any{"session_id": "bs_nope"}, 404},
	} {
		if status, out := r.call(t, "POST", c.path, extOrigin, access, c.body); status != c.want {
			t.Errorf("%s %v: %d %v", c.path, c.body, status, out)
		}
	}
	if status, _ := r.call(t, "GET", "/v1/session/bs_nope", extOrigin, access, nil); status != 404 {
		t.Errorf("unknown: %d", status)
	}
	if status, _ := r.call(t, "PUT", "/v1/session", extOrigin, access, nil); status != 405 {
		t.Errorf("method: %d", status)
	}
}
