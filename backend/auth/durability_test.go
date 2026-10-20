package auth

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/insightlabs38-pixel/vsf/backend/storage/secret"
)

func auditRec(n int) AuditRecord {
	return AuditRecord{At: epoch, Event: EventIssued, Grant: "gr_" + strings.Repeat("a", n), Reason: ReasonOK}
}

func TestAnAuditFileSurvivesATornTailRefusesTamperingAndRotatesAsOneChain(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")
	a, rec, err := OpenAuditFile(path)
	if err != nil || rec.Records != 0 {
		t.Fatalf("%+v %v", rec, err)
	}
	for i := 1; i <= 3; i++ {
		if err := a.Append(auditRec(i)); err != nil {
			t.Fatal(err)
		}
	}
	_ = a.Close()
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Fatalf("the log is private: %v", st.Mode())
	}
	raw, _ := os.ReadFile(path)
	if err := os.WriteFile(path, append(append([]byte(nil), raw...), []byte(`{"seq":4,"at":1,"event":"grant_iss`)...), 0o600); err != nil {
		t.Fatal(err)
	}
	a, rec, err = OpenAuditFile(path)
	if err != nil || rec.Records != 3 || rec.TruncatedBytes == 0 {
		t.Fatalf("a torn final line is cut off and reported: %+v %v", rec, err)
	}
	if err := a.Append(auditRec(4)); err != nil {
		t.Fatal(err)
	}
	if err := a.Rotate(filepath.Join(dir, "audit.1")); err != nil {
		t.Fatal(err)
	}
	if err := a.Append(auditRec(5)); err != nil {
		t.Fatal(err)
	}
	_, head := a.Head()
	_ = a.Close()
	old, _ := os.ReadFile(filepath.Join(dir, "audit.1"))
	n, last, seq, err := VerifyAuditFrom(bytes.NewReader(old), genesis, 0)
	if err != nil || n != 5 {
		t.Fatalf("the archived segment verifies on its own: %d %v", n, err)
	}
	cur, _ := os.ReadFile(path)
	n2, last2, _, err := VerifyAuditFrom(bytes.NewReader(cur), last, seq)
	if err != nil || n2 != 1 || last2 != head {
		t.Fatalf("and the new segment continues it: %d %v", n2, err)
	}
	if _, _, _, err := VerifyAuditFrom(bytes.NewReader(cur), genesis, 0); !errors.Is(err, ErrAuditTampered) {
		t.Fatalf("a segment cannot pass for the start of a chain: %v", err)
	}
	b, _, err := OpenAuditFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if seq, h := b.Head(); seq != 6 || h != head {
		t.Fatalf("reopening continues the chain: %d", seq)
	}
	_ = b.Close()
	lines := bytes.Split(bytes.TrimSpace(old), []byte("\n"))
	tampered := bytes.Join([][]byte{lines[0], bytes.Replace(lines[1], []byte("gr_aa"), []byte("gr_zz"), 1), lines[2], lines[3], lines[4]}, []byte("\n"))
	tp := filepath.Join(dir, "tampered.log")
	_ = os.WriteFile(tp, append(tampered, '\n'), 0o600)
	if _, _, err := OpenAuditFile(tp); !errors.Is(err, ErrAuditTampered) {
		t.Fatalf("damage that is not a torn tail is tampering and the log is not opened: %v", err)
	}
}

type brokenFile struct {
	tmpFile
	failWrite, failSync bool
}

func (f brokenFile) Write(p []byte) (int, error) {
	if f.failWrite {
		_, _ = f.tmpFile.Write(p[:len(p)/2])
		return len(p) / 2, errors.New("no space left on device")
	}
	return f.tmpFile.Write(p)
}

func (f brokenFile) Sync() error {
	if f.failSync {
		return errors.New("input/output error")
	}
	return f.tmpFile.Sync()
}

func vaultWith(t *testing.T) (*FileVault, string) {
	t.Helper()
	dir := t.TempDir()
	v, err := NewFileVault(filepath.Join(dir, "v"), secret.Generate())
	if err != nil {
		t.Fatal(err)
	}
	return v, dir
}

