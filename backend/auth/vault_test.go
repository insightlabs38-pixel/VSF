package auth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/insightlabs38-pixel/vsf/backend/storage/secret"
)

const token = "canary-bearer-7f3a9c21d4e85b60"

func TestFileVaultSealsSecretsUnderOpaqueNamesWithPrivatePermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "vault")
	v, err := NewFileVault(dir, secret.Generate())
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Put("acme", "gr_abc/access", []byte(token)); err != nil {
		t.Fatal(err)
	}
	got, err := v.Get("acme", "gr_abc/access")
	if err != nil || string(got) != token {
		t.Fatalf("%q %v", got, err)
	}
	var raw []byte
	var modes []os.FileMode
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		modes = append(modes, info.Mode().Perm())
		if !info.IsDir() {
			b, _ := os.ReadFile(p)
			raw = append(append(raw, []byte(p)...), b...)
		}
		return nil
	})
	if bytes.Contains(raw, []byte(token)) || bytes.Contains(raw, []byte("gr_abc")) || bytes.Contains(raw, []byte("acme")) {
		t.Fatal("neither the secret nor its name nor the account may appear on disk")
	}
	for _, m := range modes {
		if m&0o077 != 0 {
			t.Fatalf("others can reach the vault: %o", m)
		}
	}
	if _, err := v.Get("acme", "gr_abc/refresh"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("%v", err)
	}
	if _, err := v.Get("globex", "gr_abc/access"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("another account sees nothing: %v", err)
	}
	if err := v.Delete("acme", "gr_abc/access"); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Get("acme", "gr_abc/access"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("%v", err)
	}
	if err := v.Put("acme", "bad name", nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("%v", err)
	}
}

func TestFileVaultRefusesTamperedSwappedOrForeignFiles(t *testing.T) {
	dir := t.TempDir()
	keys := secret.Generate()
	v, _ := NewFileVault(dir, keys)
	_ = v.Put("acme", "one", []byte("first-value-1234"))
	_ = v.Put("acme", "two", []byte("second-value-5678"))
	p1, p2 := v.path("acme", "one"), v.path("acme", "two")
	b1, _ := os.ReadFile(p1)
	b2, _ := os.ReadFile(p2)
	_ = os.WriteFile(p1, b2, 0o600)
	if _, err := v.Get("acme", "one"); !errors.Is(err, ErrUntrusted) {
		t.Fatalf("a file moved under another name must not open: %v", err)
	}
	b2[len(b2)-1] ^= 1
	_ = os.WriteFile(p2, b2, 0o600)
	if _, err := v.Get("acme", "two"); !errors.Is(err, ErrUntrusted) {
		t.Fatalf("%v", err)
	}
	_ = os.WriteFile(p1, b1, 0o600)
	other, _ := NewFileVault(dir, secret.Generate())
	if _, err := other.Get("acme", "one"); !errors.Is(err, ErrNotFound) && !errors.Is(err, ErrUntrusted) {
		t.Fatalf("another keyring gets nothing useful: %v", err)
	}
}

type fakeRunner struct {
	store    map[string][]byte
	argv     [][]string
	stdins   [][]byte
	down     bool
	lastExit int
}

func (f *fakeRunner) Run(_ context.Context, stdin []byte, name string, args ...string) ([]byte, int, error) {
	f.argv = append(f.argv, append([]string{name}, args...))
	f.stdins = append(f.stdins, stdin)
	if f.down {
		return nil, 0, errors.New("dbus: no session bus")
	}
	if f.store == nil {
		f.store = map[string][]byte{}
	}
	var rest []string
	for _, a := range args[1:] {
		if !strings.HasPrefix(a, "--") {
			rest = append(rest, a)
		}
	}
	k := strings.Join(rest, " ")
	switch args[0] {
	case "store":
		f.store[k] = append([]byte(nil), stdin...)
	case "lookup":
		if v, ok := f.store[k]; ok {
			return append(v, '\n'), 0, nil
		}
		return nil, 1, nil
	case "clear":
		delete(f.store, k)
	}
	return nil, 0, nil
}

