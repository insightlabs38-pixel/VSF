package auth

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/insightlabs38-pixel/vsf/backend/execution"
)

func TestAChallengeAtTheSourcePausesTheGrantAndOnlyAFreshCleanObservationResumesIt(t *testing.T) {
	r := newSig(t)
	g := r.signIn()
	b := r.b
	cancelled := 0
	b.Track(g.ID, "run-1", func() { cancelled++ })
	r.tick(time.Minute)
	if _, err := r.br.Accept(r.ev("bs_alice1", func(e *Evidence) { e.Challenge = ChallengeMFA })); err != nil {
		t.Fatal(err)
	}
	if !b.Paused(g.ID) {
		t.Fatal("an MFA prompt pauses the grant")
	}
	if _, err := b.Authorize(Check{GrantID: g.ID, Owner: "alice", Origin: mail.Origin, Audience: g.Audience}); !errors.Is(err, ErrReauth) {
		t.Fatalf("%v", err)
	}
	if cancelled != 0 {
		t.Fatal("a pause waits for the person; it does not cancel work")
	}
	r.tick(time.Second)
	// A heartbeat that says "still signed in" at the very instant of the pause must not resume anything.
	pausedAt := r.cl.t
	if got, err := r.br.Accept(r.ev("bs_alice1", func(e *Evidence) { e.ObservedAt = pausedAt.Add(-time.Second) })); err == nil || got.State == StateActive {
		t.Fatalf("an observation from before the pause: %+v %v", got, err)
	}
	r.tick(time.Second)
	cases := map[string]struct {
		owner string
		mut   func(*Observation)
		want  error
	}{
		"another caller":          {"mallory", nil, ErrDenied},
		"another source":          {"alice", func(o *Observation) { o.Source = Source{AppID: "app.mail", Origin: "https://evil.example.com"} }, ErrDenied},
		"before the pause":        {"alice", func(o *Observation) { o.ObservedAt = g.ObservedAt }, ErrReauth},
		"no observation time":     {"alice", func(o *Observation) { o.ObservedAt = time.Time{} }, ErrReauth},
		"challenge still showing": {"alice", func(o *Observation) { o.Challenge = ChallengeMFA }, ErrReauth},
		"signed out":              {"alice", func(o *Observation) { o.Authenticated = false }, ErrReauth},
		"not from the authority":  {"alice", func(o *Observation) { o.verified = false }, ErrDenied},
	}
	for name, c := range cases {
		o := r.obs("bs_alice1", nil)
		if c.mut != nil {
			c.mut(&o)
		}
		if _, err := b.Resume(g.ID, c.owner, o); !errors.Is(err, c.want) {
			t.Fatalf("%s: %v", name, err)
		}
		if !b.Paused(g.ID) {
			t.Fatalf("%s lifted the pause", name)
		}
		r.tick(time.Millisecond)
	}
	good := r.obs("bs_alice1", nil)
	got, err := b.Resume(g.ID, "alice", good)
	if err != nil || got.State != StateActive || got.Epoch != 2 || b.Paused(g.ID) {
		t.Fatalf("%+v %v", got, err)
	}
	if cancelled != 1 {
		t.Fatal("work started before the challenge belongs to the earlier epoch and is cancelled")
	}
	if _, err := b.Authorize(Check{GrantID: g.ID, Owner: "alice", Origin: mail.Origin, Audience: g.Audience}); err != nil {
		t.Fatalf("%v", err)
	}
	if _, err := b.Authorize(Check{GrantID: g.ID, Owner: "alice", Origin: mail.Origin, Audience: g.Audience, Generation: g.Generation()}); !errors.Is(err, ErrReauth) {
		t.Fatalf("work bound to the earlier sign-in generation is refused: %v", err)
	}
	if _, err := b.Resume(g.ID, "alice", good); !errors.Is(err, ErrDenied) {
		t.Fatalf("a spent pause cannot be resumed again: %v", err)
	}
}

