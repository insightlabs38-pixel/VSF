package pairing

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	ext   = "chrome-extension://lgnekpogndcjggibkbaekekanfonjhie"
	other = "chrome-extension://aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func rig(t *testing.T, mutate func(*Config)) (*Service, *clock) {
	t.Helper()
	c := &clock{t: time.Unix(1_800_000_000, 0)}
	cfg := Config{Peers: []string{ext, other}, Now: c.now}
	if mutate != nil {
		mutate(&cfg)
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return s, c
}

const nonceC = "00112233445566778899aabbccddeeff"

// paired runs the whole exchange for peer and returns the credentials.
func paired(t *testing.T, s *Service, peer string) Credentials {
	t.Helper()
	b, err := s.Begin(peer, nonceC)
	if err != nil {
		t.Fatal(err)
	}
	code, err := s.Approve(b.PairID)
	if err != nil {
		t.Fatal(err)
	}
	creds, err := s.Finish(b.PairID, peer, Proof(code, b.PairID, peer, nonceC, b.NonceS))
	if err != nil {
		t.Fatal(err)
	}
	return creds
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	if Code(err) != code {
		t.Fatalf("error %v, want code %q", err, code)
	}
}

func TestExplicitApprovalIsRequiredAndTheCodeOpensExactlyOnePairing(t *testing.T) {
	s, _ := rig(t, nil)
	b, err := s.Begin(ext, nonceC)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Pending(); len(got) != 1 || got[0].Peer != ext || got[0].Approved {
		t.Fatalf("pending %+v", got)
	}
	_, err = s.Finish(b.PairID, ext, strings.Repeat("0", 64))
	wantCode(t, err, "pairing.not_approved")
	code, err := s.Approve(b.PairID)
	if err != nil {
		t.Fatal(err)
	}
	if len(code) != 10 || strings.Trim(code, alphabet) != "" {
		t.Fatalf("code %q", code)
	}
	_, err = s.Approve(b.PairID)
	wantCode(t, err, "pairing.already_approved")
	proof := Proof(code, b.PairID, ext, nonceC, b.NonceS)
	creds, err := s.Finish(b.PairID, ext, proof)
	if err != nil {
		t.Fatal(err)
	}
	if p, err := s.Authenticate(ext, creds.Access); err != nil || p.Peer != ext || p.PairingID != creds.PairingID {
		t.Fatalf("authenticate %+v %v", p, err)
	}
	_, err = s.Finish(b.PairID, ext, proof)
	wantCode(t, err, "pairing.unknown")
	if len(s.Pending()) != 0 {
		t.Fatal("a finished pairing must not stay pending")
	}
}

func TestOnlyAllowedPeersCanAskAndBadRequestsAreRefused(t *testing.T) {
	s, _ := rig(t, nil)
	for _, peer := range []string{"https://evil.test", "http://127.0.0.1:5", "null", "", "chrome-extension://bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"} {
		_, err := s.Begin(peer, nonceC)
		wantCode(t, err, "pairing.peer_not_allowed")
	}
	for _, n := range []string{"", "short", strings.Repeat("g", 32), strings.Repeat("a", 34)} {
		_, err := s.Begin(ext, n)
		wantCode(t, err, "pairing.bad_request")
	}
	if len(s.Pending()) != 0 {
		t.Fatal("refused requests left state behind")
	}
	if _, err := New(Config{Peers: []string{"*"}}); err == nil {
		t.Fatal("wildcard peers must be refused")
	}
}

func TestWrongProofsBurnThePairingAndTheRightOneNoLongerWorks(t *testing.T) {
	s, _ := rig(t, nil)
	b, _ := s.Begin(ext, nonceC)
	code, _ := s.Approve(b.PairID)
	bad := Proof("AAAAAAAAAA", b.PairID, ext, nonceC, b.NonceS)
	_, err := s.Finish(b.PairID, ext, bad)
	wantCode(t, err, "pairing.bad_proof")
	_, err = s.Finish(b.PairID, ext, bad)
	wantCode(t, err, "pairing.bad_proof")
	_, err = s.Finish(b.PairID, ext, bad)
	wantCode(t, err, "pairing.attempts_exceeded")
	_, err = s.Finish(b.PairID, ext, Proof(code, b.PairID, ext, nonceC, b.NonceS))
	wantCode(t, err, "pairing.unknown")
}

