package auth

import (
	"errors"
	"sync"
)

var ErrNotFound = errors.New("auth.not_found")

// Vault holds the secrets of grants. Implementations keep them out of every shared structure; callers never read them directly but through a lease.
type Vault interface {
	Put(account, name string, secret []byte) error
	Get(account, name string) ([]byte, error)
	Delete(account, name string) error
}

// MemVault keeps secrets in memory, for tests and for processes that must not persist them.
type MemVault struct {
	mu sync.Mutex
	m  map[string][]byte
}

func NewMemVault() *MemVault { return &MemVault{m: map[string][]byte{}} }

func key(account, name string) string { return account + "\x00" + name }

func (v *MemVault) Put(account, name string, secret []byte) error {
	if !idRe.MatchString(account) || !nameRe.MatchString(name) {
		return ErrInvalid
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.m[key(account, name)] = append([]byte(nil), secret...)
	return nil
}

func (v *MemVault) Get(account, name string) ([]byte, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	b, ok := v.m[key(account, name)]
	if !ok {
		return nil, ErrNotFound
	}
	return append([]byte(nil), b...), nil
}

func (v *MemVault) Delete(account, name string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	delete(v.m, key(account, name))
	return nil
}
