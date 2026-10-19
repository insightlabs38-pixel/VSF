package execution

import (
	"context"
	"strings"
	"sync"
	"testing"
)

type revSet struct {
	mu    sync.Mutex
	items map[string]string
}

func (r *revSet) RevokedContract(digest, op, version string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, k := range []string{digest, op + "@" + version, op + "@*"} {
		if why, ok := r.items[k]; ok {
			return why, true
		}
	}
	return "", false
}

func (r *revSet) add(k, why string) {
	r.mu.Lock()
	r.items[k] = why
	r.mu.Unlock()
}

func withRevocations(t *testing.T) (*rig, *revSet) {
	t.Helper()
	r := newRig(t, nil)
	rev := &revSet{items: map[string]string{}}
	r.cfg.Revocations = rev
	var err error
	if r.rt, err = NewRuntime(r.cfg); err != nil {
		t.Fatal(err)
	}
	return r, rev
}

func TestARevokedContractNeverRunsEvenThoughItIsPinned(t *testing.T) {
	cases := map[string]func(q Request) string{
		"document": func(q Request) string { return q.Pin.Digest },
		"version":  func(q Request) string { return "app.records.update_stage@" + q.Pin.Version },
		"every":    func(q Request) string { return "app.records.update_stage@*" },
	}
	for name, key := range cases {
		t.Run(name, func(t *testing.T) {
			r, rev := withRevocations(t)
			q := r.request("req-1")
			rev.add(key(q), "operation_revoked")
			_, err := r.rt.Submit(context.Background(), q)
			if got := reasonFrom(t, err); !strings.HasPrefix(got, "contract_revoked") {
				t.Fatalf("refused for %q", got)
			}
			if r.ex.applyCount() != 0 || r.ex.readCount() != 0 {
				t.Fatal("the source was touched")
			}
		})
	}
}

func TestARevocationPublishedWhileQueuedStopsTheWriteBeforeTheSource(t *testing.T) {
	r, rev := withRevocations(t)
	r.ex.onRead = func() { rev.add(r.request("x").Pin.Digest, "artifact_revoked") }
	rec, _ := r.run(r.request("req-1"))
	mustState(t, rec, Failed, "")
	if !strings.HasPrefix(rec.Reason, "contract_revoked") || r.ex.applyCount() != 0 {
		t.Fatalf("reason %q applies %d", rec.Reason, r.ex.applyCount())
	}
}

func TestAContractThatIsNotRevokedStillRuns(t *testing.T) {
	r, rev := withRevocations(t)
	rev.add("app.records.update_stage@0.0.1", "old")
	rec, err := r.run(r.request("req-1"))
	if err != nil {
		t.Fatal(err)
	}
	mustState(t, rec, Verified, "verified")
}