func TestAProofIsBoundToItsExchangePeerAndNonces(t *testing.T) {
	s, _ := rig(t, func(c *Config) { c.MaxAttempts = 100 })
	b, _ := s.Begin(ext, nonceC)
	code, _ := s.Approve(b.PairID)
	cases := map[string]string{
		"other peer":   Proof(code, b.PairID, other, nonceC, b.NonceS),
		"other client": Proof(code, b.PairID, ext, strings.Repeat("11", 16), b.NonceS),
		"other server": Proof(code, b.PairID, ext, nonceC, strings.Repeat("22", 16)),
		"other code":   Proof("AAAAAAAAAA", b.PairID, ext, nonceC, b.NonceS),
		"not hex":      "zz",
		"empty":        "",
	}
	for name, proof := range cases {
		if _, err := s.Finish(b.PairID, ext, proof); Code(err) != "pairing.bad_proof" {
			t.Fatalf("%s: %v", name, err)
		}
	}
	_, err := s.Finish(b.PairID, other, Proof(code, b.PairID, other, nonceC, b.NonceS))
	wantCode(t, err, "pairing.peer_mismatch")
	if _, err := s.Finish(b.PairID, ext, Proof(code, b.PairID, ext, nonceC, b.NonceS)); err != nil {
		t.Fatalf("the genuine proof still works after refused attempts: %v", err)
	}
}

func TestCodesAndRequestsExpire(t *testing.T) {
	s, c := rig(t, nil)
	b, _ := s.Begin(ext, nonceC)
	code, _ := s.Approve(b.PairID)
	c.advance(2*time.Minute + time.Second)
	_, err := s.Finish(b.PairID, ext, Proof(code, b.PairID, ext, nonceC, b.NonceS))
	wantCode(t, err, "pairing.unknown")
	if len(s.Pending()) != 0 {
		t.Fatal("expired request still listed")
	}
	_, err = s.Approve(b.PairID)
	wantCode(t, err, "pairing.unknown")
}

func TestFloodingIsLimited(t *testing.T) {
	s, c := rig(t, nil)
	for i := 0; i < 4; i++ {
		if _, err := s.Begin(ext, nonceC); err != nil {
			t.Fatal(err)
		}
	}
	_, err := s.Begin(ext, nonceC)
	wantCode(t, err, "pairing.too_many_pending")
	c.advance(3 * time.Minute)
	for i := 0; i < 6; i++ {
		if _, err := s.Begin(other, nonceC); err != nil {
			t.Fatal(err)
		}
		c.advance(time.Millisecond)
		for _, p := range s.Pending() {
			s.Deny(p.ID)
		}
	}
	_, err = s.Begin(other, nonceC)
	wantCode(t, err, "pairing.rate_limited")
	if _, err := s.Begin(ext, nonceC); err != nil {
		t.Fatalf("one peer's flood must not block another: %v", err)
	}
}

func TestATokenWorksOnlyForItsOwnPeerAndUntilItExpires(t *testing.T) {
	s, c := rig(t, nil)
	creds := paired(t, s, ext)
	_, err := s.Authenticate(other, creds.Access)
	wantCode(t, err, "auth.peer_mismatch")
	for _, bad := range []string{"", "vsf_a_", "vsf_a_" + strings.Repeat("0", 64), creds.Refresh, strings.TrimPrefix(creds.Access, "vsf_a_")} {
		_, err = s.Authenticate(ext, bad)
		wantCode(t, err, "auth.invalid")
	}
	c.advance(15*time.Minute - time.Second)
	if _, err := s.Authenticate(ext, creds.Access); err != nil {
		t.Fatal(err)
	}
	c.advance(2 * time.Second)
	_, err = s.Authenticate(ext, creds.Access)
	wantCode(t, err, "auth.expired")
}

func TestRenewalRotatesAndReuseOfAnOldRefreshRevokesThePairing(t *testing.T) {
	s, c := rig(t, nil)
	first := paired(t, s, ext)
	c.advance(20 * time.Minute)
	second, err := s.Renew(ext, first.Refresh)
	if err != nil {
		t.Fatal(err)
	}
	if second.Access == first.Access || second.Refresh == first.Refresh || second.PairingID != first.PairingID {
		t.Fatal("renewal must issue new secrets for the same pairing")
	}
	if _, err := s.Authenticate(ext, second.Access); err != nil {
		t.Fatal(err)
	}
	_, err = s.Authenticate(ext, first.Access)
	wantCode(t, err, "auth.invalid")
	_, err = s.Renew(other, second.Refresh)
	wantCode(t, err, "auth.peer_mismatch")
	if _, err := s.Authenticate(ext, second.Access); err != nil {
		t.Fatal("a refused foreign renewal must not revoke the pairing")
	}
	_, err = s.Renew(ext, first.Refresh)
	wantCode(t, err, "auth.refresh_reused")
	_, err = s.Authenticate(ext, second.Access)
	wantCode(t, err, "auth.revoked")
	_, err = s.Renew(ext, second.Refresh)
	wantCode(t, err, "auth.revoked")
}

func TestRefreshTokensExpireAndMalformedOnesAreRefused(t *testing.T) {
	s, c := rig(t, nil)
	creds := paired(t, s, ext)
	for _, bad := range []string{"", "vsf_r_", "vsf_r_" + strings.Repeat("1", 64), creds.Access} {
		_, err := s.Renew(ext, bad)
		wantCode(t, err, "auth.invalid")
	}
	c.advance(31 * 24 * time.Hour)
	_, err := s.Renew(ext, creds.Refresh)
	wantCode(t, err, "auth.expired")
}

