package execution

import (
	"context"
	"sync"
	"time"
)

type progressKey struct{}

type ProgressEvent struct {
	Seq      int
	At       time.Time
	Stage    string
	Fraction float64
	Message  string
}

// Progress lets an executor report how far a long action has got.
func Progress(ctx context.Context, fraction float64, message string) {
	if f, ok := ctx.Value(progressKey{}).(func(string, float64, string)); ok {
		f("executor", fraction, message)
	}
}

type CancelStatus string

const (
	CancelPending       CancelStatus = ""
	CancelBeforeEffect  CancelStatus = "cancelled_before_effect"
	CancelEffectUnknown CancelStatus = "effect_unknown"
	CancelTooLate       CancelStatus = "too_late"
	CancelFinished      CancelStatus = "already_finished"
)

type CancelAck struct {
	Requested bool
	Status    CancelStatus
}

// Handle is the queryable face of one operation; it never claims more than the journal records.
type Handle struct {
	rt     *Runtime
	key    string
	done   chan struct{}
	mu     sync.Mutex
	events []ProgressEvent
	cancel context.CancelFunc
	asked  bool
	stage  string
	status CancelStatus
	err    error
	obs    *Observation
}

func newHandle(rt *Runtime, key string) *Handle {
	return &Handle{rt: rt, key: key, done: make(chan struct{}), stage: "pre"}
}

func (h *Handle) Key() string { return h.key }

// Observation is what the source itself showed for this run; it is held in memory only and never journaled.
type Observation struct {
	Version string
	Fields  map[string]string
}

// Observed returns the values the source showed while this process ran the operation.
func (h *Handle) Observed() (Observation, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.obs == nil {
		return Observation{}, false
	}
	fields := make(map[string]string, len(h.obs.Fields))
	for k, v := range h.obs.Fields {
		fields[k] = v
	}
	return Observation{Version: h.obs.Version, Fields: fields}, true
}

func (h *Handle) setObserved(version string, fields map[string]string) {
	kept := make(map[string]string, len(fields))
	for k, v := range fields {
		kept[k] = v
	}
	h.mu.Lock()
	h.obs = &Observation{Version: version, Fields: kept}
	h.mu.Unlock()
}

func (h *Handle) Record() Record {
	r, _ := h.rt.j.Get(h.key)
	return r
}

func (h *Handle) State() State { return h.Record().State }

func (h *Handle) Done() <-chan struct{} { return h.done }

func (h *Handle) Wait(ctx context.Context) (Record, error) {
	select {
	case <-h.done:
	case <-ctx.Done():
		return h.Record(), ctx.Err()
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.Record(), h.err
}

func (h *Handle) progress(stage string, fraction float64, msg string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events = append(h.events, ProgressEvent{Seq: len(h.events) + 1, At: h.rt.clock.Now(), Stage: stage, Fraction: fraction, Message: msg})
}

// Events returns the progress events after seq, oldest first.
func (h *Handle) Events(since int) []ProgressEvent {
	h.mu.Lock()
	defer h.mu.Unlock()
	if since >= len(h.events) {
		return nil
	}
	return append([]ProgressEvent(nil), h.events[since:]...)
}

func (h *Handle) setStage(s string) {
	h.mu.Lock()
	h.stage = s
	h.mu.Unlock()
}

func (h *Handle) cancelRequested() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.asked
}

func (h *Handle) CancelOutcome() CancelStatus {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.status
}

// Cancel asks the run to stop; it says nothing about the source until the run has ended.
func (h *Handle) Cancel() CancelAck {
	h.mu.Lock()
	defer h.mu.Unlock()
	select {
	case <-h.done:
		return CancelAck{Status: CancelFinished}
	default:
	}
	h.asked = true
	if h.cancel != nil {
		h.cancel()
	}
	return CancelAck{Requested: true}
}

func (h *Handle) finish(err error) {
	h.mu.Lock()
	if err != nil && h.err == nil {
		h.err = err
	}
	if h.asked && h.status == CancelPending {
		h.status = CancelTooLate
	}
	h.mu.Unlock()
	select {
	case <-h.done:
	default:
		close(h.done)
	}
}

func (h *Handle) setCancelStatus(s CancelStatus) {
	h.mu.Lock()
	if h.asked {
		h.status = s
	}
	h.mu.Unlock()
}
