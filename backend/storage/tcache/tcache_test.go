package tcache

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/insightlabs38-pixel/vsf/backend/storage/blob"
	"github.com/insightlabs38-pixel/vsf/backend/storage/secret"
)

var ctx = context.Background()

func newCache(quota int64) (*Cache, *blob.Mem, *atomic.Int64) {
	var clock atomic.Int64
	clock.Store(1_000)
	mem := blob.NewMem()
	mem.Now = func() int64 { return clock.Add(1) }
	return &Cache{Blobs: mem, Keys: secret.Generate(), Quota: quota, TTL: time.Hour, Now: clock.Load}, mem, &clock
}

func TestAValueRoundTripsAndIsStoredSealedUnderAnOpaqueName(t *testing.T) {
	c, mem, _ := newCache(1 << 20)
	if err := c.Put(ctx, "acme", "inbox/summary", []byte("canary-cached-balance-1200")); err != nil {
		t.Fatal(err)
	}
	got, ok, err := c.Get(ctx, "acme", "inbox/summary")
	if err != nil || !ok || string(got) != "canary-cached-balance-1200" {
		t.Fatalf("%q %v %v", got, ok, err)
	}
	objs, _ := mem.List(ctx, "", "", 0)
	if len(objs) != 1 {
		t.Fatalf("%d", len(objs))
	}
	raw, _ := mem.Get(ctx, objs[0].Key)
	if bytes.Contains(raw, []byte("canary-cached")) || bytes.Contains([]byte(objs[0].Key), []byte("inbox")) {
		t.Fatalf("neither the value nor its name may be readable from the store: %s", objs[0].Key)
	}
	if _, ok, err := c.Get(ctx, "acme", "missing"); ok || err != nil {
		t.Fatalf("%v %v", ok, err)
	}
	if err := c.Put(ctx, "acme", "inbox/summary", []byte("newer")); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := c.Get(ctx, "acme", "inbox/summary"); string(got) != "newer" {
		t.Fatalf("a put replaces the earlier value: %q", got)
	}
}

func TestOneTenantCannotReadProbeOrCollideWithAnother(t *testing.T) {
	c, mem, _ := newCache(1 << 20)
	if err := c.Put(ctx, "acme", "token-cache", []byte("acme-only")); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := c.Get(ctx, "globex", "token-cache"); ok || err != nil {
		t.Fatalf("a tenant guessing the exact name must see a plain miss: %v %v", ok, err)
	}
	if err := c.Put(ctx, "globex", "token-cache", []byte("acme-only")); err != nil {
		t.Fatal(err)
	}
	objs, _ := mem.List(ctx, "", "", 0)
	if len(objs) != 2 {
		t.Fatalf("each tenant has its own entry: %d", len(objs))
	}
	a, _ := mem.Get(ctx, objs[0].Key)
	b, _ := mem.Get(ctx, objs[1].Key)
	if bytes.Equal(a, b) || objs[0].Key[len("cache/acme/"):] == objs[1].Key[len("cache/globex/"):] {
		t.Fatal("equal names and values must look unrelated across tenants")
	}
	// An operator copies acme's sealed entry into globex's namespace under globex's own name.
	key := c.key("globex", "token-cache")
	if err := mem.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if err := mem.Put(ctx, key, func() []byte {
		for _, o := range objs {
			if len(o.Key) > len("cache/acme/") && o.Key[:len("cache/acme/")] == "cache/acme/" {
				v, _ := mem.Get(ctx, o.Key)
				return v
			}
		}
		return nil
	}()); err != nil {
		t.Fatal(err)
	}
	if v, ok, err := c.Get(ctx, "globex", "token-cache"); ok || !errors.Is(err, ErrTampered) || v != nil {
		t.Fatalf("a transplanted entry must not decrypt: %q %v %v", v, ok, err)
	}
	if _, err := mem.Stat(ctx, key); !errors.Is(err, blob.ErrNotFound) {
		t.Fatal("and it is dropped")
	}
}

