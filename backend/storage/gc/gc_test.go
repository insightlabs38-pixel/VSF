package gc

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/insightlabs38-pixel/vsf/backend/storage/artifact"
	"github.com/insightlabs38-pixel/vsf/backend/storage/blob"
	"github.com/insightlabs38-pixel/vsf/backend/storage/meta"
)

var ctx = context.Background()

const hour = int64(time.Hour / time.Millisecond)

type rig struct {
	t     *testing.T
	path  string
	meta  *meta.SQLite
	mem   *blob.Mem
	store *artifact.Store
	gc    *Collector
	clock atomic.Int64
	pub   ed25519.PublicKey
	priv  ed25519.PrivateKey
}

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{t: t, path: filepath.Join(t.TempDir(), "meta.db"), mem: blob.NewMem()}
	r.clock.Store(1_000 * hour)
	r.mem.Now = r.clock.Load
	var err error
	if r.pub, r.priv, err = ed25519.GenerateKey(rand.Reader); err != nil {
		t.Fatal(err)
	}
	r.open()
	return r
}

func (r *rig) open() {
	r.t.Helper()
	var err error
	if r.meta, err = meta.OpenSQLite(ctx, r.path, meta.Options{Now: r.clock.Load}); err != nil {
		r.t.Fatal(err)
	}
	r.t.Cleanup(func() { _ = r.meta.Close() })
	r.store, err = artifact.New(artifact.Config{Meta: r.meta, Blobs: r.mem, Now: r.clock.Load,
		Signer: &artifact.Signer{KeyID: "k1", Key: r.priv}, Trust: artifact.Trust{Keys: map[string]ed25519.PublicKey{"k1": r.pub}, Revoked: map[string]bool{}}})
	if err != nil {
		r.t.Fatal(err)
	}
	r.gc = &Collector{Meta: r.meta, Maint: r.meta, Blobs: r.mem, Exclusive: r.store.Exclusive, Now: r.clock.Load}
}

func (r *rig) advance(h int64) { r.clock.Add(h * hour) }

// version publishes one single-artifact manifest and returns its id and artifact digest.
func (r *rig) version(tn meta.Tenant, body string, class meta.Class, parent string) (string, string) {
	r.t.Helper()
	d, err := r.store.PutArtifact(ctx, tn, []byte(body), artifact.ArtifactInput{Class: class})
	if err != nil {
		r.t.Fatal(err)
	}
	id, err := r.store.PublishManifest(ctx, tn, artifact.ManifestInput{Kind: "plan", Version: body, Parent: parent,
		Artifacts: []artifact.ArtifactRef{{Role: "body", Digest: d}}})
	if err != nil {
		r.t.Fatal(err)
	}
	return id, d
}

func (r *rig) release(tn meta.Tenant, channel, id string) {
	r.t.Helper()
	cur, err := r.meta.Root(ctx, tn, channel)
	seq := int64(0)
	if err == nil {
		seq = cur.Seq
	} else if !errors.Is(err, meta.ErrNotFound) {
		r.t.Fatal(err)
	}
	if _, err := r.meta.SetRoot(ctx, tn, channel, id, seq); err != nil {
		r.t.Fatal(err)
	}
}

func (r *rig) exists(tn meta.Tenant, class meta.Class, digest string) bool {
	_, err := r.mem.Stat(ctx, artifact.DefaultLayout{}.Key(tn, class, digest))
	return err == nil
}

func (r *rig) manifestExists(tn meta.Tenant, id string) bool {
	_, err := r.meta.Manifest(ctx, tn, id)
	return err == nil
}

func (r *rig) mustCollect(tn meta.Tenant, p Policy) Report {
	r.t.Helper()
	rep, err := r.gc.Collect(ctx, tn, p)
	if err != nil {
		r.t.Fatal(err)
	}
	return rep
}

// consistent proves nothing visible is broken: every manifest verifies and every record has its object.
func (r *rig) consistent(tn meta.Tenant) {
	r.t.Helper()
	ms, err := r.meta.Manifests(ctx, tn, "", 0)
	if err != nil {
		r.t.Fatal(err)
	}
	for _, m := range ms {
		if err := r.store.VerifyManifest(ctx, tn, m.ID); err != nil {
			r.t.Fatalf("manifest %s no longer verifies: %v", m.ID, err)
		}
	}
	as, err := r.meta.Artifacts(ctx, tn, "", 0)
	if err != nil {
		r.t.Fatal(err)
	}
	for _, a := range as {
		if !r.exists(tn, a.Class, a.Digest) {
			r.t.Fatalf("artifact %s has a record but no object", a.Digest)
		}
	}
}

