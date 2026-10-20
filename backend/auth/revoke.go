package auth

import (
	"errors"
	"time"
)

var zeroTime time.Time

var errUnchanged = errors.New("unchanged")

const maxSwapTries = 16

// update applies fn to the freshest record until Store.Swap accepts the result. fn may return errUnchanged to leave the record alone; any other error aborts. A revoked grant is never changed to anything else.
func (b *Broker) update(id string, fn func(g *Grant) error) (Grant, bool, error) {
	for range maxSwapTries {
		cur, ok := b.Store.Get(id)
		if !ok {
			return Grant{}, false, ErrDenied
		}
		next := cur
		next.Scopes = append([]string(nil), cur.Scopes...)
		if err := fn(&next); err != nil {
			if errors.Is(err, errUnchanged) {
				return cur, false, nil
			}
			return cur, false, err
		}
		got, err := b.Store.Swap(cur.Rev, next)
		switch {
		case err == nil:
			return got, true, nil
		case errors.Is(err, ErrConflict):
			continue
		case errors.Is(err, ErrEnded):
			return cur, false, ErrDenied
		default:
			return cur, false, err
		}
	}
	return Grant{}, false, ErrConflict
}

// Track registers work that depends on a grant; cancel runs once when the grant ends or its sign-in epoch moves on, so nothing keeps acting for a sign-in that is gone. If the grant has already ended, cancel runs now and Track reports false.
func (b *Broker) Track(grantID, key string, cancel func()) bool {
	g, ok := b.Store.Get(grantID)
	epoch := 0
	if ok {
		epoch = g.Epoch
	}
	b.mu.Lock()
	if b.dependents[grantID] == nil {
		b.dependents[grantID] = map[string]dependent{}
	}
	b.dependents[grantID][key] = dependent{cancel: cancel, epoch: epoch}
	b.mu.Unlock()
	if now, still := b.Store.Get(grantID); !still || now.State == StateRevoked || now.Epoch != epoch {
		b.mu.Lock()
		d, mine := b.dependents[grantID][key]
		delete(b.dependents[grantID], key)
		b.mu.Unlock()
		if mine {
			d.cancel()
		}
		return false
	}
	return true
}

// Untrack forgets finished work.
func (b *Broker) Untrack(grantID, key string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.dependents[grantID], key)
	if len(b.dependents[grantID]) == 0 {
		delete(b.dependents, grantID)
	}
}

// cancelDependents cancels work bound to the grant; with below > 0 only work bound to an epoch earlier than that.
func (b *Broker) cancelDependents(g Grant, actor string, below int) int {
	var run []func()
	b.mu.Lock()
	for k, d := range b.dependents[g.ID] {
		if below == 0 || d.epoch < below {
			run = append(run, d.cancel)
			delete(b.dependents[g.ID], k)
		}
	}
	if len(b.dependents[g.ID]) == 0 {
		delete(b.dependents, g.ID)
	}
	b.mu.Unlock()
	for _, cancel := range run {
		cancel()
		b.note(EventCancelled, g, actor, ReasonRevoked)
	}
	return len(run)
}

// wipe deletes a grant's secrets. A failure does not make the grant usable (it is already revoked); the grant stays queued for FlushWipes.
func (b *Broker) wipe(g Grant) error {
	if b.Vault == nil {
		return nil
	}
	mu := b.stripe(g.ID)
	mu.Lock()
	defer mu.Unlock()
	an, rn := secretNames(g.ID)
	var errs []error
	for _, n := range []string{an, rn} {
		if err := b.Vault.Delete(g.Account, n); err != nil {
			errs = append(errs, err)
		}
	}
	b.mu.Lock()
	if len(errs) == 0 {
		delete(b.wipes, g.ID)
	} else {
		b.wipes[g.ID] = g
	}
	b.mu.Unlock()
	if len(errs) > 0 {
		_ = b.mustNote(EventWipeFailed, g, g.Owner, ReasonWipe)
		return ErrWipePending
	}
	return nil
}

// FlushWipes retries physical deletion for revoked grants whose secrets could not be removed, and returns how many remain.
func (b *Broker) FlushWipes() int {
	b.mu.Lock()
	todo := make([]Grant, 0, len(b.wipes))
	for _, g := range b.wipes {
		todo = append(todo, g)
	}
	b.mu.Unlock()
	for _, g := range todo {
		_ = b.wipe(g)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.wipes)
}

// WipesPending is the number of revoked grants whose secrets are not yet confirmed deleted.
func (b *Broker) WipesPending() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.wipes)
}

// Revoke ends a grant for good: it stops being usable at once, its dependent work is cancelled, then its secrets are wiped. Only its owner may do it here; revoking twice is harmless. ErrWipePending or ErrAudit report a revocation that took effect but whose cleanup or record needs a retry.
func (b *Broker) Revoke(id, actor string) error {
	g, ok := b.Store.Get(id)
	if !ok || g.Owner != actor {
		b.note(EventRefused, Grant{ID: id}, safeActor(actor), ReasonUnknownGrant)
		return ErrDenied
	}
	_, err := b.endGrant(id, actor, ReasonRevoked)
	return err
}

// endGrant revokes without an owner check, for sign-out observed at the source, expiry and administrative revocation.
func (b *Broker) endGrant(id, actor string, why Reason) (Grant, error) {
	g, changed, err := b.update(id, func(g *Grant) error {
		if g.State == StateRevoked {
			return errUnchanged
		}
		g.State, g.PausedAt, g.PauseUntil = StateRevoked, zeroTime, zeroTime
		return nil
	})
	if err != nil {
		return g, err
	}
	var auditErr error
	if changed {
		auditErr = b.mustNote(EventRevoked, g, actor, why)
	}
	b.cancelDependents(g, actor, 0)
	wipeErr := b.wipe(g)
	return g, errors.Join(auditErr, wipeErr)
}
