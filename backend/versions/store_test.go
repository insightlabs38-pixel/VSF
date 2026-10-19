//go:build integration

package versions

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/insightlabs38-pixel/vsf/backend/execution"
)

const fixtureDir = "../../fixtures/versions"

type sample struct {
	ids    map[string]string
	docs   map[string][]byte
	blobs  map[string][]byte
	trust  []byte
	nowMs  int64
	sealed map[string][]string
}

func load(t testing.TB) *sample {
	t.Helper()
	s := &sample{docs: map[string][]byte{}, blobs: map[string][]byte{}, nowMs: 1_792_368_500_000}
	raw, err := os.ReadFile(filepath.Join(fixtureDir, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &s.ids); err != nil {
		t.Fatal(err)
	}
	for name := range s.ids {
		b, err := os.ReadFile(filepath.Join(fixtureDir, "manifests", name+".json"))
		if err != nil {
			t.Fatal(err)
		}
		s.docs[name] = b
	}
	entries, err := os.ReadDir(filepath.Join(fixtureDir, "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(fixtureDir, "blobs", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		s.blobs[strings.Replace(e.Name(), "sha256-", "sha256:", 1)] = b
	}
	if s.trust, err = os.ReadFile(filepath.Join(fixtureDir, "trust.json")); err != nil {
		t.Fatal(err)
	}
	return s
}

func worker(t testing.TB) *execution.Worker {
	t.Helper()
	p := os.Getenv("VSF_FABRIC")
	if p == "" {
		p = filepath.Join("..", "..", "artifacts", "bin", "vsf-fabric")
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("native worker not found at %s (run scripts/build-native.sh or set VSF_FABRIC): %v", p, err)
	}
	w, err := execution.StartWorker(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return w
}

type rig struct {
	t      *testing.T
	dir    string
	s      *Sample
	w      *execution.Worker
	now    int64
	crash  string
	store  *Store
	sample *sample
}

type Sample = sample

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{t: t, dir: t.TempDir(), w: worker(t), sample: load(t)}
	r.now = r.sample.nowMs
	r.open()
	return r
}

func (r *rig) cfg() Config {
	return Config{
		Verifier: NewWorkerVerifier(r.w), Trust: r.sample.trust, Now: func() int64 { return r.now },
		CrashAt: func(p string) bool { return r.crash != "" && r.crash == p },
	}
}

func (r *rig) open() {
	r.t.Helper()
	s, err := Open(r.dir, r.cfg())
	if err != nil {
		r.t.Fatal(err)
	}
	r.store = s
	r.t.Cleanup(func() { _ = s.Close() })
}

func (r *rig) reopen() {
	r.t.Helper()
	_ = r.store.Close()
	r.crash = ""
	r.open()
}

func (r *rig) req(name string) Request {
	return Request{Doc: r.sample.docs[name], Blobs: r.sample.blobs}
}

func (r *rig) promote(name string) string {
	r.t.Helper()
	id, err := r.store.Promote(r.req(name))
	if err != nil {
		r.t.Fatalf("promote %s: %v", name, err)
	}
	if id != r.sample.ids[name] {
		r.t.Fatalf("promote %s: id %s want %s", name, id, r.sample.ids[name])
	}
	return id
}

func reason(t *testing.T, err error) string {
	t.Helper()
	var ref *Refusal
	if !errors.As(err, &ref) {
		t.Fatalf("not a refusal: %v", err)
	}
	return ref.Reason
}

func TestAHistoryPromotesInOrderAndEveryHeadIsWhole(t *testing.T) {
	r := newRig(t)
	want := []string{"m1", "m2", "m3", "rollback"}
	for _, n := range want {
		id := r.promote(n)
		if head, ok := r.store.Head("stable"); !ok || head != id {
			t.Fatalf("after %s head is %s", n, head)
		}
		if err := r.store.Verify(); err != nil {
			t.Fatalf("after %s: %v", n, err)
		}
		if n == "m2" {
			r.promote("branch")
			if head, _ := r.store.Head("stable"); head != r.sample.ids["m2"] {
				t.Fatal("a preview manifest moved the stable head")
			}
		}
	}
	for d, b := range r.sample.blobs {
		if got, ok := r.store.Blob(d); ok && string(got) != string(b) {
			t.Fatalf("blob %s differs from what was promoted", d)
		}
	}
	if doc, ok := r.store.Manifest(r.sample.ids["rollback"]); !ok || string(doc) != string(r.sample.docs["rollback"]) {
		t.Fatal("the stored rollback manifest differs from what was promoted")
	}
}

func TestPromotingTheSameManifestAgainChangesNothing(t *testing.T) {
	r := newRig(t)
	r.promote("m1")
	r.promote("m1")
	if err := r.store.Verify(); err != nil {
		t.Fatal(err)
	}
}

func TestPartialAndMismatchedPromotionsAreRejectedWithoutTrace(t *testing.T) {
	r := newRig(t)
	var m1 struct {
		Body struct {
			Artifacts []struct{ Role, Digest string } `json:"artifacts"`
		} `json:"body"`
	}
	if err := json.Unmarshal(r.sample.docs["m1"], &m1); err != nil {
		t.Fatal(err)
	}
	pres := ""
	for _, a := range m1.Body.Artifacts {
		if a.Role == "presentation" {
			pres = a.Digest
		}
	}
	cases := []struct {
		name   string
		edit   func(q *Request)
		prefix string
	}{
		{"an artifact is missing", func(q *Request) {
			blobs := map[string][]byte{}
			for d, b := range q.Blobs {
				if d != pres {
					blobs[d] = b
				}
			}
			q.Blobs = blobs
		}, "artifact_missing"},
		{"an artifact has the wrong bytes", func(q *Request) {
			blobs := map[string][]byte{}
			for d, b := range q.Blobs {
				blobs[d] = b
			}
			blobs[pres] = append(append([]byte{}, blobs[pres]...), ' ')
			q.Blobs = blobs
		}, "artifact_size_mismatch"},
		{"an artifact is another document", func(q *Request) {
			blobs := map[string][]byte{}
			for d, b := range q.Blobs {
				blobs[d] = b
			}
			for d, b := range q.Blobs {
				if d != pres && strings.Contains(string(b), "vsf.presentation/") {
					blobs[pres] = b
				}
			}
			q.Blobs = blobs
		}, "artifact_"},
		{"the manifest is cut short", func(q *Request) { q.Doc = q.Doc[:len(q.Doc)/2] }, "undecodable"},
		{"the manifest was edited", func(q *Request) {
			q.Doc = []byte(strings.Replace(string(q.Doc), `"pv1"`, `"pv9"`, 1))
		}, "unverified:"},
	}
	for _, c := range cases {
		q := r.req("m1")
		c.edit(&q)
		_, err := r.store.Promote(q)
		got := reason(t, err)
		if !strings.HasPrefix(got, c.prefix) {
			t.Errorf("%s: refused for %q, want %q", c.name, got, c.prefix)
		}
		if _, ok := r.store.Head("stable"); ok {
			t.Errorf("%s: a head exists", c.name)
		}
		if _, ok := r.store.Manifest(r.sample.ids["m1"]); ok {
			t.Errorf("%s: the manifest was stored", c.name)
		}
	}
	if err := r.store.Verify(); err != nil {
		t.Fatal(err)
	}
	r.promote("m1")
}

func TestOrderingSequenceExpiryAndTrustAreEnforced(t *testing.T) {
	r := newRig(t)
	if _, err := r.store.Promote(r.req("m2")); !strings.HasPrefix(reason(t, err), "missing_parent") {
		t.Fatalf("a child before its parent: %v", err)
	}
	r.promote("m1")
	r.promote("m2")
	if _, err := r.store.Promote(r.req("m1")); !strings.HasPrefix(reason(t, err), "unverified:promotion.rollback") {
		t.Fatalf("replaying an older promotion: %v", err)
	}
	if _, err := r.store.Promote(r.req("rollback")); !strings.HasPrefix(reason(t, err), "missing_parent") {
		t.Fatalf("skipping a generation: %v", err)
	}
	r.now = 9_999_999_999_999
	if _, err := r.store.Promote(r.req("m3")); !strings.HasPrefix(reason(t, err), "unverified:promotion.expired") {
		t.Fatalf("an expired window: %v", err)
	}
	r.now = r.sample.nowMs
	r.sample.trust = []byte(strings.Replace(string(r.sample.trust), `"active"`, `"revoked"`, 1))
	r.reopen()
	r.promote("branch")
	if _, err := r.store.Promote(r.req("m3")); !strings.HasPrefix(reason(t, err), "unverified:manifest.untrusted_key") {
		t.Fatalf("a revoked signing key: %v", err)
	}
	if head, _ := r.store.Head("stable"); head != r.sample.ids["m2"] {
		t.Fatalf("head moved to %s", head)
	}
}

func TestACrashAtAnyPointLeavesTheOldOrTheNewStateNeverAMixture(t *testing.T) {
	points := []struct {
		at      string
		forward bool
	}{{"blobs", false}, {"manifest", false}, {"intent", true}, {"pointer", true}}
	for _, from := range []string{"", "m1"} {
		to := "m1"
		if from == "m1" {
			to = "m2"
		}
		for _, p := range points {
			t.Run(from+"->"+to+" crash at "+p.at, func(t *testing.T) {
				r := newRig(t)
				if from != "" {
					r.promote(from)
				}
				r.crash = p.at
				if _, err := r.store.Promote(r.req(to)); !errors.Is(err, ErrCrashed) {
					t.Fatalf("expected a crash, got %v", err)
				}
				r.reopen()
				if err := r.store.Verify(); err != nil {
					t.Fatalf("the store is damaged after recovery: %v", err)
				}
				head, ok := r.store.Head("stable")
				wantOld := from
				if wantOld != "" {
					wantOld = r.sample.ids[from]
				}
				wantNew := r.sample.ids[to]
				switch {
				case p.forward && (!ok || head != wantNew):
					t.Fatalf("an intent that could complete was not rolled forward: head %q", head)
				case !p.forward && ((from == "" && ok) || (from != "" && head != wantOld)):
					t.Fatalf("a promotion that never became durable changed the head: %q", head)
				}
				r.promote(to)
				if head, _ := r.store.Head("stable"); head != wantNew {
					t.Fatalf("retrying did not reach the new head: %s", head)
				}
				if err := r.store.Verify(); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestARecoveryNeverPointsAHeadAtMissingArtifacts(t *testing.T) {
	r := newRig(t)
	r.promote("m1")
	r.crash = "intent"
	if _, err := r.store.Promote(r.req("m2")); !errors.Is(err, ErrCrashed) {
		t.Fatal(err)
	}
	_ = r.store.Close()
	for d := range r.sample.blobs {
		if strings.Contains(string(r.sample.blobs[d]), `"density":"compact"`) {
			if err := os.Remove(filepath.Join(r.dir, "blobs", name(d))); err != nil {
				t.Fatal(err)
			}
		}
	}
	r.crash = ""
	r.open()
	if head, _ := r.store.Head("stable"); head != r.sample.ids["m1"] {
		t.Fatalf("recovery rolled forward over a missing artifact: %s", head)
	}
	if err := r.store.Verify(); err != nil {
		t.Fatal(err)
	}
}

func TestOnlyOneProcessMayOwnAStore(t *testing.T) {
	r := newRig(t)
	if _, err := Open(r.dir, r.cfg()); !errors.Is(err, ErrLocked) {
		t.Fatalf("second open: %v", err)
	}
}

func TestRollbackIsAPromotionThatRestoresAnOlderInterface(t *testing.T) {
	r := newRig(t)
	for _, n := range []string{"m1", "m2", "branch", "m3", "rollback"} {
		r.promote(n)
	}
	head, _ := r.store.Head("stable")
	if head != r.sample.ids["rollback"] {
		t.Fatal(head)
	}
	if err := r.store.Executable("stable"); err != nil {
		t.Fatal(err)
	}
	if err := r.store.Verify(); err != nil {
		t.Fatal(err)
	}
}