func TestSecretServiceKeepsTheSecretOffTheCommandLine(t *testing.T) {
	f := &fakeRunner{}
	k := &SecretService{Run: f}
	raw := []byte("binary\x00value\nwith newline " + token)
	if err := k.Put("acme", "gr_abc/access", raw); err != nil {
		t.Fatal(err)
	}
	got, err := k.Get("acme", "gr_abc/access")
	if err != nil || !bytes.Equal(got, raw) {
		t.Fatalf("%q %v", got, err)
	}
	for _, argv := range f.argv {
		if strings.Contains(strings.Join(argv, " "), token) || strings.Contains(strings.Join(argv, " "), base64.StdEncoding.EncodeToString(raw)) {
			t.Fatalf("the secret is on a command line, visible to every local user: %v", argv)
		}
	}
	if _, err := k.Get("acme", "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("%v", err)
	}
	if err := k.Delete("acme", "gr_abc/access"); err != nil {
		t.Fatal(err)
	}
	if _, err := k.Get("acme", "gr_abc/access"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("%v", err)
	}
}

func TestAnUnreachableKeychainStoresNothingUnlessAFallbackWasAllowed(t *testing.T) {
	down := &SecretService{Run: &fakeRunner{down: true}}
	fb, _ := NewFileVault(t.TempDir(), secret.Generate())
	strict := &Chain{Primary: down, Fallback: fb}
	if err := strict.Put("acme", "n", []byte(token)); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("%v", err)
	}
	if _, err := fb.Get("acme", "n"); !errors.Is(err, ErrNotFound) {
		t.Fatal("nothing may land in the fallback unless it was allowed")
	}
	if _, err := strict.Get("acme", "n"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("%v", err)
	}
	lenient := &Chain{Primary: down, Fallback: fb, AllowFallback: true}
	if err := lenient.Put("acme", "n", []byte(token)); err != nil {
		t.Fatal(err)
	}
	if got, err := lenient.Get("acme", "n"); err != nil || string(got) != token {
		t.Fatalf("%q %v", got, err)
	}
	if err := lenient.Delete("acme", "n"); err == nil || !errors.Is(err, ErrUnavailable) {
		if _, gerr := fb.Get("acme", "n"); !errors.Is(gerr, ErrNotFound) {
			t.Fatal("the fallback copy is removed too")
		}
	}
}

func credsRig(t *testing.T) (*Credentials, *Broker, *clock, Grant, *bytes.Buffer) {
	b, cl, audit := newBroker(t)
	b.Vault = NewMemVault()
	g, err := b.Issue(grant("alice"))
	if err != nil {
		t.Fatal(err)
	}
	_ = b.Vault.Put(g.Account, g.ID+"/access", []byte(token))
	return &Credentials{Broker: b, Redact: NewRedactor()}, b, cl, g, audit
}

func leaseCheck(g Grant) Check {
	return Check{GrantID: g.ID, Owner: "alice", Origin: g.Source.Origin, Audience: g.Audience, Scopes: []string{"mail.read"}}
}

func TestALeaseWorksForItsOriginAndOnlyForItsLifetimeAndUses(t *testing.T) {
	c, _, cl, g, _ := credsRig(t)
	l, err := c.Lease(leaseCheck(g), "access", time.Minute, 2)
	if err != nil {
		t.Fatal(err)
	}
	var seen []byte
	var held []byte
	if err := l.Inject(g.Source.Origin, func(s []byte) error { held = s; seen = append(seen, s...); return nil }); err != nil || string(seen) != token {
		t.Fatalf("%q %v", seen, err)
	}
	if !bytes.Equal(held, make([]byte, len(held))) {
		t.Fatal("the copy handed to the caller is wiped when the call returns")
	}
	for _, o := range []string{"https://evil.example.com", "http://mail.example.com", "https://mail.example.com.evil.net", "https://sub.mail.example.com", ""} {
		if err := l.Inject(o, func([]byte) error { t.Fatal("called"); return nil }); !errors.Is(err, ErrDenied) {
			t.Fatalf("%q: %v", o, err)
		}
	}
	if err := l.Inject(g.Source.Origin, func([]byte) error { return nil }); err != nil {
		t.Fatalf("refused attempts do not use up the lease: %v", err)
	}
	if err := l.Inject(g.Source.Origin, func([]byte) error { t.Fatal("called"); return nil }); !errors.Is(err, ErrDenied) {
		t.Fatalf("a lease with no uses left is spent: %v", err)
	}
	l2, _ := c.Lease(leaseCheck(g), "access", time.Minute, 5)
	cl.t = cl.t.Add(time.Minute)
	if err := l2.Inject(g.Source.Origin, func([]byte) error { t.Fatal("called"); return nil }); !errors.Is(err, ErrExpired) {
		t.Fatalf("%v", err)
	}
	if l2.value != nil {
		t.Fatal("an expired lease wipes its secret")
	}
}

