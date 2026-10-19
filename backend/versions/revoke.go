package versions

import (
	"encoding/json"
	"errors"
	"os"
	"sort"
)

// Revocations only ever grow: a revoked version stays revoked, whatever pins it.
type Revocations struct {
	Manifests  []string `json:"manifests"`
	Artifacts  []string `json:"artifacts"`
	Operations []string `json:"operations"`
}

func (s *Store) loadRevocations() (Revocations, error) {
	raw, err := os.ReadFile(s.path("revocations.json"))
	if errors.Is(err, os.ErrNotExist) {
		return Revocations{}, nil
	}
	if err != nil {
		return Revocations{}, err
	}
	var r Revocations
	return r, json.Unmarshal(raw, &r)
}

func union(a, b []string) []string {
	set := map[string]struct{}{}
	for _, x := range append(append([]string{}, a...), b...) {
		set[x] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for x := range set {
		out = append(out, x)
	}
	sort.Strings(out)
	return out
}

// Revoke adds to the revocation set durably; entries are `operation@version` or `operation@*` for operations.
func (s *Store) Revoke(add Revocations) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, err := s.loadRevocations()
	if err != nil {
		return err
	}
	cur.Manifests = union(cur.Manifests, add.Manifests)
	cur.Artifacts = union(cur.Artifacts, add.Artifacts)
	cur.Operations = union(cur.Operations, add.Operations)
	raw, _ := json.Marshal(cur)
	return s.writeAtomic(s.path("revocations.json"), raw)
}

func contains(list []string, x string) bool {
	for _, y := range list {
		if y == x {
			return true
		}
	}
	return false
}

func (s *Store) revokedManifest(rep Report) (string, bool) {
	r, err := s.loadRevocations()
	if err != nil {
		return "revocations_unreadable", true
	}
	if contains(r.Manifests, rep.ID) {
		return "manifest", true
	}
	for _, a := range rep.Artifacts {
		if contains(r.Artifacts, a.Digest) {
			return "artifact", true
		}
	}
	return "", false
}

// RevokedContract is true when a contract's document, or its operation at that version, has been revoked.
func (s *Store) RevokedContract(documentDigest, operation, version string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.loadRevocations()
	if err != nil {
		return "revocations_unreadable", true
	}
	switch {
	case contains(r.Artifacts, documentDigest):
		return "artifact_revoked", true
	case contains(r.Operations, operation+"@"+version), contains(r.Operations, operation+"@*"):
		return "operation_revoked", true
	}
	return "", false
}

// Executable says whether a channel's head may run: nothing revoked may, whatever pinned it.
func (s *Store) Executable(channel string) error {
	s.mu.Lock()
	p, ok, err := s.head(channel)
	s.mu.Unlock()
	if err != nil {
		return err
	}
	if !ok {
		return refuse("no_head:%s", channel)
	}
	doc, found := s.Manifest(p.ID)
	if !found {
		return refuse("manifest_missing:%s", p.ID)
	}
	rep, err := s.cfg.Verifier.VerifyPromotion(doc, s.cfg.Now(), s.cfg.Trust, nil)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if reason, revoked := s.revokedManifest(rep); revoked {
		return refuse("revoked:%s", reason)
	}
	return nil
}
