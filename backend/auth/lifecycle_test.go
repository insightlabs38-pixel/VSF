package auth

import (
	"bytes"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// hookStore wraps a store so a test can interleave another writer at an exact point.
type hookStore struct {
	Store
	before func(prev uint64, next Grant) error
}

func (h *hookStore) Swap(prev uint64, next Grant) (Grant, error) {
	if h.before != nil {
		if err := h.before(prev, next); err != nil {
			return Grant{}, err
		}
	}
	return h.Store.Swap(prev, next)
}

func TestTheStoreContractKeepsRevocationTerminalAndIdentityFixed(t *testing.T) {
	for name, mk := range map[string]func() Store{"mem": func() Store { return NewMemStore() }, "hooked": func() Store { return &hookStore{Store: NewMemStore()} }} {
		s := mk()
		g := grant("alice")
		g.ID, g.State, g.Epoch = "gr_contract", StateActive, 1
		if err := s.Create(g); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if err := s.Create(g); !errors.Is(err, ErrConflict) {
			t.Fatalf("%s: ids are unique: %v", name, err)
		}
		cur, _ := s.Get(g.ID)
		if cur.Rev != 1 {
			t.Fatalf("%s: %d", name, cur.Rev)
		}
		bump := cur
		bump.Epoch = 2
		got, err := s.Swap(1, bump)
		if err != nil || got.Rev != 2 {
			t.Fatalf("%s: %+v %v", name, got, err)
		}
		if _, err := s.Swap(1, bump); !errors.Is(err, ErrConflict) {
			t.Fatalf("%s: a change from an old revision is refused: %v", name, err)
		}
		for what, mut := range map[string]func(*Grant){
			"owner":         func(x *Grant) { x.Owner = "mallory" },
			"source":        func(x *Grant) { x.Source.Origin = "https://evil.example.com" },
			"audience":      func(x *Grant) { x.Audience = "https://evil.example.com" },
			"principal":     func(x *Grant) { x.PrincipalRef = "pr_other" },
			"method":        func(x *Grant) { x.Method = MethodVault },
			"provider":      func(x *Grant) { x.Provider = "other" },
			"epoch falling": func(x *Grant) { x.Epoch = 1 },
			"wider scopes":  func(x *Grant) { x.Scopes = append(x.Scopes, "mail.admin") },
		} {
			bad := got
			mut(&bad)
			if _, err := s.Swap(got.Rev, bad); !errors.Is(err, ErrInvalid) {
				t.Fatalf("%s: changing %s must be refused: %v", name, what, err)
			}
		}
		narrow := got
		narrow.Scopes = narrow.Scopes[:1]
		got, err = s.Swap(got.Rev, narrow)
		if err != nil {
			t.Fatalf("%s: scopes may narrow: %v", name, err)
		}
		dead := got
		dead.State = StateRevoked
		got, err = s.Swap(got.Rev, dead)
		if err != nil {
			t.Fatal(err)
		}
		for _, back := range []State{StateActive, StatePaused} {
			x := got
			x.State = back
			if _, err := s.Swap(got.Rev, x); !errors.Is(err, ErrEnded) {
				t.Fatalf("%s: a revoked grant cannot become %s: %v", name, back, err)
			}
		}
	}
}

func TestAStaleWriterCannotReviveARevokedGrant(t *testing.T) {
	b, _, _ := newBroker(t)
	g, _ := b.Issue(grant("alice"))
	stale, _ := b.Store.Get(g.ID)
	if err := b.Revoke(g.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	stale.ExpiresAt = stale.ExpiresAt.Add(time.Hour)
	if _, err := b.Store.Swap(stale.Rev, stale); !errors.Is(err, ErrConflict) {
		t.Fatalf("%v", err)
	}
	cur, _ := b.Store.Get(g.ID)
	fresh := cur
	fresh.State = StateActive
	if _, err := b.Store.Swap(cur.Rev, fresh); !errors.Is(err, ErrEnded) {
		t.Fatalf("%v", err)
	}
	if _, err := b.Authorize(use(g, "alice")); !errors.Is(err, ErrDenied) {
		t.Fatalf("%v", err)
	}
}

func TestRefreshRacingRevokeNeverResurrectsCredentials(t *testing.T) {
	r := newRig(t)
	g, _ := r.run(t, "alice")
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	r.idp.gate = func() { once.Do(func() { close(started) }); <-release }
	done := make(chan error, 1)
	go func() { _, err := r.o.Refresh("alice", g.ID, r.idp.provider()); done <- err }()
	<-started
	if err := r.b.Revoke(g.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.vault.Get("acme", g.ID+"/refresh"); !errors.Is(err, ErrNotFound) {
		t.Fatal("revocation wipes at once")
	}
	close(release)
	if err := <-done; !errors.Is(err, ErrDenied) {
		t.Fatalf("a refresh that finishes after revocation is discarded: %v", err)
	}
	cur, _ := r.b.Store.Get(g.ID)
	if cur.State != StateRevoked || len(r.vault.m) != 0 {
		t.Fatalf("revoked grants stay revoked and empty: %v %d", cur.State, len(r.vault.m))
	}
}

func TestARevocationBetweenTheCheckAndTheCommitOfARefreshStillWins(t *testing.T) {
	r := newRig(t)
	hs := &hookStore{Store: r.b.Store}
	r.b.Store = hs
	g, _ := r.run(t, "alice")
	var fired atomic.Bool
	hs.before = func(prev uint64, next Grant) error {
		if next.ID == g.ID && next.State == StateActive && fired.CompareAndSwap(false, true) {
			dead := next
			dead.State = StateRevoked
			dead.ExpiresAt = g.ExpiresAt
			_, err := hs.Store.Swap(prev, dead)
			return err
		}
		return nil
	}
	if _, err := r.o.Refresh("alice", g.ID, r.idp.provider()); !errors.Is(err, ErrDenied) {
		t.Fatalf("%v", err)
	}
	if cur, _ := r.b.Store.Get(g.ID); cur.State != StateRevoked {
		t.Fatal("still revoked")
	}
	if _, err := r.vault.Get("acme", g.ID+"/access"); !errors.Is(err, ErrNotFound) {
		t.Fatal("tokens stored by the lost race are removed")
	}
	if _, err := r.vault.Get("acme", g.ID+"/refresh"); !errors.Is(err, ErrNotFound) {
		t.Fatal("including the rotated refresh token")
	}
}

func TestConcurrentRefreshesShareOneRequestSoARotatingTokenIsSpentOnce(t *testing.T) {
	r := newRig(t)
	r.idp.strict = true
	g, _ := r.run(t, "alice")
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	r.idp.gate = func() { once.Do(func() { close(started) }); <-release }
	res := make(chan Grant, 2)
	errs := make(chan error, 2)
	call := func() {
		x, err := r.o.Refresh("alice", g.ID, r.idp.provider())
		res <- x
		errs <- err
	}
	go call()
	<-started
	go call()
	for r.b.joins.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	close(release)
	a, b := <-res, <-res
	if e1, e2 := <-errs, <-errs; e1 != nil || e2 != nil {
		t.Fatalf("%v %v", e1, e2)
	}
	if a.ID != b.ID || a.Rev != b.Rev || r.idp.refreshCalls != 1 {
		t.Fatalf("one request, one outcome: %d %+v %+v", r.idp.refreshCalls, a, b)
	}
	if rt, _ := r.vault.Get("acme", g.ID+"/refresh"); string(rt) != "rt-2" {
		t.Fatalf("the rotated token was stored exactly once: %q", rt)
	}
	r.idp.gate = nil
	if _, err := r.o.Refresh("alice", g.ID, r.idp.provider()); err != nil {
		t.Fatalf("the next refresh uses the rotated token: %v", err)
	}
	if rt, _ := r.vault.Get("acme", g.ID+"/refresh"); string(rt) != "rt-3" {
		t.Fatalf("%q", rt)
	}
}

func TestAStaleResumeCannotUnpauseANewerPause(t *testing.T) {
	r := newSig(t)
	hs := &hookStore{Store: r.b.Store}
	r.b.Store = hs
	g := r.signIn()
	_, _ = r.b.Pause(g.ID, "alice", ChallengeMFA)
	r.tick(time.Second)
	o := r.obs("bs_alice1", nil)
	var fired atomic.Bool
	hs.before = func(prev uint64, next Grant) error {
		if next.State == StateActive && fired.CompareAndSwap(false, true) {
			cur, _ := hs.Store.Get(g.ID)
			a := cur
			a.State, a.Epoch = StateActive, cur.Epoch+1
			a.PausedAt, a.PauseUntil = time.Time{}, time.Time{}
			a, _ = hs.Store.Swap(cur.Rev, a)
			p := a
			p.State, p.PausedAt, p.PauseUntil = StatePaused, r.cl.t, r.cl.t.Add(pauseTTL)
			_, _ = hs.Store.Swap(a.Rev, p)
		}
		return nil
	}
	if _, err := r.b.Resume(g.ID, "alice", o); !errors.Is(err, ErrDenied) {
		t.Fatalf("%v", err)
	}
	cur, _ := r.b.Store.Get(g.ID)
	if cur.State != StatePaused || cur.Epoch != 2 {
		t.Fatalf("the newer pause stands: %+v", cur)
	}
}

func TestConcurrentPausesResumesAndRevokesEndInAWellDefinedState(t *testing.T) {
	r := newSig(t)
	g := r.signIn()
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(3)
		go func() { defer wg.Done(); _, _ = r.b.Pause(g.ID, "alice", ChallengeMFA) }()
		go func() { defer wg.Done(); _, _ = r.b.Resume(g.ID, "alice", Observation{}) }()
		go func() { defer wg.Done(); r.b.Sweep() }()
	}
	wg.Wait()
	if err := r.b.Revoke(g.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 16; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); _, _ = r.b.Pause(g.ID, "alice", ChallengeMFA) }()
		go func() { defer wg.Done(); _ = r.b.Revoke(g.ID, "alice") }()
	}
	wg.Wait()
	if cur, _ := r.b.Store.Get(g.ID); cur.State != StateRevoked {
		t.Fatalf("%v", cur.State)
	}
}

