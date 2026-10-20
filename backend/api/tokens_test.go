package api

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/insightlabs38-pixel/vsf/backend/execution"
)

type dir struct {
	mu sync.Mutex
	m  map[string]execution.Caller
}

func (d *dir) Lookup(id string) (execution.Caller, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	c, ok := d.m[id]
	return c, ok
}

func newDir(ids ...string) *dir {
	d := &dir{m: map[string]execution.Caller{}}
	for _, id := range ids {
		d.m[id] = execution.Caller{ID: id}
	}
	return d
}

type tclock struct{ t time.Time }

func (c *tclock) now() time.Time { return c.t }

func TestCredentialsWorkOnlyOnTheSurfaceTheyWereIssuedFor(t *testing.T) {
	a, _ := NewTokenAuth(newDir("ada", "bob"), nil, nil)
	surfaces := []Surface{SurfaceREST, SurfaceCLI, SurfaceMCP}
	toks := map[Surface]string{}
	for _, s := range surfaces {
		tok, _, err := a.Issue("ada", s, time.Hour, "")
		if err != nil {
			t.Fatal(err)
		}
		toks[s] = tok
	}
	for _, issued := range surfaces {
		for _, used := range surfaces {
			p, err := a.Authenticate(used, toks[issued])
			switch {
			case issued == used && (err != nil || p.Caller != "ada" || p.Surface != used):
				t.Fatalf("%s on %s: %v %+v", issued, used, err, p)
			case issued != used && (err == nil || err.Code != CodeWrongSurface):
				t.Fatalf("%s credential accepted on %s", issued, used)
			}
		}
	}
	if toks[SurfaceREST] == toks[SurfaceCLI] {
		t.Fatal("credentials must be independent")
	}
}

func TestACredentialIsNeverTheCallersIdentityOrGuessable(t *testing.T) {
	a, _ := NewTokenAuth(newDir("ada", "bob"), nil, nil)
	ta, ia, _ := a.Issue("ada", SurfaceREST, time.Hour, "laptop")
	tb, _, _ := a.Issue("bob", SurfaceREST, time.Hour, "")
	pa, _ := a.Authenticate(SurfaceREST, ta)
	pb, _ := a.Authenticate(SurfaceREST, tb)
	if pa.Caller != "ada" || pb.Caller != "bob" {
		t.Fatalf("%+v %+v", pa, pb)
	}
	if strings.Contains(ta, "ada") || pa.Credential != ia.ID || strings.Contains(pa.Credential, strings.TrimPrefix(ta, "vsf_rest_")) {
		t.Fatalf("the principal must carry the credential id, never the secret: %+v", pa)
	}
	for _, bad := range []string{"", ta + "x", ta[:len(ta)-1], strings.ToUpper(ta), " " + ta, tb + ta, "vsf_rest_", strings.Repeat("a", maxCredentialLength+1), "Bearer " + ta} {
		if _, err := a.Authenticate(SurfaceREST, bad); err == nil || err.Code != CodeUnauthenticated {
			t.Fatalf("%q accepted", bad[:min(len(bad), 12)])
		}
	}
}

func TestExpiryRevocationAndCallerRemovalEndAccessAtOnce(t *testing.T) {
	d := newDir("ada", "bob")
	clk := &tclock{t: time.UnixMilli(1_000_000)}
	a, _ := NewTokenAuth(d, clk.now, nil)
	tok, info, _ := a.Issue("ada", SurfaceCLI, time.Minute, "")
	if _, err := a.Authenticate(SurfaceCLI, tok); err != nil {
		t.Fatal(err)
	}
	clk.t = clk.t.Add(time.Minute)
	if _, err := a.Authenticate(SurfaceCLI, tok); err == nil || err.Reason != "credential_inactive" {
		t.Fatalf("an expired credential must not work: %v", err)
	}
	clk.t = time.UnixMilli(1_000_000)
	if !a.Revoke(info.ID) {
		t.Fatal("revoke")
	}
	if _, err := a.Authenticate(SurfaceCLI, tok); err == nil {
		t.Fatal("a revoked credential must not work")
	}
	if a.Revoke(info.ID) {
		t.Fatal("revoking twice must report nothing to do")
	}
	t2, _, _ := a.Issue("bob", SurfaceMCP, time.Hour, "")
	delete(d.m, "bob")
	if _, err := a.Authenticate(SurfaceMCP, t2); err == nil || err.Reason != "caller_revoked" {
		t.Fatalf("a credential must not outlive its caller: %v", err)
	}
	a2, _ := NewTokenAuth(newDir("carol"), nil, nil)
	t3, _, _ := a2.Issue("carol", SurfaceREST, time.Hour, "")
	t4, _, _ := a2.Issue("carol", SurfaceMCP, time.Hour, "")
	if n := a2.RevokeCaller("carol"); n != 2 {
		t.Fatalf("revoked %d", n)
	}
	for _, c := range []struct {
		s Surface
		t string
	}{{SurfaceREST, t3}, {SurfaceMCP, t4}} {
		if _, err := a2.Authenticate(c.s, c.t); err == nil {
			t.Fatal("revoke-caller must end every surface")
		}
	}
}

