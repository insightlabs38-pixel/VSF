package blob

import (
	"bytes"
	"context"
	"sort"
	"strings"
	"sync"
)

// Mem is an in-memory backend for tests and as the reference the others are compared with.
type Mem struct {
	mu   sync.Mutex
	objs map[string][]byte
}

func NewMem() *Mem { return &Mem{objs: map[string][]byte{}} }

func (m *Mem) Name() string { return "memory" }

func (m *Mem) Put(_ context.Context, key string, data []byte) error {
	if !ValidKey(key) {
		return ErrBadKey
	}
	if len(data) > MaxObject {
		return ErrTooLarge
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if old, ok := m.objs[key]; ok {
		if bytes.Equal(old, data) {
			return nil
		}
		return ErrOverwrite
	}
	m.objs[key] = append([]byte(nil), data...)
	return nil
}

func (m *Mem) Get(_ context.Context, key string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.objs[key]
	if !ok {
		return nil, ErrNotFound
	}
	return append([]byte(nil), b...), nil
}

func (m *Mem) Stat(_ context.Context, key string) (Info, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.objs[key]
	if !ok {
		return Info{}, ErrNotFound
	}
	return Info{Key: key, Size: int64(len(b))}, nil
}

func (m *Mem) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.objs[key]; !ok {
		return ErrNotFound
	}
	delete(m.objs, key)
	return nil
}

func (m *Mem) List(_ context.Context, prefix, after string, limit int) ([]Info, error) {
	if limit <= 0 || limit > 10000 {
		limit = 10000
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	keys := make([]string, 0, len(m.objs))
	for k := range m.objs {
		if strings.HasPrefix(k, prefix) && k > after {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	if len(keys) > limit {
		keys = keys[:limit]
	}
	out := make([]Info, len(keys))
	for i, k := range keys {
		out[i] = Info{Key: k, Size: int64(len(m.objs[k]))}
	}
	return out, nil
}

// Tamper replaces an object's bytes behind the interface's back, to model bit rot in tests.
func (m *Mem) Tamper(key string, data []byte) {
	m.mu.Lock()
	m.objs[key] = append([]byte(nil), data...)
	m.mu.Unlock()
}

var _ Backend = (*Mem)(nil)
