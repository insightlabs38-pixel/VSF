package bundle

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"path/filepath"
	"testing"

	"github.com/insightlabs38-pixel/vsf/backend/storage/artifact"
	"github.com/insightlabs38-pixel/vsf/backend/storage/blob"
	"github.com/insightlabs38-pixel/vsf/backend/storage/meta"
)

var ctx = context.Background()

type side struct {
	meta  *meta.SQLite
	mem   *blob.Mem
	store *artifact.Store
}

func keys(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

func newSide(t *testing.T, pub ed25519.PublicKey, priv ed25519.PrivateKey) *side {
	t.Helper()
	m, err := meta.OpenSQLite(ctx, filepath.Join(t.TempDir(), "meta.db"), meta.Options{Now: func() int64 { return 5_000 }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Close() })
	mem := blob.NewMem()
	cfg := artifact.Config{Meta: m, Blobs: mem, Now: func() int64 { return 5_000 }}
	cfg.Trust = artifact.Trust{Keys: map[string]ed25519.PublicKey{"k1": pub}, Revoked: map[string]bool{}}
	if priv != nil {
		cfg.Signer = &artifact.Signer{KeyID: "k1", Key: priv}
	}
	s, err := artifact.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return &side{meta: m, mem: mem, store: s}
}

func (s *side) put(tn meta.Tenant, body string, class meta.Class, prov string) string {
	d, err := s.store.PutArtifact(ctx, tn, []byte(body), artifact.ArtifactInput{Class: class, Provenance: []byte(prov)})
	if err != nil {
		panic(err)
	}
	return d
}

func (s *side) publish(tn meta.Tenant, version, parent string, ds ...string) string {
	refs := make([]artifact.ArtifactRef, len(ds))
	for i, d := range ds {
		refs[i] = artifact.ArtifactRef{Role: string(rune('a' + i)), Digest: d}
	}
	id, err := s.store.PublishManifest(ctx, tn, artifact.ManifestInput{Kind: "plan", Version: version, Parent: parent, Artifacts: refs})
	if err != nil {
		panic(err)
	}
	return id
}

// populate gives acme a two-version chain with public and private artifacts, a root, a pin and a hold.
func (s *side) populate(t *testing.T) (v1, v2 string) {
	t.Helper()
	pub1 := s.put("acme", "shared-runtime-v1", meta.Public, `{"compiler":"0.1"}`)
	priv1 := s.put("acme", "customer plan v1", meta.Private, "")
	v1 = s.publish("acme", "1", "", pub1, priv1)
	priv2 := s.put("acme", "customer plan v2 with more steps", meta.Private, `{"note":"second"}`)
	v2 = s.publish("acme", "2", v1, pub1, priv2)
	if _, err := s.meta.SetRoot(ctx, "acme", "stable", v2, 0); err != nil {
		t.Fatal(err)
	}
	if err := s.meta.Pin(ctx, "acme", meta.Pin{ManifestID: v1, Reason: "contract", Entitlement: "enterprise"}); err != nil {
		t.Fatal(err)
	}
	if err := s.meta.Hold(ctx, "acme", meta.Hold{ManifestID: v2, Holder: "legal", Reason: "case 7"}); err != nil {
		t.Fatal(err)
	}
	return v1, v2
}

type counts struct{ artifacts, manifests, roots, pins, holds, intents, objects int }

func (s *side) counts(tn meta.Tenant) counts {
	as, _ := s.meta.Artifacts(ctx, tn, "", 0)
	ms, _ := s.meta.Manifests(ctx, tn, "", 0)
	rs, _ := s.meta.Roots(ctx, tn)
	ps, _ := s.meta.Pins(ctx, tn)
	hs, _ := s.meta.Holds(ctx, tn)
	in, _ := s.meta.PendingIntents(ctx, tn)
	os, _ := s.mem.List(ctx, "", "", 0)
	return counts{len(as), len(ms), len(rs), len(ps), len(hs), len(in), len(os)}
}

func (s *side) export(t *testing.T, sel Select) []byte {
	t.Helper()
	var buf bytes.Buffer
	if _, err := Export(ctx, Source{Store: s.store, Meta: s.meta}, "acme", sel, &buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func (s *side) imp(b []byte, tn meta.Tenant) (Result, error) {
	return Import(ctx, Dest{Store: s.store, Meta: s.meta}, tn, bytes.NewReader(b), Limits{TempDir: ""})
}

type rec struct {
	tag          byte
	start, n, at int
}

func records(t *testing.T, b []byte) []rec {
	t.Helper()
	var out []rec
	for pos := len(magic); pos < len(b)-trailerBytes; {
		n := int(binary.BigEndian.Uint64(b[pos+1 : pos+9]))
		out = append(out, rec{tag: b[pos], start: pos + 9, n: n, at: pos})
		pos += 9 + n
	}
	return out
}

func reseal(b []byte) []byte {
	out := append([]byte(nil), b...)
	sum := sha256.Sum256(out[:len(out)-trailerBytes])
	copy(out[len(out)-sha256.Size:], sum[:])
	return out
}

func mustFailUntouched(t *testing.T, dst *side, b []byte, tn meta.Tenant, what string) {
	t.Helper()
	before := dst.counts("acme")
	if _, err := dst.imp(b, tn); err == nil {
		t.Fatalf("%s: a damaged bundle was accepted", what)
	}
	if after := dst.counts("acme"); after != before {
		t.Fatalf("%s: a failed import left state behind: %+v -> %+v", what, before, after)
	}
}

func TestExportThenImportReproducesVersionsRootsPinsAndHolds(t *testing.T) {
	pub, priv := keys(t)
	src, dst := newSide(t, pub, priv), newSide(t, pub, nil)
	v1, v2 := src.populate(t)
	b := src.export(t, Select{})
	res, err := dst.imp(b, "acme")
	if err != nil {
		t.Fatal(err)
	}
	if res != (Result{Artifacts: 3, Manifests: 2, Roots: 1, Pins: 1, Holds: 1}) {
		t.Fatalf("%+v", res)
	}
	for _, id := range []string{v1, v2} {
		if err := dst.store.VerifyManifest(ctx, "acme", id); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
	}
	if r, err := dst.meta.Root(ctx, "acme", "stable"); err != nil || r.ManifestID != v2 {
		t.Fatalf("%+v %v", r, err)
	}
	if ps, _ := dst.meta.Pins(ctx, "acme"); len(ps) != 1 || ps[0].Entitlement != "enterprise" {
		t.Fatalf("%+v", ps)
	}
	a, _ := dst.meta.Artifact(ctx, "acme", artifact.Digest([]byte("customer plan v2 with more steps")))
	if a.Provenance != `{"note":"second"}` || a.Class != meta.Private {
		t.Fatalf("%+v", a)
	}
	if !bytes.Equal(dst.export(t, Select{}), b) {
		t.Fatal("exporting the imported store must give the same bytes")
	}
	if c := dst.counts("acme"); c.intents != 0 {
		t.Fatalf("the import intent is closed: %+v", c)
	}
}

func TestExportIsDeterministic(t *testing.T) {
	pub, priv := keys(t)
	src := newSide(t, pub, priv)
	src.populate(t)
	if !bytes.Equal(src.export(t, Select{}), src.export(t, Select{})) {
		t.Fatal("two exports of one state must be byte-identical")
	}
}

func TestImportingTwiceChangesNothingTheSecondTime(t *testing.T) {
	pub, priv := keys(t)
	src, dst := newSide(t, pub, priv), newSide(t, pub, nil)
	src.populate(t)
	b := src.export(t, Select{})
	if _, err := dst.imp(b, "acme"); err != nil {
		t.Fatal(err)
	}
	before := dst.counts("acme")
	res, err := dst.imp(b, "acme")
	if err != nil || res.Artifacts != 0 || res.Manifests != 0 || res.ArtifactsPresent != 3 || res.ManifestsPresent != 2 {
		t.Fatalf("%+v %v", res, err)
	}
	if dst.counts("acme") != before {
		t.Fatal("a repeat import must not change the store")
	}
}

func TestSelectingAVersionBringsItsAncestorsButNotItsDescendants(t *testing.T) {
	pub, priv := keys(t)
	src, dst := newSide(t, pub, priv), newSide(t, pub, nil)
	v1, v2 := src.populate(t)
	res, err := dst.imp(src.export(t, Select{Manifests: []string{v1}}), "acme")
	if err != nil || res.Manifests != 1 || res.Roots != 0 || res.Pins != 1 || res.Holds != 0 {
		t.Fatalf("%+v %v", res, err)
	}
	if _, err := dst.meta.Manifest(ctx, "acme", v2); !errors.Is(err, meta.ErrNotFound) {
		t.Fatal("the child must not travel with its parent")
	}
	if _, err := Export(ctx, Source{Store: src.store, Meta: src.meta}, "acme", Select{Manifests: []string{"sha256:" + string(bytes.Repeat([]byte("0"), 64))}}, &bytes.Buffer{}); !errors.Is(err, meta.ErrNotFound) {
		t.Fatalf("an unknown version is refused: %v", err)
	}
	dst2 := newSide(t, pub, nil)
	if res, err := dst2.imp(src.export(t, Select{Manifests: []string{v2}}), "acme"); err != nil || res.Manifests != 2 {
		t.Fatalf("a child brings its parent: %+v %v", res, err)
	}
}

func TestAnyDamagedByteOrTruncationFailsWithoutTouchingTheStore(t *testing.T) {
	pub, priv := keys(t)
	src, dst := newSide(t, pub, priv), newSide(t, pub, nil)
	src.populate(t)
	b := src.export(t, Select{})
	for i := range b {
		if i > 128 && i < len(b)-128 && i%5 != 0 {
			continue
		}
		bad := append([]byte(nil), b...)
		bad[i] ^= 0xFF
		mustFailUntouched(t, dst, bad, "acme", "flip byte")
	}
	for n := 0; n < len(b); n += 7 {
		mustFailUntouched(t, dst, b[:n], "acme", "truncate")
	}
	mustFailUntouched(t, dst, b[:len(b)-1], "acme", "truncate by one")
	mustFailUntouched(t, dst, append(append([]byte(nil), b...), 0), "acme", "trailing byte")
	if c := dst.counts("acme"); c != (counts{}) {
		t.Fatalf("nothing was ever written: %+v", c)
	}
	if _, err := dst.imp(b, "acme"); err != nil {
		t.Fatalf("the intact bundle still imports: %v", err)
	}
}

func TestTamperingWithAResealedChecksumIsStillCaughtByDigestsAndSignatures(t *testing.T) {
	pub, priv := keys(t)
	src, dst := newSide(t, pub, priv), newSide(t, pub, nil)
	src.populate(t)
	b := src.export(t, Select{})
	var arts, mans int
	for _, r := range records(t, b) {
		switch r.tag {
		case tagArtifact:
			arts++
			bad := append([]byte(nil), b...)
			bad[r.start+r.n-1] ^= 1
			mustFailUntouched(t, dst, reseal(bad), "acme", "artifact bytes")
		case tagManifest:
			mans++
			for _, off := range []int{20, r.n / 2, r.n - 10} {
				bad := append([]byte(nil), b...)
				bad[r.start+off] ^= 1
				mustFailUntouched(t, dst, reseal(bad), "acme", "manifest")
			}
		}
	}
	if arts != 3 || mans != 2 {
		t.Fatalf("%d %d", arts, mans)
	}
}

func TestADroppedArtifactLeavesAManifestDanglingAndIsRefused(t *testing.T) {
	pub, priv := keys(t)
	src, dst := newSide(t, pub, priv), newSide(t, pub, nil)
	src.populate(t)
	b := src.export(t, Select{})
	rs := records(t, b)
	var cut rec
	for _, r := range rs {
		if r.tag == tagArtifact {
			cut = r
			break
		}
	}
	bad := append(append([]byte(nil), b[:cut.at]...), b[cut.start+cut.n:]...)
	mustFailUntouched(t, dst, reseal(bad), "acme", "dropped artifact")
}

func TestABundleForAnotherTenantOrSignedByAnUntrustedKeyIsRefused(t *testing.T) {
	pub, priv := keys(t)
	src := newSide(t, pub, priv)
	src.populate(t)
	b := src.export(t, Select{})
	dst := newSide(t, pub, nil)
	before := dst.counts("globex")
	if _, err := dst.imp(b, "globex"); !errors.Is(err, ErrTenant) {
		t.Fatalf("%v", err)
	}
	if dst.counts("globex") != before {
		t.Fatal("a refused import changes nothing")
	}
	otherPub, _ := keys(t)
	stranger := newSide(t, otherPub, nil)
	if _, err := stranger.imp(b, "acme"); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("a manifest signed by a key this store does not trust must be refused: %v", err)
	}
	if c := stranger.counts("acme"); c != (counts{}) {
		t.Fatalf("%+v", c)
	}
}

func TestAnImportThatDiesPartWayIsCompletedByImportingAgain(t *testing.T) {
	for phase, steps := range map[string]int{"artifacts": 3, "manifests": 2} {
		for at := 0; at < steps; at++ {
			pub, priv := keys(t)
			src, dst := newSide(t, pub, priv), newSide(t, pub, nil)
			v1, v2 := src.populate(t)
			b := src.export(t, Select{})
			crash := func(p string, n int) bool { return p == phase && n == at }
			if _, err := Import(ctx, Dest{Store: dst.store, Meta: dst.meta, CrashAt: crash}, "acme", bytes.NewReader(b), Limits{}); !errors.Is(err, ErrCrashed) {
				t.Fatalf("%s@%d: %v", phase, at, err)
			}
			ms, _ := dst.meta.Manifests(ctx, "acme", "", 0)
			for _, m := range ms {
				if err := dst.store.VerifyManifest(ctx, "acme", m.ID); err != nil {
					t.Fatalf("%s@%d: a visible manifest is broken: %v", phase, at, err)
				}
			}
			if _, err := dst.imp(b, "acme"); err != nil {
				t.Fatalf("%s@%d: %v", phase, at, err)
			}
			for _, id := range []string{v1, v2} {
				if err := dst.store.VerifyManifest(ctx, "acme", id); err != nil {
					t.Fatalf("%s@%d: %v", phase, at, err)
				}
			}
			if !bytes.Equal(dst.export(t, Select{}), b) {
				t.Fatalf("%s@%d: the completed import differs from the source", phase, at)
			}
		}
	}
}

func TestAnImportNeverMovesAChannelThatAlreadyPointsElsewhere(t *testing.T) {
	pub, priv := keys(t)
	src, dst := newSide(t, pub, priv), newSide(t, pub, priv)
	_, v2 := src.populate(t)
	d := dst.put("acme", "local plan", meta.Private, "")
	local := dst.publish("acme", "local", "", d)
	if _, err := dst.meta.SetRoot(ctx, "acme", "stable", local, 0); err != nil {
		t.Fatal(err)
	}
	res, err := dst.imp(src.export(t, Select{}), "acme")
	if err != nil || res.RootsKept != 1 || res.Roots != 0 {
		t.Fatalf("%+v %v", res, err)
	}
	if r, _ := dst.meta.Root(ctx, "acme", "stable"); r.ManifestID != local {
		t.Fatal("the live channel must stay where it was")
	}
	if _, err := dst.meta.Manifest(ctx, "acme", v2); err != nil {
		t.Fatal("the imported version is still there to be released deliberately")
	}
}

func TestImportRefusesABundleOverTheSizeLimit(t *testing.T) {
	pub, priv := keys(t)
	src, dst := newSide(t, pub, priv), newSide(t, pub, nil)
	src.populate(t)
	b := src.export(t, Select{})
	if _, err := Import(ctx, Dest{Store: dst.store, Meta: dst.meta}, "acme", bytes.NewReader(b), Limits{MaxBytes: int64(len(b) - 1)}); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("%v", err)
	}
	if dst.counts("acme") != (counts{}) {
		t.Fatal("nothing written")
	}
}
