package bench

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"math/rand"
)

// rng is a seeded source whose stream does not depend on the Go version.
type rng struct{ s uint64 }

func newRng(seed uint64) *rng { return &rng{s: seed*2862933555777941757 + 3037000493} }

func (r *rng) next() uint64 {
	r.s ^= r.s << 13
	r.s ^= r.s >> 7
	r.s ^= r.s << 17
	return r.s
}

func (r *rng) intn(n int) int { return int(r.next() % uint64(n)) }

func (r *rng) fill(b []byte) {
	for i := 0; i+8 <= len(b); i += 8 {
		binary.LittleEndian.PutUint64(b[i:], r.next())
	}
	for i := len(b) &^ 7; i < len(b); i++ {
		b[i] = byte(r.next())
	}
}

// Object is one stored item: a key derived from its content and the bytes.
type Object struct {
	Key  string
	Data []byte
}

func keyOf(prefix string, data []byte) string {
	sum := sha256.Sum256(data)
	h := hex.EncodeToString(sum[:])
	return prefix + "/" + h[:2] + "/" + h
}

// Tiny returns n small objects of 100 to 2000 bytes; the same seed gives the same corpus.
func Tiny(seed uint64, n int) []Object {
	r := newRng(seed)
	out := make([]Object, n)
	for i := range out {
		b := make([]byte, 100+r.intn(1900))
		r.fill(b)
		binary.LittleEndian.PutUint64(b, uint64(i))
		out[i] = Object{Key: keyOf("pub", b), Data: b}
	}
	return out
}

// Versions returns n large objects each differing from the last in about changePct percent of its bytes, in scattered runs.
func Versions(seed uint64, n, size int, changePct float64) []Object {
	r := newRng(seed)
	cur := make([]byte, size)
	r.fill(cur)
	out := make([]Object, n)
	for i := range out {
		next := append([]byte(nil), cur...)
		runs := 8
		per := int(float64(size)*changePct/100) / runs
		for k := 0; k < runs; k++ {
			at := r.intn(size - per)
			r.fill(next[at : at+per])
		}
		cur = next
		out[i] = Object{Key: keyOf("ver", cur), Data: append([]byte(nil), cur...)}
	}
	return out
}

// Shuffled returns the indices 0..n-1 in a seeded random order.
func Shuffled(seed uint64, n int) []int {
	p := rand.New(rand.NewSource(int64(seed))).Perm(n)
	return p
}