func TestAPrincipalSwitchCancelsQueuedWorkAndRefusesItsGeneration(t *testing.T) {
	r := newSig(t)
	g := r.signIn()
	ran := 0
	if !r.b.Track(g.ID, "queued", func() { ran++ }) {
		t.Fatal("tracked")
	}
	r.tick(time.Second)
	r.session("bs_alice2", "alice", "pr_two", "rt_2", mail)
	_ = r.br.Expect("alice", "acme", mail, []string{"mail.read"})
	if _, err := r.br.Accept(r.ev("bs_alice2", nil)); err != nil {
		t.Fatal(err)
	}
	if ran != 1 {
		t.Fatal("queued work for the earlier person is cancelled")
	}
	chk := Check{GrantID: g.ID, Owner: "alice", Origin: mail.Origin, Audience: g.Audience, Generation: g.Generation()}
	if _, err := r.b.Authorize(chk); !errors.Is(err, ErrDenied) {
		t.Fatalf("%v", err)
	}
}

type flakyVault struct {
	*MemVault
	failPut, failDelete func(name string) bool
}

func (f *flakyVault) Put(a, n string, v []byte) error {
	if f.failPut != nil && f.failPut(n) {
		return errors.New("disk full")
	}
	return f.MemVault.Put(a, n, v)
}

