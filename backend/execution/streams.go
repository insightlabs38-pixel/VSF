package execution

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Refusal reasons for streams; none of them has touched the source.
const (
	StreamOwnerMismatch   = "stream.owner_mismatch"
	StreamNotFound        = "stream.not_found"
	StreamGap             = "stream.gap"
	StreamBackpressure    = "stream.backpressure"
	StreamClosed          = "stream.closed"
	StreamLimit           = "stream.limit"
	StreamFrameTooLarge   = "stream.frame_too_large"
	StreamSourceSession   = "stream.source_session_changed"
	StreamAlreadyAttach   = "stream.already_attached"
	StreamInputOutOfOrder = "stream.input_out_of_order"
)

// Owner is the exact identity a process is bound to; every call must present all of it.
type Owner struct{ Tenant, Caller, Session string }

func (o Owner) valid() bool {
	return validID(o.Tenant, 128) && validID(o.Caller, 128) && validID(o.Session, 128)
}

// StreamState is the life of one process as the service knows it.
type StreamState string

const (
	StreamOpen         StreamState = "open"
	StreamDisconnected StreamState = "disconnected"
	StreamExited       StreamState = "exited"
	StreamCancelled    StreamState = "cancelled"
	StreamFailed       StreamState = "failed"
	// StreamUnknown: input may or may not have reached the process, and nothing can say which.
	StreamUnknown StreamState = "unknown"
)

func (s StreamState) terminal() bool {
	return s == StreamExited || s == StreamCancelled || s == StreamFailed || s == StreamUnknown
}

// InputState is the fate of one chunk of caller input; input is at-most-once and is never sent again.
type InputState string

const (
	InputQueued InputState = "queued"
	InputSent   InputState = "sent"
	InputAcked  InputState = "acked"
	// InputUnknown: it was handed to the process and the connection went away before an acknowledgement.
	InputUnknown   InputState = "unknown"
	InputDiscarded InputState = "discarded_before_send"
)

// Frame is one ordered piece of output.
type Frame struct {
	Seq  uint64
	Data []byte
	// Exit is set on the last frame of a process that ended on its own.
	Exit *int
}

// Conn is the connection to the source's process, as an adapter provides it.
type Conn interface {
	// Write hands bytes to the process and returns once the process has taken them.
	Write(ctx context.Context, data []byte) error
	// Output delivers frames until the process ends or the connection drops; it is closed afterwards.
	Output() <-chan Frame
	// SourceSession is the identity of the source-side session this connection runs in.
	SourceSession() string
	Close() error
}

// Dialer opens or re-attaches the connection to a process.
type Dialer interface {
	Open(ctx context.Context, spec string) (Conn, error)
	// Attach re-joins an existing process after a disconnect; it must refuse a process it cannot find.
	Attach(ctx context.Context, spec string, id string) (Conn, error)
}

// StreamConfig bounds what one hub keeps.
type StreamConfig struct {
	MaxPerOwner   int
	MaxFrameBytes int
	// ReplayBytes is how much output stays available to a re-attaching caller; older output is reported as a gap.
	ReplayBytes int
	// MaxQueuedInput bounds input waiting for the process.
	MaxQueuedInput int
	IdleTimeout    time.Duration
	Clock          Clock
}

// Hub owns the processes of many callers and lets only the exact owner reach each one.
type Hub struct {
	cfg   StreamConfig
	d     Dialer
	mu    sync.Mutex
	procs map[string]*Process
	next  int
}

func NewHub(d Dialer, cfg StreamConfig) *Hub {
	if cfg.MaxPerOwner <= 0 {
		cfg.MaxPerOwner = 8
	}
	if cfg.MaxFrameBytes <= 0 {
		cfg.MaxFrameBytes = 64 << 10
	}
	if cfg.ReplayBytes <= 0 {
		cfg.ReplayBytes = 1 << 20
	}
	if cfg.MaxQueuedInput <= 0 {
		cfg.MaxQueuedInput = 64
	}
	if cfg.Clock == nil {
		cfg.Clock = realClock{}
	}
	return &Hub{cfg: cfg, d: d, procs: map[string]*Process{}}
}

// Process is one running thing in the source, bound to its owner and to the source session it started in.
type Process struct {
	hub  *Hub
	id   string
	spec string

	mu        sync.Mutex
	owner     Owner
	source    string
	state     StreamState
	conn      Conn
	attached  bool
	frames    []Frame
	bytes     int
	firstSeq  uint64
	lastSeq   uint64
	dropped   uint64
	inputs    map[uint64]InputState
	nextInput uint64
	queued    int
	lastTouch time.Time
	exit      *int
	events    []string
}

