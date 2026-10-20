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
	gen     string
	secrets int
	mu      sync.Mutex
	value   []byte
	origin  string
	expires time.Time
	uses    int
}

// Lease checks the request like any other use of a grant, then loads the secret named name ("access" or "refresh") from the vault. A lease is a snapshot: it dies with the grant, with a pause, with a new sign-in epoch and with a token refresh, after which a new lease is needed. Issuing one is a mandatory audit event; if it cannot be recorded no lease is given.
func (c *Credentials) Lease(chk Check, name string, ttl time.Duration, uses int) (*Lease, error) {
	if (name != "access" && name != "refresh") || ttl <= 0 || ttl > maxLeaseTTL || uses < 1 || uses > maxLeaseUses || c.Broker.Vault == nil {
		return nil, ErrInvalid
	}
	var g Grant
	var val []byte
	for range 3 {
		var err error
		if g, err = c.Broker.Authorize(chk); err != nil {
			return nil, err
		}
		if val, err = c.Broker.Vault.Get(g.Account, g.ID+"/"+name); err != nil {
			c.Broker.note(EventRefused, g, g.Owner, ReasonPaused)
			return nil, ErrReauth
		}
		if after, ok := c.Broker.Store.Get(g.ID); ok && after.State == StateActive && after.Epoch == g.Epoch && after.SecretGen == g.SecretGen {
			break
		}
		zero(val)
		val = nil
	}
	if val == nil {
		return nil, ErrReauth
	}
	if err := c.Broker.mustNote(EventLease, g, g.Owner, ReasonOK); err != nil {
		zero(val)
		return nil, err
	}
	if c.Redact != nil {
		_ = c.Redact.Add(val)
	}
	exp := c.Broker.Now().Add(ttl)
	if g.ExpiresAt.Before(exp) {
		exp = g.ExpiresAt
	}
	return &Lease{c: c, grant: g, gen: g.Generation(), secrets: g.SecretGen, value: val, origin: g.Source.Origin, expires: exp, uses: uses}, nil
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
	case !ok || g.State != StateActive || !now.Before(g.ExpiresAt) || g.Generation() != l.gen:
		l.wipeLocked()
		return ErrDenied
	case g.SecretGen != l.secrets:
		l.wipeLocked()
		return ErrReauth
	case !now.Before(l.expires) || l.uses < 1:
		l.wipeLocked()
		return ErrExpired
	case origin != l.origin:
		l.c.Broker.note(EventRefused, g, g.Owner, ReasonSource)
		return ErrDenied
	}
	if err := l.c.Broker.mustNote(EventRevealed, g, g.Owner, ReasonOK); err != nil {
		return err
	}
	l.uses--
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
