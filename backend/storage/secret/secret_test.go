package secret

import (
	"bytes"
	"context"
	"crypto/ed25519"
	crand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/insightlabs38-pixel/vsf/backend/storage/artifact"
	"github.com/insightlabs38-pixel/vsf/backend/storage/blob"
	"github.com/insightlabs38-pixel/vsf/backend/storage/bundle"
	"github.com/insightlabs38-pixel/vsf/backend/storage/meta"
)

var ctx = context.Background()

func TestSealRoundTripsAndBindsTenantAndIdentifier(t *testing.T) {
	s := Generate().Sealer()
	id := s.Identify("acme", []byte("account cookie jar"))
	sealed, err := s.Seal("acme", id, []byte("account cookie jar"))
	if err != nil {
		t.Fatal(err)
	}
	if len(sealed) != len("account cookie jar")+s.Overhead() || bytes.Contains(sealed, []byte("cookie")) {
		t.Fatal("sealed bytes must be longer by the overhead and hide the content")
	}
	got, err := s.Open("acme", id, sealed)
	if err != nil || string(got) != "account cookie jar" {
		t.Fatalf("%q %v", got, err)
	}
	if _, err := s.Open("globex", id, sealed); !errors.Is(err, ErrOpen) {
		t.Fatalf("another tenant must not open it: %v", err)
	}
	if _, err := s.Open("acme", s.Identify("acme", []byte("other")), sealed); !errors.Is(err, ErrOpen) {
		t.Fatalf("the identifier is part of what is authenticated: %v", err)
	}
	for i := range sealed {
		bad := append([]byte(nil), sealed...)
		bad[i] ^= 1
		if _, err := s.Open("acme", id, bad); err == nil {
			t.Fatalf("a flipped bit at %d went unnoticed", i)
		}
	}
	if _, err := Generate().Sealer().Open("acme", id, sealed); !errors.Is(err, ErrOpen) {
		t.Fatalf("another keyring must not open it: %v", err)
	}
}

func TestIdentifiersAreKeyedPerTenantAndNeverThePlaintextHash(t *testing.T) {
	s := Generate().Sealer()
	a, b := s.Identify("acme", []byte("same bytes")), s.Identify("globex", []byte("same bytes"))
	if a == b {
		t.Fatal("two tenants holding the same content must not share an identifier")
	}
	if a == artifact.Digest([]byte("same bytes")) {
		t.Fatal("the identifier must not be the plaintext hash, which anyone could compute to confirm the content")
	}
	if a != s.Identify("acme", []byte("same bytes")) || !meta.ValidDigest(a) {
		t.Fatal("identifiers are stable and shaped like digests")
	}
	if Generate().Sealer().Identify("acme", []byte("same bytes")) == a {
		t.Fatal("identifiers depend on the keyring")
	}
	x, _ := s.Seal("acme", a, []byte("same bytes"))
	y, _ := s.Seal("acme", a, []byte("same bytes"))
	if !bytes.Equal(x, y) {
		t.Fatal("sealing within a tenant is repeatable, which is what lets storage stay idempotent")
	}
}

func TestRotationKeepsIdentifiersAndOldDataReadable(t *testing.T) {
	k := Generate()
	s := k.Sealer()
	id := s.Identify("acme", []byte("v1 data"))
	old, _ := s.Seal("acme", id, []byte("v1 data"))
	v, err := k.Rotate()
	if err != nil || v != 2 || k.Active() != 2 {
		t.Fatalf("%d %v", v, err)
	}
	if s.Identify("acme", []byte("v1 data")) != id {
		t.Fatal("rotating data keys must not rename stored artifacts")
	}
	fresh, _ := s.Seal("acme", id, []byte("v1 data"))
	if old[0] != 1 || fresh[0] != 2 || bytes.Equal(old, fresh) {
		t.Fatalf("new writes use the new key: %d %d", old[0], fresh[0])
	}
	for _, sealed := range [][]byte{old, fresh} {
		if got, err := s.Open("acme", id, sealed); err != nil || string(got) != "v1 data" {
			t.Fatalf("%q %v", got, err)
		}
	}
	if got := k.Versions(); len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("%v", got)
	}
}

