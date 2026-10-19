package execution

import (
	"context"
	"errors"
	"time"
)

type State string

const (
	Prepared State = "prepared"
	Admitted State = "admitted"
	Running  State = "running"
	Verified State = "verified"
	Failed   State = "failed"
	Unknown  State = "unknown"
)

const (
	ReasonUnknownCommit = "UNKNOWN_COMMIT"
	ReasonUnverified    = "UNVERIFIED_EFFECT"
)

func (s State) Terminal() bool { return s == Verified || s == Failed }

var transitions = map[State][]State{
	Prepared: {Admitted, Failed},
	Admitted: {Running, Failed},
	Running:  {Verified, Failed, Unknown},
	Unknown:  {Verified, Failed},
}

func canMove(from, to State) bool {
	for _, t := range transitions[from] {
		if t == to {
			return true
		}
	}
	return false
}

// Outcome is the result taxonomy every executor reports, whatever it drives.
type Outcome string

const (
	OutcomeCommitted   Outcome = "committed"
	OutcomeRejected    Outcome = "rejected"
	OutcomeConflict    Outcome = "conflict"
	OutcomeThrottled   Outcome = "throttled"
	OutcomeUnavailable Outcome = "unavailable"
	OutcomeUnknown     Outcome = "unknown"
)

// NoEffect is true only for outcomes the source itself proved left it unchanged.
func (o Outcome) NoEffect() bool {
	switch o {
	case OutcomeRejected, OutcomeConflict, OutcomeThrottled, OutcomeUnavailable:
		return true
	}
	return false
}

type Result struct {
	Outcome    Outcome
	Detail     string
	Echo       map[string]string
	RetryAfter time.Duration
}

type Snapshot struct {
	Version              string
	Fields               map[string]string
	ConfirmationRequired bool
	ObservedAt           time.Time
}

type Target struct {
	OperationID string
	Record      string
	Params      map[string]any
}

type Call struct {
	Key        string
	Target     Target
	DedupToken string
	IfMatch    string
	Confirmed  bool
}

type Capabilities struct {
	DedupTokens bool
	ReadBack    bool
}

type Executor interface {
	Kind() string
	Capabilities() Capabilities
	Read(ctx context.Context, t Target) (Snapshot, error)
	Apply(ctx context.Context, c Call) (Result, error)
}

// ErrNotSent marks an executor failure that happened before anything reached the source.
var ErrNotSent = errors.New("request was not sent")

type Pin struct {
	Version string
	Digest  string
}

type Seen struct {
	Version    string
	ObservedAt time.Time
}

type Request struct {
	RequestID    string
	Caller       string
	SessionID    string
	Contract     []byte
	Pin          Pin
	Params       map[string]any
	Record       string
	Expect       map[string]string
	Seen         *Seen
	Confirmation *Confirmation
	Strategy     string
}

// Refusal is an admission or precondition failure; no source side effect has happened.
type Refusal struct{ Reason string }

func (r *Refusal) Error() string { return "refused: " + r.Reason }

func refuse(reason string) error { return &Refusal{Reason: reason} }

type Clock interface {
	Now() time.Time
	Sleep(ctx context.Context, d time.Duration) error
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func (realClock) Sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
