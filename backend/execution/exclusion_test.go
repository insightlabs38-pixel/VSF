package execution

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

var errAmbiguous = errors.New("connection reset by peer")

// gate lets a test hold the first write inside the source and end it ambiguously on demand.
type gate struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func newGate() *gate { return &gate{entered: make(chan struct{}), release: make(chan struct{})} }

func (g *gate) apply(ctx context.Context, _ Call) (Result, error) {
	g.once.Do(func() { close(g.entered) })
	select {
	case <-g.release:
	case <-ctx.Done():
	}
	return Result{}, errAmbiguous
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func setScope(scope string) func(map[string]any) {
	return func(b map[string]any) { b["effect"].(map[string]any)["concurrency"] = scope }
}

func TestADifferentRequestAdmittedBeforeAnAmbiguousWriteNeverReachesTheSource(t *testing.T) {
	r := newRig(t, nil)
	g := newGate()
	r.ex.applyFn = g.apply
	first, err := r.rt.Submit(context.Background(), r.request("first"))
	if err != nil {
		t.Fatal(err)
	}
	<-g.entered
	second, err := r.rt.Submit(context.Background(), r.request("second"))
	if err != nil {
		t.Fatalf("the second request is admitted while the first is still executing: %v", err)
	}
	waitFor(t, "the second request to queue behind the first", func() bool {
		return r.rt.locks.Waiting(Footprint{App: appID, Session: sessionOne, Record: "o1", Level: LevelRecord}) == 1
	})
	close(g.release)
	rec1, _ := first.Wait(context.Background())
	mustState(t, rec1, Unknown, ReasonUnknownCommit)
	rec2, _ := second.Wait(context.Background())
	mustState(t, rec2, Failed, "unresolved_unknown_commit")
	for _, e := range rec2.Events {
		if e.To == Running {
			t.Fatal("the second operation reached Running")
		}
	}
	if n := r.ex.applyCount(); n != 1 {
		t.Fatalf("the source received %d writes", n)
	}
	if h := second.CancelOutcome(); h != CancelPending {
		t.Fatalf("cancel status %q", h)
	}
}

func TestManyDistinctRequestsBehindOneAmbiguousWriteAllStopBeforeTheSource(t *testing.T) {
	r := newRig(t, nil)
	g := newGate()
	r.ex.applyFn = g.apply
	first, _ := r.rt.Submit(context.Background(), r.request("req-0"))
	<-g.entered
	const n = 12
	var hs []*Handle
	for i := 1; i <= n; i++ {
		h, err := r.rt.Submit(context.Background(), r.request(fmt.Sprintf("req-%d", i)))
		if err != nil {
			t.Fatal(err)
		}
		hs = append(hs, h)
	}
	waitFor(t, "every request to queue", func() bool {
		return r.rt.locks.Waiting(Footprint{App: appID, Session: sessionOne, Record: "o1", Level: LevelRecord}) == n
	})
	close(g.release)
	first.Wait(context.Background())
	for _, h := range hs {
		rec, _ := h.Wait(context.Background())
		mustState(t, rec, Failed, "unresolved_unknown_commit")
	}
	if r.ex.applyCount() != 1 {
		t.Fatalf("applies %d", r.ex.applyCount())
	}
}

func TestUnresolvedWritesBlockExactlyTheScopesTheyOverlap(t *testing.T) {
	cases := []struct {
		name    string
		first   string
		second  func(*Request)
		blocked bool
	}{
		{"record scope blocks the same record", "source_session_and_record", func(q *Request) {}, true},
		{"record scope leaves other records alone", "source_session_and_record", func(q *Request) { q.Record = "o2" }, false},
		{"session scope blocks every record of the session", "source_session", func(q *Request) { q.Record = "o2" }, true},
		{"session scope leaves other sessions alone", "source_session", func(q *Request) { q.SessionID = "sess-2" }, false},
		{"global scope blocks other sessions", "global_source", func(q *Request) { q.SessionID, q.Record = "sess-2", "o9" }, true},
		{"no scope still blocks its own record", "none", func(q *Request) {}, true},
		{"no scope leaves other records alone", "none", func(q *Request) { q.Record = "o2" }, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t, setScope(c.first))
			r.ex.applyFn = func(context.Context, Call) (Result, error) { return Result{}, errAmbiguous }
			rec, _ := r.run(r.request("first"))
			mustState(t, rec, Unknown, ReasonUnknownCommit)
			r.ex.applyFn = nil
			q := r.request("second")
			c.second(&q)
			_, err := r.rt.Submit(context.Background(), q)
			if c.blocked {
				if reasonFrom(t, err) != "unresolved_unknown_commit" {
					t.Fatalf("not blocked: %v", err)
				}
				if r.ex.applyCount() != 1 {
					t.Fatalf("applies %d", r.ex.applyCount())
				}
				return
			}
			if err != nil {
				t.Fatalf("blocked without overlap: %v", err)
			}
		})
	}
}