func TestLeaseRequestsAreCheckedLikeAnyUseOfTheGrant(t *testing.T) {
	c, b, _, g, _ := credsRig(t)
	for name, chk := range map[string]Check{
		"another caller":   {GrantID: g.ID, Owner: "mallory", Origin: g.Source.Origin, Audience: g.Audience},
		"another audience": {GrantID: g.ID, Owner: "alice", Origin: g.Source.Origin, Audience: "https://x.example.com"},
		"another scope":    {GrantID: g.ID, Owner: "alice", Origin: g.Source.Origin, Audience: g.Audience, Scopes: []string{"mail.admin"}},
	} {
		if l, err := c.Lease(chk, "access", time.Minute, 1); err == nil || l != nil {
			t.Fatalf("%s got a lease", name)
		}
	}
	for _, bad := range []struct {
		name string
		ttl  time.Duration
		uses int
		kind string
	}{{"long ttl", time.Hour, 1, "access"}, {"zero uses", time.Minute, 0, "access"}, {"too many uses", time.Minute, 99, "access"}, {"odd secret", time.Minute, 1, "../etc"}} {
		if _, err := c.Lease(leaseCheck(g), bad.kind, bad.ttl, bad.uses); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%s: %v", bad.name, err)
		}
	}
	if _, err := c.Lease(leaseCheck(g), "refresh", time.Minute, 1); !errors.Is(err, ErrReauth) {
		t.Fatalf("a missing secret asks for sign-in again: %v", err)
	}
	short := grant("alice")
	short.ExpiresAt = epoch.Add(30 * time.Second)
	g2, _ := b.Issue(short)
	_ = b.Vault.Put(g2.Account, g2.ID+"/access", []byte(token))
	l, err := c.Lease(leaseCheck(g2), "access", 5*time.Minute, 1)
	if err != nil || !l.expires.Equal(short.ExpiresAt) {
		t.Fatalf("a lease never outlives its grant: %v", err)
	}
}

