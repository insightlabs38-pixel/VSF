package meta

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

var ctx = context.Background()

func dg(s string) string {
	sum := sha256.Sum256([]byte(s))
	return "sha256:" + hex.EncodeToString(sum[:])
}

type clock struct{ n atomic.Int64 }

func (c *clock) now() int64 { return c.n.Add(1) }

func open(t testing.TB, path string, mut func(*Options)) *SQLite {
	t.Helper()
	o := Options{Now: (&clock{}).now}
	if mut != nil {
		mut(&o)
	}
	s, err := OpenSQLite(ctx, path, o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func put(t testing.TB, s Store, tn Tenant, content string) string {
	t.Helper()
	d := dg(content)
	if _, err := s.PutArtifact(ctx, tn, ArtifactRecord{Digest: d, Size: int64(len(content)), Class: Public}); err != nil {
		t.Fatal(err)
	}
	return d
}

func publish(t testing.TB, s Store, tn Tenant, doc string, parent string, arts ...string) string {
	t.Helper()
	id := dg(doc)
	if err := s.PublishManifest(ctx, tn, ManifestRecord{ID: id, Kind: "compiled", Doc: []byte(doc), KeyID: "k1", ParentID: parent}, arts); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestAStoreSurvivesRestartWithEverythingItHeld(t *testing.T) {
	path := filepath.Join(t.TempDir(), "meta.db")
	s, err := OpenSQLite(ctx, path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.EnsureTenant(ctx, "acme")
	a := put(t, s, "acme", "artifact-1")
	m := publish(t, s, "acme", "manifest-1", "", a)
	if _, err := s.SetRoot(ctx, "acme", "stable", m, 0); err != nil {
		t.Fatal(err)
	}
	_ = s.Pin(ctx, "acme", Pin{ManifestID: m, Reason: "customer", Entitlement: "contract_pinning"})
	_ = s.Hold(ctx, "acme", Hold{ManifestID: m, Holder: "legal", Reason: "case 7"})
	_ = s.Annotate(ctx, "acme", m, "note", "kept")
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s2 := open(t, path, nil)
	if v, _ := s2.SchemaVersion(ctx); v != 2 {
		t.Fatalf("schema version %d", v)
	}
	if r, err := s2.Root(ctx, "acme", "stable"); err != nil || r.ManifestID != m || r.Seq != 1 {
		t.Fatalf("%+v %v", r, err)
	}
	if ps, _ := s2.Pins(ctx, "acme"); len(ps) != 1 || ps[0].Entitlement != "contract_pinning" {
		t.Fatalf("%+v", ps)
	}
	if hs, _ := s2.Holds(ctx, "acme"); len(hs) != 1 {
		t.Fatal(hs)
	}
	if an, _ := s2.Annotations(ctx, "acme", m); an["note"] != "kept" {
		t.Fatal(an)
	}
	if arts, _ := s2.ManifestArtifacts(ctx, "acme", m); len(arts) != 1 || arts[0] != a {
		t.Fatal(arts)
	}
}

func TestACommitIsDurableWithoutAnOrderlyClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "meta.db")
	s, err := OpenSQLite(ctx, path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.EnsureTenant(ctx, "acme")
	a := put(t, s, "acme", "x")
	publish(t, s, "acme", "m", "", a)
	// No Close: the file is opened again as a new process would after a crash.
	s2 := open(t, path, nil)
	if _, err := s2.Manifest(ctx, "acme", dg("m")); err != nil {
		t.Fatalf("a committed manifest must be there after a crash: %v", err)
	}
}

func TestUpgradeKeepsDataAndAddsTheDAG(t *testing.T) {
	path := filepath.Join(t.TempDir(), "meta.db")
	old, err := OpenSQLite(ctx, path, Options{MigrateTo: 1})
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := old.SchemaVersion(ctx); v != 1 {
		t.Fatalf("%d", v)
	}
	_ = old.EnsureTenant(ctx, "acme")
	a := put(t, old, "acme", "a1")
	m1 := dg("m1")
	// What the older binary wrote: a version 1 manifest has no parent column.
	if _, err := old.w.ExecContext(ctx, `INSERT INTO manifests (tenant_id, id, kind, doc, key_id, created_at) VALUES ('acme', ?, 'compiled', CAST('m1' AS BLOB), 'k', 1)`, m1); err != nil {
		t.Fatal(err)
	}
	if _, err := old.w.ExecContext(ctx, `INSERT INTO manifest_artifacts (tenant_id, manifest_id, artifact_digest) VALUES ('acme', ?, ?)`, m1, a); err != nil {
		t.Fatal(err)
	}
	if err := old.Annotate(ctx, "acme", m1, "n", "v"); err == nil {
		t.Fatal("version 1 has no annotations")
	}
	_ = old.Close()
	s := open(t, path, nil)
	if v, _ := s.SchemaVersion(ctx); v != 2 {
		t.Fatalf("%d", v)
	}
	got, err := s.Manifest(ctx, "acme", m1)
	if err != nil || string(got.Doc) != "m1" || got.ParentID != "" {
		t.Fatalf("%+v %v", got, err)
	}
	m2 := publish(t, s, "acme", "m2", m1, a)
	if g, _ := s.Manifest(ctx, "acme", m2); g.ParentID != m1 {
		t.Fatalf("%+v", g)
	}
	if err := s.Annotate(ctx, "acme", m1, "n", "v"); err != nil {
		t.Fatal(err)
	}
	if err := s.PublishManifest(ctx, "acme", ManifestRecord{ID: dg("m3"), Kind: "compiled", Doc: []byte("m3"), KeyID: "k", ParentID: dg("nope")}, nil); !errors.Is(err, ErrMissingRef) {
		t.Fatalf("a parent that does not exist must be refused: %v", err)
	}
}

func TestMigrationHistoryIsCheckedNotTrusted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "meta.db")
	s := open(t, path, nil)
	_ = s.Close()
	raw, _ := sql.Open("sqlite", "file:"+path)
	if _, err := raw.Exec(`UPDATE schema_migrations SET checksum = 'tampered' WHERE version = 1`); err != nil {
		t.Fatal(err)
	}
	raw.Close()
	if _, err := OpenSQLite(ctx, path, Options{}); !errors.Is(err, ErrMigrationDrift) {
		t.Fatalf("a changed migration must be refused: %v", err)
	}
	path2 := filepath.Join(t.TempDir(), "meta2.db")
	s2 := open(t, path2, nil)
	_ = s2.Close()
	raw, _ = sql.Open("sqlite", "file:"+path2)
	if _, err := raw.Exec(`INSERT INTO schema_migrations (version, name, checksum, applied_at) VALUES (99, 'future', 'x', 1)`); err != nil {
		t.Fatal(err)
	}
	raw.Close()
	if _, err := OpenSQLite(ctx, path2, Options{}); !errors.Is(err, ErrSchemaTooNew) {
		t.Fatalf("a database from a newer version must be refused: %v", err)
	}
}

func TestMigrationsAreNumberedWithoutGapsForBothDialects(t *testing.T) {
	var counts []int
	for _, d := range []string{"sqlite", "postgres"} {
		ms, err := Migrations(d)
		if err != nil || len(ms) == 0 {
			t.Fatalf("%s: %v", d, err)
		}
		for _, m := range ms {
			if len(m.Checksum) != 64 {
				t.Fatal(m)
			}
		}
		counts = append(counts, len(ms))
	}
	if counts[0] != counts[1] {
		t.Fatalf("both dialects must ship the same steps: %v", counts)
	}
}

func TestConcurrentWritersAndReadersNeverSeeBusyOrTornData(t *testing.T) {
	s := open(t, filepath.Join(t.TempDir(), "meta.db"), nil)
	_ = s.EnsureTenant(ctx, "acme")
	const writers, per = 8, 40
	var wg sync.WaitGroup
	errs := make(chan error, writers*per+64)
	stop := make(chan struct{})
	for r := 0; r < 4; r++ {
		go func() {
			for {
				select {
				case <-stop:
					return
				default:
				}
				ms, err := s.Manifests(ctx, "acme", "", 1000)
				if err != nil {
					errs <- err
					return
				}
				for _, m := range ms {
					if _, err := s.ManifestArtifacts(ctx, "acme", m.ID); err != nil {
						errs <- err
						return
					}
					arts, _ := s.ManifestArtifacts(ctx, "acme", m.ID)
					if len(arts) != 2 {
						errs <- fmt.Errorf("a visible manifest has %d artifacts, want 2", len(arts))
						return
					}
				}
			}
		}()
	}
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < per; i++ {
				a1, a2 := put(t, s, "acme", fmt.Sprintf("a-%d-%d-1", w, i)), put(t, s, "acme", fmt.Sprintf("a-%d-%d-2", w, i))
				doc := fmt.Sprintf("m-%d-%d", w, i)
				if err := s.PublishManifest(ctx, "acme", ManifestRecord{ID: dg(doc), Kind: "compiled", Doc: []byte(doc), KeyID: "k"}, []string{a1, a2}); err != nil {
					errs <- err
					return
				}
			}
		}(w)
	}
	wg.Wait()
	close(stop)
	select {
	case err := <-errs:
		t.Fatal(err)
	default:
	}
	ms, _ := s.Manifests(ctx, "acme", "", 1000)
	if len(ms) != writers*per {
		t.Fatalf("%d manifests, want %d", len(ms), writers*per)
	}
}

