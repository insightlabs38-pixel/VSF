package auth

import (
	"sync"
	"time"
)

func init() { addReasons(ReasonSelf) }

// ReasonSelf marks a delegation a person tried to give to themself.
const ReasonSelf Reason = "self_delegation"

const maxDelegation = 30 * 24 * time.Hour

type pair struct{ person, executor string }

// Delegations records which callers a person lets run what they confirm. A delegation names exactly one person and one caller, expires, and can be withdrawn at any time, which also ends the use of confirmations already given under it.
type Delegations struct {
	Now   func() time.Time
	Audit *Audit

	mu sync.Mutex
	m  map[pair]time.Time
}

func NewDelegations(now func() time.Time, audit *Audit) *Delegations {
	if now == nil {
		now = time.Now
	}
	return &Delegations{Now: now, Audit: audit, m: map[pair]time.Time{}}
}

func (d *Delegations) note(ev Event, person, executor string, r Reason) {
	if d.Audit != nil {
		_ = d.Audit.Append(AuditRecord{At: d.Now(), Event: ev, Account: executor, Actor: person, Reason: r})
	}
}

// Allow lets person's confirmations be used by executor until the given time (at most thirty days).
func (d *Delegations) Allow(person, executor string, until time.Time) error {
	if !idRe.MatchString(person) || !idRe.MatchString(executor) {
		return ErrInvalid
	}
	if person == executor {
		d.note(EventRefused, person, executor, ReasonSelf)
		return ErrInvalid
	}
	now := d.Now()
	if !until.After(now) || until.After(now.Add(maxDelegation)) {
		return ErrInvalid
	}
	d.mu.Lock()
	d.m[pair{person, executor}] = until
	d.mu.Unlock()
	d.note(EventDelegated, person, executor, ReasonOK)
	return nil
}

// Withdraw ends a delegation.
func (d *Delegations) Withdraw(person, executor string) {
	d.mu.Lock()
	_, had := d.m[pair{person, executor}]
	delete(d.m, pair{person, executor})
	d.mu.Unlock()
	if had {
		d.note(EventUndelegated, person, executor, ReasonRevoked)
	}
}

// Allows reports whether person currently lets executor run what they confirm.
func (d *Delegations) Allows(person, executor string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	until, ok := d.m[pair{person, executor}]
	return ok && d.Now().Before(until)
}
