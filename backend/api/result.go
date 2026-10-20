package api

import (
	"sort"
	"strings"

	"github.com/insightlabs38-pixel/vsf/backend/execution"
)

// Surface names one of the interfaces a caller reaches the service through.
type Surface string

const (
	SurfaceREST Surface = "rest"
	SurfaceCLI  Surface = "cli"
	SurfaceMCP  Surface = "mcp"
	SurfaceUI   Surface = "ui"
)

// Principal is a caller whose credential was verified for exactly one surface.
type Principal struct {
	Caller     string
	Surface    Surface
	Credential string
}

// SeenIn is the record version and time the caller last read.
type SeenIn struct {
	Version    string `json:"version"`
	ObservedAt int64  `json:"observed_at"`
}

// ConfirmationIn is a person's confirmation of one exact request.
type ConfirmationIn struct {
	Token     string `json:"token"`
	By        string `json:"by"`
	Digest    string `json:"digest"`
	ExpiresAt int64  `json:"expires_at"`
}

// Invocation is the single request envelope every surface produces.
type Invocation struct {
	Operation      string            `json:"operation"`
	ContractVer    string            `json:"contract_version,omitempty"`
	ContractDigest string            `json:"contract_digest,omitempty"`
	RequestID      string            `json:"request_id,omitempty"`
	SessionID      string            `json:"session_id"`
	Record         string            `json:"record,omitempty"`
	Params         map[string]any    `json:"params"`
	Expect         map[string]string `json:"expect,omitempty"`
	Seen           *SeenIn           `json:"seen,omitempty"`
	Confirmation   *ConfirmationIn   `json:"confirmation,omitempty"`
	Strategy       string            `json:"strategy,omitempty"`
	WaitMs         *int64            `json:"wait_ms,omitempty"`
	DryRun         bool              `json:"dry_run,omitempty"`
}

// Observed is what the source itself showed for this run, bounded and never taken from a request.
type Observed struct {
	Version   string            `json:"version,omitempty"`
	Fields    map[string]string `json:"fields,omitempty"`
	Truncated bool              `json:"truncated,omitempty"`
}

// Result is the one outcome shape every surface returns for an invocation.
type Result struct {
	Invocation      string      `json:"invocation,omitempty"`
	RequestID       string      `json:"request_id,omitempty"`
	Operation       string      `json:"operation"`
	ContractVersion string      `json:"contract_version"`
	ContractDigest  string      `json:"contract_digest"`
	Category        Category    `json:"category"`
	State           string      `json:"state,omitempty"`
	Effect          Effect      `json:"effect"`
	Retry           Retry       `json:"retry"`
	Reason          string      `json:"reason,omitempty"`
	Cancel          string      `json:"cancel,omitempty"`
	Observed        *Observed   `json:"observed,omitempty"`
	Error           *Error      `json:"error,omitempty"`
	Disclosure      *Disclosure `json:"disclosure,omitempty"`
	Progress        []Progress  `json:"progress,omitempty"`
	// Reconciliation is present only on the result of a reconcile call.
	Reconciliation *Reconciliation `json:"reconciliation,omitempty"`
}

// Progress is one step a long run reported; it carries no source content.
type Progress struct {
	Seq      int     `json:"seq"`
	Stage    string  `json:"stage"`
	Fraction float64 `json:"fraction,omitempty"`
}

const (
	maxObservedFields = 256
	maxObservedValue  = MaxTextChars
)

// CategoryValidated marks a dry run: arguments were checked and nothing else happened.
const CategoryValidated Category = "validated"

func observedOf(h *execution.Handle) *Observed {
	o, ok := h.Observed()
	if !ok {
		return nil
	}
	out := &Observed{Version: o.Version}
	keys := make([]string, 0, len(o.Fields))
	for k := range o.Fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) > maxObservedFields {
		keys, out.Truncated = keys[:maxObservedFields], true
	}
	if len(keys) > 0 {
		out.Fields = make(map[string]string, len(keys))
	}
	for _, k := range keys {
		v := o.Fields[k]
		if len(v) > maxObservedValue {
			v, out.Truncated = strings.ToValidUTF8(v[:maxObservedValue], ""), true
		}
		out.Fields[k] = v
	}
	return out
}

func progressOf(h *execution.Handle) []Progress {
	evs := h.Events(0)
	out := make([]Progress, 0, len(evs))
	for _, e := range evs {
		out = append(out, Progress{Seq: e.Seq, Stage: e.Stage, Fraction: e.Fraction})
	}
	return out
}

// stateOutcome maps what the journal recorded to the public category and, for anything not a success, a code.
func stateOutcome(rec execution.Record) (Category, *Error) {
	switch rec.State {
	case execution.Verified:
		return CategorySucceeded, nil
	case execution.Unknown:
		if rec.Reason == execution.ReasonUnverified {
			return CategoryUnverifiedEffect, errWith(CodeUnverifiedEffect, detailReason(rec.Detail), nil)
		}
		return CategoryUnknownCommit, newError(CodeUnknownCommit, "")
	case execution.Failed:
		code := ClassifyReason(rec.Reason)
		return InfoOf(code).Category, errWith(code, reasonText(rec.Reason), nil)
	}
	return CategoryInProgress, nil
}

// reasonText keeps only the reason's class; a parameter name may follow but source content never does.
func reasonText(r string) string {
	if len(r) > 96 {
		return r[:96]
	}
	return r
}

func detailReason(d string) string {
	head, _, _ := strings.Cut(d, ":")
	return reasonText(head)
}

// ResultOf renders a journaled operation; it states no more than the journal and the source's read-back established.
func ResultOf(spec *Spec, h *execution.Handle, withProgress bool) Result {
	rec := h.Record()
	cat, perr := stateOutcome(rec)
	r := Result{
		Invocation: rec.Key, Operation: spec.Name, ContractVersion: spec.ContractVersion, ContractDigest: spec.ContractDigest,
		Category: cat, State: string(rec.State), Effect: EffectNone, Retry: RetryNever,
	}
	switch rec.State {
	case execution.Verified:
		if spec.Effect.Write {
			r.Effect = EffectApplied
		}
		r.Observed = observedOf(h)
		r.Reason = rec.Reason
	case execution.Unknown, execution.Failed:
		r.Error = perr
		r.Effect, r.Retry, r.Reason = perr.Effect, perr.Retry, perr.Reason
		if rec.State == execution.Unknown && rec.Reason == execution.ReasonUnverified {
			r.Observed = observedOf(h)
		}
	default:
		r.Retry = RetrySameRequest
		if spec.Effect.Write {
			r.Effect = EffectUnknown
		}
	}
	if c := h.CancelOutcome(); c != execution.CancelPending {
		r.Cancel = string(c)
	}
	if withProgress {
		r.Progress = progressOf(h)
	}
	return r
}

// RefusedResult is the outcome for a request that never became an operation.
func RefusedResult(spec *Spec, err *Error) Result {
	r := Result{Category: CategoryRefused, State: "refused", Effect: err.Effect, Retry: err.Retry, Reason: err.Reason, Error: err}
	if spec != nil {
		r.Operation, r.ContractVersion, r.ContractDigest = spec.Name, spec.ContractVersion, spec.ContractDigest
	}
	return r
}

// ExitCodeOf is the process exit status of a result, fixed by the taxonomy and shared by every command-line client.
func ExitCodeOf(r Result) int {
	if r.Error != nil {
		return InfoOf(r.Error.Code).Exit
	}
	if r.Category == CategoryInProgress {
		return ExitInProgress
	}
	return ExitOK
}