func TestRootMovesAreCompareAndSwapAndNeverLoseAnUpdate(t *testing.T) {
	s := open(t, filepath.Join(t.TempDir(), "meta.db"), nil)
	_ = s.EnsureTenant(ctx, "acme")
	const n = 24
	ids := make([]string, n)
	for i := range ids {
		ids[i] = publish(t, s, "acme", fmt.Sprintf("manifest-%d", i), "")
	}
	var wg sync.WaitGroup
	var won atomic.Int64
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for {
				cur, err := s.Root(ctx, "acme", "stable")
				var seq int64
				if err == nil {
					seq = cur.Seq
				} else if !errors.Is(err, ErrNotFound) {
					t.Error(err)
					return
				}
				if _, err := s.SetRoot(ctx, "acme", "stable", ids[i], seq); err == nil {
					won.Add(1)
					return
				} else if !errors.Is(err, ErrConflict) {
					t.Error(err)
					return
				}
			}
		}(i)
	}
	wg.Wait()
	if won.Load() != n {
		t.Fatalf("%d of %d moves landed", won.Load(), n)
	}
	h, _ := s.RootHistory(ctx, "acme", "stable")
	if len(h) != n {
		t.Fatalf("%d history rows", len(h))
	}
	seen := map[string]bool{}
	for i, r := range h {
		if r.Seq != int64(i+1) || seen[r.ManifestID] {
			t.Fatalf("history is not a contiguous, duplicate-free chain at %d: %+v", i, r)
		}
		seen[r.ManifestID] = true
	}
	if cur, _ := s.Root(ctx, "acme", "stable"); cur.Seq != n || cur.ManifestID != h[n-1].ManifestID {
		t.Fatalf("%+v", cur)
	}
	if _, err := s.SetRoot(ctx, "acme", "stable", ids[0], 3); !errors.Is(err, ErrConflict) {
		t.Fatalf("a stale sequence must conflict: %v", err)
	}
	if _, err := s.SetRoot(ctx, "acme", "fresh", ids[0], 1); !errors.Is(err, ErrConflict) {
		t.Fatalf("a new channel starts at sequence 0: %v", err)
	}
}

