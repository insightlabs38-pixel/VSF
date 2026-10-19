package execution

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/insightlabs38-pixel/vsf/backend/execution/internal/fixtureapp"
)

type apiRig struct {
	*rig
	app *fixtureapp.App
}

func newAPIRig(t *testing.T, dedup bool, edit func(map[string]any)) *apiRig {
	t.Helper()
	app := fixtureapp.New(dedup)
	t.Cleanup(app.Close)
	r := newRig(t, edit)
	api := NewAPIExecutor(APIConfig{
		BaseURL: app.URL(), ReadPath: "/api/orders/%s", WritePath: "/api/orders/%s/stage", Dedup: dedup,
		Client: &http.Client{Timeout: 2 * time.Second},
	})
	r.cfg.Executors = map[string]Executor{"provider_api_authorized": api}
	r.cfg.ApplyTimeout = 400 * time.Millisecond
	var err error
	if r.rt, err = NewRuntime(r.cfg); err != nil {
		t.Fatal(err)
	}
	return &apiRig{rig: r, app: app}
}

func TestAPIExecutorCommitsAndTheReadBackVerifiesIt(t *testing.T) {
	r := newAPIRig(t, false, nil)
	rec, err := r.run(r.request("req-1"))
	if err != nil {
		t.Fatal(err)
	}
	mustState(t, rec, Verified, "verified")
	if got := r.app.Order("o1"); got.Stage != "won" || r.app.Commits() != 1 {
		t.Fatalf("order %+v commits %d", got, r.app.Commits())
	}
}

func TestDisconnectAfterTheSourceCommittedEndsInUnknownCommitNeverADuplicate(t *testing.T) {
	r := newAPIRig(t, false, nil)
	r.app.SetFaults(fixtureapp.Faults{DropAfterCommit: 1})
	q := r.request("req-1")
	rec, _ := r.run(q)
	mustState(t, rec, Unknown, ReasonUnknownCommit)
	if r.app.Commits() != 1 {
		t.Fatalf("commits %d", r.app.Commits())
	}
	posts := len(r.app.Requests())

	again, err := r.rt.Submit(context.Background(), q)
	if err != nil || again.Key() != rec.Key {
		t.Fatalf("resubmission: %v %v", again, err)
	}
	if len(r.app.Requests()) != posts {
		t.Fatal("resubmitting the same request touched the source again")
	}

	q2 := r.request("req-2")
	_, err = r.rt.Submit(context.Background(), q2)
	if reasonFrom(t, err) != "unresolved_unknown_commit" {
		t.Fatalf("a new request for the same record while the first is unknown: %v", err)
	}
	if r.app.Commits() != 1 {
		t.Fatal("a duplicate was committed")
	}

	got, err := r.rt.Reconcile(context.Background(), userCaller, rec.Key, q.Expect)
	if err != nil {
		t.Fatal(err)
	}
	mustState(t, got, Verified, "reconciled_committed")
	if r.app.Commits() != 1 {
		t.Fatal("reconciliation wrote")
	}
}

func TestDisconnectBeforeCommitReconcilesToFailedNotCommitted(t *testing.T) {
	r := newAPIRig(t, false, nil)
	r.app.SetFaults(fixtureapp.Faults{DropBeforeCommit: 1})
	q := r.request("req-1")
	rec, _ := r.run(q)
	mustState(t, rec, Unknown, ReasonUnknownCommit)
	if r.app.Commits() != 0 {
		t.Fatal("the source committed")
	}
	got, err := r.rt.Reconcile(context.Background(), userCaller, rec.Key, q.Expect)
	if err != nil {
		t.Fatal(err)
	}
	mustState(t, got, Failed, "reconciled_not_committed")
	next, err := r.run(r.request("req-2"))
	if err != nil {
		t.Fatal(err)
	}
	mustState(t, next, Verified, "")
	if r.app.Commits() != 1 {
		t.Fatalf("commits %d", r.app.Commits())
	}
}