func TestAnUnresolvedWriteBlocksAWriteDeclaringAnotherScope(t *testing.T) {
	r := newRig(t, nil)
	r.ex.applyFn = func(context.Context, Call) (Result, error) { return Result{}, errAmbiguous }
	rec, _ := r.run(r.request("first"))
	mustState(t, rec, Unknown, "")
	wide := contractDoc(t, setScope("source_session"))
	q := r.request("second")
	q.Contract, q.Pin = wide, contractPin(t, wide)
	if _, err := r.rt.Submit(context.Background(), q); reasonFrom(t, err) != "unresolved_unknown_commit" {
		t.Fatalf("a session-wide write ran past an unresolved record write: %v", err)
	}
}

func TestAWriteWithoutAScopeNeedsAConditionToBeSafe(t *testing.T) {
	r := newRig(t, func(b map[string]any) {
		b["effect"].(map[string]any)["concurrency"] = "none"
		b["freshness"].(map[string]any)["before_write"] = "none"
	})
	_, err := r.rt.Submit(context.Background(), r.request("req-1"))
	if reasonFrom(t, err) != "write_scope_insufficient" {
		t.Fatal(err)
	}
	if r.ex.applyCount() != 0 || r.ex.readCount() != 0 {
		t.Fatal("the source was touched")
	}
}

func TestACancelledWaiterNeverContactsTheSourceAndLeavesTheLineMoving(t *testing.T) {
	r := newRig(t, nil)
	hold := make(chan struct{})
	r.ex.applyFn = func(ctx context.Context, c Call) (Result, error) {
		<-hold
		r.ex.mu.Lock()
		r.ex.fields["stage"] = "won"
		r.ex.mu.Unlock()
		return Result{Outcome: OutcomeCommitted, Echo: map[string]string{}}, nil
	}
	first, _ := r.rt.Submit(context.Background(), r.request("first"))
	waitFor(t, "the first write", func() bool { return r.ex.applyCount() == 1 })
	second, _ := r.rt.Submit(context.Background(), r.request("second"))
	third, _ := r.rt.Submit(context.Background(), r.request("third"))
	fp := Footprint{App: appID, Session: sessionOne, Record: "o1", Level: LevelRecord}
	waitFor(t, "both to queue", func() bool { return r.rt.locks.Waiting(fp) == 2 })
	second.Cancel()
	rec, _ := second.Wait(context.Background())
	mustState(t, rec, Failed, "cancelled_before_run")
	if second.CancelOutcome() != CancelBeforeEffect {
		t.Fatalf("cancel status %q", second.CancelOutcome())
	}
	close(hold)
	first.Wait(context.Background())
	rec, _ = third.Wait(context.Background())
	if rec.State == Running || rec.State == Admitted {
		t.Fatalf("the line stalled behind a cancelled waiter: %s", rec.State)
	}
	if r.ex.applyCount() > 2 {
		t.Fatalf("applies %d", r.ex.applyCount())
	}
}

func TestCancellingDuringAnAmbiguousWriteKeepsTheExclusionUntilReconciled(t *testing.T) {
	r := newRig(t, nil)
	g := newGate()
	r.ex.applyFn = g.apply
	h, _ := r.rt.Submit(context.Background(), r.request("first"))
	<-g.entered
	if ack := h.Cancel(); !ack.Requested {
		t.Fatal("cancel was not accepted")
	}
	rec, _ := h.Wait(context.Background())
	mustState(t, rec, Unknown, ReasonUnknownCommit)
	if h.CancelOutcome() != CancelEffectUnknown {
		t.Fatalf("cancel status %q", h.CancelOutcome())
	}
	r.ex.applyFn = nil
	if _, err := r.rt.Submit(context.Background(), r.request("second")); reasonFrom(t, err) != "unresolved_unknown_commit" {
		t.Fatal("cancelling released the reservation")
	}
}