func TestOldUnprotectedVersionsAreRemovedWithTheirObjects(t *testing.T) {
	r := newRig(t)
	old, oldD := r.version("acme", "v1", meta.Private, "")
	r.advance(2)
	cur, curD := r.version("acme", "v2", meta.Private, "")
	r.release("acme", "stable", cur)
	r.advance(2)
	rep := r.mustCollect("acme", Policy{})
	if len(rep.ManifestsDeleted) != 1 || rep.ManifestsDeleted[0] != old || rep.ArtifactsDeleted != 1 || rep.ObjectsDeleted != 1 || rep.BytesReclaimed != 2 {
		t.Fatalf("%+v", rep)
	}
	if r.manifestExists("acme", old) || r.exists("acme", meta.Private, oldD) {
		t.Fatal("the old version and its object are gone")
	}
	if !r.manifestExists("acme", cur) || !r.exists("acme", meta.Private, curD) || rep.Kept[ByRoot] != 1 {
		t.Fatalf("the released version stays: %+v", rep)
	}
	r.consistent("acme")
}

func TestPinsHoldsLeasesRootHistoryAndYoungVersionsAllSurvive(t *testing.T) {
	r := newRig(t)
	pinned, _ := r.version("acme", "pinned", meta.Private, "")
	held, _ := r.version("acme", "held", meta.Private, "")
	leased, _ := r.version("acme", "leased", meta.Private, "")
	hist, _ := r.version("acme", "history", meta.Private, "")
	gone, _ := r.version("acme", "gone", meta.Private, "")
	r.release("acme", "stable", hist)
	head, _ := r.version("acme", "head", meta.Private, "")
	r.release("acme", "stable", head)
	if err := r.meta.Pin(ctx, "acme", meta.Pin{ManifestID: pinned, Reason: "customer-contract"}); err != nil {
		t.Fatal(err)
	}
	if err := r.meta.Hold(ctx, "acme", meta.Hold{ManifestID: held, Holder: "legal", Reason: "case 7"}); err != nil {
		t.Fatal(err)
	}
	if err := r.meta.Acquire(ctx, "acme", meta.Lease{ID: "run-1", ManifestID: leased, Owner: "worker", ExpiresAt: r.clock.Load() + 100*hour}); err != nil {
		t.Fatal(err)
	}
	r.advance(2)
	young, _ := r.version("acme", "young", meta.Private, "")
	rep := r.mustCollect("acme", Policy{KeepRootHistory: 1})
	for name, id := range map[string]string{"pinned": pinned, "held": held, "leased": leased, "history": hist, "head": head, "young": young} {
		if !r.manifestExists("acme", id) {
			t.Fatalf("%s was collected", name)
		}
	}
	if r.manifestExists("acme", gone) || len(rep.ManifestsDeleted) != 1 {
		t.Fatalf("only the unprotected version goes: %+v", rep)
	}
	for _, k := range []Reason{ByPin, ByHold, ByLease, ByHistory, ByRoot, ByMinAge} {
		if rep.Kept[k] != 1 {
			t.Fatalf("%s: %+v", k, rep.Kept)
		}
	}
	r.consistent("acme")

	r.advance(200)
	rep = r.mustCollect("acme", Policy{KeepRootHistory: 1})
	if r.manifestExists("acme", leased) || r.manifestExists("acme", young) {
		t.Fatalf("an expired lease and an aged version no longer protect: %+v", rep)
	}
	for _, id := range []string{pinned, held, hist, head} {
		if !r.manifestExists("acme", id) {
			t.Fatalf("%s lost though still protected", id)
		}
	}
	if err := r.meta.Unpin(ctx, "acme", pinned, "customer-contract"); err != nil {
		t.Fatal(err)
	}
	if err := r.meta.Release(ctx, "acme", held, "legal"); err != nil {
		t.Fatal(err)
	}
	r.mustCollect("acme", Policy{KeepRootHistory: 1})
	if r.manifestExists("acme", pinned) || r.manifestExists("acme", held) {
		t.Fatal("releasing a pin or hold makes the version collectable")
	}
	if !r.manifestExists("acme", head) || !r.manifestExists("acme", hist) {
		t.Fatal("the root and its kept history stay")
	}
	r.consistent("acme")
}

