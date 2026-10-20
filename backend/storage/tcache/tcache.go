// Package tcache is a tenant-isolated cache for data derived from a tenant's accounts: names are opaque, values are sealed, and eviction never crosses tenants.
package tcache

import (
	"context"
	"encoding/binary"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/insightlabs38-pixel/vsf/backend/storage/blob"
	"github.com/insightlabs38-pixel/vsf/backend/storage/meta"
	"github.com/insightlabs38-pixel/vsf/backend/storage/secret"
)

var (
	ErrTampered = errors.New("a cached value failed its integrity check and was dropped")
	ErrInvalid  = errors.New("invalid cache argument")
	ErrTooBig   = errors.New("the value exceeds the tenant's cache quota")
)

const purpose = "cache"

// Cache keeps at most Quota bytes per tenant; a value older than TTL is a miss.
type Cache struct {
	Blobs blob.Backend
	Keys  *secret.Keyring
	// Quota bounds the stored bytes of each tenant separately, so one tenant's load can never evict another's entries.
	Quota int64
	TTL   time.Duration
	Now   func() int64

	mu    sync.Mutex
	usage map[meta.Tenant]int64
}

func (c *Cache) now() int64 {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now().UnixMilli()
}

func prefix(t meta.Tenant) string { return "cache/" + string(t) + "/" }

func (c *Cache) key(t meta.Tenant, name string) string {
	n := c.Keys.Box(t, purpose).Name(name)
	return prefix(t) + n[:2] + "/" + n
}

func (c *Cache) check(t meta.Tenant, name string) error {
	if !meta.ValidTenant(t) || name == "" || len(name) > 1024 || c.Blobs == nil || c.Keys == nil || c.Quota <= 0 {
		return ErrInvalid
	}
	return nil
}

func (c *Cache) aad(key string) []byte { return []byte("vsf/cache/v1|" + key) }

// Put stores a value under a name for the tenant, replacing any earlier one, then evicts the tenant's oldest entries over quota.
func (c *Cache) Put(ctx context.Context, t meta.Tenant, name string, value []byte) error {
	if err := c.check(t, name); err != nil {
		return err
	}
	k := c.key(t, name)
	expiry := int64(0)
	if c.TTL > 0 {
		expiry = c.now() + c.TTL.Milliseconds()
	}
	plain := make([]byte, 8, 8+len(value))
	binary.BigEndian.PutUint64(plain, uint64(expiry))
	sealed, err := c.Keys.Box(t, purpose).Seal(c.aad(k), append(plain, value...))
	if err != nil {
		return err
	}
	if int64(len(sealed)) > c.Quota {
		return ErrTooBig
	}
	c.mu.Lock()
	_, err = c.used(ctx, t)
	c.mu.Unlock()
	if err != nil {
		return err
	}
	if err := c.drop(ctx, t, k); err != nil {
		return err
	}
	if err := c.Blobs.Put(ctx, k, sealed); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.usage[t] += int64(len(sealed))
	if c.usage[t] <= c.Quota {
		return nil
	}
	return c.evict(ctx, t, k)
}

func (c *Cache) drop(ctx context.Context, t meta.Tenant, k string) error {
	info, err := c.Blobs.Stat(ctx, k)
	if errors.Is(err, blob.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := c.Blobs.Delete(ctx, k); err != nil && !errors.Is(err, blob.ErrNotFound) {
		return err
	}
	c.mu.Lock()
	if u, ok := c.usage[t]; ok {
		c.usage[t] = max(0, u-info.Size)
	}
	c.mu.Unlock()
	return nil
}

// used returns the tenant's stored bytes, measuring them once after a start; the caller holds c.mu.
func (c *Cache) used(ctx context.Context, t meta.Tenant) (int64, error) {
	if c.usage == nil {
		c.usage = map[meta.Tenant]int64{}
	}
	if u, ok := c.usage[t]; ok {
		return u, nil
	}
	var total int64
	for after := ""; ; {
		page, err := c.Blobs.List(ctx, prefix(t), after, 1000)
		if err != nil {
			return 0, err
		}
		for _, o := range page {
			total += o.Size
			after = o.Key
		}
		if len(page) < 1000 {
			break
		}
	}
	c.usage[t] = total
	return total, nil
}

// evict removes the tenant's oldest entries, sparing the one just written, until the tenant fits its quota; the caller holds c.mu.
func (c *Cache) evict(ctx context.Context, t meta.Tenant, spare string) error {
	var all []blob.Info
	for after := ""; ; {
		page, err := c.Blobs.List(ctx, prefix(t), after, 1000)
		if err != nil {
			return err
		}
		all = append(all, page...)
		if len(page) < 1000 {
			break
		}
		after = page[len(page)-1].Key
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].MTime != all[j].MTime {
			return all[i].MTime < all[j].MTime
		}
		return all[i].Key < all[j].Key
	})
	var total int64
	for _, o := range all {
		total += o.Size
	}
	for _, o := range all {
		if total <= c.Quota {
			break
		}
		if o.Key == spare {
			continue
		}
		if err := c.Blobs.Delete(ctx, o.Key); err != nil && !errors.Is(err, blob.ErrNotFound) {
			return err
		}
		total -= o.Size
	}
	c.usage[t] = total
	return nil
}