// ErrStream carries a stable reason a caller can act on.
type ErrStream struct{ Reason string }

func (e *ErrStream) Error() string { return "stream: " + e.Reason }

func deny(reason string) error { return &ErrStream{Reason: reason} }

// StreamReason reads the reason out of an error from this file.
func StreamReason(err error) string {
	var e *ErrStream
	if errors.As(err, &e) {
		return e.Reason
	}
	return ""
}

func (h *Hub) countOf(o Owner) int {
	n := 0
	for _, p := range h.procs {
		if p.owner == o && !p.stateLocked().terminal() {
			n++
		}
	}
	return n
}

func (p *Process) stateLocked() StreamState {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state
}

// Open starts a process for the owner. The source session the connection reports is remembered for good.
func (h *Hub) Open(ctx context.Context, o Owner, spec string) (*Process, error) {
	if !o.valid() {
		return nil, deny(StreamOwnerMismatch)
	}
	h.mu.Lock()
	if h.countOf(o) >= h.cfg.MaxPerOwner {
		h.mu.Unlock()
		return nil, deny(StreamLimit)
	}
	h.next++
	id := fmt.Sprintf("proc-%d", h.next)
	h.mu.Unlock()
	conn, err := h.d.Open(ctx, spec)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotSent, err)
	}
	p := &Process{hub: h, id: id, spec: spec, owner: o, source: conn.SourceSession(), state: StreamOpen, conn: conn,
		attached: true, inputs: map[uint64]InputState{}, firstSeq: 1, lastTouch: h.cfg.Clock.Now()}
	h.mu.Lock()
	h.procs[id] = p
	h.mu.Unlock()
	go p.pump(conn)
	return p, nil
}

func (h *Hub) find(o Owner, id string) (*Process, error) {
	h.mu.Lock()
	p := h.procs[id]
	h.mu.Unlock()
	// A process that belongs to someone else is indistinguishable from one that does not exist.
	if p == nil || !o.valid() || p.owner != o {
		return nil, deny(StreamNotFound)
	}
	return p, nil
}

func (p *Process) ID() string { return p.id }

// pump copies output into the replay buffer and turns the end of a connection into a state.
func (p *Process) pump(c Conn) {
	for f := range c.Output() {
		p.mu.Lock()
		if p.conn != c {
			p.mu.Unlock()
			return
		}
		p.lastSeq++
		f.Seq = p.lastSeq
		p.frames = append(p.frames, f)
		p.bytes += len(f.Data)
		for p.bytes > p.hub.cfg.ReplayBytes && len(p.frames) > 1 {
			p.bytes -= len(p.frames[0].Data)
			p.firstSeq = p.frames[1].Seq
			p.dropped++
			p.frames = p.frames[1:]
		}
		if f.Exit != nil {
			code := *f.Exit
			p.exit = &code
			p.state = StreamExited
			p.settleInputsLocked(InputAcked)
		}
		p.lastTouch = p.hub.cfg.Clock.Now()
		p.mu.Unlock()
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.conn != c || p.state.terminal() {
		return
	}
	p.attached = false
	p.state = StreamDisconnected
	p.events = append(p.events, "disconnected")
	p.settleInputsLocked(InputUnknown)
}

// settleInputsLocked decides every input still in flight: sent but unanswered becomes the given state, queued is dropped.
func (p *Process) settleInputsLocked(sentAs InputState) {
	for seq, st := range p.inputs {
		switch st {
		case InputSent:
			p.inputs[seq] = sentAs
		case InputQueued:
			p.inputs[seq] = InputDiscarded
		}
	}
	p.queued = 0
}

// SendInput sends one chunk; a repeated sequence number returns the recorded fate and sends nothing.
func (p *Process) SendInput(ctx context.Context, o Owner, seq uint64, data []byte) (InputState, error) {
	if o != p.owner {
		return "", deny(StreamNotFound)
	}
	if len(data) > p.hub.cfg.MaxFrameBytes {
		return "", deny(StreamFrameTooLarge)
	}
	p.mu.Lock()
	if st, seen := p.inputs[seq]; seen {
		p.mu.Unlock()
		return st, nil
	}
	if p.state.terminal() {
		p.mu.Unlock()
		return "", deny(StreamClosed)
	}
	if p.state != StreamOpen || !p.attached {
		p.mu.Unlock()
		return "", deny(StreamClosed)
	}
	if seq != p.nextInput+1 {
		p.mu.Unlock()
		return "", deny(StreamInputOutOfOrder)
	}
	if p.queued >= p.hub.cfg.MaxQueuedInput {
		p.mu.Unlock()
		return "", deny(StreamBackpressure)
	}
	p.nextInput = seq
	p.inputs[seq] = InputSent
	p.queued++
	conn := p.conn
	p.mu.Unlock()
	err := conn.Write(ctx, data)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.queued--
	switch {
	case err == nil:
		if p.inputs[seq] == InputSent {
			p.inputs[seq] = InputAcked
		}
	case errors.Is(err, ErrNotSent):
		p.inputs[seq] = InputDiscarded
	default:
		p.inputs[seq] = InputUnknown
	}
	return p.inputs[seq], nil
}

// Attachment is what a caller gets back when it re-joins: output it missed, and anything that must be reported.
type Attachment struct {
	Frames []Frame
	// Gap is the number of output frames lost because they were older than the replay buffer.
	Gap uint64
	// Unknown lists input sequence numbers whose effect cannot be told; they are never sent again.
	Unknown []uint64
	State   StreamState
}

// Reattach replays output after `after` if still buffered, reports a hole as a gap, and fails closed on another session.
func (h *Hub) Reattach(ctx context.Context, o Owner, id string, after uint64) (*Attachment, error) {
	p, err := h.find(o, id)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	state := p.state
	if state.terminal() {
		att := p.attachmentLocked(after)
		p.mu.Unlock()
		return att, nil
	}
	if p.attached {
		p.mu.Unlock()
		return nil, deny(StreamAlreadyAttach)
	}
	spec := p.spec
	p.mu.Unlock()
	conn, err := h.d.Attach(ctx, spec, id)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotSent, err)
	}
	p.mu.Lock()
	if conn.SourceSession() != p.source {
		p.state = StreamFailed
		p.events = append(p.events, "source_session_changed")
		p.mu.Unlock()
		_ = conn.Close()
		return nil, deny(StreamSourceSession)
	}
	p.conn, p.attached, p.state = conn, true, StreamOpen
	p.events = append(p.events, "reattached")
	att := p.attachmentLocked(after)
	p.mu.Unlock()
	go p.pump(conn)
	return att, nil
}