func TestAJournalFailureAtTheAmbiguousTransitionStopsEverythingBehindIt(t *testing.T) {
	r := newRig(t, nil)
	r.cfg.Journal, _ = OpenJournal(&failingStore{inner: r.store, failAfter: 5})
	var err error
	if r.rt, err = NewRuntime(r.cfg); err != nil {
		t.Fatal(err)
	}
	g := newGate()
	r.ex.applyFn = g.apply
	first, _ := r.rt.Submit(context.Background(), r.request("first"))
	<-g.entered
	second, err := r.rt.Submit(context.Background(), r.request("second"))
	if err != nil {
		t.Fatal(err)
	}
	close(g.release)
	if _, err := first.Wait(context.Background()); err == nil {
		t.Fatal("the lost record was not reported")
	}
	second.Wait(context.Background())
	if second.Record().State == Running || second.Record().State == Verified {
		t.Fatalf("second state %s", second.Record().State)
	}
	if r.ex.applyCount() != 1 {
		t.Fatalf("applies %d", r.ex.applyCount())
	}
	if _, err := r.rt.Submit(context.Background(), r.request("third")); err == nil {
		t.Fatal("a broken journal still admits work")
	}
}

func TestExclusionSurvivesARestartAndEndsOnlyThroughReconciliation(t *testing.T) {
	r := newRig(t, nil)
	g := newGate()
	r.ex.applyFn = g.apply
	h, _ := r.rt.Submit(context.Background(), r.request("first"))
	<-g.entered
	lines, _ := r.store.ReadAll()
	crashed := &MemStore{}
	for _, l := range lines {
		_ = crashed.Append(l)
	}
	j2, err := OpenJournal(crashed)
	if err != nil {
		t.Fatal(err)
	}
	r.cfg.Journal = j2
	r.ex.applyFn = nil
	rt2, err := NewRuntime(r.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := rt2.Get(h.Key()); got.State != Unknown {
		t.Fatalf("recovered %s", got.State)
	}
	if _, err := rt2.Submit(context.Background(), r.request("second")); reasonFrom(t, err) != "unresolved_unknown_commit" {
		t.Fatal("a restart forgot the unresolved write")
	}
	if n := len(j2.Unresolved()); n != 1 {
		t.Fatalf("%d unresolved", n)
	}
	close(g.release)
	h.Wait(context.Background())
}

func TestScopeKeysCannotCollideAcrossSessionsRecordsAndSources(t *testing.T) {
	rec := func(app, session, record string) Footprint {
		return Footprint{App: app, Session: session, Record: record, Level: LevelRecord, Exclusive: true}
	}
	if rec("a", "x:y", "z").Overlaps(rec("a", "x", "y:z")) {
		t.Fatal("separator characters let two scopes collide")
	}
	if rec("a", "s", "r").Overlaps(rec("b", "s", "r")) {
		t.Fatal("two applications share a scope")
	}
	if rec("a", "s1", "r").Overlaps(rec("a", "s2", "r")) {
		t.Fatal("two sessions share a record scope")
	}
	global := Footprint{App: "a", Level: LevelGlobal, Exclusive: true}
	if !global.Overlaps(rec("a", "s", "r")) || global.Overlaps(rec("b", "s", "r")) {
		t.Fatal("a global scope must cover its own application and no other")
	}
	r := newRig(t, nil)
	for _, bad := range []func(*Request){
		func(q *Request) { q.RequestID = "a\x00b" },
		func(q *Request) { q.Caller = "c\x00d" },
		func(q *Request) { q.SessionID = "s\nx" },
		func(q *Request) { q.Record = "o1\x00" },
	} {
		q := r.request("req-x")
		bad(&q)
		if _, err := r.rt.Submit(context.Background(), q); reasonFrom(t, err) != "request_malformed" {
			t.Fatal(err)
		}
	}
}

func TestLocksExcludeOverlappingScopesAndNotDisjointOnes(t *testing.T) {
	l := NewLocks()
	sess := Footprint{App: "a", Session: "s", Level: LevelSession, Exclusive: true}
	rec := func(r string) Footprint {
		return Footprint{App: "a", Session: "s", Record: r, Level: LevelRecord, Exclusive: true}
	}
	relS, _ := l.Acquire(context.Background(), sess)
	got := make(chan struct{})
	go func() {
		rel, _ := l.Acquire(context.Background(), rec("1"))
		close(got)
		rel()
	}()
	waitFor(t, "a record waiter", func() bool { return l.Waiting(rec("1")) == 1 })
	select {
	case <-got:
		t.Fatal("a record lock was granted under a session lock")
	case <-time.After(20 * time.Millisecond):
	}
	relS()
	<-got
	rel1, _ := l.Acquire(context.Background(), rec("1"))
	rel2, err := l.Acquire(context.Background(), rec("2"))
	if err != nil {
		t.Fatal(err)
	}
	rel1()
	rel2()
}

func TestAWaiterBehindACancelledWaiterIsGrantedWhenTheHolderLeaves(t *testing.T) {
	l := NewLocks()
	fp := Footprint{App: "a", Session: "s", Record: "1", Level: LevelRecord, Exclusive: true}
	hold, _ := l.Acquire(context.Background(), fp)
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { _, err := l.Acquire(ctx, fp); errc <- err }()
	waitFor(t, "first waiter", func() bool { return l.Waiting(fp) == 1 })
	done := make(chan struct{})
	go func() { rel, _ := l.Acquire(context.Background(), fp); rel(); close(done) }()
	waitFor(t, "second waiter", func() bool { return l.Waiting(fp) == 2 })
	cancel()
	if err := <-errc; err == nil {
		t.Fatal("acquired after cancel")
	}
	hold()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the line stalled behind a cancelled waiter")
	}
}