func (f *flakyVault) Delete(a, n string) error {
	if f.failDelete != nil && f.failDelete(n) {
		return errors.New("permission denied")
	}
	return f.MemVault.Delete(a, n)
}

func hasSuffix(s, suf string) bool { return len(s) >= len(suf) && s[len(s)-len(suf):] == suf }

func TestAFailedSecretWriteNeverLeavesAnActiveGrantWithoutCredentials(t *testing.T) {
	for _, bad := range []string{"/access", "/refresh"} {
		r := newRig(t)
		fv := &flakyVault{MemVault: r.vault, failPut: func(n string) bool { return hasSuffix(n, bad) }}
		r.b.Vault = fv
		if _, err := r.run(t, "alice"); err == nil {
			t.Fatalf("storing %s failed, so sign-in fails", bad)
		}
		for _, g := range r.b.List("alice") {
			if g.State != StateRevoked {
				t.Fatalf("a half-stored sign-in is not left active: %+v", g)
			}
		}
		if len(r.vault.m) != 0 {
			t.Fatal("and nothing is left in the vault")
		}
	}
}

func TestRefreshStorageFailuresEndOrKeepTheGrantHonestly(t *testing.T) {
	r := newRig(t)
	g, _ := r.run(t, "alice")
	fv := &flakyVault{MemVault: r.vault, failPut: func(n string) bool { return hasSuffix(n, "/access") }}
	r.b.Vault = fv
	if _, err := r.o.Refresh("alice", g.ID, r.idp.provider()); err == nil {
		t.Fatal("the access token could not be stored")
	}
	cur, _ := r.b.Store.Get(g.ID)
	if cur.State != StateActive || !cur.ExpiresAt.Equal(g.ExpiresAt) || cur.SecretGen != g.SecretGen {
		t.Fatalf("the grant still describes what is stored: %+v", cur)
	}
	if rt, _ := r.vault.Get("acme", g.ID+"/refresh"); string(rt) != "rt-2" {
		t.Fatalf("the rotated refresh token is kept, it is the only valid one: %q", rt)
	}
	fv.failPut = func(n string) bool { return hasSuffix(n, "/refresh") }
	if _, err := r.o.Refresh("alice", g.ID, r.idp.provider()); !errors.Is(err, ErrReauth) {
		t.Fatalf("%v", err)
	}
	if cur, _ := r.b.Store.Get(g.ID); cur.State != StateRevoked {
		t.Fatal("a rotated credential that cannot be stored ends the grant: it cannot be used again")
	}
}