func (p *Process) attachmentLocked(after uint64) *Attachment {
	att := &Attachment{State: p.state}
	start := after + 1
	if start < p.firstSeq {
		att.Gap = p.firstSeq - start
		start = p.firstSeq
	}
	for _, f := range p.frames {
		if f.Seq >= start {
			att.Frames = append(att.Frames, f)
		}
	}
	for seq, st := range p.inputs {
		if st == InputUnknown {
			att.Unknown = append(att.Unknown, seq)
		}
	}
	sortUint64(att.Unknown)
	return att
}

func sortUint64(a []uint64) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j-1] > a[j]; j-- {
			a[j-1], a[j] = a[j], a[j-1]
		}
	}
}

// Cancel ends the process for its owner; clean if no input was sent, otherwise the earlier input's effect is unknown.
func (p *Process) Cancel(o Owner) (StreamState, error) {
	if o != p.owner {
		return "", deny(StreamNotFound)
	}
	p.mu.Lock()
	if p.state.terminal() {
		st := p.state
		p.mu.Unlock()
		return st, nil
	}
	inFlight := false
	for _, st := range p.inputs {
		if st == InputSent || st == InputUnknown {
			inFlight = true
		}
	}
	conn := p.conn
	p.attached = false
	p.settleInputsLocked(InputUnknown)
	if inFlight {
		p.state = StreamUnknown
	} else {
		p.state = StreamCancelled
	}
	st := p.state
	p.mu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
	return st, nil
}

// State is the owner's view of the process.
func (p *Process) State(o Owner) (StreamState, uint64, error) {
	if o != p.owner {
		return "", 0, deny(StreamNotFound)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state, p.lastSeq, nil
}

// ExitCode is set only when the process ended on its own.
func (p *Process) ExitCode(o Owner) (int, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if o != p.owner || p.exit == nil {
		return 0, false
	}
	return *p.exit, true
}

// Events is the ordered record of connection changes, for evidence.
func (p *Process) Events(o Owner) []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if o != p.owner {
		return nil
	}
	return append([]string(nil), p.events...)
}

// Sweep ends processes nobody has touched for the idle timeout; it returns how many it closed.
func (h *Hub) Sweep() int {
	if h.cfg.IdleTimeout <= 0 {
		return 0
	}
	now := h.cfg.Clock.Now()
	h.mu.Lock()
	var stale []*Process
	for _, p := range h.procs {
		p.mu.Lock()
		if !p.state.terminal() && now.Sub(p.lastTouch) > h.cfg.IdleTimeout {
			stale = append(stale, p)
		}
		p.mu.Unlock()
	}
	h.mu.Unlock()
	for _, p := range stale {
		_, _ = p.Cancel(p.owner)
	}
	return len(stale)
}