func TestAuthorizationThatLapsesDuringTheSourceReadStopsTheWrite(t *testing.T) {
	cases := []struct {
		name   string
		change func(r *rig)
		reason string
	}{
		{"the session expires", func(r *rig) {
			s := goodSession(sessionOne, userCaller)
			s.ExpiresAt = r.clock.Now().Add(-time.Second)
			r.dir.setSession(s)
		}, "session_expired"},
		{"the session is revoked", func(r *rig) {
			s := goodSession(sessionOne, userCaller)
			s.Revoked = true
			r.dir.setSession(s)
		}, "session_revoked"},
		{"the caller loses the role", func(r *rig) { r.dir.revoke(userCaller) }, "unknown_caller"},
		{"the source capabilities change", func(r *rig) {
			s := goodSession(sessionOne, userCaller)
			s.CapabilityDigest = "sha256:" + fmt.Sprintf("%064d", 7)
			r.dir.setSession(s)
		}, "environment_changed"},
		{"the browser runtime is swapped", func(r *rig) {
			s := goodSession(sessionOne, userCaller)
			s.RuntimeID = "rt_other"
			r.dir.setSession(s)
		}, "environment_changed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t, nil)
			r.ex.onRead = func() { c.change(r) }
			rec, _ := r.run(r.request("req-1"))
			mustState(t, rec, Failed, c.reason)
			if r.ex.applyCount() != 0 {
				t.Fatal("the write reached the source")
			}
		})
	}
}

func TestANonAuthoritativeSourceStatusIsNotTakenAsProofOfNoEffect(t *testing.T) {
	cases := []struct {
		name    string
		setup   func(r *rig)
		outcome Outcome
		state   State
		reason  string
	}{
		{"untouched state allows a failure", func(r *rig) {}, OutcomeRejected, Failed, "rejected"},
		{"a changed version does not", func(r *rig) {
			r.ex.applyFn = func(context.Context, Call) (Result, error) {
				r.ex.mu.Lock()
				r.ex.version = "v9"
				r.ex.mu.Unlock()
				return Result{Outcome: OutcomeRejected}, nil
			}
		}, OutcomeRejected, Unknown, ReasonUnknownCommit},
		{"an unreadable source does not", func(r *rig) {
			r.ex.applyFn = func(context.Context, Call) (Result, error) {
				r.ex.mu.Lock()
				r.ex.readErr = errors.New("offline")
				r.ex.mu.Unlock()
				return Result{Outcome: OutcomeConflict}, nil
			}
		}, OutcomeConflict, Unknown, ReasonUnknownCommit},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t, nil)
			r.ex.caps.AuthoritativeStatus = false
			r.ex.results = []Result{{Outcome: c.outcome}}
			c.setup(r)
			rec, _ := r.run(r.request("req-1"))
			mustState(t, rec, c.state, c.reason)
		})
	}
}

func TestThrottlingFromANonAuthoritativeSourceIsNotRetried(t *testing.T) {
	r := newRig(t, nil)
	r.ex.caps.AuthoritativeStatus = false
	r.ex.results = []Result{{Outcome: OutcomeThrottled}, {Outcome: OutcomeCommitted}}
	r.run(r.request("req-1"))
	if r.ex.applyCount() != 1 {
		t.Fatalf("applies %d; an unproven throttle was retried", r.ex.applyCount())
	}
}
