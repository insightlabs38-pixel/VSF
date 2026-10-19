package execution

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var lk = Footprint{App: "a", Session: "s", Record: "k", Level: LevelRecord, Exclusive: true}

func TestLocksServeWaitersInArrivalOrder(t *testing.T) {
	l := NewLocks()
	rel, _ := l.Acquire(context.Background(), lk)
	var order []int
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 1; i <= 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := l.Acquire(context.Background(), lk)
			if err != nil {
				return
			}
			mu.Lock()
			order = append(order, i)
			mu.Unlock()
			r()
		}()
		for l.Waiting(lk) < i {
			time.Sleep(time.Millisecond)
		}
	}
	rel()
	wg.Wait()
	for i, v := range order {
		if v != i+1 {
			t.Fatalf("order %v", order)
		}
	}
}

func TestAWaiterThatGivesUpNeverHoldsTheLock(t *testing.T) {
	l := NewLocks()
	rel, _ := l.Acquire(context.Background(), lk)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() { _, err := l.Acquire(ctx, lk); done <- err }()
	for l.Waiting(lk) == 0 {
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-done; err == nil {
		t.Fatal("acquired after cancel")
	}
	rel()
	r2, err := l.Acquire(context.Background(), lk)
	if err != nil {
		t.Fatal(err)
	}
	r2()
	if l.Waiting(lk) != 0 {
		t.Fatal("stale waiter")
	}
}

func TestReleasingTwiceDoesNotHandOverTheLockTwice(t *testing.T) {
	l := NewLocks()
	rel, _ := l.Acquire(context.Background(), lk)
	rel()
	rel()
	var held atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, _ := l.Acquire(context.Background(), lk)
			if held.Add(1) != 1 {
				t.Error("two holders")
			}
			time.Sleep(2 * time.Millisecond)
			held.Add(-1)
			r()
		}()
	}
	wg.Wait()
}

// concurrency launches n operations and reports the most that ran inside the executor at once.
func concurrency(t *testing.T, edit func(map[string]any), n int, shape func(i int, q *Request)) int {
	t.Helper()
	r := newRig(t, edit)
	hold := make(chan struct{})
	entered := make(chan struct{}, n)
	r.ex.applyFn = func(ctx context.Context, c Call) (Result, error) {
		entered <- struct{}{}
		<-hold
		r.ex.mu.Lock()
		r.ex.fields["stage"] = "won"
		r.ex.mu.Unlock()
		return Result{Outcome: OutcomeCommitted, Echo: map[string]string{}}, nil
	}
	var hs []*Handle
	for i := 0; i < n; i++ {
		q := r.request(fmt.Sprintf("req-%d", i))
		shape(i, &q)
		h, err := r.rt.Submit(context.Background(), q)
		if err != nil {
			t.Fatal(err)
		}
		hs = append(hs, h)
	}
	time.Sleep(50 * time.Millisecond)
	close(hold)
	for _, h := range hs {
		if _, err := h.Wait(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	return r.ex.maxIn
}

func TestConcurrencyScopesDecideWhatMayOverlap(t *testing.T) {
	scope := func(s string) func(map[string]any) {
		return func(b map[string]any) { b["effect"].(map[string]any)["concurrency"] = s }
	}
	sameRecord := func(i int, q *Request) {}
	otherRecord := func(i int, q *Request) { q.Record = fmt.Sprintf("o%d", i+1) }
	otherSession := func(i int, q *Request) {
		q.SessionID = []string{sessionOne, "sess-2"}[i%2]
		q.Record = fmt.Sprintf("o%d", i+1)
	}
	cases := []struct {
		name  string
		scope string
		shape func(int, *Request)
		want  int
	}{
		{"same record in one session serializes", "source_session_and_record", sameRecord, 1},
		{"other records in one session overlap", "source_session_and_record", otherRecord, 3},
		{"a session scope serializes every record", "source_session", otherRecord, 1},
		{"sessions do not block each other under a session scope", "source_session", otherSession, 2},
		{"a global scope serializes across sessions", "global_source", otherSession, 1},
		{"no scope overlaps freely", "none", sameRecord, 3},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := concurrency(t, scope(c.scope), 3, c.shape); got != c.want {
				t.Fatalf("max in flight %d, want %d", got, c.want)
			}
		})
	}
}

