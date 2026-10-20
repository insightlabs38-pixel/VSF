package bridge

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// Store keeps paired clients between runs. It holds hashes only, so reading it reveals no usable credential.
type Store interface {
	Load() ([]Client, error)
	Save([]Client) error
}

type memoryStore struct{}

func (memoryStore) Load() ([]Client, error) { return nil, nil }
func (memoryStore) Save([]Client) error     { return nil }

// FileStore writes the registry atomically with owner-only permissions.
type FileStore struct{ Path string }

func (f FileStore) Load() ([]Client, error) {
	raw, err := os.ReadFile(f.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var doc struct {
		Clients []Client `json:"clients"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, errors.New("pairing: the registry is unreadable and was not replaced")
	}
	return doc.Clients, nil
}

func (f FileStore) Save(list []Client) error {
	raw, err := json.MarshalIndent(struct {
		Clients []Client `json:"clients"`
	}{list}, "", " ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(f.Path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".pairing-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), f.Path)
}
