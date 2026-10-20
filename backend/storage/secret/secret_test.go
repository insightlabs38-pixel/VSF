package secret

import (
	"bytes"
	"context"
	"crypto/ed25519"
	crand "crypto/rand"
	"encoding/hex"
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