func TestEvictionStaysInsideTheTenantThatCausedIt(t *testing.T) {
	c, mem, _ := newCache(600)
	if err := c.Put(ctx, "quiet", "mine", bytes.Repeat([]byte("q"), 200)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 30; i++ {
		if err := c.Put(ctx, "noisy", fmt.Sprintf("k%d", i), bytes.Repeat([]byte("n"), 200)); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok, err := c.Get(ctx, "quiet", "mine"); !ok || err != nil {
		t.Fatalf("a noisy neighbour must not evict another tenant's entry: %v %v", ok, err)
	}
	objs, _ := mem.List(ctx, "cache/noisy/", "", 0)
	var total int64
	for _, o := range objs {
		total += o.Size
	}
	if total > 600 || len(objs) == 0 {
		t.Fatalf("the noisy tenant is held to its own quota: %d bytes in %d entries", total, len(objs))
	}
	if _, ok, _ := c.Get(ctx, "noisy", "k29"); !ok {
		t.Fatal("the newest entry survives eviction")
	}
	if _, ok, _ := c.Get(ctx, "noisy", "k0"); ok {
		t.Fatal("the oldest entry goes first")
	}
	if err := c.Put(ctx, "noisy", "huge", make([]byte, 5000)); !errors.Is(err, ErrTooBig) {
		t.Fatalf("%v", err)
	}
}

func TestEntriesExpireAndTamperedEntriesAreDropped(t *testing.T) {
	c, mem, clock := newCache(1 << 20)
	if err := c.Put(ctx, "acme", "short-lived", []byte("v")); err != nil {
		t.Fatal(err)
	}
	clock.Add(int64(2 * time.Hour / time.Millisecond))
	if _, ok, err := c.Get(ctx, "acme", "short-lived"); ok || err != nil {
		t.Fatalf("an expired entry is a miss: %v %v", ok, err)
	}
	if objs, _ := mem.List(ctx, "", "", 0); len(objs) != 0 {
		t.Fatal("and is removed")
	}
	if err := c.Put(ctx, "acme", "k", []byte("value")); err != nil {
		t.Fatal(err)
	}
	k := c.key("acme", "k")
	raw, _ := mem.Get(ctx, k)
	raw[len(raw)-1] ^= 1
	_ = mem.Delete(ctx, k)
	_ = mem.Put(ctx, k, raw)
	if _, ok, err := c.Get(ctx, "acme", "k"); ok || !errors.Is(err, ErrTampered) {
		t.Fatalf("%v %v", ok, err)
	}
}

func TestPurgeRemovesOnlyThatTenantsEntriesAndRestartKeepsAccounting(t *testing.T) {
	c, mem, _ := newCache(1 << 20)
	for i := 0; i < 5; i++ {
		_ = c.Put(ctx, "acme", fmt.Sprintf("a%d", i), []byte("x"))
		_ = c.Put(ctx, "globex", fmt.Sprintf("g%d", i), []byte("x"))
	}
	n, err := c.Purge(ctx, "acme")
	if err != nil || n != 5 {
		t.Fatalf("%d %v", n, err)
	}
	if objs, _ := mem.List(ctx, "cache/globex/", "", 0); len(objs) != 5 {
		t.Fatal("another tenant's entries stay")
	}
	small := &Cache{Blobs: mem, Keys: c.Keys, Quota: 150, TTL: time.Hour, Now: c.Now}
	if err := small.Put(ctx, "globex", "late", bytes.Repeat([]byte("z"), 80)); err != nil {
		t.Fatal(err)
	}
	objs, _ := mem.List(ctx, "cache/globex/", "", 0)
	var total int64
	for _, o := range objs {
		total += o.Size
	}
	if total > 150 {
		t.Fatalf("a cache started over existing entries still enforces its quota: %d", total)
	}
	if err := c.Put(ctx, "Bad Tenant", "x", nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("%v", err)
	}
	if err := c.Put(ctx, "acme", "", nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("%v", err)
	}
}

func TestCacheEntriesRewrapUnderTheNewestKey(t *testing.T) {
	keys := secret.Generate()
	mem := blob.NewMem()
	c := &Cache{Blobs: mem, Keys: keys, Quota: 1 << 20}
	for i := 0; i < 3; i++ {
		if err := c.Put(ctx, "acme", fmt.Sprintf("k%d", i), []byte(fmt.Sprintf("value-%d", i))); err != nil {
			t.Fatal(err)
		}
	}
	_, _ = keys.Rotate()
	n, err := c.Rewrap(ctx, "acme")
	if err != nil || n != 3 {
		t.Fatalf("%d %v", n, err)
	}
	objs, _ := mem.List(ctx, "cache/acme/", "", 0)
	for _, o := range objs {
		b, _ := mem.Get(ctx, o.Key)
		if b[0] != 2 {
			t.Fatalf("%d", b[0])
		}
	}
	if got, ok, err := c.Get(ctx, "acme", "k1"); err != nil || !ok || string(got) != "value-1" {
		t.Fatalf("%q %v %v", got, ok, err)
	}
	if n, _ := c.Rewrap(ctx, "acme"); n != 0 {
		t.Fatal("nothing left to rewrap")
	}
}