func TestRecordScopeNeedsARecordKey(t *testing.T) {
	r := newRig(t, nil)
	q := r.request("req-1")
	q.Record = ""
	_, err := r.rt.Submit(context.Background(), q)
	if reasonFrom(t, err) != "record_required" {
		t.Fatal(err)
	}
}

func TestResourceBudgetCapsSimultaneousCallsToOneSource(t *testing.T) {
	r := newRig(t, func(b map[string]any) { b["effect"].(map[string]any)["concurrency"] = "none" })
	r.cfg.Limiter = NewLimiter(r.clock, LimiterConfig{MaxInFlight: 2, MaxInterval: time.Second})
	var err error
	r.rt, err = NewRuntime(r.cfg)
	if err != nil {
		t.Fatal(err)
	}
	hold := make(chan struct{})
	r.ex.applyFn = func(ctx context.Context, c Call) (Result, error) {
		<-hold
		r.ex.mu.Lock()
		r.ex.fields["stage"] = "won"
		r.ex.mu.Unlock()
		return Result{Outcome: OutcomeCommitted, Echo: map[string]string{}}, nil
	}
	var hs []*Handle
	for i := 0; i < 5; i++ {
		h, err := r.rt.Submit(context.Background(), r.request(fmt.Sprintf("req-%d", i)))
		if err != nil {
			t.Fatal(err)
		}
		hs = append(hs, h)
	}
	time.Sleep(50 * time.Millisecond)
	close(hold)
	for _, h := range hs {
		h.Wait(context.Background())
	}
	if r.ex.maxIn != 2 {
		t.Fatalf("max in flight %d, want the budget of 2", r.ex.maxIn)
	}
}

func TestLimiterBacksOffOnRefusalAndRecoversOnSuccess(t *testing.T) {
	c := newClock()
	l := NewLimiter(c, LimiterConfig{MaxInFlight: 1, Interval: 100 * time.Millisecond, MaxInterval: 800 * time.Millisecond, Recover: 2})
	for i, want := range []time.Duration{200, 400, 800, 800} {
		l.Slow("a", 0)
		if got := l.Interval("a"); got != want*time.Millisecond {
			t.Fatalf("step %d interval %v", i, got)
		}
	}
	l.Ok("a")
	if l.Interval("a") != 800*time.Millisecond {
		t.Fatal("recovered too early")
	}
	l.Ok("a")
	if l.Interval("a") != 400*time.Millisecond {
		t.Fatalf("interval %v", l.Interval("a"))
	}
	l.Ok("a")
	l.Ok("a")
	l.Ok("a")
	l.Ok("a")
	if l.Interval("a") != 100*time.Millisecond {
		t.Fatalf("interval %v; it must return to the base and no lower", l.Interval("a"))
	}
	if l.Interval("other") != 100*time.Millisecond {
		t.Fatal("one source's refusal slowed another")
	}
}

func TestLimiterHonoursTheSourcesRetryHintBeforeTheNextCall(t *testing.T) {
	c := newClock()
	l := NewLimiter(c, LimiterConfig{MaxInFlight: 1, Interval: 0, MaxInterval: time.Second})
	rel, err := l.Acquire(context.Background(), "a")
	if err != nil {
		t.Fatal(err)
	}
	rel()
	l.Slow("a", 5*time.Second)
	start := c.Now()
	rel, err = l.Acquire(context.Background(), "a")
	if err != nil {
		t.Fatal(err)
	}
	rel()
	if waited := c.Now().Sub(start); waited < 5*time.Second {
		t.Fatalf("waited %v; the source said 5s", waited)
	}
}

func TestLimiterWaitCanBeCancelled(t *testing.T) {
	c := &blockingClock{fakeClock: newClock()}
	l := NewLimiter(c, LimiterConfig{MaxInFlight: 1, MaxInterval: time.Second})
	rel, _ := l.Acquire(context.Background(), "a")
	defer rel()
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error)
	go func() { _, err := l.Acquire(ctx, "a"); errc <- err }()
	time.Sleep(10 * time.Millisecond)
	cancel()
	if err := <-errc; err == nil {
		t.Fatal("acquired over the budget")
	}
}

type blockingClock struct{ *fakeClock }

func (b *blockingClock) Sleep(ctx context.Context, d time.Duration) error {
	<-ctx.Done()
	return ctx.Err()
}