func TestPinnedVersionSurvivesCollectionAndRestart(t *testing.T) {
	r := newRig(t)
	pinned, d := r.version("acme", "contracted", meta.Private, "")
	if err := r.meta.Pin(ctx, "acme", meta.Pin{ManifestID: pinned, Reason: "contract"}); err != nil {
		t.Fatal(err)
	}
	r.advance(500)
	r.mustCollect("acme", Policy{})
	if err := r.meta.Close(); err != nil {
		t.Fatal(err)
	}
	r.open()
	r.mustCollect("acme", Policy{})
	if !r.manifestExists("acme", pinned) || !r.exists("acme", meta.Private, d) {
		t.Fatal("the pinned version did not survive")
	}
	if err := r.store.VerifyManifest(ctx, "acme", pinned); err != nil {
		t.Fatal(err)
	}
}

func TestAncestorsOfLiveVersionsAreKept(t *testing.T) {
	r := newRig(t)
	a, _ := r.version("acme", "a", meta.Private, "")
	b, _ := r.version("acme", "b", meta.Private, a)
	c, _ := r.version("acme", "c", meta.Private, b)
	lone, _ := r.version("acme", "lone", meta.Private, "")
	r.release("acme", "stable", c)
	r.advance(5)
	rep := r.mustCollect("acme", Policy{})
	for _, id := range []string{a, b, c} {
		if !r.manifestExists("acme", id) {
			t.Fatal("a chain is kept whole while its head is live")
		}
	}
	if r.manifestExists("acme", lone) || rep.Kept[ByParent] != 2 {
		t.Fatalf("%+v", rep)
	}
	next, _ := r.version("acme", "next", meta.Private, "")
	r.release("acme", "stable", next)
	rep = r.mustCollect("acme", Policy{})
	if len(rep.ManifestsDeleted) != 3 || r.manifestExists("acme", a) {
		t.Fatalf("a dead chain goes children first: %+v", rep)
	}
	r.consistent("acme")
}

func TestSharedPublicObjectsOutliveOneTenantsRecord(t *testing.T) {
	r := newRig(t)
	_, d := r.version("alpha", "shared-bytes", meta.Public, "")
	keep, d2 := r.version("beta", "shared-bytes", meta.Public, "")
	r.version("alpha", "private-bytes", meta.Private, "")
	r.release("beta", "stable", keep)
	r.advance(5)
	rep := r.mustCollect("alpha", Policy{})
	if d != d2 || rep.SharedObjectsKept != 1 || !r.exists("beta", meta.Public, d) {
		t.Fatalf("beta still needs the shared object: %+v", rep)
	}
	if rep.ObjectsDeleted != 1 {
		t.Fatalf("only alpha's private object is deleted: %+v", rep)
	}
	r.consistent("beta")
}

func TestDryRunReportsAndChangesNothing(t *testing.T) {
	r := newRig(t)
	old, d := r.version("acme", "v1", meta.Private, "")
	r.advance(5)
	cur, _ := r.version("acme", "v2", meta.Private, "")
	r.release("acme", "stable", cur)
	rep := r.mustCollect("acme", Policy{DryRun: true})
	if len(rep.ManifestsDeleted) != 1 || rep.ArtifactsDeleted != 1 || rep.BytesReclaimed != 2 {
		t.Fatalf("%+v", rep)
	}
	if !r.manifestExists("acme", old) || !r.exists("acme", meta.Private, d) {
		t.Fatal("a dry run deletes nothing")
	}
	if p, _ := r.meta.PendingIntents(ctx, "acme"); len(p) != 0 {
		t.Fatal("a dry run leaves no intent")
	}
}

func TestCollectionThatDiesAtAnyPointIsFinishedByRecovery(t *testing.T) {
	phases := []string{"manifests", "artifacts", "objects"}
	for _, phase := range phases {
		for at := 0; at < 4; at++ {
			t.Run(fmt.Sprintf("%s@%d", phase, at), func(t *testing.T) {
				r := newRig(t)
				var keep []string
				for i := 0; i < 5; i++ {
					id, _ := r.version("acme", fmt.Sprintf("old-%d", i), meta.Private, "")
					_ = id
				}
				cur, curD := r.version("acme", "current", meta.Private, "")
				pin, pinD := r.version("acme", "pinned", meta.Private, "")
				keep = append(keep, cur, pin)
				r.release("acme", "stable", cur)
				if err := r.meta.Pin(ctx, "acme", meta.Pin{ManifestID: pin, Reason: "p"}); err != nil {
					t.Fatal(err)
				}
				r.advance(5)
				r.gc.CrashAt = func(p string, done int) bool { return p == phase && done == at }
				if _, err := r.gc.Collect(ctx, "acme", Policy{}); !errors.Is(err, ErrCrashed) {
					t.Fatalf("expected the simulated crash, got %v", err)
				}
				r.consistent("acme")
				r.gc.CrashAt = nil
				n, err := r.gc.Recover(ctx, "acme")
				if err != nil || n != 1 {
					t.Fatalf("recover %d %v", n, err)
				}
				ms, _ := r.meta.Manifests(ctx, "acme", "", 0)
				if len(ms) != 2 {
					t.Fatalf("recovery finishes the sweep: %d manifests left", len(ms))
				}
				for _, id := range keep {
					if !r.manifestExists("acme", id) {
						t.Fatal("a protected version was lost")
					}
				}
				if !r.exists("acme", meta.Private, curD) || !r.exists("acme", meta.Private, pinD) {
					t.Fatal("a protected object was lost")
				}
				// A death between the record and its object leaves an orphan, which only the grace-delayed sweep may take.
				r.advance(2)
				if _, err := r.gc.SweepOrphans(ctx, Policy{}); err != nil {
					t.Fatal(err)
				}
				objs, _ := r.mem.List(ctx, "", "", 0)
				if len(objs) != 2 {
					t.Fatalf("no object is left behind: %d", len(objs))
				}
				r.consistent("acme")
				if p, _ := r.meta.PendingIntents(ctx, "acme"); len(p) != 0 {
					t.Fatalf("intents closed: %+v", p)
				}
			})
		}
	}
}

