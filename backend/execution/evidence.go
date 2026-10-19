package execution

import "context"

// EvidenceLevel says how much an observation proves about one particular operation, not about the source's state.
type EvidenceLevel string

const (
	// ProvenCommitted rests on a source receipt keyed to this operation.
	ProvenCommitted EvidenceLevel = "proven_committed"
	// ProvenNotCommitted rests on a fenced receipt: the source will never apply this request.
	ProvenNotCommitted EvidenceLevel = "proven_not_committed"
	// StateMatchesUnproven means the intended values are visible but another actor could have written them.
	StateMatchesUnproven EvidenceLevel = "state_matches_unproven"
	// Inconclusive means the source showed nothing that decides the question.
	Inconclusive EvidenceLevel = "inconclusive"
)

type Evidence struct {
	Level   EvidenceLevel `json:"level"`
	Basis   string        `json:"basis"`
	Version string        `json:"version,omitempty"`
	Detail  string        `json:"detail,omitempty"`
}

// Probe identifies one earlier write to a source that can answer for it.
type Probe struct {
	Key        string
	DedupToken string
	Target     Target
	PreVersion string
	Expect     map[string]string
}

// Reconciler is implemented by executors whose source can vouch for a specific request.
type Reconciler interface {
	Reconcile(ctx context.Context, p Probe) (Evidence, error)
}

// admit keeps an executor's claim only when its declared capabilities could support it.
func admit(c Capabilities, ev Evidence) Evidence {
	switch {
	case ev.Level == ProvenCommitted && c.ReceiptLookup && c.DedupTokens:
		return ev
	case ev.Level == ProvenNotCommitted && c.ReceiptLookup && c.DedupTokens && c.Fencing:
		return ev
	case ev.Level == StateMatchesUnproven || ev.Level == Inconclusive:
		return ev
	}
	return Evidence{Level: Inconclusive, Basis: ev.Basis, Detail: "claim_exceeds_declared_capability:" + string(ev.Level)}
}