func TestReconciliationRefusesToGuessWhenTheSourceChangedMeanwhile(t *testing.T) {
	r := newAPIRig(t, false, nil)
	r.app.SetFaults(fixtureapp.Faults{DropBeforeCommit: 1})
	q := r.request("req-1")
	rec, _ := r.run(q)
	r.app.Change("o1", "qualified")
	_, err := r.rt.Reconcile(context.Background(), userCaller, rec.Key, q.Expect)
	if !errors.Is(err, ErrInconclusive) {
		t.Fatalf("err %v", err)
	}
	if got, _ := r.rt.Get(rec.Key); got.State != Unknown {
		t.Fatalf("state %s", got.State)
	}
}

func TestReconciliationChecksWhoAsksAndWhatForIt(t *testing.T) {
	r := newAPIRig(t, false, nil)
	r.app.SetFaults(fixtureapp.Faults{DropAfterCommit: 1})
	q := r.request("req-1")
	rec, _ := r.run(q)
	ctx := context.Background()
	if _, err := r.rt.Reconcile(ctx, "caller-bob", rec.Key, q.Expect); reasonFrom(t, err) != "other_caller" {
		t.Fatal(err)
	}
	if _, err := r.rt.Reconcile(ctx, userCaller, rec.Key, map[string]string{"stage": "qualified"}); reasonFrom(t, err) != "expect_mismatch" {
		t.Fatal(err)
	}
	if _, err := r.rt.Reconcile(ctx, userCaller, "op_missing", q.Expect); reasonFrom(t, err) != "not_unknown" {
		t.Fatal(err)
	}
	r.dir.revoke(userCaller)
	if _, err := r.rt.Reconcile(ctx, userCaller, rec.Key, q.Expect); reasonFrom(t, err) != "unknown_caller" {
		t.Fatal(err)
	}
	if got, _ := r.rt.Get(rec.Key); got.State != Unknown {
		t.Fatalf("state %s", got.State)
	}
}

func TestTimeoutWhileTheSourceIsStillCommittingIsUnknownThenReconcilable(t *testing.T) {
	r := newAPIRig(t, false, nil)
	r.app.SetFaults(fixtureapp.Faults{DelayBeforeCommit: 900 * time.Millisecond})
	q := r.request("req-1")
	rec, _ := r.run(q)
	mustState(t, rec, Unknown, ReasonUnknownCommit)
	deadline := time.Now().Add(3 * time.Second)
	for r.app.Commits() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if r.app.Commits() != 1 {
		t.Fatal("the delayed commit never happened")
	}
	got, err := r.rt.Reconcile(context.Background(), userCaller, rec.Key, q.Expect)
	if err != nil {
		t.Fatal(err)
	}
	mustState(t, got, Verified, "reconciled_committed")
}

func TestARestartAfterTheProcessDiedMidWriteMarksItUnknownCommit(t *testing.T) {
	r := newAPIRig(t, false, nil)
	r.app.SetFaults(fixtureapp.Faults{DelayBeforeCommit: 300 * time.Millisecond})
	h, err := r.rt.Submit(context.Background(), r.request("req-1"))
	if err != nil {
		t.Fatal(err)
	}
	for h.State() != Running {
		time.Sleep(time.Millisecond)
	}
	lines, _ := r.store.ReadAll()
	crashed := &MemStore{}
	for _, l := range lines {
		_ = crashed.Append(l)
	}
	j2, err := OpenJournal(crashed)
	if err != nil {
		t.Fatal(err)
	}
	cfg := r.cfg
	cfg.Journal = j2
	rt2, err := NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := rt2.Get(h.Key())
	mustState(t, got, Unknown, ReasonUnknownCommit)
	if got.Detail != "recovered_while_running" {
		t.Fatalf("detail %q", got.Detail)
	}
	h.Wait(context.Background())
}

