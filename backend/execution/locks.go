package execution

import (
	"context"
	"sync"
)

type waiter struct {
	fp      Footprint
	ready   chan struct{}
	granted bool
}

// Locks excludes overlapping footprints inside this process, serving waiters in arrival order.
// It says nothing about other processes: a distributed worker pool needs a shared lease service.
type Locks struct {
	mu    sync.Mutex
	held  map[*Footprint]struct{}
	queue []*waiter
}

func NewLocks() *Locks { return &Locks{held: map[*Footprint]struct{}{}} }

func (l *Locks) blockedByHeld(fp Footprint) bool {
	for h := range l.held {
		if h.Overlaps(fp) {
			return true
		}
	}
	return false
}

// pump grants every waiter that neither a holder nor an earlier waiter stands in front of.
func (l *Locks) pump() {
	var ahead []Footprint
	rest := l.queue[:0]
	for _, w := range l.queue {
		blocked := l.blockedByHeld(w.fp)
		for _, a := range ahead {
			blocked = blocked || a.Overlaps(w.fp)
		}
		if blocked {
			ahead = append(ahead, w.fp)
			rest = append(rest, w)
			continue
		}
		fp := w.fp
		l.held[&fp] = struct{}{}
		w.granted = true
		close(w.ready)
	}
	for i := len(rest); i < len(l.queue); i++ {
		l.queue[i] = nil
	}
	l.queue = rest
}

func (l *Locks) Acquire(ctx context.Context, fp Footprint) (func(), error) {
	if !fp.Exclusive {
		return func() {}, ctx.Err()
	}
	w := &waiter{fp: fp, ready: make(chan struct{})}
	l.mu.Lock()
	l.queue = append(l.queue, w)
	l.pump()
	l.mu.Unlock()
	select {
	case <-w.ready:
		return l.releaser(fp), nil
	case <-ctx.Done():
		l.mu.Lock()
		if w.granted {
			l.mu.Unlock()
			l.releaser(fp)()
			return nil, ctx.Err()
		}
		for i, q := range l.queue {
			if q == w {
				l.queue = append(l.queue[:i], l.queue[i+1:]...)
				break
			}
		}
		l.pump()
		l.mu.Unlock()
		return nil, ctx.Err()
	}
}

func (l *Locks) releaser(fp Footprint) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			defer l.mu.Unlock()
			for h := range l.held {
				if *h == fp {
					delete(l.held, h)
					break
				}
			}
			l.pump()
		})
	}
}

// Waiting reports how many queued callers overlap fp.
func (l *Locks) Waiting(fp Footprint) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, w := range l.queue {
		if w.fp.Overlaps(fp) {
			n++
		}
	}
	return n
}
