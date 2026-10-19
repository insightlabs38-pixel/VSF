package execution

import (
	"context"
	"sync"
	"time"
)

type LimiterConfig struct {
	MaxInFlight int
	Interval    time.Duration
	MaxInterval time.Duration
	Recover     int
}

type bucket struct {
	inflight  int
	interval  time.Duration
	notBefore time.Time
	good      int
}

// Limiter paces calls per source: a refusal slows the source down, sustained success speeds it back up.
type Limiter struct {
	mu    sync.Mutex
	clock Clock
	cfg   LimiterConfig
	apps  map[string]*bucket
}

func NewLimiter(clock Clock, cfg LimiterConfig) *Limiter {
	if cfg.MaxInFlight <= 0 {
		cfg.MaxInFlight = 1
	}
	if cfg.MaxInterval < cfg.Interval {
		cfg.MaxInterval = cfg.Interval
	}
	if cfg.Recover <= 0 {
		cfg.Recover = 3
	}
	return &Limiter{clock: clock, cfg: cfg, apps: map[string]*bucket{}}
}

func (l *Limiter) bucket(app string) *bucket {
	b := l.apps[app]
	if b == nil {
		b = &bucket{interval: l.cfg.Interval}
		l.apps[app] = b
	}
	return b
}

func (l *Limiter) Acquire(ctx context.Context, app string) (func(), error) {
	for {
		l.mu.Lock()
		b := l.bucket(app)
		now := l.clock.Now()
		wait := b.notBefore.Sub(now)
		if b.inflight < l.cfg.MaxInFlight && wait <= 0 {
			b.inflight++
			b.notBefore = now.Add(b.interval)
			l.mu.Unlock()
			var once sync.Once
			return func() {
				once.Do(func() {
					l.mu.Lock()
					b.inflight--
					l.mu.Unlock()
				})
			}, nil
		}
		l.mu.Unlock()
		if wait <= 0 {
			wait = time.Millisecond
		}
		if err := l.clock.Sleep(ctx, wait); err != nil {
			return nil, err
		}
	}
}

// Slow doubles the pacing interval and honours the source's own retry hint.
func (l *Limiter) Slow(app string, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.bucket(app)
	b.good = 0
	b.interval *= 2
	if b.interval == 0 {
		b.interval = l.cfg.MaxInterval / 8
	}
	if b.interval > l.cfg.MaxInterval {
		b.interval = l.cfg.MaxInterval
	}
	if t := l.clock.Now().Add(retryAfter); t.After(b.notBefore) {
		b.notBefore = t
	}
}

func (l *Limiter) Ok(app string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.bucket(app)
	if b.good++; b.good >= l.cfg.Recover && b.interval > l.cfg.Interval {
		b.interval = max(b.interval/2, l.cfg.Interval)
		b.good = 0
	}
}

func (l *Limiter) Interval(app string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.bucket(app).interval
}