func TestIssuingRefusesWhatCannotBeBoundHonestly(t *testing.T) {
	a, _ := NewTokenAuth(newDir("ada"), nil, nil)
	cases := map[string]func() error{
		"unknown caller":  func() error { _, _, e := a.Issue("mallory", SurfaceREST, time.Hour, ""); return e },
		"unknown surface": func() error { _, _, e := a.Issue("ada", "admin", time.Hour, ""); return e },
		"no ttl":          func() error { _, _, e := a.Issue("ada", SurfaceREST, 0, ""); return e },
		"ttl too long":    func() error { _, _, e := a.Issue("ada", SurfaceREST, MaxTokenTTL+time.Second, ""); return e },
		"control label":   func() error { _, _, e := a.Issue("ada", SurfaceREST, time.Hour, "a\nb"); return e },
		"long label":      func() error { _, _, e := a.Issue("ada", SurfaceREST, time.Hour, strings.Repeat("x", 65)); return e },
	}
	for n, f := range cases {
		if f() == nil {
			t.Errorf("%s was issued", n)
		}
	}
	for i := 0; i < MaxTokensPerCaller; i++ {
		if _, _, err := a.Issue("ada", SurfaceREST, time.Hour, ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := a.Issue("ada", SurfaceREST, time.Hour, ""); err == nil {
		t.Fatal("active credentials per caller must be bounded")
	}
}

func TestTheStoreHoldsDigestsOnlyAndSurvivesARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokens.json")
	d := newDir("ada")
	a, err := NewTokenAuth(d, nil, FileTokenStore{path})
	if err != nil {
		t.Fatal(err)
	}
	tok, info, _ := a.Issue("ada", SurfaceREST, time.Hour, "")
	raw, _ := os.ReadFile(path)
	secret := strings.TrimPrefix(tok, "vsf_rest_")
	if strings.Contains(string(raw), secret) || strings.Contains(string(raw), tok) {
		t.Fatal("the store must never contain a credential")
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode().Perm())
	}
	b, _ := NewTokenAuth(d, nil, FileTokenStore{path})
	if _, err := b.Authenticate(SurfaceREST, tok); err != nil {
		t.Fatalf("a credential must survive a restart: %v", err)
	}
	a.Revoke(info.ID)
	c, _ := NewTokenAuth(d, nil, FileTokenStore{path})
	if _, err := c.Authenticate(SurfaceREST, tok); err == nil {
		t.Fatal("a revocation must survive a restart")
	}
	_ = os.Chmod(path, 0o644)
	if _, err := NewTokenAuth(d, nil, FileTokenStore{path}); err == nil {
		t.Fatal("a store readable by others must be refused")
	}
	_ = os.Chmod(path, 0o600)
	_ = os.WriteFile(path, []byte("not json"), 0o600)
	if _, err := NewTokenAuth(d, nil, FileTokenStore{path}); err == nil {
		t.Fatal("a corrupt store must be refused, not treated as empty")
	}
}

func TestListingShowsNoSecrets(t *testing.T) {
	a, _ := NewTokenAuth(newDir("ada"), nil, nil)
	tok, _, _ := a.Issue("ada", SurfaceREST, time.Hour, "laptop")
	l := a.List("ada")
	if len(l) != 1 || l[0].Label != "laptop" || strings.Contains(l[0].ID, strings.TrimPrefix(tok, "vsf_rest_")) {
		t.Fatalf("%+v", l)
	}
	if len(a.List("bob")) != 0 {
		t.Fatal("another caller's credentials must not be listed")
	}
}