func TestADifferentPersonCompletingTheChallengeEndsTheGrantAndItsWork(t *testing.T) {
	r := newSig(t)
	g := r.signIn()
	b := r.b
	cancelled := 0
	b.Track(g.ID, "run-1", func() { cancelled++ })
	if _, err := b.Pause(g.ID, "alice", ChallengePasskey); err != nil {
		t.Fatal(err)
	}
	r.tick(time.Minute)
	o := r.obs("bs_alice1", nil)
	o.PrincipalRef = "pr_two"
	if _, err := b.Resume(g.ID, "alice", o); !errors.Is(err, ErrDenied) {
		t.Fatalf("%v", err)
	}
	if cur, _ := b.Store.Get(g.ID); cur.State != StateRevoked || cancelled != 1 {
		t.Fatalf("the earlier person's work must not continue as someone else: %v %d", cur.State, cancelled)
	}
}

func TestAPauseNobodyAnswersEndsTheGrantAndSweepCancelsWhatDependedOnIt(t *testing.T) {
	r := newSig(t)
	g := r.signIn()
	b, cl := r.b, r.cl
	var calls []string
	b.Track(g.ID, "run-1", func() { calls = append(calls, "run-1") })
	b.Track(g.ID, "run-2", func() { calls = append(calls, "run-2") })
	b.Untrack(g.ID, "run-2")
	_, _ = b.Pause(g.ID, "alice", ChallengeMFA)
	cl.t = cl.t.Add(pauseTTL - time.Second)
	if b.Sweep() != 0 {
		t.Fatal("a pause still within its time stays")
	}
	cl.t = cl.t.Add(2 * time.Second)
	if n := b.Sweep(); n != 1 {
		t.Fatalf("%d", n)
	}
	if cur, _ := b.Store.Get(g.ID); cur.State != StateRevoked || len(calls) != 1 || calls[0] != "run-1" {
		t.Fatalf("%v %v", cur.State, calls)
	}
	if b.Sweep() != 0 || len(calls) != 1 {
		t.Fatal("cancellation happens once")
	}
	other, _ := b.Issue(grant("alice"))
	b.Track(other.ID, "run-9", func() { calls = append(calls, "run-9") })
	cl.t = other.ExpiresAt
	if b.Sweep() != 1 || len(calls) != 2 || calls[1] != "run-9" {
		t.Fatalf("an expired grant cancels its work too: %v", calls)
	}
}

func TestResumeAfterThePauseExpiredIsRefusedAndEndsTheGrant(t *testing.T) {
	r := newSig(t)
	g := r.signIn()
	_, _ = r.b.Pause(g.ID, "alice", ChallengeReauth)
	r.tick(pauseTTL - time.Second)
	o := r.obs("bs_alice1", nil)
	r.tick(time.Second)
	if _, err := r.b.Resume(g.ID, "alice", o); !errors.Is(err, ErrExpired) {
		t.Fatalf("%v", err)
	}
	if cur, _ := r.b.Store.Get(g.ID); cur.State != StateRevoked {
		t.Fatal("a stale pause is not left open")
	}
}

func TestSigningOutRevokingAndCancellingInvalidateWhatDependsOnTheSignIn(t *testing.T) {
	r := newSig(t)
	g := r.signIn()
	b := r.b
	n := 0
	b.Track(g.ID, "a", func() { n++ })
	b.Track(g.ID, "b", func() { n++ })
	r.tick(time.Second)
	if _, err := r.br.Accept(r.ev("bs_alice1", func(e *Evidence) { e.Authenticated = false })); err != nil || n != 2 {
		t.Fatalf("%v %d", err, n)
	}
	if err := b.Revoke(g.ID, "mallory"); !errors.Is(err, ErrDenied) {
		t.Fatalf("only the owner revokes: %v", err)
	}
	if err := b.Revoke(g.ID, "alice"); err != nil || n != 2 {
		t.Fatalf("revoking twice is harmless and cancels nothing more: %v %d", err, n)
	}
	if _, err := b.Pause(g.ID, "alice", ChallengeMFA); !errors.Is(err, ErrDenied) {
		t.Fatalf("a revoked grant cannot be paused back to life: %v", err)
	}
	if b.Track(g.ID, "late", func() { n++ }) || n != 3 {
		t.Fatal("work that starts after the grant ended is cancelled at once")
	}

	o := NewOAuth(b)
	rg := newRig(t)
	u, _ := o.Begin("alice", "acme", rg.idp.provider())
	state, code := rg.idp.authorize(u)
	o.Cancel("mallory", state)
	o.Cancel("alice", state)
	if _, err := o.Complete("alice", state, code, ""); !errors.Is(err, ErrDenied) {
		t.Fatalf("a cancelled sign-in cannot be completed: %v", err)
	}
	r.br.Cancel("alice", mail)
	r.tick(time.Second)
	if _, err := r.br.Accept(r.ev("bs_alice1", nil)); !errors.Is(err, ErrDenied) {
		t.Fatalf("%v", err)
	}
}

