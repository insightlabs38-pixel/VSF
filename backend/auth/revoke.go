package auth

// Track registers work that depends on a grant; cancel runs once when the grant ends for any reason, so nothing keeps acting for a sign-in that is gone.
func (b *Broker) Track(grantID, key string, cancel func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.dependents[grantID] == nil {
		b.dependents[grantID] = map[string]func(){}
	}
	b.dependents[grantID][key] = cancel
}

// Untrack forgets finished work.
func (b *Broker) Untrack(grantID, key string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.dependents[grantID], key)
}

func (b *Broker) cancelDependents(g Grant, actor string) int {
	b.mu.Lock()
	deps := b.dependents[g.ID]
	delete(b.dependents, g.ID)
	b.mu.Unlock()
	for _, cancel := range deps {
		cancel()
		b.note(EventCancelled, g, actor, ReasonRevoked)
	}
	return len(deps)
}

func (b *Broker) wipe(g Grant) {
	if b.Vault == nil {
		return
	}
	an, rn := secretNames(g.ID)
	_ = b.Vault.Delete(g.Account, an)
	_ = b.Vault.Delete(g.Account, rn)
}

// Revoke ends a grant for good: its secrets are wiped and everything that depends on it is cancelled. Only its owner may do it here; revoking twice is harmless.
func (b *Broker) Revoke(id, actor string) error {
	g, ok := b.Store.Get(id)
	if !ok || g.Owner != actor {
		b.note(EventRefused, Grant{ID: id}, safeActor(actor), ReasonUnknownGrant)
		return ErrDenied
	}
	return b.end(g, actor)
}

// end revokes without an owner check, for sign-out observed at the source and for administrative revocation.
func (b *Broker) end(g Grant, actor string) error {
	if g.State != StateRevoked {
		g.State = StateRevoked
		if err := b.Store.Put(g); err != nil {
			return err
		}
		b.note(EventRevoked, g, actor, ReasonRevoked)
	}
	b.wipe(g)
	b.cancelDependents(g, actor)
	return nil
}
