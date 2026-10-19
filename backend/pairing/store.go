package pairing

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const fileName = "pairings.json"

type record struct {
	ID             string    `json:"id"`
	Peer           string    `json:"peer"`
	CreatedAt      time.Time `json:"created_at"`
	Revoked        bool      `json:"revoked,omitempty"`
	Reason         string    `json:"reason,omitempty"`
	AccessHash     string    `json:"access_hash"`
	AccessExpires  time.Time `json:"access_expires"`
	RefreshHash    string    `json:"refresh_hash"`
	RefreshExpires time.Time `json:"refresh_expires"`
	Used           []string  `json:"used_refresh,omitempty"`
}

type disk struct {
	Version  int       `json:"version"`
	Pairings []*record `json:"pairings"`
}

// store keeps hashes only, never a usable token; with a directory it survives restarts and fails closed when damaged.
type store struct {
	path    string
	records map[string]*record
}

func openStore(dir string) (*store, error) {
	s := &store{records: map[string]*record{}}
	if dir == "" {
		return s, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	s.path = filepath.Join(dir, fileName)
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var d disk
	if err := json.Unmarshal(b, &d); err != nil || d.Version != 1 {
		return nil, fmt.Errorf("pairing: %s is damaged or from another version; remove it to pair again", s.path)
	}
	for _, r := range d.Pairings {
		if r == nil || r.ID == "" || r.Peer == "" {
			return nil, fmt.Errorf("pairing: %s holds an incomplete record", s.path)
		}
		s.records[r.ID] = r
	}
	return s, nil
}

func (s *store) save() error {
	if s.path == "" {
		return nil
	}
	d := disk{Version: 1}
	for _, r := range s.records {
		d.Pairings = append(d.Pairings, r)
	}
	sort.Slice(d.Pairings, func(i, j int) bool { return d.Pairings[i].ID < d.Pairings[j].ID })
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".pairings-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, s.path); err != nil {
		os.Remove(name)
		return err
	}
	if d, err := os.Open(filepath.Dir(s.path)); err == nil {
		_ = d.Sync()
		d.Close()
	}
	return nil
}

// add records a new pairing and retires any earlier active one for the same peer.
func (s *store) add(r *record, _ time.Time) error {
	for _, old := range s.records {
		if old.Peer == r.Peer && !old.Revoked {
			old.Revoked, old.Reason = true, "superseded"
		}
	}
	s.records[r.ID] = r
	return s.save()
}

func (s *store) replace(r *record) error {
	old := s.records[r.ID]
	s.records[r.ID] = r
	if err := s.save(); err != nil {
		s.records[r.ID] = old
		return err
	}
	return nil
}

func (s *store) revoke(id, reason string) error {
	r, ok := s.records[id]
	if !ok {
		return fail("pairing.unknown")
	}
	if r.Revoked {
		return nil
	}
	r.Revoked, r.Reason = true, reason
	return s.save()
}

func (s *store) byAccess(h string) *record {
	for _, r := range s.records {
		if r.AccessHash == h {
			return r
		}
	}
	return nil
}

// byRefresh also reports whether the hash is an earlier, already rotated refresh token.
func (s *store) byRefresh(h string) (*record, bool) {
	for _, r := range s.records {
		if r.RefreshHash == h {
			return r, false
		}
		for _, u := range r.Used {
			if u == h {
				return r, true
			}
		}
	}
	return nil, false
}

func (s *store) list() []Info {
	out := make([]Info, 0, len(s.records))
	for _, r := range s.records {
		out = append(out, Info{ID: r.ID, Peer: r.Peer, CreatedAt: r.CreatedAt, Revoked: r.Revoked, Reason: r.Reason})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}
