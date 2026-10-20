package api

import (
	"sync"
	"time"

	"github.com/insightlabs38-pixel/vsf/backend/api/jsonschema"
)

// CallLimiter is a per-caller token bucket; it bounds what one caller can ask of the service.
type CallLimiter struct {
	mu      sync.Mutex
	now     func() time.Time
	burst   float64
	perSec  float64
	buckets map[string]*bucket
	maxKeys int
}

type bucket struct {
	tokens float64
	at     time.Time
}

func NewCallLimiter(burst int, perSecond float64, now func() time.Time) *CallLimiter {
	if now == nil {
		now = time.Now
	}
	return &CallLimiter{now: now, burst: float64(burst), perSec: perSecond, buckets: map[string]*bucket{}, maxKeys: 10000}
}

// Allow spends one token, or says how long until one is available.
func (l *CallLimiter) Allow(caller string) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	t := l.now()
	b := l.buckets[caller]
	if b == nil {
		if len(l.buckets) >= l.maxKeys {
			for k, v := range l.buckets {
				if t.Sub(v.at) > time.Minute {
					delete(l.buckets, k)
				}
			}
		}
		b = &bucket{tokens: l.burst, at: t}
		l.buckets[caller] = b
	}
	b.tokens += t.Sub(b.at).Seconds() * l.perSec
	if b.tokens > l.burst {
		b.tokens = l.burst
	}
	b.at = t
	if b.tokens < 1 {
		return time.Duration((1 - b.tokens) / l.perSec * float64(time.Second)), false
	}
	b.tokens--
	return 0, true
}

type schemaCache struct {
	mu    sync.Mutex
	max   int
	items map[string]*jsonschema.Schema
	order []string
}

func newSchemaCache(max int) *schemaCache {
	return &schemaCache{max: max, items: map[string]*jsonschema.Schema{}}
}

// get compiles an operation's input schema once per contract digest.
func (c *schemaCache) get(digest string, schema map[string]any) (*jsonschema.Schema, error) {
	c.mu.Lock()
	if s, ok := c.items[digest]; ok {
		c.mu.Unlock()
		return s, nil
	}
	c.mu.Unlock()
	s, err := jsonschema.Compile(schema)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.order) >= c.max {
		delete(c.items, c.order[0])
		c.order = c.order[1:]
	}
	c.items[digest] = s
	c.order = append(c.order, digest)
	return s, nil
}