func TestRevocationIsImmediateAndPairingAgainSupersedesTheOld(t *testing.T) {
	s, _ := rig(t, nil)
	a := paired(t, s, ext)
	if err := s.Revoke(a.PairingID); err != nil {
		t.Fatal(err)
	}
	_, err := s.Authenticate(ext, a.Access)
	wantCode(t, err, "auth.revoked")
	wantCode(t, s.Revoke("nope"), "pairing.unknown")
	if err := s.Revoke(a.PairingID); err != nil {
		t.Fatal("revoking twice is harmless")
	}
	b := paired(t, s, ext)
	c := paired(t, s, ext)
	_, err = s.Authenticate(ext, b.Access)
	wantCode(t, err, "auth.revoked")
	if _, err := s.Authenticate(ext, c.Access); err != nil {
		t.Fatal(err)
	}
	d := paired(t, s, other)
	if _, err := s.Authenticate(ext, c.Access); err != nil {
		t.Fatal("pairing another peer must not end this one")
	}
	reasons := map[string]string{}
	for _, i := range s.List() {
		reasons[i.ID] = i.Reason
	}
	if reasons[a.PairingID] != "revoked" || reasons[b.PairingID] != "superseded" || reasons[d.PairingID] != "" {
		t.Fatalf("reasons %v", reasons)
	}
}

func TestStateSurvivesARestartHoldsNoUsableSecretAndFailsClosedWhenDamaged(t *testing.T) {
	dir := t.TempDir()
	c := &clock{t: time.Unix(1_800_000_000, 0)}
	cfg := Config{Dir: dir, Peers: []string{ext}, Now: c.now}
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	creds := paired(t, s, ext)
	renewed, err := s.Renew(ext, creds.Refresh)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "pairings.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{creds.Access, creds.Refresh, renewed.Access, renewed.Refresh, strings.TrimPrefix(renewed.Access, "vsf_a_")} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("the pairing file holds a usable token")
		}
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", info.Mode())
	}
	s2, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s2.Authenticate(ext, renewed.Access); err != nil {
		t.Fatalf("authentication after restart: %v", err)
	}
	_, err = s2.Renew(ext, creds.Refresh)
	wantCode(t, err, "auth.refresh_reused")
	var d struct {
		Pairings []map[string]any `json:"pairings"`
	}
	raw, _ = os.ReadFile(path)
	if err := json.Unmarshal(raw, &d); err != nil || len(d.Pairings) != 1 || d.Pairings[0]["revoked"] != true {
		t.Fatalf("revocation not persisted: %s", raw)
	}
	for name, content := range map[string]string{"garbage": "{not json", "version": `{"version":2,"pairings":[]}`, "hollow": `{"version":1,"pairings":[{"id":""}]}`} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := New(cfg); err == nil {
			t.Fatalf("%s: a damaged file must stop the service, not reset it", name)
		}
	}
}

func TestNoTemporaryFilesAreLeftBehind(t *testing.T) {
	dir := t.TempDir()
	s, err := New(Config{Dir: dir, Peers: []string{ext}})
	if err != nil {
		t.Fatal(err)
	}
	creds := paired(t, s, ext)
	for i := 0; i < 3; i++ {
		creds, err = s.Renew(ext, creds.Refresh)
		if err != nil {
			t.Fatal(err)
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != "pairings.json" {
		t.Fatalf("directory %v", entries)
	}
}

func TestConcurrentUseIsRaceFree(t *testing.T) {
	s, _ := rig(t, nil)
	creds := paired(t, s, ext)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				_, _ = s.Authenticate(ext, creds.Access)
				_ = s.List()
				_ = s.Pending()
			}
		}()
	}
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if b, err := s.Begin(other, nonceC); err == nil {
				s.Deny(b.PairID)
			}
		}()
	}
	wg.Wait()
}

func TestProofMatchesTheCrossLanguageVector(t *testing.T) {
	got := Proof("abcde-fghjk", "p1", "chrome-extension://x", "n1", "n2")
	if _, err := hex.DecodeString(got); err != nil || len(got) != 64 {
		t.Fatalf("proof %q", got)
	}
	if got != Proof("ABCDEFGHJK", "p1", "chrome-extension://x", "n1", "n2") {
		t.Fatal("a typed code with a dash and lower case must give the same proof")
	}
	if got == Proof("ABCDEFGHJK", "p1", "chrome-extension://x", "n1", "n3") {
		t.Fatal("the nonces are part of the proof")
	}
}

func TestProofMatchesTheSharedVector(t *testing.T) {
	raw, err := os.ReadFile("../../fixtures/pairing/proof-vector.json")
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Cases []map[string]string `json:"cases"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil || len(wire.Cases) < 3 {
		t.Fatalf("vector: %v", err)
	}
	for _, c := range wire.Cases {
		if got := Proof(c["code"], c["pair_id"], c["peer"], c["nonce_c"], c["nonce_s"]); got != c["proof"] {
			t.Fatalf("%s: got %s want %s", c["name"], got, c["proof"])
		}
	}
}