func leftovers(t *testing.T, v *FileVault) int {
	n := 0
	_ = filepath.Walk(v.Dir, func(p string, i os.FileInfo, _ error) error {
		if i != nil && strings.HasPrefix(i.Name(), ".put-") {
			n++
		}
		return nil
	})
	return n
}

func TestAFileVaultReplacementThatFailsAtAnyStepKeepsTheOldSecretAndLeavesNothingBehind(t *testing.T) {
	const old, fresh = "old-secret-value-1111", "new-secret-value-2222"
	steps := map[string]func(*FileVault){
		"disk full while writing": func(v *FileVault) {
			real := v.fs.createTemp
			v.fs.createTemp = func(d, p string) (tmpFile, error) {
				f, e := real(d, p)
				return brokenFile{tmpFile: f, failWrite: true}, e
			}
		},
		"sync failure": func(v *FileVault) {
			real := v.fs.createTemp
			v.fs.createTemp = func(d, p string) (tmpFile, error) {
				f, e := real(d, p)
				return brokenFile{tmpFile: f, failSync: true}, e
			}
		},
		"interrupted replacement": func(v *FileVault) { v.fs.rename = func(string, string) error { return os.ErrPermission } },
		"permission denied": func(v *FileVault) {
			v.fs.createTemp = func(string, string) (tmpFile, error) { return nil, os.ErrPermission }
		},
	}
	for name, inject := range steps {
		v, _ := vaultWith(t)
		if err := v.Put("acme", "gr_x/access", []byte(old)); err != nil {
			t.Fatal(err)
		}
		inject(v)
		err := v.Put("acme", "gr_x/access", []byte(fresh))
		if err == nil {
			t.Fatalf("%s must be reported", name)
		}
		if strings.Contains(err.Error(), fresh) || strings.Contains(err.Error(), old) || strings.Contains(err.Error(), v.Dir) {
			t.Fatalf("%s: the failure leaks a secret or a path: %v", name, err)
		}
		v.fs = osOps()
		got, gerr := v.Get("acme", "gr_x/access")
		if gerr != nil || string(got) != old {
			t.Fatalf("%s: the old secret is intact: %q %v", name, got, gerr)
		}
		if leftovers(t, v) != 0 {
			t.Fatalf("%s left a temporary file", name)
		}
	}
}

func TestAFileVaultWhoseDirectorySyncFailsReportsItButHoldsTheNewSecret(t *testing.T) {
	v, _ := vaultWith(t)
	v.fs.syncDir = func(string) error { return errors.New("input/output error") }
	err := v.Put("acme", "gr_x/access", []byte("value-1234567"))
	if err == nil || !strings.Contains(err.Error(), "not synced") {
		t.Fatalf("the caller is told durability is not assured: %v", err)
	}
	if got, _ := v.Get("acme", "gr_x/access"); string(got) != "value-1234567" {
		t.Fatalf("the rename happened, so the secret is whole: %q", got)
	}
}

func TestAFileVaultRecoversFromCrashesAndReportsCorruptionAndFailedDeletes(t *testing.T) {
	v, _ := vaultWith(t)
	_ = v.Put("acme", "gr_x/access", []byte("value-1234567"))
	dirs, _ := os.ReadDir(v.Dir)
	stale := filepath.Join(v.Dir, dirs[0].Name(), ".put-crashed")
	_ = os.WriteFile(stale, []byte("half"), 0o600)
	if n, _ := v.Recover(); n != 1 {
		t.Fatalf("%d", n)
	}
	if _, err := os.Stat(stale); err == nil {
		t.Fatal("stale temporary removed")
	}
	p := v.path("acme", "gr_x/access")
	for name, content := range map[string][]byte{"empty": {}, "truncated": []byte("short"), "garbage": bytes.Repeat([]byte{0xAA}, 200)} {
		_ = os.WriteFile(p, content, 0o600)
		if _, err := v.Get("acme", "gr_x/access"); !errors.Is(err, ErrUntrusted) {
			t.Fatalf("%s corruption is reported, not mistaken for absence: %v", name, err)
		}
	}
	_ = v.Put("acme", "gr_x/access", []byte("value-1234567"))
	v.fs.remove = func(string) error { return os.ErrPermission }
	if err := v.Delete("acme", "gr_x/access"); err == nil {
		t.Fatal("a failed delete is visible")
	}
	v.fs = osOps()
	if got, _ := v.Get("acme", "gr_x/access"); string(got) != "value-1234567" {
		t.Fatal("and the secret is still there to retry against")
	}
	if err := v.Delete("acme", "gr_x/access"); err != nil {
		t.Fatal(err)
	}
	if err := v.Delete("acme", "gr_x/access"); err != nil {
		t.Fatalf("deleting twice is harmless: %v", err)
	}
}

