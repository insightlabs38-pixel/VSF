package execution

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

type fakeConn struct {
	mu      sync.Mutex
	out     chan Frame
	session string
	writes  [][]byte
	block   chan struct{} // when set, Write waits for it or for the connection to drop
	failW   error
	dropped chan struct{}
	closed  bool
}

func newFakeConn(session string) *fakeConn {
	return &fakeConn{out: make(chan Frame, 1024), session: session, dropped: make(chan struct{})}
}

func (c *fakeConn) Write(ctx context.Context, data []byte) error {
	c.mu.Lock()
	c.writes = append(c.writes, append([]byte(nil), data...))
	block, fail := c.block, c.failW
	c.mu.Unlock()
	if block != nil {
		select {
		case <-block:
		case <-c.dropped:
			return errors.New("connection lost before the acknowledgement")
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return fail
}

func (c *fakeConn) Output() <-chan Frame  { return c.out }
func (c *fakeConn) SourceSession() string { return c.session }
func (c *fakeConn) Close() error          { c.drop(); return nil }

// drop is the network going away: output ends and pending writes fail.
func (c *fakeConn) drop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.closed {
		c.closed = true
		close(c.dropped)
		close(c.out)
	}
}

func (c *fakeConn) emit(s string) { c.out <- Frame{Data: []byte(s)} }

func (c *fakeConn) written() int { c.mu.Lock(); defer c.mu.Unlock(); return len(c.writes) }

type fakeDialer struct {
	mu       sync.Mutex
	opened   []*fakeConn
	attaches []*fakeConn
	session  string
}

func (d *fakeDialer) Open(context.Context, string) (Conn, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	c := newFakeConn(d.session)
	d.opened = append(d.opened, c)
	return c, nil
}

func (d *fakeDialer) Attach(_ context.Context, _ string, _ string) (Conn, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	c := newFakeConn(d.session)
	d.attaches = append(d.attaches, c)
	return c, nil
}

func (d *fakeDialer) first() *fakeConn { d.mu.Lock(); defer d.mu.Unlock(); return d.opened[0] }
func (d *fakeDialer) last() *fakeConn {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.attaches[len(d.attaches)-1]
}

var alice = Owner{Tenant: "t1", Caller: "alice", Session: "s1"}

func newHubFor(cfg StreamConfig) (*Hub, *fakeDialer) {
	d := &fakeDialer{session: "src-1"}
	return NewHub(d, cfg), d
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for i := 0; i < 400; i++ {
		if ok() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func waitState(t *testing.T, p *Process, want StreamState) {
	t.Helper()
	eventually(t, "state "+string(want), func() bool { st, _, _ := p.State(alice); return st == want })
}

func waitSeq(t *testing.T, p *Process, seq uint64) {
	t.Helper()
	eventually(t, fmt.Sprintf("output frame %d", seq), func() bool { _, last, _ := p.State(alice); return last >= seq })
}

func TestOutputIsOrderedAndAReconnectReplaysOnlyWhatWasMissed(t *testing.T) {
	h, d := newHubFor(StreamConfig{})
	p, err := h.Open(context.Background(), alice, "shell")
	if err != nil {
		t.Fatal(err)
	}
	d.first().emit("one")
	d.first().emit("two")
	waitSeq(t, p, 2)
	d.first().drop()
	waitState(t, p, StreamDisconnected)
	att, err := h.Reattach(context.Background(), alice, p.ID(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(att.Frames) != 1 || string(att.Frames[0].Data) != "two" || att.Frames[0].Seq != 2 || att.Gap != 0 {
		t.Fatalf("%+v", att)
	}
	d.last().emit("three")
	waitSeq(t, p, 3)
	again, _ := h.Reattach(context.Background(), alice, p.ID(), 3)
	if again != nil {
		t.Fatalf("a process that is attached was attached twice")
	}
}

func TestInputInFlightWhenTheConnectionDropsIsUnknownAndNeverSentAgain(t *testing.T) {
	h, d := newHubFor(StreamConfig{})
	p, _ := h.Open(context.Background(), alice, "shell")
	c := d.first()
	c.block = make(chan struct{})
	done := make(chan InputState, 1)
	go func() {
		st, _ := p.SendInput(context.Background(), alice, 1, []byte("rm -rf build\n"))
		done <- st
	}()
	eventually(t, "the write to start", func() bool { return c.written() == 1 })
	c.drop()
	if st := <-done; st != InputUnknown {
		t.Fatalf("input state %s", st)
	}
	waitState(t, p, StreamDisconnected)
	if st, err := p.SendInput(context.Background(), alice, 1, []byte("rm -rf build\n")); err != nil || st != InputUnknown || c.written() != 1 {
		t.Fatalf("a repeated sequence number must report the recorded fate and send nothing: %s %v writes=%d", st, err, c.written())
	}
	if _, err := p.SendInput(context.Background(), alice, 2, []byte("ls\n")); StreamReason(err) != StreamClosed {
		t.Fatalf("input while disconnected: %v", err)
	}
	att, err := h.Reattach(context.Background(), alice, p.ID(), 0)
	if err != nil || len(att.Unknown) != 1 || att.Unknown[0] != 1 {
		t.Fatalf("%+v %v", att, err)
	}
	if d.last().written() != 0 {
		t.Fatal("reattaching replayed input into the process")
	}
}

func TestADisconnectBeforeAnyInputOrOutputLeavesNothingUnknown(t *testing.T) {
	h, d := newHubFor(StreamConfig{})
	p, _ := h.Open(context.Background(), alice, "shell")
	d.first().drop()
	waitState(t, p, StreamDisconnected)
	att, err := h.Reattach(context.Background(), alice, p.ID(), 0)
	if err != nil || len(att.Unknown) != 0 || len(att.Frames) != 0 || att.State != StreamOpen {
		t.Fatalf("%+v %v", att, err)
	}
	if st, err := p.SendInput(context.Background(), alice, 1, []byte("x")); err != nil || st != InputAcked {
		t.Fatalf("%s %v", st, err)
	}
}

func TestACompletedInputStaysAckedAcrossAReconnect(t *testing.T) {
	h, d := newHubFor(StreamConfig{})
	p, _ := h.Open(context.Background(), alice, "shell")
	if st, _ := p.SendInput(context.Background(), alice, 1, []byte("a")); st != InputAcked {
		t.Fatal(st)
	}
	d.first().drop()
	waitState(t, p, StreamDisconnected)
	att, _ := h.Reattach(context.Background(), alice, p.ID(), 0)
	if len(att.Unknown) != 0 {
		t.Fatalf("%+v", att)
	}
}

func TestAReconnectToADifferentSourceSessionFailsClosed(t *testing.T) {
	h, d := newHubFor(StreamConfig{})
	p, _ := h.Open(context.Background(), alice, "shell")
	d.first().drop()
	waitState(t, p, StreamDisconnected)
	d.mu.Lock()
	d.session = "src-2" // the source logged the user in again as someone or something else
	d.mu.Unlock()
	if _, err := h.Reattach(context.Background(), alice, p.ID(), 0); StreamReason(err) != StreamSourceSession {
		t.Fatalf("%v", err)
	}
	if st, _, _ := p.State(alice); st != StreamFailed {
		t.Fatalf("state %s", st)
	}
	if _, err := p.SendInput(context.Background(), alice, 1, []byte("x")); StreamReason(err) != StreamClosed {
		t.Fatalf("input into a failed process: %v", err)
	}
	if !d.last().closed {
		t.Fatal("the connection to the other session stayed open")
	}
}

func TestOnlyTheExactOwnerReachesAProcessAndOthersLearnNothing(t *testing.T) {
	h, _ := newHubFor(StreamConfig{})
	p, _ := h.Open(context.Background(), alice, "shell")
	for _, o := range []Owner{
		{Tenant: "t2", Caller: "alice", Session: "s1"},
		{Tenant: "t1", Caller: "mallory", Session: "s1"},
		{Tenant: "t1", Caller: "alice", Session: "s2"},
		{},
	} {
		if _, err := p.SendInput(context.Background(), o, 1, []byte("x")); StreamReason(err) != StreamNotFound {
			t.Errorf("%+v sent input: %v", o, err)
		}
		if _, err := p.Cancel(o); StreamReason(err) != StreamNotFound {
			t.Errorf("%+v cancelled: %v", o, err)
		}
		_, errReal := h.Reattach(context.Background(), o, p.ID(), 0)
		_, errNone := h.Reattach(context.Background(), o, "proc-999", 0)
		if StreamReason(errReal) != StreamNotFound || errReal.Error() != errNone.Error() {
			t.Errorf("%+v can tell a real process from a missing one: %v vs %v", o, errReal, errNone)
		}
		if _, _, err := p.State(o); err == nil {
			t.Errorf("%+v read the state", o)
		}
	}
	if st, _, _ := p.State(alice); st != StreamOpen {
		t.Fatalf("an outsider changed the process: %s", st)
	}
}

func TestCancellingBeforeAnyInputIsCleanAndAfterInputLeavesTheEffectUnknown(t *testing.T) {
	h, d := newHubFor(StreamConfig{})
	clean, _ := h.Open(context.Background(), alice, "a")
	if st, err := clean.Cancel(alice); err != nil || st != StreamCancelled {
		t.Fatalf("%s %v", st, err)
	}
	busy, _ := h.Open(context.Background(), alice, "b")
	c := d.opened[1]
	c.block = make(chan struct{})
	go func() { _, _ = busy.SendInput(context.Background(), alice, 1, []byte("make install\n")) }()
	eventually(t, "the write", func() bool { return c.written() == 1 })
	if st, _ := busy.Cancel(alice); st != StreamUnknown {
		t.Fatalf("cancel with input in flight: %s", st)
	}
	if st, _ := busy.Cancel(alice); st != StreamUnknown {
		t.Fatalf("cancel is not idempotent: %s", st)
	}
	if _, err := busy.SendInput(context.Background(), alice, 2, []byte("x")); StreamReason(err) != StreamClosed {
		t.Fatalf("%v", err)
	}
}

func TestOutputOlderThanTheReplayBufferIsReportedAsAGapNeverSkipped(t *testing.T) {
	h, d := newHubFor(StreamConfig{ReplayBytes: 20})
	p, _ := h.Open(context.Background(), alice, "shell")
	for i := 0; i < 10; i++ {
		d.first().emit("0123456789")
	}
	waitSeq(t, p, 10)
	d.first().drop()
	waitState(t, p, StreamDisconnected)
	att, _ := h.Reattach(context.Background(), alice, p.ID(), 0)
	if att.Gap == 0 || len(att.Frames) == 0 || att.Frames[0].Seq != att.Gap+1 {
		t.Fatalf("gap=%d first=%d frames=%d", att.Gap, att.Frames[0].Seq, len(att.Frames))
	}
	for i, f := range att.Frames {
		if f.Seq != att.Frames[0].Seq+uint64(i) {
			t.Fatalf("frames are not contiguous: %d", f.Seq)
		}
	}
}

func TestSlowProcessesPushBackInsteadOfBufferingWithoutBound(t *testing.T) {
	h, d := newHubFor(StreamConfig{MaxQueuedInput: 1})
	p, _ := h.Open(context.Background(), alice, "shell")
	c := d.first()
	c.block = make(chan struct{})
	go func() { _, _ = p.SendInput(context.Background(), alice, 1, []byte("a")) }()
	eventually(t, "the first write", func() bool { return c.written() == 1 })
	if _, err := p.SendInput(context.Background(), alice, 2, []byte("b")); StreamReason(err) != StreamBackpressure {
		t.Fatalf("%v", err)
	}
	close(c.block)
}

func TestSequenceNumbersMustRiseByOneAndFramesAreBounded(t *testing.T) {
	h, _ := newHubFor(StreamConfig{MaxFrameBytes: 8})
	p, _ := h.Open(context.Background(), alice, "shell")
	if _, err := p.SendInput(context.Background(), alice, 2, []byte("x")); StreamReason(err) != StreamInputOutOfOrder {
		t.Fatalf("%v", err)
	}
	if _, err := p.SendInput(context.Background(), alice, 1, make([]byte, 9)); StreamReason(err) != StreamFrameTooLarge {
		t.Fatalf("%v", err)
	}
	if st, err := p.SendInput(context.Background(), alice, 1, []byte("x")); err != nil || st != InputAcked {
		t.Fatalf("%s %v", st, err)
	}
}

func TestOwnersAreLimitedAndTheLimitIsPerOwner(t *testing.T) {
	h, _ := newHubFor(StreamConfig{MaxPerOwner: 2})
	for i := 0; i < 2; i++ {
		if _, err := h.Open(context.Background(), alice, "s"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := h.Open(context.Background(), alice, "s"); StreamReason(err) != StreamLimit {
		t.Fatalf("%v", err)
	}
	other := Owner{Tenant: "t1", Caller: "bob", Session: "s9"}
	if _, err := h.Open(context.Background(), other, "s"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Open(context.Background(), Owner{Tenant: "t1", Caller: "bad\nid", Session: "s"}, "s"); err == nil {
		t.Fatal("an owner with control characters was accepted")
	}
}

func TestAProcessThatExitsKeepsItsCodeAndRefusesMoreInput(t *testing.T) {
	h, d := newHubFor(StreamConfig{})
	p, _ := h.Open(context.Background(), alice, "shell")
	code := 3
	d.first().out <- Frame{Data: []byte("bye"), Exit: &code}
	waitState(t, p, StreamExited)
	if c, ok := p.ExitCode(alice); !ok || c != 3 {
		t.Fatalf("%d %v", c, ok)
	}
	if _, err := p.SendInput(context.Background(), alice, 1, []byte("x")); StreamReason(err) != StreamClosed {
		t.Fatalf("%v", err)
	}
	d.first().drop()
	att, _ := h.Reattach(context.Background(), alice, p.ID(), 0)
	if att.State != StreamExited || len(att.Frames) != 1 {
		t.Fatalf("%+v", att)
	}
}

func TestHighVolumeOutputKeepsMemoryWithinTheBufferAndOrderIntact(t *testing.T) {
	h, d := newHubFor(StreamConfig{ReplayBytes: 64 << 10})
	p, _ := h.Open(context.Background(), alice, "shell")
	payload := make([]byte, 100)
	const frames = 200000
	go func() {
		for i := 0; i < frames; i++ {
			d.first().out <- Frame{Data: payload}
		}
	}()
	waitSeq(t, p, frames)
	p.mu.Lock()
	kept, bytes, dropped := len(p.frames), p.bytes, p.dropped
	p.mu.Unlock()
	if bytes > 64<<10 || kept > 700 || dropped != uint64(frames-kept) {
		t.Fatalf("kept=%d bytes=%d dropped=%d", kept, bytes, dropped)
	}
}

func TestIdleProcessesAreSweptButActiveOnesAreNot(t *testing.T) {
	clk := &stepClock{t: time.Unix(5000, 0)}
	h, d := newHubFor(StreamConfig{IdleTimeout: time.Minute, Clock: clk})
	idle, _ := h.Open(context.Background(), alice, "a")
	active, _ := h.Open(context.Background(), alice, "b")
	_ = clk.Sleep(context.Background(), 45*time.Second)
	d.opened[1].emit("still here")
	waitSeq(t, active, 1)
	_ = clk.Sleep(context.Background(), 30*time.Second)
	if n := h.Sweep(); n != 1 {
		t.Fatalf("swept %d", n)
	}
	if st, _, _ := idle.State(alice); st != StreamCancelled {
		t.Fatalf("%s", st)
	}
	if st, _, _ := active.State(alice); st != StreamOpen {
		t.Fatalf("%s", st)
	}
}