func TestRevocationIsImmediateEvenWhenPhysicalDeletionFailsAndIsRetried(t *testing.T) {
	r := newRig(t)
	g, _ := r.run(t, "alice")
	fv := &flakyVault{MemVault: r.vault, failDelete: func(string) bool { return true }}
	r.b.Vault = fv
	creds := &Credentials{Broker: r.b}
	l, err := creds.Lease(Check{GrantID: g.ID, Owner: "alice", Origin: g.Source.Origin, Audience: g.Audience, Scopes: []string{"mail.read"}}, "access", time.Minute, 5)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	r.b.Track(g.ID, "run", func() { n++ })
	if err := r.b.Revoke(g.ID, "alice"); !errors.Is(err, ErrWipePending) {
		t.Fatalf("the caller learns the cleanup is outstanding: %v", err)
	}
	if n != 1 || r.b.WipesPending() != 1 {
		t.Fatalf("work is cancelled and the wipe is queued: %d %d", n, r.b.WipesPending())
	}
	if err := l.Inject(g.Source.Origin, func([]byte) error { return nil }); !errors.Is(err, ErrDenied) {
		t.Fatalf("a failed deletion never implies the credential is still authorized: %v", err)
	}
	if _, err := creds.Lease(Check{GrantID: g.ID, Owner: "alice", Origin: g.Source.Origin, Audience: g.Audience}, "access", time.Minute, 1); !errors.Is(err, ErrDenied) {
		t.Fatalf("%v", err)
	}
	if r.b.FlushWipes() != 1 {
		t.Fatal("still failing")
	}
	fv.failDelete = nil
	if r.b.FlushWipes() != 0 {
		t.Fatal("deleted once the vault recovers")
	}
	if _, err := r.vault.Get("acme", g.ID+"/access"); !errors.Is(err, ErrNotFound) {
		t.Fatal("the secret is gone")
	}
	if !bytes.Contains(r.logOf(), []byte("secret_delete_failed")) {
		t.Fatal("the failure is on the audit log")
	}
}

func (r *rig) logOf() []byte { return r.buf.Bytes() }

func TestALeaseDoesNotSurviveARefreshAnEpochChangeOrAPause(t *testing.T) {
	r := newRig(t)
	g, _ := r.run(t, "alice")
	creds := &Credentials{Broker: r.b}
	chk := Check{GrantID: g.ID, Owner: "alice", Origin: g.Source.Origin, Audience: g.Audience, Scopes: []string{"mail.read"}}
	l1, _ := creds.Lease(chk, "access", time.Minute, 5)
	if _, err := r.o.Refresh("alice", g.ID, r.idp.provider()); err != nil {
		t.Fatal(err)
	}
	if err := l1.Inject(g.Source.Origin, func([]byte) error { return nil }); !errors.Is(err, ErrReauth) {
		t.Fatalf("a snapshot taken before token replacement is superseded: %v", err)
	}
	l2, _ := creds.Lease(chk, "access", time.Minute, 5)
	if _, err := r.b.Pause(g.ID, "alice", ChallengeMFA); err != nil {
		t.Fatal(err)
	}
	if err := l2.Inject(g.Source.Origin, func([]byte) error { return nil }); !errors.Is(err, ErrDenied) {
		t.Fatalf("a paused grant's lease is dead: %v", err)
	}
}