type lyingRunner struct {
	*fakeRunner
	altered, keepOnClear bool
}

func (l *lyingRunner) Run(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, int, error) {
	if l.altered && args[0] == "store" {
		stdin = []byte("dGFtcGVyZWQ=")
	}
	if l.keepOnClear && args[0] == "clear" {
		return nil, 0, nil
	}
	return l.fakeRunner.Run(ctx, stdin, name, args...)
}

func TestAKeychainWriteIsVerifiedAndADeleteThatDidNotRemoveAnythingIsAnError(t *testing.T) {
	f := &fakeRunner{}
	l := &lyingRunner{fakeRunner: f, altered: true}
	k := &SecretService{Run: l}
	if err := k.Put("acme", "gr_x/access", []byte("the-real-secret")); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("a store that did not keep what was given is a failure: %v", err)
	}
	if _, err := k.Get("acme", "gr_x/access"); !errors.Is(err, ErrNotFound) {
		t.Fatal("and the wrong value is not left live")
	}
	l.altered = false
	_ = k.Put("acme", "gr_x/access", []byte("the-real-secret"))
	l.keepOnClear = true
	if err := k.Delete("acme", "gr_x/access"); err == nil {
		t.Fatal("a delete the keychain ignored must not look like success")
	}
	f.down = true
	if err := k.Delete("acme", "gr_x/access"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("%v", err)
	}
	if _, err := k.Get("acme", "gr_x/access"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("%v", err)
	}
}

func TestAFallbackIsUsedOnlyOnPurposeAndNeverShadowsALaterPrimaryWrite(t *testing.T) {
	down := &fakeRunner{down: true}
	primary := &SecretService{Run: down}
	fb := NewMemVault()
	c := &Chain{Primary: primary, Fallback: fb}
	if err := c.Put("acme", "n", []byte("v1")); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("an unreachable keychain stores nothing unless a fallback was allowed: %v", err)
	}
	if _, err := fb.Get("acme", "n"); !errors.Is(err, ErrNotFound) {
		t.Fatal("nothing in the fallback")
	}
	c.AllowFallback = true
	if err := c.Put("acme", "n", []byte("v1")); err != nil {
		t.Fatal(err)
	}
	down.down = false
	if err := c.Put("acme", "n", []byte("v2")); err != nil {
		t.Fatal(err)
	}
	if _, err := fb.Get("acme", "n"); !errors.Is(err, ErrNotFound) {
		t.Fatal("a stale fallback copy is removed once the primary holds the secret")
	}
	if got, _ := c.Get("acme", "n"); string(got) != "v2" {
		t.Fatalf("%q", got)
	}
}

func TestAKeyStillSealingVaultSecretsCannotBeRetired(t *testing.T) {
	keys := secret.Generate()
	v, err := NewFileVault(filepath.Join(t.TempDir(), "v"), keys)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Put("acme", "gr_x/access", []byte("value-1234567")); err != nil {
		t.Fatal(err)
	}
	if _, err := keys.Rotate(); err != nil {
		t.Fatal(err)
	}
	if err := keys.Retire(context.Background(), 1); !errors.Is(err, secret.ErrInUse) {
		t.Fatalf("credentials sealed under the key keep it alive: %v", err)
	}
	if err := v.Put("acme", "gr_x/access", []byte("value-1234567")); err != nil {
		t.Fatal(err)
	}
	if err := keys.Retire(context.Background(), 1); err != nil {
		t.Fatalf("once re-sealed under the newest key: %v", err)
	}
	if got, err := v.Get("acme", "gr_x/access"); err != nil || string(got) != "value-1234567" {
		t.Fatalf("%q %v", got, err)
	}
}
