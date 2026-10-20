package artifact

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/insightlabs38-pixel/vsf/backend/storage/blob"
	"github.com/insightlabs38-pixel/vsf/backend/storage/meta"
)

var ctx = context.Background()

type rig struct {
	t      *testing.T
	meta   *meta.SQLite
	mem    *blob.Mem
	store  *Store
	signer *Signer
	trust  Trust
	crash  string
	path   string
	clock  atomic.Int64
}

func keypair(t testing.TB, id string) (*Signer, ed25519.PublicKey) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return &Signer{KeyID: id, Key: priv}, pub
}

func newRig(t *testing.T, mut func(*Config)) *rig {
	t.Helper()
	r := &rig{t: t, path: filepath.Join(t.TempDir(), "meta.db"), mem: blob.NewMem()}
	var pub ed25519.PublicKey
	r.signer, pub = keypair(t, "k1")
	r.trust = Trust{Keys: map[string]ed25519.PublicKey{"k1": pub}, Revoked: map[string]bool{}}
	var err error
	r.meta, err = meta.OpenSQLite(ctx, r.path, meta.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.meta.Close() })
	r.build(mut)
	return r
}

func (r *rig) build(mut func(*Config)) {
	cfg := Config{Meta: r.meta, Blobs: r.mem, Signer: r.signer, Trust: r.trust, Now: func() int64 { return r.clock.Add(1) },
		CrashAt: func(p string) bool { return r.crash == p }}
	if mut != nil {
		mut(&cfg)
	}
	var err error
	if r.store, err = New(cfg); err != nil {
		r.t.Fatal(err)
	}
}

func (r *rig) put(tn meta.Tenant, s string, class meta.Class) string {
	r.t.Helper()
	d, err := r.store.PutArtifact(ctx, tn, []byte(s), ArtifactInput{Class: class})
	if err != nil {
		r.t.Fatal(err)
	}
	return d
}

func refs(ds ...string) []ArtifactRef {
	out := make([]ArtifactRef, len(ds))
	for i, d := range ds {
		out[i] = ArtifactRef{Role: fmt.Sprintf("part%d", i), Digest: d}
	}
	return out
}