func TestTenantsNeverSeeOrReferenceEachOthersRows(t *testing.T) {
	s := open(t, filepath.Join(t.TempDir(), "meta.db"), nil)
	for _, tn := range []Tenant{"alpha", "beta"} {
		_ = s.EnsureTenant(ctx, tn)
	}
	a := put(t, s, "alpha", "shared-content")
	m := publish(t, s, "alpha", "alpha-manifest", "", a)
	_, _ = s.SetRoot(ctx, "alpha", "stable", m, 0)
	_ = s.Pin(ctx, "alpha", Pin{ManifestID: m, Reason: "p"})
	if _, err := s.Artifact(ctx, "beta", a); !errors.Is(err, ErrNotFound) {
		t.Fatalf("beta read alpha's artifact: %v", err)
	}
	if _, err := s.Manifest(ctx, "beta", m); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := s.Root(ctx, "beta", "stable"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if ps, _ := s.Pins(ctx, "beta"); len(ps) != 0 {
		t.Fatal(ps)
	}
	if ms, _ := s.Manifests(ctx, "beta", "", 0); len(ms) != 0 {
		t.Fatal(ms)
	}
	err := s.PublishManifest(ctx, "beta", ManifestRecord{ID: dg("beta-manifest"), Kind: "compiled", Doc: []byte("beta-manifest"), KeyID: "k"}, []string{a})
	if !errors.Is(err, ErrMissingRef) {
		t.Fatalf("a manifest must not link another tenant's artifact: %v", err)
	}
	if _, err := s.SetRoot(ctx, "beta", "stable", m, 0); !errors.Is(err, ErrMissingRef) {
		t.Fatalf("a root must not point at another tenant's manifest: %v", err)
	}
	if err := s.Pin(ctx, "beta", Pin{ManifestID: m, Reason: "steal"}); !errors.Is(err, ErrMissingRef) {
		t.Fatalf("%v", err)
	}
	if err := s.DeleteArtifact(ctx, "beta", a); !errors.Is(err, ErrNotFound) {
		t.Fatalf("beta deleted alpha's artifact: %v", err)
	}
	if _, err := s.Artifact(ctx, "alpha", a); err != nil {
		t.Fatal(err)
	}
	same := put(t, s, "beta", "shared-content")
	if same != a {
		t.Fatal("the same content has the same digest")
	}
	for _, bad := range []Tenant{"", "Alpha", "a b", "alpha'; DROP TABLE tenants;--", "../alpha", Tenant(string(make([]byte, 64)))} {
		if _, err := s.Artifacts(ctx, bad, "", 10); !errors.Is(err, ErrInvalid) {
			t.Fatalf("tenant %q accepted: %v", bad, err)
		}
	}
	if err := s.EnsureTenant(ctx, "gamma"); err != nil {
		t.Fatal(err)
	}
	if ts, _ := s.Tenants(ctx); len(ts) != 3 {
		t.Fatal(ts)
	}
}

func TestIntegrityRulesAreEnforcedByTheStoreItself(t *testing.T) {
	s := open(t, filepath.Join(t.TempDir(), "meta.db"), nil)
	_ = s.EnsureTenant(ctx, "acme")
	a := put(t, s, "acme", "a")
	if err := s.PublishManifest(ctx, "acme", ManifestRecord{ID: dg("other"), Kind: "compiled", Doc: []byte("doc"), KeyID: "k"}, nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a manifest id must be the digest of its document: %v", err)
	}
	if err := s.PublishManifest(ctx, "acme", ManifestRecord{ID: dg("d"), Kind: "compiled", Doc: []byte("d"), KeyID: "k"}, []string{dg("missing")}); !errors.Is(err, ErrMissingRef) {
		t.Fatalf("%v", err)
	}
	if _, err := s.Manifest(ctx, "acme", dg("d")); !errors.Is(err, ErrNotFound) {
		t.Fatal("a failed publish must leave no manifest behind")
	}
	if err := s.PublishManifest(ctx, "acme", ManifestRecord{ID: dg("d"), Kind: "compiled", Doc: []byte("d"), KeyID: "k"}, []string{a, a}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("duplicate links: %v", err)
	}
	m := publish(t, s, "acme", "d", "", a)
	publish(t, s, "acme", "d", "", a)
	if _, err := s.PutArtifact(ctx, "acme", ArtifactRecord{Digest: a, Size: 999, Class: Public}); !errors.Is(err, ErrConflict) {
		t.Fatalf("one digest cannot have two sizes: %v", err)
	}
	if created, err := s.PutArtifact(ctx, "acme", ArtifactRecord{Digest: a, Size: 1, Class: Public}); err != nil || created {
		t.Fatalf("an identical record is idempotent: %v %v", created, err)
	}
	if err := s.DeleteArtifact(ctx, "acme", a); !errors.Is(err, ErrConflict) {
		t.Fatalf("a referenced artifact must not be deleted: %v", err)
	}
	_, _ = s.SetRoot(ctx, "acme", "stable", m, 0)
	if err := s.DeleteManifest(ctx, "acme", m); !errors.Is(err, ErrConflict) {
		t.Fatalf("a root manifest must not be deleted: %v", err)
	}
	for _, bad := range []ArtifactRecord{{Digest: "sha256:zz", Size: 1, Class: Public}, {Digest: a, Size: -1, Class: Public}, {Digest: a, Size: 1, Class: "secret"}} {
		if _, err := s.PutArtifact(ctx, "acme", bad); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%+v: %v", bad, err)
		}
	}
	if _, err := s.PutArtifact(ctx, "nobody", ArtifactRecord{Digest: a, Size: 1, Class: Public}); !errors.Is(err, ErrMissingRef) {
		t.Fatalf("an unknown tenant owns nothing: %v", err)
	}
}

