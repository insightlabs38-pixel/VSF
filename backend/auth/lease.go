package auth

import (
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

const (
	maxLeaseTTL  = 5 * time.Minute
	maxLeaseUses = 16
)

// Credentials hands out secrets only as short-lived leases that work for one origin.
type Credentials struct {
	Broker *Broker
	Redact *Redactor
}

// Lease is permission to use one secret for a short time, a few times, against one origin. It cannot be printed, marshalled or logged with its value.
type Lease struct {
	c       *Credentials
	grant   Grant
	mu      sync.Mutex
	value   []byte
	origin  string
	expires time.Time
	uses    int
}

// Lease checks the request like any other use of a grant, then loads the secret named name ("access" or "refresh") from the vault.
func (c *Credentials) Lease(chk Check, name string, ttl time.Duration, uses int) (*Lease, error) {
	if (name != "access" && name != "refresh") || ttl <= 0 || ttl > maxLeaseTTL || uses < 1 || uses > maxLeaseUses || c.Broker.Vault == nil {
		return nil, ErrInvalid
	}
	g, err := c.Broker.Authorize(chk)
	if err != nil {
		return nil, err
	}
	val, err := c.Broker.Vault.Get(g.Account, g.ID+"/"+name)
	if err != nil {
		c.Broker.note(EventRefused, g, g.Owner, ReasonPaused)
		return nil, ErrReauth
	}
	if c.Redact != nil {
		_ = c.Redact.Add(val)
	}
	exp := c.Broker.Now().Add(ttl)
	if g.ExpiresAt.Before(exp) {
		exp = g.ExpiresAt
	}
	c.Broker.note(EventLease, g, g.Owner, ReasonOK)
	return &Lease{c: c, grant: g, value: val, origin: g.Source.Origin, expires: exp, uses: uses}, nil
}

func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// Inject calls fn with a copy of the secret if the target is exactly the leased origin and the lease and its grant are still good. The copy is wiped when fn returns.
func (l *Lease) Inject(origin string, fn func(val []byte) error) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.value == nil {
		return ErrDenied
	}
	now := l.c.Broker.Now()
	g, ok := l.c.Broker.Store.Get(l.grant.ID)
	switch {
	case !ok || g.State != StateActive || !now.Before(g.ExpiresAt):
		l.wipeLocked()
		return ErrDenied
	case !now.Before(l.expires) || l.uses < 1:
		l.wipeLocked()
		return ErrExpired
	case origin != l.origin:
		l.c.Broker.note(EventRefused, g, g.Owner, ReasonSource)
		return ErrDenied
	}
	l.uses--
	l.c.Broker.note(EventRevealed, g, g.Owner, ReasonOK)
	cp := append([]byte(nil), l.value...)
	defer zero(cp)
	if err := fn(cp); err != nil {
		if l.c.Redact != nil {
			return l.c.Redact.Err(err)
		}
		return errors.New("injection failed")
	}
	if l.uses == 0 {
		l.wipeLocked()
	}
	return nil
}

func (l *Lease) wipeLocked() {
	zero(l.value)
	l.value = nil
}

// Close wipes the secret; a lease is also wiped when it expires, runs out of uses or its grant ends.
func (l *Lease) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.wipeLocked()
}

func (*Lease) String() string               { return "[lease redacted]" }
func (*Lease) GoString() string             { return "[lease redacted]" }
func (*Lease) MarshalJSON() ([]byte, error) { return []byte(`"[lease redacted]"`), nil }
func (*Lease) MarshalText() ([]byte, error) { return []byte("[lease redacted]"), nil }
func (*Lease) LogValue() slog.Value         { return slog.StringValue("[lease redacted]") }
func (*Lease) Format(f fmt.State, _ rune)   { _, _ = f.Write([]byte("[lease redacted]")) }
