package execution

import (
	"context"
	"testing"
	"time"
)

func TestLongTasksEmitQueryableProgress(t *testing.T) {
	r := newRig(t, nil)
	r.ex.applyFn = func(ctx context.Context, c Call) (Result, error) {
		Progress(ctx, 0.25, "uploading")
		Progress(ctx, 0.75, "committing")
		r.ex.mu.Lock()
		r.ex.fields["stage"] = "won"
		r.ex.mu.Unlock()
		return Result{Outcome: OutcomeCommitted, Echo: map[string]string{}}, nil
	}
	h, err := r.rt.Submit(context.Background(), r.request("req-1"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	ev := h.Events(0)
	var stages []string
	for i, e := range ev {
		if e.Seq != i+1 {
			t.Fatalf("event %d has seq %d", i, e.Seq)
		}
		stages = append(stages, e.Stage)
	}
	want := []string{"prepared", "admitted", "queued", "reading", "running", "applying", "executor", "executor", "verifying", "verified"}
	if len(stages) != len(want) {
		t.Fatalf("stages %v", stages)
	}
	for i := range want {
		if stages[i] != want[i] {
			t.Fatalf("stages %v", stages)
		}
	}
	if ev[6].Fraction != 0.25 || ev[7].Message != "committing" {
		t.Fatalf("progress %+v %+v", ev[6], ev[7])
	}
	if got := h.Events(len(ev) - 1); len(got) != 1 || got[0].Stage != "verified" {
		t.Fatalf("since: %+v", got)
	}
	if len(h.Events(len(ev))) != 0 {
		t.Fatal("events beyond the end")
	}
}

func TestCancellingWhileQueuedReallyCancels(t *testing.T) {
	r := newRig(t, nil)
	started, release := make(chan struct{}), make(chan struct{})
	r.ex.applyFn = func(ctx context.Context, c Call) (Result, error) {
		close(started)
		<-release
		return Result{Outcome: OutcomeCommitted, Echo: map[string]string{}}, nil
	}
	first, _ := r.rt.Submit(context.Background(), r.request("first"))
	<-started
	q := r.request("second")
	q.Params["stage"] = "proposal"
	second, _ := r.rt.Submit(context.Background(), q)
	for r.rt.locks.Waiting(Footprint{App: appID, Session: sessionOne, Record: "o1", Level: LevelRecord}) == 0 {
		time.Sleep(time.Millisecond)
	}
	if ack := second.Cancel(); !ack.Requested {
		t.Fatalf("ack %+v", ack)
	}
	rec, _ := second.Wait(context.Background())
	mustState(t, rec, Failed, "cancelled_before_run")
	if second.CancelOutcome() != CancelBeforeEffect {
		t.Fatalf("outcome %q", second.CancelOutcome())
	}
	close(release)
	first.Wait(context.Background())
	if r.ex.applyCount() != 1 {
		t.Fatalf("applies %d", r.ex.applyCount())
	}
}

func TestCancellingDuringTheWriteNeverClaimsTheWriteWasCancelled(t *testing.T) {
	r := newRig(t, nil)
	started := make(chan struct{})
	r.ex.applyFn = func(ctx context.Context, c Call) (Result, error) {
		close(started)
		<-ctx.Done()
		return Result{}, ctx.Err()
	}
	h, _ := r.rt.Submit(context.Background(), r.request("req-1"))
	<-started
	h.Cancel()
	rec, _ := h.Wait(context.Background())
	mustState(t, rec, Unknown, ReasonUnknownCommit)
	if h.CancelOutcome() != CancelEffectUnknown {
		t.Fatalf("outcome %q", h.CancelOutcome())
	}
	for _, e := range rec.Events {
		if e.To == Failed {
			t.Fatal("a cancelled write was recorded as failed")
		}
	}
}

func TestCancellingAfterTheSourceCommittedStillVerifiesTheTruth(t *testing.T) {
	r := newRig(t, nil)
	var h *Handle
	started := make(chan struct{})
	r.ex.applyFn = func(ctx context.Context, c Call) (Result, error) {
		r.ex.mu.Lock()
		r.ex.fields["stage"] = "won"
		r.ex.mu.Unlock()
		close(started)
		time.Sleep(20 * time.Millisecond)
		return Result{Outcome: OutcomeCommitted, Echo: map[string]string{}}, nil
	}
	h, _ = r.rt.Submit(context.Background(), r.request("req-1"))
	<-started
	h.Cancel()
	rec, _ := h.Wait(context.Background())
	mustState(t, rec, Verified, "verified")
	if h.CancelOutcome() != CancelTooLate {
		t.Fatalf("outcome %q", h.CancelOutcome())
	}
}

func TestCancellingAFinishedOperationSaysSo(t *testing.T) {
	r := newRig(t, nil)
	h, _ := r.rt.Submit(context.Background(), r.request("req-1"))
	h.Wait(context.Background())
	if ack := h.Cancel(); ack.Requested || ack.Status != CancelFinished {
		t.Fatalf("ack %+v", ack)
	}
}

func TestCancellingBeforeAnyEffectInPreconditionsIsATrueCancellation(t *testing.T) {
	r := newRig(t, nil)
	started := make(chan struct{})
	slow := &slowReader{scripted: r.ex, started: started}
	r.cfg.Executors = map[string]Executor{"provider_api_authorized": slow}
	rt2, err := NewRuntime(r.cfg)
	if err != nil {
		t.Fatal(err)
	}
	h, _ := rt2.Submit(context.Background(), r.request("req-1"))
	<-started
	h.Cancel()
	rec, _ := h.Wait(context.Background())
	mustState(t, rec, Failed, "")
	if r.ex.applyCount() != 0 {
		t.Fatal("applied after cancellation")
	}
	if h.CancelOutcome() != CancelBeforeEffect {
		t.Fatalf("outcome %q", h.CancelOutcome())
	}
}

type slowReader struct {
	*scripted
	started chan struct{}
}

func (s *slowReader) Read(ctx context.Context, t Target) (Snapshot, error) {
	close(s.started)
	<-ctx.Done()
	return Snapshot{}, ctx.Err()
}