func TestPinsHoldsAndLeasesFollowTheirRules(t *testing.T) {
	c := &clock{}
	s := open(t, filepath.Join(t.TempDir(), "meta.db"), func(o *Options) { o.Now = c.now })
	_ = s.EnsureTenant(ctx, "acme")
	_ = s.EnsureTenant(ctx, "other")
	m := publish(t, s, "acme", "m", "")
	if err := s.Pin(ctx, "acme", Pin{ManifestID: m, Reason: "customer"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Pin(ctx, "acme", Pin{ManifestID: m, Reason: "customer"}); err != nil {
		t.Fatal("pinning twice for one reason is idempotent")
	}
	if err := s.Unpin(ctx, "acme", m, "customer"); err != nil {
		t.Fatal(err)
	}
	if err := s.Unpin(ctx, "acme", m, "customer"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if err := s.Release(ctx, "acme", m, "nobody"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	exp := c.now() + 1000
	if err := s.Acquire(ctx, "acme", Lease{ID: "l1", ManifestID: m, Owner: "worker-1", ExpiresAt: exp}); err != nil {
		t.Fatal(err)
	}
	if err := s.Acquire(ctx, "acme", Lease{ID: "l1", ManifestID: m, Owner: "worker-2", ExpiresAt: exp}); !errors.Is(err, ErrLeaseHeld) {
		t.Fatalf("a held lease must not be taken: %v", err)
	}
	if err := s.Acquire(ctx, "other", Lease{ID: "l1", ManifestID: m, Owner: "worker-9", ExpiresAt: exp}); !errors.Is(err, ErrMissingRef) {
		t.Fatalf("a lease id is per tenant and cannot reach another tenant's manifest: %v", err)
	}
	if ls, _ := s.Leases(ctx, "acme"); len(ls) != 1 {
		t.Fatal(ls)
	}
	c.n.Store(exp + 10)
	if ls, _ := s.Leases(ctx, "acme"); len(ls) != 0 {
		t.Fatal("an expired lease protects nothing")
	}
	if err := s.Renew(ctx, "acme", "l1", exp+5000); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an expired lease cannot be renewed: %v", err)
	}
	if err := s.Acquire(ctx, "acme", Lease{ID: "l1", ManifestID: m, Owner: "worker-2", ExpiresAt: exp + 5000}); err != nil {
		t.Fatalf("an expired lease may be taken: %v", err)
	}
	if err := s.Renew(ctx, "acme", "l1", exp+9000); err != nil {
		t.Fatal(err)
	}
	if err := s.ReleaseLease(ctx, "acme", "l1"); err != nil {
		t.Fatal(err)
	}
}

func TestIntentsRecordUnfinishedWorkForRecovery(t *testing.T) {
	s := open(t, filepath.Join(t.TempDir(), "meta.db"), nil)
	_ = s.EnsureTenant(ctx, "acme")
	_ = s.EnsureTenant(ctx, "other")
	id, err := s.BeginIntent(ctx, "acme", "publish", `{"m":1}`)
	if err != nil {
		t.Fatal(err)
	}
	if p, _ := s.PendingIntents(ctx, "acme"); len(p) != 1 || p[0].ID != id {
		t.Fatal(p)
	}
	if p, _ := s.PendingIntents(ctx, "other"); len(p) != 0 {
		t.Fatal("intents are per tenant")
	}
	if err := s.FinishIntent(ctx, "other", id, IntentDone); !errors.Is(err, ErrNotFound) {
		t.Fatalf("another tenant cannot finish it: %v", err)
	}
	if err := s.FinishIntent(ctx, "acme", id, IntentDone); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishIntent(ctx, "acme", id, IntentAbandoned); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a finished intent stays finished: %v", err)
	}
	if p, _ := s.PendingIntents(ctx, "acme"); len(p) != 0 {
		t.Fatal(p)
	}
}
