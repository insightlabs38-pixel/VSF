package blob

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

// FS keeps one file per object under a root, written through a temporary file, synced and renamed into place.
type FS struct {
	root string
	lock *os.File
	// CrashAt is for tests: returning true at a named point abandons the write as a dead process would.
	CrashAt func(point string) bool
	// NoSync skips fsync; only benchmarks that name the loss of durability may set it.
	NoSync bool
}

// ErrLocked means another process owns the root.
var ErrLocked = errors.New("another process holds this object store")

// NewFS takes the root exclusively and removes the temporary files a dead writer left behind.
func NewFS(root string) (*FS, error) {
	for _, d := range []string{"objects", "tmp"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o700); err != nil {
			return nil, err
		}
	}
	lock, err := os.OpenFile(filepath.Join(root, "lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = lock.Close()
		return nil, ErrLocked
	}
	f := &FS{root: root, lock: lock}
	left, _ := os.ReadDir(filepath.Join(root, "tmp"))
	for _, e := range left {
		_ = os.Remove(filepath.Join(root, "tmp", e.Name()))
	}
	return f, nil
}

func (f *FS) Close() error {
	_ = syscall.Flock(int(f.lock.Fd()), syscall.LOCK_UN)
	return f.lock.Close()
}

func (f *FS) Name() string { return "filesystem" }

func (f *FS) path(key string) string {
	return filepath.Join(f.root, "objects", filepath.FromSlash(key))
}

func (f *FS) crash(point string) bool { return f.CrashAt != nil && f.CrashAt(point) }

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func (f *FS) Put(ctx context.Context, key string, data []byte) error {
	if !ValidKey(key) {
		return ErrBadKey
	}
	if len(data) > MaxObject {
		return ErrTooLarge
	}
	final := f.path(key)
	if old, err := os.ReadFile(final); err == nil {
		if bytes.Equal(old, data) {
			return nil
		}
		return ErrOverwrite
	}
	return f.install(final, data)
}

// Replace makes key hold data; a reader sees the old bytes or the new, never a mixture, and a crash leaves one of the two.
func (f *FS) Replace(ctx context.Context, key string, data []byte) error {
	if !ValidKey(key) {
		return ErrBadKey
	}
	if len(data) > MaxObject {
		return ErrTooLarge
	}
	return f.install(f.path(key), data)
}

func (f *FS) install(final string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(final), 0o700); err != nil {
		return err
	}
	var rnd [8]byte
	_, _ = rand.Read(rnd[:])
	tmp := filepath.Join(f.root, "tmp", "w-"+hex.EncodeToString(rnd[:]))
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	half := len(data) / 2
	if f.crash("torn") {
		_, _ = file.Write(data[:half])
		_ = file.Close()
		return ErrCrashed
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(tmp)
		return err
	}
	if !f.NoSync {
		if err := file.Sync(); err != nil {
			_ = file.Close()
			_ = os.Remove(tmp)
			return err
		}
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if f.crash("before-rename") {
		return ErrCrashed
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if f.crash("after-rename") {
		return ErrCrashed
	}
	if f.NoSync {
		return nil
	}
	return syncDir(filepath.Dir(final))
}

func (f *FS) Get(ctx context.Context, key string) ([]byte, error) {
	if !ValidKey(key) {
		return nil, ErrBadKey
	}
	b, err := os.ReadFile(f.path(key))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound
	}
	return b, err
}

func (f *FS) Stat(ctx context.Context, key string) (Info, error) {
	if !ValidKey(key) {
		return Info{}, ErrBadKey
	}
	st, err := os.Stat(f.path(key))
	if errors.Is(err, fs.ErrNotExist) {
		return Info{}, ErrNotFound
	}
	if err != nil {
		return Info{}, err
	}
	return Info{Key: key, Size: st.Size(), MTime: st.ModTime().UnixMilli()}, nil
}

func (f *FS) Delete(ctx context.Context, key string) error {
	if !ValidKey(key) {
		return ErrBadKey
	}
	err := os.Remove(f.path(key))
	if errors.Is(err, fs.ErrNotExist) {
		return ErrNotFound
	}
	return err
}

// List walks the keys under a prefix in order. It reads whole directories, so it is meant for sweeps, not for the hot path.
func (f *FS) List(ctx context.Context, prefix, after string, limit int) ([]Info, error) {
	if limit <= 0 || limit > 10000 {
		limit = 10000
	}
	base := filepath.Join(f.root, "objects")
	var out []Info
	var walk func(dir string) error
	walk = func(dir string) error {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, e := range entries {
			if len(out) >= limit {
				return nil
			}
			p := filepath.Join(dir, e.Name())
			rel, _ := filepath.Rel(base, p)
			key := filepath.ToSlash(rel)
			if e.IsDir() {
				if !strings.HasPrefix(prefix, key+"/") && !strings.HasPrefix(key+"/", prefix) && prefix != "" {
					continue
				}
				if err := walk(p); err != nil {
					return err
				}
				continue
			}
			if !strings.HasPrefix(key, prefix) || key <= after {
				continue
			}
			st, err := e.Info()
			if err != nil {
				return err
			}
			out = append(out, Info{Key: key, Size: st.Size(), MTime: st.ModTime().UnixMilli()})
		}
		return nil
	}
	return out, walk(base)
}

var _ Backend = (*FS)(nil)
