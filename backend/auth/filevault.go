package auth

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/insightlabs38-pixel/vsf/backend/storage/meta"
	"github.com/insightlabs38-pixel/vsf/backend/storage/secret"
)

const (
	vaultPurpose = "vault"
	maxSecret    = 64 << 10
)

// FileVault keeps each secret in its own private file, sealed under the account's key, with an opaque name. It is the vault where no OS keychain exists.
type FileVault struct {
	Dir  string
	Keys *secret.Keyring
}

func NewFileVault(dir string, keys *secret.Keyring) (*FileVault, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, err
	}
	return &FileVault{Dir: dir, Keys: keys}, nil
}

func (v *FileVault) box(account string) *secret.Box {
	return v.Keys.Box(meta.Tenant(account), vaultPurpose)
}

func (v *FileVault) path(account, name string) string {
	return filepath.Join(v.Dir, v.box(account).Name("dir:"+account), v.box(account).Name(name))
}

func aad(account, name string) []byte { return []byte("vsf/vault/v1|" + account + "|" + name) }

func (v *FileVault) Put(account, name string, value []byte) error {
	if !idRe.MatchString(account) || !nameRe.MatchString(name) || len(value) > maxSecret {
		return ErrInvalid
	}
	sealed, err := v.box(account).Seal(aad(account, name), value)
	if err != nil {
		return err
	}
	p := v.path(account, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".put-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(sealed); err != nil {
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
	return os.Rename(tmp.Name(), p)
}

func (v *FileVault) Get(account, name string) ([]byte, error) {
	if !idRe.MatchString(account) || !nameRe.MatchString(name) {
		return nil, ErrInvalid
	}
	sealed, err := os.ReadFile(v.path(account, name))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	plain, err := v.box(account).Open(aad(account, name), sealed)
	if err != nil {
		return nil, fmt.Errorf("%w: sealed secret does not open", ErrUntrusted)
	}
	return plain, nil
}

func (v *FileVault) Delete(account, name string) error {
	if !idRe.MatchString(account) || !nameRe.MatchString(name) {
		return ErrInvalid
	}
	err := os.Remove(v.path(account, name))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
