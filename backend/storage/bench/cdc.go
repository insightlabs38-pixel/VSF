package bench

import "crypto/sha256"

// gear is the table of a gear hash: one fixed pseudo-random 64-bit value per byte.
var gear = func() [256]uint64 {
	var t [256]uint64
	r := newRng(0x9e3779b97f4a7c15)
	for i := range t {
		t[i] = r.next()
	}
	return t
}()

// Chunks splits data at content-defined boundaries (gear hash, average about avg bytes, bounded to avg/4 .. avg*4).
func Chunks(data []byte, avg int) [][]byte {
	mask := uint64(1)
	for mask < uint64(avg) {
		mask <<= 1
	}
	mask--
	min, max := avg/4, avg*4
	var out [][]byte
	start := 0
	var h uint64
	for i, b := range data {
		h = (h << 1) + gear[b]
		n := i - start + 1
		if (n >= min && h&mask == 0) || n >= max {
			out = append(out, data[start:i+1])
			start, h = i+1, 0
		}
	}
	if start < len(data) {
		out = append(out, data[start:])
	}
	return out
}

// DedupBytes is how many bytes would be stored if every object were split into chunks and equal chunks kept once.
func DedupBytes(objs []Object, avg int) (logical, unique int64) {
	seen := map[[32]byte]bool{}
	for _, o := range objs {
		logical += int64(len(o.Data))
		for _, c := range Chunks(o.Data, avg) {
			h := sha256.Sum256(c)
			if !seen[h] {
				seen[h] = true
				unique += int64(len(c))
			}
		}
	}
	return
}
