// Package blob is the replaceable object backend: opaque keys, immutable bytes, atomic writes.
package blob

import (
	"context"
	"errors"
	"regexp"
	"strings"
)

var (
	ErrNotFound  = errors.New("object not found")
	ErrBadKey    = errors.New("invalid object key")
	ErrCrashed   = errors.New("simulated crash")
	ErrOverwrite = errors.New("an object with this key already holds different bytes")
	ErrTooLarge  = errors.New("object too large")
	keyRe        = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,63}(/[a-z0-9][a-z0-9_.-]{0,127}){0,7}$`)
)

// MaxObject bounds one object; larger artifacts are split by the caller.
const MaxObject = 256 << 20

// ValidKey accepts lower-case path segments and nothing that could climb out of a directory.
func ValidKey(k string) bool {
	return len(k) <= 512 && keyRe.MatchString(k) && !strings.Contains(k, "..")
}

// Info is what a backend knows about an object without reading it.
type Info struct {
	Key  string
	Size int64
	// MTime is when the backend stored the object, in Unix milliseconds; a sweep never removes what is younger than its grace.
	MTime int64
}

// Backend stores immutable objects. Put is idempotent for identical bytes and refuses different bytes under a held key.
type Backend interface {
	Name() string
	Put(ctx context.Context, key string, data []byte) error
	// Replace atomically makes a key hold new bytes: readers see the old or the new, never a mixture, and a crash leaves one of them.
	Replace(ctx context.Context, key string, data []byte) error
	Get(ctx context.Context, key string) ([]byte, error)
	Stat(ctx context.Context, key string) (Info, error)
	Delete(ctx context.Context, key string) error
	// List returns keys under prefix in order, after the given key, at most limit of them.
	List(ctx context.Context, prefix, after string, limit int) ([]Info, error)
}
