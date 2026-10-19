package execution

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/insightlabs38-pixel/vsf/backend/execution/internal/fixtureapp"
)

func attempts(t *testing.T, rt *Runtime, key string) int {
	t.Helper()
	return attemptsOf(mustGet(t, rt, key))
}

func TestAnotherActorWritingTheSameValuesIsNotProofThisOperationCommitted(t *testing.T) {
	r := newAPIRig(t, false, nil)
	r.app.SetFaults(fixtureapp.Faults{DropBeforeCommit: 1})
	q := r.request("req-1")
	rec, _ := r.run(q)
	r.app.Change("o1", "won")
	got, err := r.reconcile(rec, q)
	if !errors.Is(err, ErrInconclusive) {
		t.Fatalf("err %v", err)
	}
	if got.Evidence.Level != StateMatchesUnproven {
		t.Fatalf("evidence %+v", got.Evidence)
	}
	mustState(t, got.Record, Unknown, ReasonUnknownCommit)
	if _, err := r.rt.Submit(context.Background(), r.request("req-2")); reasonFrom(t, err) != "unresolved_unknown_commit" {
		t.Fatal("an unproven outcome released the exclusion")
	}
}

func TestAnUnchangedVersionNeverProvesThatNothingWasCommitted(t *testing.T) {
	r := newAPIRig(t, false, nil)
	r.app.SetFaults(fixtureapp.Faults{DropBeforeCommit: 1})
	q := r.request("req-1")
	rec, _ := r.run(q)
	got, err := r.reconcile(rec, q)
	if !errors.Is(err, ErrInconclusive) || got.Evidence.Level != Inconclusive {
		t.Fatalf("%+v %v", got.Evidence, err)
	}
	mustState(t, got.Record, Unknown, "")
}

func TestAStaleReadCannotCloseAnOperationEitherWay(t *testing.T) {
	r := newReceiptRig(t, true, true, true, nil)
	r.app.SetFaults(fixtureapp.Faults{DropAfterCommit: 1})
	q := r.request("req-1")
	rec, _ := r.run(q)
	r.app.SetStale("o1", &fixtureapp.Order{ID: "o1", Stage: "new", Version: 1})
	got, err := r.reconcile(rec, q)
	if !errors.Is(err, ErrInconclusive) {
		t.Fatalf("a committed write was settled from a stale read: %+v %v", got, err)
	}
	if got.Evidence.Level != ProvenCommitted {
		t.Fatalf("the receipt should still be recorded: %+v", got.Evidence)
	}
	mustState(t, got.Record, Unknown, "")
	r.app.SetStale("o1", nil)
	got, err = r.reconcile(rec, q)
	if err != nil {
		t.Fatal(err)
	}
	mustState(t, got.Record, Verified, "reconciled_committed")
}

func TestAFenceStopsAWriteStillInFlightFromCommittingLater(t *testing.T) {
	r := newReceiptRig(t, true, true, true, nil)
	r.app.SetFaults(fixtureapp.Faults{DelayBeforeCommit: 600 * time.Millisecond})
	q := r.request("req-1")
	rec, _ := r.run(q)
	mustState(t, rec, Unknown, ReasonUnknownCommit)
	got, err := r.reconcile(rec, q)
	if err != nil {
		t.Fatal(err)
	}
	mustState(t, got.Record, Failed, "reconciled_not_committed")
	time.Sleep(900 * time.Millisecond)
	if r.app.Commits() != 0 || r.app.Order("o1").Stage != "new" {
		t.Fatalf("the late write committed after the source declared it never would: %+v", r.app.Order("o1"))
	}
}

func TestWithoutAFenceANegativeReceiptProvesNothing(t *testing.T) {
	r := newReceiptRig(t, true, true, false, nil)
	r.app.SetFaults(fixtureapp.Faults{DelayBeforeCommit: 1500 * time.Millisecond})
	q := r.request("req-1")
	rec, _ := r.run(q)
	got, err := r.reconcile(rec, q)
	if !errors.Is(err, ErrInconclusive) || got.Evidence.Level == ProvenNotCommitted {
		t.Fatalf("%+v %v", got.Evidence, err)
	}
	waitFor(t, "the late commit", func() bool { return r.app.Commits() == 1 })
	got, err = r.reconcile(rec, q)
	if err != nil {
		t.Fatal(err)
	}
	mustState(t, got.Record, Verified, "reconciled_committed")
}