func TestKeyFileRoundTripsAndRefusesLooseOrBrokenFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keys.json")
	k := Generate()
	if _, err := k.Rotate(); err != nil {
		t.Fatal(err)
	}
	if err := k.Save(path); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Fatalf("%o", st.Mode().Perm())
	}
	back, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	id := k.Sealer().Identify("acme", []byte("x"))
	sealed, _ := k.Sealer().Seal("acme", id, []byte("x"))
	if got, err := back.Sealer().Open("acme", id, sealed); err != nil || string(got) != "x" {
		t.Fatalf("a reloaded keyring must open what the original sealed: %q %v", got, err)
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); !errors.Is(err, ErrKeyFile) {
		t.Fatalf("a group-readable key file must be refused: %v", err)
	}
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte(`{"format":1,"id_key":"zz","data_keys":{},"active":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(bad); !errors.Is(err, ErrKeyFile) {
		t.Fatalf("%v", err)
	}
	if _, err := New(make([]byte, 5), nil, 1); !errors.Is(err, ErrKeyFile) {
		t.Fatalf("%v", err)
	}
}

func TestBoxNamesAreOpaqueAndSeparatedByTenantAndPurpose(t *testing.T) {
	k := Generate()
	a, b := k.Box("acme", "cache").Name("inbox"), k.Box("globex", "cache").Name("inbox")
	if a == b || a == k.Box("acme", "other").Name("inbox") || strings.Contains(a, "inbox") || len(a) != 64 {
		t.Fatalf("%s %s", a, b)
	}
	box := k.Box("acme", "cache")
	sealed, err := box.Seal([]byte("ctx"), []byte("v"))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := box.Open([]byte("ctx"), sealed); err != nil || string(got) != "v" {
		t.Fatalf("%q %v", got, err)
	}
	if _, err := box.Open([]byte("other"), sealed); err == nil {
		t.Fatal("the context is authenticated")
	}
	if _, err := k.Box("globex", "cache").Open([]byte("ctx"), sealed); err == nil {
		t.Fatal("another tenant cannot open it")
	}
	again, _ := box.Seal([]byte("ctx"), []byte("v"))
	if bytes.Equal(sealed, again) {
		t.Fatal("box sealing uses a fresh nonce each time")
	}
}

// rig wires a real SQLite database and a real filesystem object store, so the scan reads what actually lands on disk.
type rig struct {
	dir   string
	meta  *meta.SQLite
	blobs *blob.FS
	store *artifact.Store
	keys  *Keyring
}

func newRig(t *testing.T, keys *Keyring, pub ed25519.PublicKey, priv ed25519.PrivateKey) *rig {
	t.Helper()
	dir := t.TempDir()
	m, err := meta.OpenSQLite(ctx, filepath.Join(dir, "meta.db"), meta.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Close() })
	fs, err := blob.NewFS(filepath.Join(dir, "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := artifact.Config{Meta: m, Blobs: fs, Sealer: keys.Sealer(), Trust: artifact.Trust{Keys: map[string]ed25519.PublicKey{"k1": pub}, Revoked: map[string]bool{}}}
	if priv != nil {
		cfg.Signer = &artifact.Signer{KeyID: "k1", Key: priv}
	}
	s, err := artifact.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return &rig{dir: dir, meta: m, blobs: fs, store: s, keys: keys}
}

func signer(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	pub, priv, err := ed25519.GenerateKey(crand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

func (r *rig) version(t *testing.T, tn meta.Tenant, body string, class meta.Class) (string, string) {
	t.Helper()
	d, err := r.store.PutArtifact(ctx, tn, []byte(body), artifact.ArtifactInput{Class: class})
	if err != nil {
		t.Fatal(err)
	}
	id, err := r.store.PublishManifest(ctx, tn, artifact.ManifestInput{Kind: "cache-plan", Version: "1", Artifacts: []artifact.ArtifactRef{{Role: "body", Digest: d}}})
	if err != nil {
		t.Fatal(err)
	}
	return id, d
}

// allBytes concatenates every file under dir, including the database, its journal and every object.
func allBytes(t *testing.T, dir string) []byte {
	t.Helper()
	var out []byte
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		out = append(append(out, []byte(p)...), b...)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestPrivateContentAndKeysNeverAppearInBlobsOrTheDatabase(t *testing.T) {
	pub, priv := signer(t)
	keys := Generate()
	r := newRig(t, keys, pub, priv)
	canaries := []string{"canary-session-cookie-7f3a", "canary-account-number-90210", "canary-bearer-aaaa1111"}
	var ids []string
	for _, c := range canaries {
		_, d := r.version(t, "acme", "payload "+c+" end", meta.Private)
		ids = append(ids, d)
	}
	pubID, pubDigest := r.version(t, "acme", "shared runtime bytes", meta.Public)
	_ = pubID
	if err := r.meta.Pin(ctx, "acme", meta.Pin{ManifestID: pubID, Reason: "keep"}); err != nil {
		t.Fatal(err)
	}
	raw := allBytes(t, r.dir)
	for _, c := range canaries {
		if bytes.Contains(raw, []byte(c)) {
			t.Fatalf("plaintext %q reached disk", c)
		}
	}
	for _, c := range canaries {
		plainHash := strings.TrimPrefix(artifact.Digest([]byte("payload "+c+" end")), "sha256:")
		if bytes.Contains(raw, []byte(plainHash)) {
			t.Fatal("the plaintext hash of private content is recorded somewhere, which would let anyone confirm it")
		}
		sum := artifact.Digest([]byte("payload " + c + " end"))
		if bytes.Contains(raw, []byte(sum)) {
			t.Fatal("plaintext digest recorded")
		}
	}
	keys.mu.RLock()
	material := [][]byte{keys.idKey}
	for _, k := range keys.data {
		material = append(material, k)
	}
	keys.mu.RUnlock()
	for _, k := range material {
		if bytes.Contains(raw, k) || bytes.Contains(raw, []byte(hex.EncodeToString(k))) {
			t.Fatal("key material reached the store")
		}
	}
	if !bytes.Contains(raw, []byte(strings.TrimPrefix(pubDigest, "sha256:"))) {
		t.Fatal("public artifacts stay content-addressed by their plain digest")
	}
	for i, d := range ids {
		got, err := r.store.Artifact(ctx, "acme", d)
		if err != nil || string(got) != "payload "+canaries[i]+" end" {
			t.Fatalf("%q %v", got, err)
		}
	}
}

func TestTheSameContentIsInvisibleAcrossTenantsAndCannotBeReadWithAnotherTenantsRecord(t *testing.T) {
	pub, priv := signer(t)
	r := newRig(t, Generate(), pub, priv)
	_, a := r.version(t, "acme", "identical private bytes", meta.Private)
	_, b := r.version(t, "globex", "identical private bytes", meta.Private)
	if a == b {
		t.Fatal("identifiers must differ per tenant")
	}
	if _, err := r.store.Artifact(ctx, "globex", a); !errors.Is(err, artifact.ErrNotFound) {
		t.Fatalf("an identifier from another tenant resolves to nothing: %v", err)
	}
	if _, err := r.store.Artifact(ctx, "acme", artifact.Digest([]byte("identical private bytes"))); !errors.Is(err, artifact.ErrNotFound) {
		t.Fatalf("the plaintext hash is not an identifier: %v", err)
	}
	ao, bo := r.blobs, r.blobs
	x, _ := ao.Get(ctx, artifact.DefaultLayout{}.Key("acme", meta.Private, a))
	y, _ := bo.Get(ctx, artifact.DefaultLayout{}.Key("globex", meta.Private, b))
	if bytes.Equal(x, y) {
		t.Fatal("stored bytes must differ per tenant")
	}
	// A hostile store operator copies acme's object under globex's identifier.
	if err := r.blobs.Delete(ctx, artifact.DefaultLayout{}.Key("globex", meta.Private, b)); err != nil {
		t.Fatal(err)
	}
	if err := r.blobs.Put(ctx, artifact.DefaultLayout{}.Key("globex", meta.Private, b), x); err != nil {
		t.Fatal(err)
	}
	if _, err := r.store.Artifact(ctx, "globex", b); !errors.Is(err, artifact.ErrCorrupt) {
		t.Fatalf("a swapped object must be rejected, not decrypted: %v", err)
	}
}

func TestManifestsCarryNeitherPlaintextNorItsHash(t *testing.T) {
	pub, priv := signer(t)
	r := newRig(t, Generate(), pub, priv)
	id, _ := r.version(t, "acme", "manifest-visible-test body", meta.Private)
	rec, err := r.meta.Manifest(ctx, "acme", id)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(rec.Doc, []byte("manifest-visible-test")) || bytes.Contains(rec.Doc, []byte(strings.TrimPrefix(artifact.Digest([]byte("manifest-visible-test body")), "sha256:"))) {
		t.Fatal("a manifest must not let a reader confirm private content")
	}
	if err := r.store.VerifyManifest(ctx, "acme", id); err != nil {
		t.Fatal(err)
	}
}

func TestPrivateArtifactsTravelInBundlesOnlySealed(t *testing.T) {
	pub, priv := signer(t)
	keys := Generate()
	src, dst := newRig(t, keys, pub, priv), newRig(t, keys, pub, nil)
	_, d := src.version(t, "acme", "canary-bundle-payload-5521", meta.Private)
	src.version(t, "acme", "public companion", meta.Public)
	var buf bytes.Buffer
	if _, err := bundle.Export(ctx, bundle.Source{Store: src.store, Meta: src.meta}, "acme", bundle.Select{}, &buf); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(buf.Bytes(), []byte("canary-bundle-payload-5521")) {
		t.Fatal("an exported bundle must never carry private content in the clear")
	}
	if !bytes.Contains(buf.Bytes(), []byte("public companion")) {
		t.Fatal("public artifacts travel as they are")
	}
	res, err := bundle.Import(ctx, bundle.Dest{Store: dst.store, Meta: dst.meta}, "acme", bytes.NewReader(buf.Bytes()), bundle.Limits{})
	if err != nil || res.Artifacts != 2 || res.Manifests != 2 {
		t.Fatalf("%+v %v", res, err)
	}
	if got, err := dst.store.Artifact(ctx, "acme", d); err != nil || string(got) != "canary-bundle-payload-5521" {
		t.Fatalf("%q %v", got, err)
	}
	stranger := newRig(t, Generate(), pub, nil)
	if _, err := bundle.Import(ctx, bundle.Dest{Store: stranger.store, Meta: stranger.meta}, "acme", bytes.NewReader(buf.Bytes()), bundle.Limits{}); !errors.Is(err, bundle.ErrCorrupt) {
		t.Fatalf("a store without the tenant's keys must refuse the bundle: %v", err)
	}
	if as, _ := stranger.meta.Artifacts(ctx, "acme", "", 0); len(as) != 0 {
		t.Fatal("a refused import writes nothing")
	}
}

func TestTheScanFindsPlaintextWhenSealingIsOff(t *testing.T) {
	pub, priv := signer(t)
	r := newRig(t, Generate(), pub, priv)
	plain, err := artifact.New(artifact.Config{Meta: r.meta, Blobs: r.blobs, Signer: &artifact.Signer{KeyID: "k1", Key: priv}, Trust: artifact.Trust{Keys: map[string]ed25519.PublicKey{"k1": pub}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := plain.PutArtifact(ctx, "acme", []byte("canary-control-3141"), artifact.ArtifactInput{Class: meta.Private}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(allBytes(t, r.dir), []byte("canary-control-3141")) {
		t.Fatal("the scan must see plaintext when it is there, or its silence proves nothing")
	}
}

func privateIDs(t *testing.T, r *rig, tn meta.Tenant) []string {
	t.Helper()
	as, err := r.meta.Artifacts(ctx, tn, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, a := range as {
		if a.Class == meta.Private {
			ids = append(ids, a.Digest)
		}
	}
	return ids
}

func storedVersion(t *testing.T, r *rig, tn meta.Tenant, id string) byte {
	t.Helper()
	b, err := r.blobs.Get(ctx, artifact.DefaultLayout{}.Key(tn, meta.Private, id))
	if err != nil {
		t.Fatal(err)
	}
	return b[0]
}

func TestRewrapMovesPrivateArtifactsToTheNewestKeyWithoutRenamingThem(t *testing.T) {
	pub, priv := signer(t)
	keys := Generate()
	r := newRig(t, keys, pub, priv)
	var manifests []string
	for _, body := range []string{"plan one", "plan two", "plan three"} {
		id, _ := r.version(t, "acme", body, meta.Private)
		manifests = append(manifests, id)
	}
	r.version(t, "acme", "public runtime", meta.Public)
	before := privateIDs(t, r, "acme")
	if v, err := keys.Rotate(); err != nil || v != 2 {
		t.Fatalf("%d %v", v, err)
	}
	if d, err := r.store.PutArtifact(ctx, "acme", []byte("plan one"), artifact.ArtifactInput{Class: meta.Private}); err != nil || !contains(before, d) {
		t.Fatalf("storing known content again after a rotation is still idempotent: %v", err)
	}
	for _, id := range before {
		if storedVersion(t, r, "acme", id) != 1 {
			t.Fatal("rotation alone leaves existing objects under the old key")
		}
	}
	res, err := r.store.Rewrap(ctx, "acme")
	if err != nil || res.Checked != 3 || res.Rewrapped != 3 {
		t.Fatalf("%+v %v", res, err)
	}
	for _, id := range before {
		if storedVersion(t, r, "acme", id) != 2 {
			t.Fatal("every private object now uses the newest key")
		}
	}
	if after := privateIDs(t, r, "acme"); strings.Join(after, ",") != strings.Join(before, ",") {
		t.Fatal("identifiers must not change")
	}
	for _, m := range manifests {
		if err := r.store.VerifyManifest(ctx, "acme", m); err != nil {
			t.Fatalf("manifests still verify: %v", err)
		}
	}
	if res, err := r.store.Rewrap(ctx, "acme"); err != nil || res.Rewrapped != 0 {
		t.Fatalf("a second pass finds nothing to do: %+v %v", res, err)
	}
	if err := keys.Retire(ctx, 2); err == nil {
		t.Fatal("the active key cannot be retired")
	}
	if err := keys.Retire(ctx, 1); err != nil {
		t.Fatal(err)
	}
	for _, id := range before {
		if got, err := r.store.Artifact(ctx, "acme", id); err != nil || len(got) == 0 {
			t.Fatalf("after rewrap the old key is no longer needed: %v", err)
		}
	}
	raw := allBytes(t, r.dir)
	keys.mu.RLock()
	for _, k := range keys.data {
		if bytes.Contains(raw, k) {
			t.Fatal("key material on disk")
		}
	}
	keys.mu.RUnlock()
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

func TestRetiringAKeyTooEarlyMakesItsObjectsUnreadableWhichIsWhyRewrapComesFirst(t *testing.T) {
	pub, priv := signer(t)
	keys := Generate()
	r := newRig(t, keys, pub, priv)
	_, d := r.version(t, "acme", "needs the old key", meta.Private)
	_, _ = keys.Rotate()
	_ = keys.RetireUnchecked(1)
	if _, err := r.store.Artifact(ctx, "acme", d); !errors.Is(err, artifact.ErrCorrupt) {
		t.Fatalf("%v", err)
	}
	if res, err := r.store.Rewrap(ctx, "acme"); !errors.Is(err, artifact.ErrCorrupt) || res.Rewrapped != 0 {
		t.Fatalf("rewrap reports what it cannot open instead of skipping it: %+v %v", res, err)
	}
}

func TestARewrapThatDiesLeavesEveryObjectOpenAndFinishesOnTheNextRun(t *testing.T) {
	for _, point := range []string{"torn", "before-rename", "after-rename"} {
		t.Run(point, func(t *testing.T) {
			pub, priv := signer(t)
			keys := Generate()
			r := newRig(t, keys, pub, priv)
			var ids []string
			for _, body := range []string{"alpha payload", "beta payload", "gamma payload"} {
				_, d := r.version(t, "acme", body, meta.Private)
				ids = append(ids, d)
			}
			_, _ = keys.Rotate()
			n := 0
			r.blobs.CrashAt = func(p string) bool {
				if p == point {
					n++
				}
				return p == point && n == 2
			}
			if _, err := r.store.Rewrap(ctx, "acme"); err == nil {
				t.Fatal("the simulated crash must surface")
			}
			r.blobs.CrashAt = nil
			_ = r.blobs.Close()
			fresh, err := blob.NewFS(filepath.Join(r.dir, "blobs"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = fresh.Close() })
			store, err := artifact.New(artifact.Config{Meta: r.meta, Blobs: fresh, Sealer: keys.Sealer(), Signer: &artifact.Signer{KeyID: "k1", Key: priv},
				Trust: artifact.Trust{Keys: map[string]ed25519.PublicKey{"k1": pub}, Revoked: map[string]bool{}}})
			if err != nil {
				t.Fatal(err)
			}
			for _, d := range ids {
				if _, err := store.Artifact(ctx, "acme", d); err != nil {
					t.Fatalf("an interrupted rewrap must leave every object readable: %v", err)
				}
			}
			if _, err := store.Rewrap(ctx, "acme"); err != nil {
				t.Fatal(err)
			}
			for _, d := range ids {
				b, _ := fresh.Get(ctx, artifact.DefaultLayout{}.Key("acme", meta.Private, d))
				if b[0] != 2 {
					t.Fatalf("the next run finishes the job: %d", b[0])
				}
			}
		})
	}
}

func TestATenantsKeysTravelWrappedAndOpenItsBundlesElsewhereAndNothingElse(t *testing.T) {
	pub, priv := signer(t)
	ka, kb := Generate(), Generate()
	src, dst := newRig(t, ka, pub, priv), newRig(t, kb, pub, nil)
	_, acme := src.version(t, "acme", "acme private plan", meta.Private)
	_, globex := src.version(t, "globex", "globex private plan", meta.Private)
	_, _ = ka.Rotate()
	_, _ = src.version(t, "acme", "acme second plan", meta.Private)
	var buf bytes.Buffer
	if _, err := bundle.Export(ctx, bundle.Source{Store: src.store, Meta: src.meta}, "acme", bundle.Select{}, &buf); err != nil {
		t.Fatal(err)
	}
	imp := func() error {
		_, err := bundle.Import(ctx, bundle.Dest{Store: dst.store, Meta: dst.meta}, "acme", bytes.NewReader(buf.Bytes()), bundle.Limits{})
		return err
	}
	if err := imp(); !errors.Is(err, bundle.ErrCorrupt) {
		t.Fatalf("without the tenant's keys the bundle cannot open: %v", err)
	}
	if _, err := ka.ExportTenant("acme", "short"); err == nil {
		t.Fatal("a short passphrase is refused")
	}
	pass := "correct horse battery staple"
	wrap, err := ka.ExportTenant("acme", pass)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(wrap, []byte(hex.EncodeToString(ka.idKey))) || bytes.Contains(wrap, []byte("acme private")) {
		t.Fatal("the export is encrypted")
	}
	if _, _, err := ImportTenant(wrap, "wrong passphrase here"); !errors.Is(err, ErrOpen) {
		t.Fatalf("%v", err)
	}
	var doc map[string]any
	_ = json.Unmarshal(wrap, &doc)
	doc["iterations"] = 1000
	cheap, _ := json.Marshal(doc)
	if _, _, err := ImportTenant(cheap, pass); !errors.Is(err, ErrKeyFile) {
		t.Fatalf("a file asking for cheap key stretching is refused: %v", err)
	}
	flipped := append([]byte(nil), wrap...)
	flipped[len(flipped)-20] ^= 1
	if _, _, err := ImportTenant(flipped, pass); err == nil {
		t.Fatal("a damaged export must not open")
	}
	tn, tk, err := ImportTenant(wrap, pass)
	if err != nil || tn != "acme" {
		t.Fatalf("%v %v", tn, err)
	}
	if err := kb.Adopt(tn, tk); err != nil {
		t.Fatal(err)
	}
	if err := imp(); err != nil {
		t.Fatalf("with the tenant's keys the bundle opens: %v", err)
	}
	if got, err := dst.store.Artifact(ctx, "acme", acme); err != nil || string(got) != "acme private plan" {
		t.Fatalf("%q %v", got, err)
	}
	if _, err := kb.Sealer().Open("globex", globex, mustGet(t, src, "globex", globex)); err == nil {
		t.Fatal("one tenant's export must not open another tenant's data")
	}
	if _, err := ka.Sealer().Open("globex", globex, mustGet(t, src, "globex", globex)); err != nil {
		t.Fatal(err)
	}
	if v := kb.ActiveFor("acme"); v != 2 {
		t.Fatalf("the adopted tenant writes under its own newest key: %d", v)
	}
	if err := kb.Adopt("acme", tk); err != nil {
		t.Fatalf("adopting the same keys again is harmless: %v", err)
	}
	other := Generate()
	_, otherKeys, _ := func() (meta.Tenant, TenantKeys, error) {
		w, _ := other.ExportTenant("acme", pass)
		return ImportTenant(w, pass)
	}()
	if err := kb.Adopt("acme", otherKeys); !errors.Is(err, ErrKeyFile) {
		t.Fatalf("different keys for a tenant already held are refused: %v", err)
	}
	next, err := kb.RotateTenant("acme")
	if err != nil || next != 3 || kb.ActiveFor("acme") != 3 {
		t.Fatalf("%d %v", next, err)
	}
	if _, err := dst.store.Rewrap(ctx, "acme"); err != nil {
		t.Fatal(err)
	}
	if got, err := dst.store.Artifact(ctx, "acme", acme); err != nil || string(got) != "acme private plan" {
		t.Fatalf("an adopted tenant rewraps under its own rotation: %q %v", got, err)
	}
	if v, _ := kb.Rotate(); kb.ActiveFor("acme") != 3 || v == 0 {
		t.Fatal("a global rotation does not move an adopted tenant")
	}
	path := filepath.Join(t.TempDir(), "keys.json")
	if err := kb.Save(path); err != nil {
		t.Fatal(err)
	}
	back, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	id := back.Sealer().Identify("acme", []byte("acme private plan"))
	if id != acme {
		t.Fatal("a reloaded keyring still names the adopted tenant's artifacts the same way")
	}
}

func mustGet(t *testing.T, r *rig, tn meta.Tenant, id string) []byte {
	t.Helper()
	b, err := r.blobs.Get(ctx, artifact.DefaultLayout{}.Key(tn, meta.Private, id))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestAKeyStillProtectingDataCannotBeRetiredAndOneNobodyVouchesForCannotEither(t *testing.T) {
	pub, priv := signer(t)
	keys := Generate()
	if _, _ = keys.Rotate(); keys.Retire(ctx, 1) == nil {
		t.Fatal("with no holder of sealed data registered, nothing vouches that the key is unused")
	}
	keys = Generate()
	r := newRig(t, keys, pub, priv)
	_, d := r.version(t, "acme", "needs key one", meta.Private)
	_, _ = keys.Rotate()
	if err := keys.Retire(ctx, 1); !errors.Is(err, ErrInUse) {
		t.Fatalf("data sealed under the key blocks its retirement: %v", err)
	}
	if got, err := r.store.Artifact(ctx, "acme", d); err != nil || string(got) != "needs key one" {
		t.Fatalf("and it stays readable: %q %v", got, err)
	}
	if _, err := keys.RotateTenant("acme"); err != nil {
		t.Fatal(err)
	}
	_, _ = keys.Rotate()
	_, _ = keys.Rotate()
	if err := keys.Retire(ctx, 3); !errors.Is(err, ErrInUse) {
		t.Fatalf("a version a tenant still writes under is in use even with no object under it: %v", err)
	}
	if _, err := r.store.Rewrap(ctx, "acme"); err != nil {
		t.Fatal(err)
	}
	if err := keys.Retire(ctx, 1); err != nil {
		t.Fatalf("after rewrap nothing depends on it: %v", err)
	}
	if got, err := r.store.Artifact(ctx, "acme", d); err != nil || string(got) != "needs key one" {
		t.Fatalf("%q %v", got, err)
	}
	if _, ok := keys.tenants["acme"].Data[1]; ok {
		t.Fatal("a tenant that carries its own copy of the version loses it too")
	}
	r.blobs.CrashAt = nil
	failing := Generate()
	failing.GuardRetire(func(context.Context, byte) (int, error) { return 0, errors.New("backend down") })
	_, _ = failing.Rotate()
	if err := failing.Retire(ctx, 1); !errors.Is(err, ErrInUse) {
		t.Fatalf("a holder that cannot be scanned blocks retirement: %v", err)
	}
}

func TestAdoptingNeverDropsAnImportedVersionAndIsAllOrNothing(t *testing.T) {
	pass := "correct horse battery staple"
	src := Generate()
	_, _ = src.RotateTenant("acme")
	_, _ = src.RotateTenant("acme")
	w, _ := src.ExportTenant("acme", pass)
	tn, tk, _ := ImportTenant(w, pass)
	dst := &Keyring{idKey: src.idKey, active: src.active, tenants: map[meta.Tenant]*TenantKeys{}, data: map[byte][]byte{}}
	for v, k := range src.data {
		dst.data[v] = k
	}
	if err := dst.Adopt(tn, tk); err != nil {
		t.Fatal(err)
	}
	if dst.ActiveFor("acme") != 3 || len(dst.tenants["acme"].Data) != len(tk.Data) {
		t.Fatalf("a tenant rotated on its own keeps every version when it comes back to an installation sharing its roots: active %d, %d of %d versions", dst.ActiveFor("acme"), len(dst.tenants["acme"].Data), len(tk.Data))
	}
	conflict := tk.clone()
	conflict.Data[1] = bytes.Repeat([]byte{7}, 32)
	conflict.Data[9] = bytes.Repeat([]byte{8}, 32)
	if err := dst.Adopt(tn, *conflict); !errors.Is(err, ErrKeyFile) {
		t.Fatalf("a different key for a version already held is refused: %v", err)
	}
	if dst.tenants["acme"].Data[9] != nil {
		t.Fatal("and nothing from the refused import was merged")
	}
	fresh := Generate()
	if err := fresh.Adopt(tn, tk); err != nil {
		t.Fatal(err)
	}
	other := tk.clone()
	other.ID = bytes.Repeat([]byte{1}, 32)
	if err := fresh.Adopt(tn, *other); !errors.Is(err, ErrKeyFile) {
		t.Fatalf("keys of a different identity cannot be merged into a held tenant: %v", err)
	}
}
