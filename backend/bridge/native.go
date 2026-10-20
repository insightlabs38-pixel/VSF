package bridge

import (
	"encoding/json"
	"errors"
)

// RawWorker is the native validator's framed interface, satisfied by execution.Worker.
type RawWorker interface {
	Raw(op byte, payload []byte) ([]byte, error)
}

// NativeValidator asks the Rust worker (operation V) whether a document is valid; Go enforces none of those rules.
type NativeValidator struct{ Worker RawWorker }

func (n NativeValidator) Validate(doc []byte) (Validation, error) {
	if n.Worker == nil {
		return Validation{}, errors.New("session.validator_unavailable")
	}
	raw, err := n.Worker.Raw('V', doc)
	if err != nil {
		return Validation{}, err
	}
	var rep struct {
		Decode struct {
			Digest string `json:"digest"`
			Family string `json:"family"`
			OK     bool   `json:"ok"`
		} `json:"decode"`
		Valid      bool `json:"valid"`
		Violations []struct {
			Code string `json:"code"`
		} `json:"violations"`
	}
	if err := json.Unmarshal(raw, &rep); err != nil {
		return Validation{}, err
	}
	v := Validation{Valid: rep.Valid && rep.Decode.OK, Family: rep.Decode.Family, Digest: rep.Decode.Digest}
	for _, x := range rep.Violations {
		v.Violations = append(v.Violations, x.Code)
	}
	return v, nil
}