func TestAReceiptForAnotherRequestIsNotEvidence(t *testing.T) {
	src := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"key":"someone-elses","record":"o1","committed":true,"version":"v2"}`))
	}))
	defer src.Close()
	e := NewAPIExecutor(APIConfig{BaseURL: src.URL, Dedup: true, ReceiptPath: "/receipts/%s", Fenced: true})
	ev, err := e.Reconcile(context.Background(), Probe{DedupToken: "mine", Target: Target{Record: "o1"}})
	if err != nil || ev.Level != Inconclusive || ev.Detail != "receipt_for_another_request" {
		t.Fatalf("%+v %v", ev, err)
	}
}

func TestAnExecutorCannotClaimMoreThanItsSourceGuarantees(t *testing.T) {
	r := newRig(t, nil)
	r.ex.applyFn = func(context.Context, Call) (Result, error) { return Result{}, errAmbiguous }
	r.ex.recon = func(Probe) (Evidence, error) { return Evidence{Level: ProvenNotCommitted, Basis: "trust_me"}, nil }
	q := r.request("req-1")
	rec, _ := r.run(q)
	for _, caps := range []Capabilities{
		{ReadBack: true},
		{ReadBack: true, DedupTokens: true, ReceiptLookup: true},
	} {
		r.ex.caps = caps
		got, err := r.reconcile(rec, q)
		if !errors.Is(err, ErrInconclusive) || got.Evidence.Level != Inconclusive {
			t.Fatalf("caps %+v: %+v %v", caps, got.Evidence, err)
		}
		mustState(t, got.Record, Unknown, "")
	}
	r.ex.caps = Capabilities{ReadBack: true, DedupTokens: true, ReceiptLookup: true, Fencing: true}
	got, err := r.reconcile(rec, q)
	if err != nil {
		t.Fatal(err)
	}
	mustState(t, got.Record, Failed, "reconciled_not_committed")
}

func TestNonMonotonicVersionTokensNeverCloseAnOperation(t *testing.T) {
	r := newRig(t, nil)
	r.ex.applyFn = func(context.Context, Call) (Result, error) { return Result{}, errAmbiguous }
	q := r.request("req-1")
	rec, _ := r.run(q)
	for _, v := range []string{"v1", "v0", "", "v1", "zzz", "v99"} {
		r.ex.mu.Lock()
		r.ex.version = v
		r.ex.mu.Unlock()
		if _, err := r.reconcile(rec, q); !errors.Is(err, ErrInconclusive) {
			t.Fatalf("version %q: %v", v, err)
		}
	}
	mustState(t, mustGet(t, r.rt, rec.Key), Unknown, "")
}

func TestReconciliationRevalidatesTheSessionAndTheEnvironment(t *testing.T) {
	cases := []struct {
		name   string
		change func(r *rig)
		reason string
	}{
		{"the session expired", func(r *rig) {
			s := goodSession(sessionOne, userCaller)
			s.ExpiresAt = r.clock.Now().Add(-time.Second)
			r.dir.setSession(s)
		}, "session_expired"},
		{"the session was revoked", func(r *rig) {
			s := goodSession(sessionOne, userCaller)
			s.Revoked = true
			r.dir.setSession(s)
		}, "session_revoked"},
		{"the session lost its source scopes", func(r *rig) {
			s := goodSession(sessionOne, userCaller)
			s.SourceScopes = []string{"records.read"}
			r.dir.setSession(s)
		}, "source_scope_missing"},
		{"the caller lost its scope", func(r *rig) {
			r.dir.mu.Lock()
			r.dir.callers[userCaller] = Caller{ID: userCaller, Scopes: []string{"records.read"}}
			r.dir.mu.Unlock()
		}, "caller_scope_missing"},
		{"the session now speaks for another principal", func(r *rig) {
			s := goodSession(sessionOne, userCaller)
			s.PrincipalRef = "prin_" + "c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3"
			r.dir.setSession(s)
		}, "other_source"},
		{"the session is attached to another application", func(r *rig) {
			s := goodSession(sessionOne, userCaller)
			s.AppID = "other-console:app.other.test"
			r.dir.setSession(s)
		}, "other_source"},
		{"the source version left the contract's range", func(r *rig) {
			s := goodSession(sessionOne, userCaller)
			s.AppVersion = "3.0.0"
			r.dir.setSession(s)
		}, "source_version_out_of_range"},
		{"the validator now refuses the environment", func(r *rig) { r.auth.setDeny("capability_digest_mismatch") }, "capability_digest_mismatch"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t, nil)
			r.ex.applyFn = func(context.Context, Call) (Result, error) { return Result{}, errAmbiguous }
			q := r.request("req-1")
			rec, _ := r.run(q)
			reads := r.ex.readCount()
			c.change(r)
			_, err := r.reconcile(rec, q)
			if got := reasonFrom(t, err); got != c.reason {
				t.Fatalf("refused for %q, want %q", got, c.reason)
			}
			if r.ex.readCount() != reads {
				t.Fatal("the source was read before the caller was authorized")
			}
			mustState(t, mustGet(t, r.rt, rec.Key), Unknown, "")
			if attempts(t, r.rt, rec.Key) != 1 {
				t.Fatal("the refused attempt left no evidence")
			}
		})
	}
}

func TestAFreshSessionOfTheSamePrincipalMayReconcile(t *testing.T) {
	r := newReceiptRig(t, true, true, true, nil)
	r.app.SetFaults(fixtureapp.Faults{DropAfterCommit: 1})
	q := r.request("req-1")
	rec, _ := r.run(q)
	s := goodSession(sessionOne, userCaller)
	s.Revoked = true
	r.dir.setSession(s)
	fresh := q
	fresh.SessionID = "sess-2"
	got, err := r.reconcile(rec, fresh)
	if err != nil {
		t.Fatal(err)
	}
	mustState(t, got.Record, Verified, "reconciled_committed")
}

func TestEveryReconciliationAttemptStaysOnTheChainAndTheHistoryIsUntouched(t *testing.T) {
	r := newAPIRig(t, false, nil)
	r.app.SetFaults(fixtureapp.Faults{DropBeforeCommit: 1})
	q := r.request("req-1")
	rec, _ := r.run(q)
	before := len(rec.Events)
	for i := 0; i < 3; i++ {
		r.reconcile(rec, q)
	}
	after := mustGet(t, r.rt, rec.Key)
	if len(after.Events) != before+3 {
		t.Fatalf("events %d -> %d", before, len(after.Events))
	}
	for i, e := range rec.Events {
		if after.Events[i].Hash != e.Hash {
			t.Fatalf("event %d was rewritten", i)
		}
	}
	if attemptsOf(after) != 3 {
		t.Fatalf("attempts %d", attemptsOf(after))
	}
	lines, _ := r.store.ReadAll()
	s := &MemStore{}
	for _, l := range lines {
		_ = s.Append(l)
	}
	if _, err := OpenJournal(s); err != nil {
		t.Fatalf("the journal no longer replays: %v", err)
	}
}

func TestTwoReconciliationsOfOneOperationDoNotRace(t *testing.T) {
	r := newRig(t, nil)
	r.ex.applyFn = func(context.Context, Call) (Result, error) { return Result{}, errAmbiguous }
	q := r.request("req-1")
	rec, _ := r.run(q)
	entered, release := make(chan struct{}), make(chan struct{})
	r.ex.onRead = func() {
		select {
		case <-entered:
		default:
			close(entered)
			<-release
		}
	}
	done := make(chan error, 1)
	go func() { _, err := r.reconcile(rec, q); done <- err }()
	<-entered
	_, err := r.reconcile(rec, q)
	if reasonFrom(t, err) != "reconcile_in_progress" {
		t.Fatal(err)
	}
	close(release)
	<-done
}

func TestOnlyReconciliationLiftsTheExclusionAndOnlyOnProof(t *testing.T) {
	r := newReceiptRig(t, true, true, true, nil)
	r.app.SetFaults(fixtureapp.Faults{DropBeforeCommit: 1})
	q := r.request("req-1")
	rec, _ := r.run(q)
	if _, err := r.rt.Submit(context.Background(), r.request("req-2")); reasonFrom(t, err) != "unresolved_unknown_commit" {
		t.Fatal(err)
	}
	if _, err := r.reconcile(rec, q); err != nil {
		t.Fatal(err)
	}
	next, err := r.run(r.request("req-3"))
	if err != nil {
		t.Fatal(err)
	}
	mustState(t, next, Verified, "")
	if r.app.Commits() != 1 {
		t.Fatalf("commits %d", r.app.Commits())
	}
}