func TestOrphanObjectsGoOnlyAfterTheGraceAndOnlyWhenUnclaimed(t *testing.T) {
	r := newRig(t)
	id, d := r.version("acme", "claimed", meta.Private, "")
	r.release("acme", "stable", id)
	if err := r.mem.Put(ctx, artifact.DefaultLayout{}.Key("acme", meta.Private, artifact.Digest([]byte("torn"))), []byte("torn")); err != nil {
		t.Fatal(err)
	}
	if err := r.mem.Put(ctx, "other/foreign-object", []byte("not ours")); err != nil {
		t.Fatal(err)
	}
	if err := r.mem.Put(ctx, artifact.DefaultLayout{}.Key("", meta.Public, artifact.Digest([]byte("pub-torn"))), []byte("pub-torn")); err != nil {
		t.Fatal(err)
	}
	rep, err := r.gc.SweepOrphans(ctx, Policy{})
	if err != nil || rep.OrphansDeleted != 0 {
		t.Fatalf("young orphans are protected: %+v %v", rep, err)
	}
	r.advance(2)
	dry, err := r.gc.SweepOrphans(ctx, Policy{DryRun: true})
	if err != nil || dry.OrphansDeleted != 2 {
		t.Fatalf("%+v %v", dry, err)
	}
	rep, err = r.gc.SweepOrphans(ctx, Policy{})
	if err != nil || rep.OrphansDeleted != 2 || rep.BytesReclaimed != int64(len("torn")+len("pub-torn")) {
		t.Fatalf("%+v %v", rep, err)
	}
	if !r.exists("acme", meta.Private, d) {
		t.Fatal("a claimed object must stay")
	}
	if _, err := r.mem.Stat(ctx, "other/foreign-object"); err != nil {
		t.Fatal("a foreign object must never be touched")
	}
	r.consistent("acme")
}

func TestCollectionRacingPublishesNeverBreaksAVisibleVersion(t *testing.T) {
	r := newRig(t)
	for i := 0; i < 20; i++ {
		r.version("acme", fmt.Sprintf("stale-%d", i), meta.Private, "")
	}
	r.advance(10)
	var wg sync.WaitGroup
	stop := make(chan struct{})
	errs := make(chan error, 8)
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 120; i++ {
				select {
				case <-stop:
					return
				default:
				}
				// Re-storing bytes that already have an old record is exactly what a sweep must not race.
				body := fmt.Sprintf("stale-%d", i%20)
				if i%3 == 0 {
					body = fmt.Sprintf("fresh-%d-%d", w, i)
				}
				d, err := r.store.PutArtifact(ctx, "acme", []byte(body), artifact.ArtifactInput{Class: meta.Private})
				if err != nil {
					errs <- err
					return
				}
				_, err = r.store.PublishManifest(ctx, "acme", artifact.ManifestInput{Kind: "plan", Version: fmt.Sprintf("%d-%d", w, i),
					Artifacts: []artifact.ArtifactRef{{Role: "body", Digest: d}}})
				if err != nil && !errors.Is(err, artifact.ErrMissingArtifact) {
					errs <- err
					return
				}
			}
		}(w)
	}
	for i := 0; i < 15; i++ {
		if _, err := r.gc.Collect(ctx, "acme", Policy{MinAge: time.Millisecond}); err != nil {
			t.Fatal(err)
		}
		r.advance(1)
	}
	close(stop)
	wg.Wait()
	select {
	case err := <-errs:
		t.Fatal(err)
	default:
	}
	r.consistent("acme")
}