func TestRevokingTheGrantEndsEveryLeaseAndWipesTheVault(t *testing.T) {
	c, b, _, g, _ := credsRig(t)
	l, _ := c.Lease(leaseCheck(g), "access", time.Minute, 5)
	if err := b.Revoke(g.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	if err := l.Inject(g.Source.Origin, func([]byte) error { t.Fatal("called"); return nil }); !errors.Is(err, ErrDenied) {
		t.Fatalf("%v", err)
	}
	if _, err := b.Vault.Get(g.Account, g.ID+"/access"); !errors.Is(err, ErrNotFound) {
		t.Fatal("the vault no longer holds the secret")
	}
	if _, err := c.Lease(leaseCheck(g), "access", time.Minute, 1); !errors.Is(err, ErrDenied) {
		t.Fatalf("%v", err)
	}
}

func TestALeaseCannotBePrintedMarshalledOrLoggedWithItsValue(t *testing.T) {
	c, _, _, g, _ := credsRig(t)
	l, _ := c.Lease(leaseCheck(g), "access", time.Minute, 1)
	js, _ := json.Marshal(struct{ L *Lease }{l})
	var logs bytes.Buffer
	slog.New(slog.NewJSONHandler(&logs, nil)).Info("lease", "l", l)
	out := fmt.Sprintf("%v|%+v|%#v|%s|%q|%x|%d", l, l, l, l, l, l, l) + string(js) + logs.String()
	if strings.Contains(out, token) || strings.Contains(out, base64.StdEncoding.EncodeToString([]byte(token))) {
		t.Fatalf("a lease leaks its value: %s", out)
	}
}

func TestRedactionCoversTheFormsASecretTakesInLogsAndErrors(t *testing.T) {
	r := NewRedactor()
	if err := r.Add([]byte("abc")); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a secret too short to tell apart from text is refused: %v", err)
	}
	s := []byte(`tok/en+with"odd chars&=1`)
	_ = r.Add(s)
	for name, form := range map[string]string{
		"raw":        string(s),
		"base64":     base64.StdEncoding.EncodeToString(s),
		"base64 url": base64.URLEncoding.EncodeToString(s),
		"raw base64": base64.RawStdEncoding.EncodeToString(s),
		"hex":        fmt.Sprintf("%x", s),
		"query":      "tok%2Fen%2Bwith%22odd+chars%26%3D1",
		"json":       `tok/en+with\"odd chars&=1`,
	} {
		line := "Authorization: Bearer " + form + " end"
		if got := r.String(line); strings.Contains(got, form) || !strings.Contains(got, redacted) {
			t.Fatalf("%s survived: %s", name, got)
		}
	}
	err := r.Err(fmt.Errorf("upstream said %s: %w", s, io.ErrUnexpectedEOF))
	if strings.Contains(err.Error(), string(s)) || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("an error is cleaned yet still matches its cause: %v", err)
	}
	var out bytes.Buffer
	w := r.Writer(&out)
	half := len(s) / 2
	_, _ = w.Write([]byte("log: "))
	_, _ = w.Write(s[:half])
	_, _ = w.Write(append(append([]byte(nil), s[half:]...), '\n'))
	_, _ = w.Write([]byte("tail " + string(s)))
	_ = w.Close()
	if strings.Contains(out.String(), string(s)) || strings.Count(out.String(), redacted) != 2 {
		t.Fatalf("a secret split across writes is still caught: %q", out.String())
	}
}

func TestACredentialReachesOnlyTheSourceAndNoLogAuditOrVaultFile(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got = r.Header.Get("Authorization") }))
	defer srv.Close()
	dir := t.TempDir()
	fv, _ := NewFileVault(filepath.Join(dir, "vault"), secret.Generate())
	b, cl, audit := newBroker(t)
	b.Vault = fv
	_ = cl
	g, _ := b.Issue(grant("alice"))
	_ = fv.Put(g.Account, g.ID+"/access", []byte(token))
	var logs bytes.Buffer
	red := NewRedactor()
	lw := red.Writer(&logs)
	logger := slog.New(slog.NewTextHandler(lw, nil))
	c := &Credentials{Broker: b, Redact: red}
	l, err := c.Lease(leaseCheck(g), "access", time.Minute, 2)
	if err != nil {
		t.Fatal(err)
	}
	err = l.Inject(g.Source.Origin, func(s []byte) error {
		req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
		req.Header.Set("Authorization", "Bearer "+string(s))
		logger.Info("calling", "authorization", req.Header.Get("Authorization"), "lease", l)
		_, err := srv.Client().Do(req)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = l.Inject("https://evil.example.com", func([]byte) error { return nil })
	_ = lw.Close()
	if got != "Bearer "+token {
		t.Fatalf("the source receives the credential: %q", got)
	}
	var disk []byte
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			x, _ := os.ReadFile(p)
			disk = append(disk, x...)
		}
		return nil
	})
	for name, blob := range map[string]string{"log": logs.String(), "audit": audit.String(), "vault files": string(disk)} {
		if strings.Contains(blob, token) {
			t.Fatalf("the credential reached the %s", name)
		}
	}
	if !strings.Contains(logs.String(), redacted) || !strings.Contains(audit.String(), `"event":"credential_revealed"`) {
		t.Fatalf("the use is recorded without the value:\n%s\n%s", logs.String(), audit.String())
	}
}
