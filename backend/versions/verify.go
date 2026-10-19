package versions

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
)

// Accepted is the newest promotion a store has taken from one signing key on one channel.
type Accepted struct {
	Sequence int64  `json:"sequence"`
	Digest   string `json:"digest"`
}

type ArtifactRef struct {
	Role   string `json:"role"`
	Digest string `json:"digest"`
	Bytes  int64  `json:"bytes"`
}

// Report is Rust's verdict on a manifest document; the store trusts nothing about a manifest beyond it.
type Report struct {
	Verified     bool          `json:"verified"`
	Valid        bool          `json:"valid"`
	Promoted     bool          `json:"promoted"`
	Reason       string        `json:"reason"`
	ID           string        `json:"id"`
	Channel      string        `json:"channel"`
	Lineage      []string      `json:"lineage"`
	Previous     string        `json:"previous"`
	Artifacts    []ArtifactRef `json:"artifacts"`
	KeyID        string        `json:"key_id"`
	Purpose      string        `json:"purpose"`
	Sequence     int64         `json:"sequence"`
	NotAfter     int64         `json:"not_after"`
	SignedDigest string        `json:"signed_digest"`
}

// Verifier decides whether a manifest document may be trusted at a moment, given the trust set and the last accepted sequence.
type Verifier interface {
	VerifyPromotion(doc []byte, nowMs int64, trust []byte, last *Accepted) (Report, error)
}

// Raw is the framed call surface of the native worker.
type Raw interface {
	Raw(op byte, payload []byte) ([]byte, error)
}

type workerVerifier struct{ w Raw }

// NewWorkerVerifier asks the Rust worker (operation P) for every verdict.
func NewWorkerVerifier(w Raw) Verifier { return workerVerifier{w} }

func (v workerVerifier) VerifyPromotion(doc []byte, nowMs int64, trust []byte, last *Accepted) (Report, error) {
	if len(trust) == 0 {
		return Report{}, errors.New("a trust set is required")
	}
	ctx := map[string]any{"now_ms": nowMs, "trust": json.RawMessage(trust), "last": last}
	ctxJSON, err := json.Marshal(ctx)
	if err != nil {
		return Report{}, err
	}
	payload := make([]byte, 4, 4+len(ctxJSON)+len(doc))
	binary.LittleEndian.PutUint32(payload, uint32(len(ctxJSON)))
	payload = append(append(payload, ctxJSON...), doc...)
	raw, err := v.w.Raw('P', payload)
	if err != nil {
		return Report{}, fmt.Errorf("promotion verification unavailable: %w", err)
	}
	var r Report
	if err := json.Unmarshal(raw, &r); err != nil {
		return Report{}, errors.New("promotion verification unavailable: malformed report")
	}
	return r, nil
}