// Get returns the value for a name in the tenant's own namespace. A value that fails its check is dropped and reported.
func (c *Cache) Get(ctx context.Context, t meta.Tenant, name string) ([]byte, bool, error) {
	if err := c.check(t, name); err != nil {
		return nil, false, err
	}
	k := c.key(t, name)
	sealed, err := c.Blobs.Get(ctx, k)
	if errors.Is(err, blob.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	plain, err := c.Keys.Box(t, purpose).Open(c.aad(k), sealed)
	if err != nil || len(plain) < 8 {
		_ = c.drop(ctx, t, k)
		return nil, false, ErrTampered
	}
	if exp := int64(binary.BigEndian.Uint64(plain)); exp != 0 && c.now() >= exp {
		_ = c.drop(ctx, t, k)
		return nil, false, nil
	}
	return plain[8:], true, nil
}

// Delete removes one entry.
func (c *Cache) Delete(ctx context.Context, t meta.Tenant, name string) error {
	if err := c.check(t, name); err != nil {
		return err
	}
	return c.drop(ctx, t, c.key(t, name))
}

// Purge removes everything the tenant has cached and returns how many entries went.
func (c *Cache) Purge(ctx context.Context, t meta.Tenant) (int, error) {
	if !meta.ValidTenant(t) || c.Blobs == nil {
		return 0, ErrInvalid
	}
	n := 0
	for {
		page, err := c.Blobs.List(ctx, prefix(t), "", 1000)
		if err != nil || len(page) == 0 {
			c.mu.Lock()
			delete(c.usage, t)
			c.mu.Unlock()
			return n, err
		}
		for _, o := range page {
			if err := c.Blobs.Delete(ctx, o.Key); err != nil && !errors.Is(err, blob.ErrNotFound) {
				return n, err
			}
			n++
		}
	}
}

// Rewrap re-encrypts a tenant's entries that were sealed under an older data key and returns how many it changed. Entries that no longer open are dropped, since a cache can always be rebuilt.
func (c *Cache) Rewrap(ctx context.Context, t meta.Tenant) (int, error) {
	if !meta.ValidTenant(t) || c.Blobs == nil || c.Keys == nil {
		return 0, ErrInvalid
	}
	box := c.Keys.Box(t, purpose)
	n := 0
	for after := ""; ; {
		page, err := c.Blobs.List(ctx, prefix(t), after, 500)
		if err != nil {
			return n, err
		}
		for _, o := range page {
			after = o.Key
			sealed, err := c.Blobs.Get(ctx, o.Key)
			if err != nil {
				continue
			}
			if box.Version(sealed) == box.Active() {
				continue
			}
			plain, err := box.Open(c.aad(o.Key), sealed)
			if err != nil {
				_ = c.Blobs.Delete(ctx, o.Key)
				continue
			}
			fresh, err := box.Seal(c.aad(o.Key), plain)
			if err != nil {
				return n, err
			}
			if err := c.Blobs.Replace(ctx, o.Key, fresh); err != nil {
				return n, err
			}
			n++
		}
		if len(page) < 500 {
			return n, nil
		}
	}
}