func TestAnArtifactRoundTripsAndIsIdentifiedByItsContent(t *testing.T) {
	r := newRig(t, nil)
	d, err := r.store.PutArtifact(ctx, "acme", []byte("compiled-plan"), ArtifactInput{Class: meta.Public, Provenance: json.RawMessage(`{"compiler":"0.1"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if d != Digest([]byte("compiled-plan")) {
		t.Fatal(d)
	}
	got, err := r.store.Artifact(ctx, "acme", d)
	if err != nil || string(got) != "compiled-plan" {
		t.Fatalf("%q %v", got, err)
	}
	again, err := r.store.PutArtifact(ctx, "acme", []byte("compiled-plan"), ArtifactInput{Class: meta.Public})
	if err != nil || again != d {
		t.Fatal("storing the same bytes again is idempotent")
	}
	rec, _ := r.meta.Artifact(ctx, "acme", d)
	if rec.Size != 13 || rec.Class != meta.Public || !strings.Contains(rec.Provenance, "compiler") {
		t.Fatalf("%+v", rec)
	}
}

func TestPublicObjectsAreSharedAndPrivateOnesNeverAre(t *testing.T) {
	r := newRig(t, nil)
	pa, pb := r.put("alpha", "same-bytes", meta.Public), r.put("beta", "same-bytes", meta.Public)
	if pa != pb {
		t.Fatal("same content, same digest")
	}
	n, _ := r.mem.List(ctx, "pub/", "", 0)
	if len(n) != 1 {
		t.Fatalf("public content is held once: %d objects", len(n))
	}
	r.put("alpha", "secret-bytes", meta.Private)
	r.put("beta", "secret-bytes", meta.Private)
	priv, _ := r.mem.List(ctx, "ten/", "", 0)
	if len(priv) != 2 {
		t.Fatalf("private content is held per tenant: %d objects", len(priv))
	}
	for _, o := range priv {
		if !strings.HasPrefix(o.Key, "ten/alpha/") && !strings.HasPrefix(o.Key, "ten/beta/") {
			t.Fatal(o.Key)
		}
	}
	if _, err := r.store.Artifact(ctx, "beta", Digest([]byte("only-alpha-has-this"))); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	r.put("alpha", "only-alpha-has-this", meta.Private)
	if _, err := r.store.Artifact(ctx, "beta", Digest([]byte("only-alpha-has-this"))); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a digest alone must not reach another tenant's private artifact: %v", err)
	}
}

func TestBitRotAndLostObjectsAreDetectedNotServed(t *testing.T) {
	r := newRig(t, nil)
	d := r.put("acme", "precious", meta.Public)
	key := DefaultLayout{}.Key("acme", meta.Public, d)
	r.mem.Tamper(key, []byte("preciouz"))
	if _, err := r.store.Artifact(ctx, "acme", d); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("tampered bytes of the same length: %v", err)
	}
	r.mem.Tamper(key, []byte("short"))
	if _, err := r.store.Artifact(ctx, "acme", d); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("truncated bytes: %v", err)
	}
	_ = r.mem.Delete(ctx, key)
	if _, err := r.store.Artifact(ctx, "acme", d); !errors.Is(err, ErrMissingObject) {
		t.Fatalf("a lost object: %v", err)
	}
	r.mem.Tamper(key, []byte("not-the-content"))
	if _, err := r.store.PutArtifact(ctx, "acme", []byte("precious"), ArtifactInput{Class: meta.Public}); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("an existing object with other bytes must be reported, never overwritten: %v", err)
	}
}

func TestInvalidInputsAreRefusedBeforeAnythingIsWritten(t *testing.T) {
	r := newRig(t, func(c *Config) { c.MaxArtifact = 16 })
	for name, f := range map[string]func() error{
		"bad tenant": func() error {
			_, e := r.store.PutArtifact(ctx, "Bad Tenant", []byte("x"), ArtifactInput{Class: meta.Public})
			return e
		},
		"bad class": func() error {
			_, e := r.store.PutArtifact(ctx, "acme", []byte("x"), ArtifactInput{Class: "secret"})
			return e
		},
		"too large": func() error {
			_, e := r.store.PutArtifact(ctx, "acme", make([]byte, 17), ArtifactInput{Class: meta.Public})
			return e
		},
		"provenance array": func() error {
			_, e := r.store.PutArtifact(ctx, "acme", []byte("x"), ArtifactInput{Class: meta.Public, Provenance: json.RawMessage(`[1]`)})
			return e
		},
		"provenance huge": func() error {
			_, e := r.store.PutArtifact(ctx, "acme", []byte("x"), ArtifactInput{Class: meta.Public, Provenance: json.RawMessage(`{"a":"` + strings.Repeat("x", 5000) + `"}`)})
			return e
		},
	} {
		if f() == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if o, _ := r.mem.List(ctx, "", "", 0); len(o) != 0 {
		t.Fatalf("refused input left %d objects", len(o))
	}
	if a, _ := r.meta.Artifacts(ctx, "acme", "", 0); len(a) != 0 {
		t.Fatal("refused input left records")
	}
}

func TestACrashDuringAnArtifactWriteLeavesNoRecordWithoutItsObject(t *testing.T) {
	for _, point := range []string{"before-object", "after-object"} {
		t.Run(point, func(t *testing.T) {
			r := newRig(t, nil)
			r.crash = point
			if _, err := r.store.PutArtifact(ctx, "acme", []byte("x"), ArtifactInput{Class: meta.Public}); !errors.Is(err, ErrCrashed) {
				t.Fatal(err)
			}
			if _, err := r.meta.Artifact(ctx, "acme", Digest([]byte("x"))); !errors.Is(err, meta.ErrNotFound) {
				t.Fatalf("no record may exist: %v", err)
			}
			objs, _ := r.mem.List(ctx, "", "", 0)
			if (point == "after-object") != (len(objs) == 1) {
				t.Fatalf("%d objects after a crash at %s", len(objs), point)
			}
			r.crash = ""
			if _, err := r.store.PutArtifact(ctx, "acme", []byte("x"), ArtifactInput{Class: meta.Public}); err != nil {
				t.Fatalf("a retry must complete the write: %v", err)
			}
			if got, err := r.store.Artifact(ctx, "acme", Digest([]byte("x"))); err != nil || string(got) != "x" {
				t.Fatal(err)
			}
		})
	}
}

func TestAManifestIsSignedBindsItsArtifactsAndAppearsAtOnce(t *testing.T) {
	r := newRig(t, nil)
	a, b := r.put("acme", "plan", meta.Public), r.put("acme", "ir", meta.Public)
	id, err := r.store.PublishManifest(ctx, "acme", ManifestInput{Kind: "compiled", Version: "1.3.0", Artifacts: refs(a, b), Provenance: json.RawMessage(`{"suite":"s1"}`)})
	if err != nil {
		t.Fatal(err)
	}
	m, err := r.store.Manifest(ctx, "acme", id)
	if err != nil {
		t.Fatal(err)
	}
	if m.Kind != "compiled" || m.Version != "1.3.0" || m.Classification != meta.Public || len(m.Artifacts) != 2 || m.KeyID != "k1" || m.CreatedAt == 0 || !strings.Contains(string(m.Provenance), "s1") {
		t.Fatalf("%+v", m)
	}
	for _, ref := range m.Artifacts {
		if ref.Size == 0 || ref.Class != meta.Public {
			t.Fatalf("a reference must bind size and class: %+v", ref)
		}
	}
	if err := r.store.VerifyManifest(ctx, "acme", id); err != nil {
		t.Fatal(err)
	}
	links, _ := r.meta.ManifestArtifacts(ctx, "acme", id)
	if len(links) != 2 {
		t.Fatal(links)
	}
	id2, err := r.store.PublishManifest(ctx, "acme", ManifestInput{Kind: "compiled", Version: "1.3.0", Artifacts: refs(a, b), Provenance: json.RawMessage(`{"suite":"s1"}`)})
	if err != nil || id2 == id {
		t.Fatalf("a manifest binds its time, so a republish is a new manifest: %v", err)
	}
	priv := r.put("acme", "tenant-data", meta.Private)
	pid, _ := r.store.PublishManifest(ctx, "acme", ManifestInput{Kind: "compiled", Version: "1.4.0", Parent: id, Artifacts: refs(a, priv)})
	pm, _ := r.store.Manifest(ctx, "acme", pid)
	if pm.Classification != meta.Private || pm.Parent != id {
		t.Fatalf("one private artifact makes the manifest private: %+v", pm)
	}
}

func TestAManifestNeverAppearsHalfBuilt(t *testing.T) {
	r := newRig(t, nil)
	a := r.put("acme", "a", meta.Public)
	b := r.put("acme", "b", meta.Public)
	missing := Digest([]byte("never stored"))
	if _, err := r.store.PublishManifest(ctx, "acme", ManifestInput{Kind: "compiled", Artifacts: refs(a, missing)}); !errors.Is(err, ErrMissingArtifact) {
		t.Fatalf("%v", err)
	}
	if ms, _ := r.meta.Manifests(ctx, "acme", "", 0); len(ms) != 0 {
		t.Fatal("a refused manifest must leave nothing")
	}
	_ = r.mem.Delete(ctx, DefaultLayout{}.Key("acme", meta.Public, b))
	if _, err := r.store.PublishManifest(ctx, "acme", ManifestInput{Kind: "compiled", Artifacts: refs(a, b)}); !errors.Is(err, ErrMissingObject) {
		t.Fatalf("an artifact whose object is gone cannot be published: %v", err)
	}
	r.mem.Tamper(DefaultLayout{}.Key("acme", meta.Public, b), []byte("b!"))
	if _, err := r.store.PublishManifest(ctx, "acme", ManifestInput{Kind: "compiled", Artifacts: refs(a, b)}); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("a corrupt artifact cannot be published: %v", err)
	}
	r.mem.Tamper(DefaultLayout{}.Key("acme", meta.Public, b), []byte("b"))
	for _, point := range []string{"before-manifest", "after-manifest"} {
		r.crash = point
		_, err := r.store.PublishManifest(ctx, "acme", ManifestInput{Kind: "compiled", Version: point, Artifacts: refs(a, b)})
		if !errors.Is(err, ErrCrashed) {
			t.Fatal(err)
		}
		ms, _ := r.meta.Manifests(ctx, "acme", "", 0)
		complete := point == "after-manifest"
		if complete != (len(ms) == 1) {
			t.Fatalf("%s: %d manifests visible", point, len(ms))
		}
		if complete {
			if err := r.store.VerifyManifest(ctx, "acme", ms[0].ID); err != nil {
				t.Fatalf("a visible manifest is complete: %v", err)
			}
		}
	}
	r.crash = ""
	if _, err := r.store.PublishManifest(ctx, "acme", ManifestInput{Kind: "compiled", Artifacts: nil}); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	dup := []ArtifactRef{{Role: "x", Digest: a}, {Role: "x", Digest: b}}
	if _, err := r.store.PublishManifest(ctx, "acme", ManifestInput{Kind: "compiled", Artifacts: dup}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a role is named once: %v", err)
	}
}

func TestManifestsAreRefusedWhenTamperedForgedRevokedOrMoved(t *testing.T) {
	r := newRig(t, nil)
	a := r.put("acme", "a", meta.Public)
	id, err := r.store.PublishManifest(ctx, "acme", ManifestInput{Kind: "compiled", Artifacts: refs(a)})
	if err != nil {
		t.Fatal(err)
	}
	rec, _ := r.meta.Manifest(ctx, "acme", id)

	raw, _ := sql.Open("sqlite", "file:"+r.path)
	defer raw.Close()
	tamper := func(doc []byte) {
		if _, err := raw.Exec(`UPDATE manifests SET doc = ? WHERE tenant_id = 'acme' AND id = ?`, doc, id); err != nil {
			t.Fatal(err)
		}
	}
	tamper(bytes.Replace(rec.Doc, []byte(`"compiled"`), []byte(`"hacked"`), 1))
	if _, err := r.store.Manifest(ctx, "acme", id); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("an edited document no longer has its identity: %v", err)
	}
	tamper(rec.Doc)
	if _, err := r.store.Manifest(ctx, "acme", id); err != nil {
		t.Fatal(err)
	}

	forger, _ := keypair(t, "k1")
	forged := newRig(t, func(c *Config) { c.Signer = forger })
	fa := forged.put("acme", "a", meta.Public)
	fid, _ := forged.store.PublishManifest(ctx, "acme", ManifestInput{Kind: "compiled", Artifacts: refs(fa)})
	if _, err := r.store.verifyManifest("acme", mustRec(t, forged.meta, "acme", fid)); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("a manifest signed by another key under a trusted key id: %v", err)
	}
	other, _ := keypair(t, "k9")
	r2 := newRig(t, func(c *Config) { c.Signer = other })
	oa := r2.put("acme", "a", meta.Public)
	oid, _ := r2.store.PublishManifest(ctx, "acme", ManifestInput{Kind: "compiled", Artifacts: refs(oa)})
	if _, err := r.store.verifyManifest("acme", mustRec(t, r2.meta, "acme", oid)); !errors.Is(err, ErrUntrustedKey) {
		t.Fatalf("an unknown key: %v", err)
	}

	r.trust.Revoked["k1"] = true
	if _, err := r.store.Manifest(ctx, "acme", id); !errors.Is(err, ErrRevokedKey) {
		t.Fatalf("a revoked key: %v", err)
	}
	delete(r.trust.Revoked, "k1")

	_ = r.meta.EnsureTenant(ctx, "other")
	if _, err := r.meta.PutArtifact(ctx, "other", meta.ArtifactRecord{Digest: a, Size: 1, Class: meta.Public}); err != nil {
		t.Fatal(err)
	}
	if err := r.meta.PublishManifest(ctx, "other", meta.ManifestRecord{ID: id, Kind: "compiled", Doc: rec.Doc, KeyID: "k1"}, []string{a}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.store.Manifest(ctx, "other", id); !errors.Is(err, ErrWrongTenant) {
		t.Fatalf("a manifest copied to another tenant: %v", err)
	}
}

func mustRec(t testing.TB, m meta.Store, tn meta.Tenant, id string) meta.ManifestRecord {
	rec, err := m.Manifest(ctx, tn, id)
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

func TestTheFilesystemBackendNeverShowsATornObject(t *testing.T) {
	for _, point := range []string{"torn", "before-rename", "after-rename"} {
		t.Run(point, func(t *testing.T) {
			dir := t.TempDir()
			fsb, err := blob.NewFS(dir)
			if err != nil {
				t.Fatal(err)
			}
			fsb.CrashAt = func(p string) bool { return p == point }
			r := newRig(t, func(c *Config) { c.Blobs = fsb })
			data := bytes.Repeat([]byte("0123456789abcdef"), 1000)
			_, err = r.store.PutArtifact(ctx, "acme", data, ArtifactInput{Class: meta.Public})
			if !errors.Is(err, ErrCrashed) {
				t.Fatal(err)
			}
			_ = fsb.Close()
			reopened, err := blob.NewFS(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			key := DefaultLayout{}.Key("acme", meta.Public, Digest(data))
			got, gerr := reopened.Get(ctx, key)
			switch {
			case point == "after-rename":
				if gerr != nil || !bytes.Equal(got, data) {
					t.Fatalf("a renamed object is whole: %v", gerr)
				}
			case !errors.Is(gerr, blob.ErrNotFound):
				t.Fatalf("a crash before the rename must leave no object: %v %d", gerr, len(got))
			}
			left, _ := filepath.Glob(filepath.Join(dir, "tmp", "*"))
			if len(left) != 0 {
				t.Fatalf("reopening must clear the dead writer's temporary files: %v", left)
			}
			r.build(func(c *Config) { c.Blobs = reopened })
			if _, err := r.store.PutArtifact(ctx, "acme", data, ArtifactInput{Class: meta.Public}); err != nil {
				t.Fatalf("the write must complete after recovery: %v", err)
			}
			if got, err := r.store.Artifact(ctx, "acme", Digest(data)); err != nil || !bytes.Equal(got, data) {
				t.Fatal(err)
			}
		})
	}
}

func TestOnlyOneProcessMayOwnAnObjectRoot(t *testing.T) {
	dir := t.TempDir()
	a, err := blob.NewFS(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := blob.NewFS(dir); !errors.Is(err, blob.ErrLocked) {
		t.Fatalf("%v", err)
	}
	_ = a.Close()
	b, err := blob.NewFS(dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = b.Close()
}

func TestBackendKeysCannotClimbOutOrCollide(t *testing.T) {
	for _, k := range []string{"", "../x", "a/../b", "/abs", "a//b", "A/b", "a b", "a/.", strings.Repeat("a", 600)} {
		if blob.ValidKey(k) {
			t.Errorf("%q must be refused", k)
		}
	}
	fsb, _ := blob.NewFS(t.TempDir())
	defer fsb.Close()
	if err := fsb.Put(ctx, "../escape", []byte("x")); !errors.Is(err, blob.ErrBadKey) {
		t.Fatal(err)
	}
	if err := fsb.Put(ctx, "a/b", []byte("1")); err != nil {
		t.Fatal(err)
	}
	if err := fsb.Put(ctx, "a/b", []byte("2")); !errors.Is(err, blob.ErrOverwrite) {
		t.Fatalf("immutable: %v", err)
	}
	if err := fsb.Put(ctx, "a/b", []byte("1")); err != nil {
		t.Fatal("identical bytes are idempotent")
	}
}