func TestLosingTheJournalAfterTheCommitLeavesRunningWhichRecoversAsUnknown(t *testing.T) {
	r := newAPIRig(t, false, nil)
	fs := &failingStore{inner: r.store, failAfter: 3}
	j, _ := OpenJournal(fs)
	r.cfg.Journal = j
	var err error
	if r.rt, err = NewRuntime(r.cfg); err != nil {
		t.Fatal(err)
	}
	h, err := r.rt.Submit(context.Background(), r.request("req-1"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.Wait(context.Background()); err == nil {
		t.Fatal("the lost final record was not reported")
	}
	if r.app.Commits() != 1 {
		t.Fatalf("commits %d", r.app.Commits())
	}
	j2, err := OpenJournal(r.store)
	if err != nil {
		t.Fatal(err)
	}
	r.cfg.Journal = j2
	rt2, err := NewRuntime(r.cfg)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := rt2.Get(h.Key())
	mustState(t, got, Unknown, ReasonUnknownCommit)
}

func TestConcurrentDoubleSubmitAppliesOnce(t *testing.T) {
	r := newAPIRig(t, true, nil)
	var wg sync.WaitGroup
	keys := make(chan string, 20)
	q := r.request("req-1")
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h, err := r.rt.Submit(context.Background(), q)
			if err != nil {
				t.Error(err)
				return
			}
			h.Wait(context.Background())
			keys <- h.Key()
		}()
	}
	wg.Wait()
	close(keys)
	first := ""
	for k := range keys {
		if first == "" {
			first = k
		}
		if k != first {
			t.Fatal("two different operations")
		}
	}
	if r.app.Commits() != 1 {
		t.Fatalf("commits %d", r.app.Commits())
	}
}

func TestReusingARequestIdForOtherParametersIsRefused(t *testing.T) {
	r := newAPIRig(t, false, nil)
	q := r.request("req-1")
	if _, err := r.run(q); err != nil {
		t.Fatal(err)
	}
	q.Params["stage"] = "proposal"
	if _, err := r.rt.Submit(context.Background(), q); reasonFrom(t, err) != "request_id_reused" {
		t.Fatal(err)
	}
	if r.app.Commits() != 1 {
		t.Fatal("applied again")
	}
}

func TestSourceThrottlingIsRetriedWithTheDocumentedHintAndBounds(t *testing.T) {
	r := newAPIRig(t, true, nil)
	r.app.SetFaults(fixtureapp.Faults{ThrottleNext: 2})
	rec, err := r.run(r.request("req-1"))
	if err != nil {
		t.Fatal(err)
	}
	mustState(t, rec, Verified, "")
	if r.app.Commits() != 1 {
		t.Fatalf("commits %d", r.app.Commits())
	}
	if got := r.clock.Sleeps(); len(got) != 2 || got[0] != time.Second || got[1] != time.Second {
		t.Fatalf("slept %v; the source asked for 1s each time", got)
	}
}

func TestAnUnreachableSourceIsAFailureWithoutAnEffectAndRetriesWhenIdempotent(t *testing.T) {
	r := newAPIRig(t, true, effectEdit("idempotent_write", "idempotent"))
	r.app.Close()
	rec, _ := r.run(r.request("req-1"))
	mustState(t, rec, Failed, "read_failed")
}

func TestStaleVersionSurfacesAsAConflictFromTheSourceItself(t *testing.T) {
	r := newAPIRig(t, false, nil)
	q := r.request("req-1")
	h, _ := r.rt.Submit(context.Background(), q)
	h.Wait(context.Background())
	r.app.Change("o1", "qualified")
	q2 := r.request("req-2")
	q2.Params["stage"] = "proposal"
	q2.Expect = map[string]string{"stage": "proposal"}
	q2.Seen = &Seen{Version: "v2", ObservedAt: r.clock.Now()}
	rec, _ := r.run(q2)
	mustState(t, rec, Failed, "stale_read")
}