type failingWriter struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	fail      bool
	partial   bool
	syncFails bool
}

func (w *failingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.fail {
		if w.partial {
			w.buf.Write(p[:len(p)/2])
			return len(p) / 2, errors.New("no space left on device")
		}
		return 0, errors.New("no space left on device")
	}
	return w.buf.Write(p)
}

func (w *failingWriter) Sync() error {
	if w.syncFails {
		return errors.New("input/output error")
	}
	return nil
}

func TestAMandatoryAuditFailureFailsTheOperationInsteadOfPassingSilently(t *testing.T) {
	w := &failingWriter{}
	cl := &clock{t: epoch}
	b := NewBroker(nil, NewAudit(w), cl.now)
	b.Vault = NewMemVault()
	if !b.AuditRequired() {
		t.Fatal("a broker with a log treats it as mandatory")
	}
	good, err := b.Issue(grant("alice"))
	if err != nil {
		t.Fatal(err)
	}
	w.fail = true
	if _, err := b.Issue(grant("alice")); !errors.Is(err, ErrAudit) {
		t.Fatalf("a grant whose issuance cannot be recorded is not handed out: %v", err)
	}
	for _, g := range b.List("alice") {
		if g.ID != good.ID && g.State != StateRevoked {
			t.Fatalf("and it does not stay live: %+v", g)
		}
	}
	creds := &Credentials{Broker: b}
	_ = b.Vault.Put("acme", good.ID+"/access", []byte("secret-value-1"))
	if _, err := creds.Lease(use(good, "alice"), "access", time.Minute, 1); !errors.Is(err, ErrAudit) {
		t.Fatalf("no lease without its audit record: %v", err)
	}
	if err := b.Revoke(good.ID, "alice"); !errors.Is(err, ErrAudit) {
		t.Fatalf("revocation takes effect and reports the missing record: %v", err)
	}
	if cur, _ := b.Store.Get(good.ID); cur.State != StateRevoked {
		t.Fatal("revoked regardless")
	}
	if _, err := b.Authorize(use(good, "alice")); !errors.Is(err, ErrDenied) {
		t.Fatalf("%v", err)
	}
	if n, _ := b.FlushAudit(); n == 0 {
		t.Fatal("unwritten mandatory records are kept for retry")
	}
	w.fail = false
	if n, err := b.FlushAudit(); err != nil || n != 0 {
		t.Fatalf("%d %v", n, err)
	}
	if n, err := VerifyAudit(bytes.NewReader(w.buf.Bytes())); err != nil || n < 3 {
		t.Fatalf("the chain survives the outage: %d %v", n, err)
	}
	if b.DroppedDiagnostics() == 0 {
		t.Fatal("best-effort refusals that could not be written are counted, not hidden")
	}
}

func TestAnAuditLogThatWroteHalfALineRefusesFurtherRecordsUntilRecovered(t *testing.T) {
	w := &failingWriter{}
	a := NewAudit(w)
	rec := AuditRecord{At: epoch, Event: EventIssued, Grant: "gr_a", Reason: ReasonOK}
	if err := a.Append(rec); err != nil {
		t.Fatal(err)
	}
	w.fail, w.partial = true, true
	if err := a.Append(rec); !errors.Is(err, ErrAudit) {
		t.Fatal(err)
	}
	w.fail = false
	if err := a.Append(rec); !errors.Is(err, ErrAudit) {
		t.Fatalf("appending after a torn write would corrupt the chain: %v", err)
	}
	w2 := &failingWriter{syncFails: true}
	a2 := NewAudit(w2)
	if err := a2.Append(rec); !errors.Is(err, ErrAudit) {
		t.Fatalf("a record that was not synced is not reported as written: %v", err)
	}
}
