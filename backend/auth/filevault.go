package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/insightlabs38-pixel/vsf/backend/storage/meta"
	"github.com/insightlabs38-pixel/vsf/backend/storage/secret"
)

const (
	vaultPurpose = "vault"
	maxSecret    = 64 << 10
)

// tmpFile is the part of a file a replacement uses; tests substitute failing ones.
type tmpFile interface {
	Write([]byte) (int, error)
	Sync() error
	Chmod(os.FileMode) error
	Close() error
	Name() string
}

type fsOps struct {
	createTemp func(dir, pattern string) (tmpFile, error)
	rename     func(oldp, newp string) error
	remove     func(p string) error
	syncDir    func(dir string) error
}

func osOps() fsOps {
	return fsOps{
		createTemp: func(dir, pattern string) (tmpFile, error) { return os.CreateTemp(dir, pattern) },
		rename:     os.Rename,
		remove:     os.Remove,
		syncDir: func(dir string) error {
			d, err := os.Open(dir)
			if err != nil {
				return err
			}
			defer d.Close()
			return d.Sync()
		},
	}
}

// FileVault keeps each secret in its own private file, sealed under the account's key, with an opaque name. It is the vault where no OS keychain exists. A Put writes a private temporary file, syncs it, renames it over the old one and syncs the directory, so a crash or an error at any step leaves either the old secret or the new one, never a mix; temporary files left by a crash are removed when the vault is opened.
type FileVault struct {
	Dir  string
	Keys *secret.Keyring
	fs   fsOps
}

func NewFileVault(dir string, keys *secret.Keyring) (*FileVault, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, err
	}
	v := &FileVault{Dir: dir, Keys: keys, fs: osOps()}
	_, _ = v.Recover()
	keys.GuardRetire(v.sealedUnder)
	return v, nil
}

// Recover removes temporary files an interrupted Put left behind and returns how many.
func (v *FileVault) Recover() (int, error) {
	n := 0
	dirs, err := os.ReadDir(v.Dir)
	if err != nil {
		return 0, err
	}
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(v.Dir, d.Name()))
		if err != nil {
			continue
		}
		for _, f := range files {
			if strings.HasPrefix(f.Name(), ".put-") && v.fs.remove(filepath.Join(v.Dir, d.Name(), f.Name())) == nil {
				n++
			}
		}
	}
	return n, nil
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
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("vault: %w", scrub(err))
	}
	tmp, err := v.fs.createTemp(dir, ".put-*")
	if err != nil {
		return fmt.Errorf("vault: %w", scrub(err))
	}
	committed := false
	defer func() {
		if !committed {
			_ = v.fs.remove(tmp.Name())
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("vault: %w", scrub(err))
	}
	if n, err := tmp.Write(sealed); err != nil || n != len(sealed) {
		tmp.Close()
		return fmt.Errorf("vault: write failed: %w", scrub(err))
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("vault: %w", scrub(err))
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("vault: %w", scrub(err))
	}
	if err := v.fs.rename(tmp.Name(), p); err != nil {
		return fmt.Errorf("vault: %w", scrub(err))
	}
	committed = true
	if err := v.fs.syncDir(dir); err != nil {
		return fmt.Errorf("vault: replaced but not synced: %w", scrub(err))
	}
	return nil
}

// scrub keeps the operation and the error class of a file error and drops its path, which names an opaque file but is still not for logs.
func scrub(err error) error {
	if err == nil {
		return errors.New("short write")
	}
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	var le *os.LinkError
	if errors.As(err, &le) {
		return le.Err
	}
	return err
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
	p := v.path(account, name)
	err := v.fs.remove(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("vault: delete: %w", scrub(err))
	}
	return v.fs.syncDir(filepath.Dir(p))
}

// sealedUnder counts the vault files sealed under a key version, so a key still protecting credentials is not retired.
func (v *FileVault) sealedUnder(_ context.Context, version byte) (int, error) {
	n := 0
	err := filepath.WalkDir(v.Dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasPrefix(d.Name(), ".put-") {
			return err
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		var b [1]byte
		if _, err := io.ReadFull(f, b[:]); err != nil {
			return err
		}
		if b[0] == version {
			n++
		}
		return nil
	})
	return n, err
}