func TestNothingInThePackageLetsVSFAnswerAChallenge(t *testing.T) {
	banned := []string{"submit", "answer", "solve", "otp", "totp", "password", "passkey", "code"}
	for _, typ := range []any{&Broker{}, &Browser{}, &OAuth{}, &Credentials{}, &Lease{}} {
		rt := reflect.TypeOf(typ)
		for i := 0; i < rt.NumMethod(); i++ {
			name := strings.ToLower(rt.Method(i).Name)
			for _, b := range banned {
				if strings.Contains(name, b) {
					t.Fatalf("%s.%s suggests VSF can answer a challenge", rt.Elem().Name(), rt.Method(i).Name)
				}
			}
		}
	}
}

type fixedSessions map[string]execution.Session

func (f fixedSessions) Lookup(id string) (execution.Session, bool) { s, ok := f[id]; return s, ok }

func TestTheRuntimeSeesPausedExpiredAndEndedSignInsAsSuch(t *testing.T) {
	r := newSig(t)
	g := r.signIn()
	b, cl := r.b, r.cl
	inner := fixedSessions{"s1": {ID: "s1", Owner: "alice", AppID: "app.mail", Origin: mail.Origin, RuntimeID: "rt_1", PrincipalRef: "pr_one", Authenticated: true},
		"s3": {ID: "s3", Owner: "alice", AppID: "app.mail", Origin: mail.Origin, RuntimeID: "rt_1", PrincipalRef: "pr_someone_else", Authenticated: true},
		"s2": {ID: "s2", Owner: "bob", AppID: "app.mail", Origin: mail.Origin, Authenticated: true}}
	s := Sessions{Broker: b, Inner: inner}
	got, ok := s.Lookup("s1")
	if !ok || !got.Authenticated || got.Revoked || got.PrincipalRef != "pr_one" || !got.ExpiresAt.Equal(g.ExpiresAt) || len(got.SourceScopes) != 1 || got.RuntimeID != "rt_1" || got.Generation != g.Generation() {
		t.Fatalf("%+v", got)
	}
	if wrong, _ := s.Lookup("s3"); wrong.Authenticated {
		t.Fatalf("a sign-in by a different person than the runtime reports does not authenticate it: %+v", wrong)
	}
	if other, _ := s.Lookup("s2"); other.Authenticated || other.Revoked || other.PrincipalRef != "" {
		t.Fatalf("another caller's session is untouched by alice's sign-in: %+v", other)
	}
	_, _ = b.Pause(g.ID, "alice", ChallengeMFA)
	if got, _ := s.Lookup("s1"); got.Authenticated || got.Revoked {
		t.Fatalf("a paused sign-in is unauthenticated, not revoked: %+v", got)
	}
	cl.t = cl.t.Add(time.Second)
	_, _ = b.Resume(g.ID, "alice", r.obs("bs_alice1", nil))
	cl.t = g.ExpiresAt
	if got, _ := s.Lookup("s1"); got.Authenticated {
		t.Fatalf("an expired sign-in no longer authenticates: %+v", got)
	}
	cl.t = epoch.Add(2 * time.Second)
	_, _ = r.br.Accept(r.ev("bs_alice1", func(e *Evidence) { e.Authenticated = false }))
	if got, _ := s.Lookup("s1"); got.Authenticated || !got.Revoked {
		t.Fatalf("signing out revokes the session: %+v", got)
	}
	if _, ok := s.Lookup("missing"); ok {
		t.Fatal("unknown sessions stay unknown")
	}
}
