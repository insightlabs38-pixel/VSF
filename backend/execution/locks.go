package execution

import (
	"context"
	"sync"
)

type lockQueue struct {
	held    bool
	waiters []chan struct{}
}

// Locks serializes work per key in arrival order and lets a waiter give up.
type Locks struct {
	mu sync.Mutex
	q  map[string]*lockQueue
}

func NewLocks() *Locks { return &Locks{q: map[string]*lockQueue{}} }

func (l *Locks) Acquire(ctx context.Context, key string) (func(), error) {
	l.mu.Lock()
	q := l.q[key]
	if q == nil {
		q = &lockQueue{}
		l.q[key] = q
	}
	if !q.held {
		q.held = true
		l.mu.Unlock()
		return l.releaser(key), nil
	}
	ch := make(chan struct{})
	q.waiters = append(q.waiters, ch)
	l.mu.Unlock()
	select {
	case <-ch:
		return l.releaser(key), nil
	case <-ctx.Done():
		l.mu.Lock()
		for i, w := range q.waiters {
			if w == ch {
				q.waiters = append(q.waiters[:i], q.waiters[i+1:]...)
				l.mu.Unlock()
				return nil, ctx.Err()
			}
		}
		l.mu.Unlock()
		<-ch
		l.releaser(key)()
		return nil, ctx.Err()
	}
}

func (l *Locks) releaser(key string) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			defer l.mu.Unlock()
			q := l.q[key]
			if len(q.waiters) > 0 {
				next := q.waiters[0]
				q.waiters = q.waiters[1:]
				close(next)
				return
			}
			delete(l.q, key)
		})
	}
}

// Waiting reports how many callers queue behind the holder of key.
func (l *Locks) Waiting(key string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	if q := l.q[key]; q != nil {
		return len(q.waiters)
	}
	return 0
}
