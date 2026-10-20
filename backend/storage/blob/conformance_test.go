package blob

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
)

var ctx = context.Background()

// Conformance is the behaviour every backend must share; a backend that differs is not interchangeable.
func Conformance(t *testing.T, mk func(t *testing.T) Backend) {
	t.Run("round trip, empty and binary", func(t *testing.T) {
		b := mk(t)
		bin := make([]byte, 100000)
		_, _ = rand.Read(bin)
		for key, data := range map[string][]byte{"c/empty": {}, "c/bin": bin, "c/text": []byte("héllo\x00world")} {
			if err := b.Put(ctx, key, data); err != nil {
				t.Fatalf("%s: %v", key, err)
			}
			got, err := b.Get(ctx, key)
			if err != nil || !bytes.Equal(got, data) {
				t.Fatalf("%s: %v", key, err)
			}
			st, err := b.Stat(ctx, key)
			if err != nil || st.Size != int64(len(data)) || st.Key != key {
				t.Fatalf("%s: %+v %v", key, st, err)
			}
		}
	})
	t.Run("a held key is immutable and writing the same bytes is idempotent", func(t *testing.T) {
		b := mk(t)
		if err := b.Put(ctx, "k/one", []byte("v1")); err != nil {
			t.Fatal(err)
		}
		if err := b.Put(ctx, "k/one", []byte("v1")); err != nil {
			t.Fatalf("idempotent put: %v", err)
		}
		if err := b.Put(ctx, "k/one", []byte("v2")); !errors.Is(err, ErrOverwrite) {
			t.Fatalf("overwrite: %v", err)
		}
		if got, _ := b.Get(ctx, "k/one"); string(got) != "v1" {
			t.Fatalf("the first bytes must survive: %q", got)
		}
	})
	t.Run("missing objects and invalid keys", func(t *testing.T) {
		b := mk(t)
		if _, err := b.Get(ctx, "nothing/here"); !errors.Is(err, ErrNotFound) {
			t.Fatal(err)
		}
		if _, err := b.Stat(ctx, "nothing/here"); !errors.Is(err, ErrNotFound) {
			t.Fatal(err)
		}
		if err := b.Delete(ctx, "nothing/here"); !errors.Is(err, ErrNotFound) {
			t.Fatal(err)
		}
		for _, bad := range []string{"", "../x", "A", "a b", "a//b"} {
			if err := b.Put(ctx, bad, []byte("x")); !errors.Is(err, ErrBadKey) {
				t.Fatalf("%q: %v", bad, err)
			}
			if _, err := b.Get(ctx, bad); !errors.Is(err, ErrBadKey) {
				t.Fatalf("%q: %v", bad, err)
			}
		}
	})
	t.Run("delete removes and the key can be reused", func(t *testing.T) {
		b := mk(t)
		_ = b.Put(ctx, "d/x", []byte("1"))
		if err := b.Delete(ctx, "d/x"); err != nil {
			t.Fatal(err)
		}
		if _, err := b.Get(ctx, "d/x"); !errors.Is(err, ErrNotFound) {
			t.Fatal(err)
		}
		if err := b.Put(ctx, "d/x", []byte("2")); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("list is ordered, prefix-bound, resumable and bounded", func(t *testing.T) {
		b := mk(t)
		for i := 0; i < 25; i++ {
			_ = b.Put(ctx, fmt.Sprintf("l/%03d", i), []byte("x"))
		}
		_ = b.Put(ctx, "m/other", []byte("y"))
		var all []string
		after := ""
		for {
			page, err := b.List(ctx, "l/", after, 10)
			if err != nil {
				t.Fatal(err)
			}
			if len(page) > 10 {
				t.Fatalf("page of %d", len(page))
			}
			for _, o := range page {
				all = append(all, o.Key)
				after = o.Key
			}
			if len(page) < 10 {
				break
			}
		}
		if len(all) != 25 {
			t.Fatalf("%d keys", len(all))
		}
		for i := 1; i < len(all); i++ {
			if all[i-1] >= all[i] {
				t.Fatalf("not ordered at %d", i)
			}
		}
		other, _ := b.List(ctx, "m/", "", 0)
		if len(other) != 1 || other[0].Key != "m/other" {
			t.Fatal(other)
		}
	})
	t.Run("concurrent writers of one key agree", func(t *testing.T) {
		b := mk(t)
		var wg sync.WaitGroup
		errs := make(chan error, 16)
		for i := 0; i < 16; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := b.Put(ctx, "race/key", []byte("same-bytes")); err != nil {
					errs <- err
				}
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Fatal(err)
		}
		if got, _ := b.Get(ctx, "race/key"); string(got) != "same-bytes" {
			t.Fatal(string(got))
		}
	})
	t.Run("a large object", func(t *testing.T) {
		b := mk(t)
		big := make([]byte, 6<<20)
		_, _ = rand.Read(big)
		if err := b.Put(ctx, "big/obj", big); err != nil {
			t.Fatal(err)
		}
		got, err := b.Get(ctx, "big/obj")
		if err != nil || !bytes.Equal(got, big) {
			t.Fatal(err)
		}
	})
}

func TestMemConformance(t *testing.T) { Conformance(t, func(*testing.T) Backend { return NewMem() }) }

func TestFSConformance(t *testing.T) {
	Conformance(t, func(t *testing.T) Backend {
		f, err := NewFS(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = f.Close() })
		return f
	})
}

func TestSQLiteConformance(t *testing.T) {
	Conformance(t, func(t *testing.T) Backend {
		s, err := OpenSQLite(filepath.Join(t.TempDir(), "objects.db"), "")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = s.Close() })
		return s
	})
}
