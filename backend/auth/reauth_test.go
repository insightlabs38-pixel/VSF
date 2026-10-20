package auth

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/insightlabs38-pixel/vsf/backend/execution"
)

func signedIn(t *testing.T) (*Browser, *Broker, *clock, Grant) {
	t.Helper()
	b, cl, _ := newBroker(t)
	b.Vault = NewMemVault()
	br := NewBrowser(b)
	_ = br.Expect("alice", "acme", mail, []string{"mail.read"})
	g, err := br.Observe(Observation{Owner: "alice", Source: mail, PrincipalRef: "pr_one", Authenticated: true, Scopes: []string{"mail.read"}})
	if err != nil {
		t.Fatal(err)
	}
	return br, b, cl, g
}

func fresh(cl *clock, mut func(*Observation)) Observation {
	o := Observation{Owner: "alice", Source: mail, PrincipalRef: "pr_one", Authenticated: true, ObservedAt: cl.t.Add(time.Second)}
	if mut != nil {
		mut(&o)
	}
	return o
}

func TestAChallengeAtTheSourcePausesTheGrantAndOnlyAFreshCleanObservationResumesIt(t *testing.T) {
	br, b, cl, g := signedIn(t)
	cancelled := 0
	b.Track(g.ID, "run-1", func() { cancelled++ })
	cl.t = cl.t.Add(time.Minute)
	if _, err := br.Observe(Observation{Owner: "alice", Source: mail, PrincipalRef: "pr_one", Authenticated: true, Challenge: ChallengeMFA}); err != nil {
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
	// A heartbeat that says "still signed in" while the prompt is open must not resume anything.
	if got, err := br.Observe(Observation{Owner: "alice", Source: mail, PrincipalRef: "pr_one", Authenticated: true}); err != nil || got.State != StatePaused {
		t.Fatalf("%+v %v", got, err)
	}
	cases := map[string]struct {
		owner string
		obs   Observation
		want  error
	}{
		"another caller":                    {"mallory", fresh(cl, func(o *Observation) { o.Owner = "mallory" }), ErrDenied},
		"observation names another source":  {"alice", fresh(cl, func(o *Observation) { o.Source = Source{AppID: "app.mail", Origin: "https://evil.example.com"} }), ErrDenied},
		"observation from before the pause": {"alice", fresh(cl, func(o *Observation) { o.ObservedAt = cl.t.Add(-time.Hour) }), ErrReauth},
		"no observation time":               {"alice", fresh(cl, func(o *Observation) { o.ObservedAt = time.Time{} }), ErrReauth},
		"challenge still showing":           {"alice", fresh(cl, func(o *Observation) { o.Challenge = ChallengeMFA }), ErrReauth},
		"signed out":                        {"alice", fresh(cl, func(o *Observation) { o.Authenticated = false }), ErrReauth},
	}
	for name, c := range cases {
		if _, err := b.Resume(g.ID, c.owner, c.obs); !errors.Is(err, c.want) {
			t.Fatalf("%s: %v", name, err)
		}
		if !b.Paused(g.ID) {
			t.Fatalf("%s lifted the pause", name)
		}
	}
	got, err := b.Resume(g.ID, "alice", fresh(cl, nil))
	if err != nil || got.State != StateActive || got.Epoch != 2 || b.Paused(g.ID) {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := b.Authorize(Check{GrantID: g.ID, Owner: "alice", Origin: mail.Origin, Audience: g.Audience}); err != nil {
		t.Fatalf("%v", err)
	}
	if _, err := b.Resume(g.ID, "alice", fresh(cl, nil)); !errors.Is(err, ErrDenied) {
		t.Fatalf("a spent pause cannot be resumed again: %v", err)
	}
}

func TestADifferentPersonCompletingTheChallengeEndsTheGrantAndItsWork(t *testing.T) {
	_, b, cl, g := signedIn(t)
	cancelled := 0
	b.Track(g.ID, "run-1", func() { cancelled++ })
	if _, err := b.Pause(g.ID, "alice", ChallengePasskey); err != nil {
		t.Fatal(err)
	}
	cl.t = cl.t.Add(time.Minute)
	if _, err := b.Resume(g.ID, "alice", fresh(cl, func(o *Observation) { o.PrincipalRef = "pr_two" })); !errors.Is(err, ErrDenied) {
		t.Fatalf("%v", err)
	}
	if cur, _ := b.Store.Get(g.ID); cur.State != StateRevoked || cancelled != 1 {
		t.Fatalf("the earlier person's work must not continue as someone else: %v %d", cur.State, cancelled)
	}
}

func TestAPauseNobodyAnswersEndsTheGrantAndSweepCancelsWhatDependedOnIt(t *testing.T) {
	_, b, cl, g := signedIn(t)
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
	_, b, cl, g := signedIn(t)
	_, _ = b.Pause(g.ID, "alice", ChallengeReauth)
	cl.t = cl.t.Add(pauseTTL)
	if _, err := b.Resume(g.ID, "alice", fresh(cl, nil)); !errors.Is(err, ErrExpired) {
		t.Fatalf("%v", err)
	}
	if cur, _ := b.Store.Get(g.ID); cur.State != StateRevoked {
		t.Fatal("a stale pause is not left open")
	}
}

func TestSigningOutRevokingAndCancellingInvalidateWhatDependsOnTheSignIn(t *testing.T) {
	br, b, _, g := signedIn(t)
	n := 0
	b.Track(g.ID, "a", func() { n++ })
	b.Track(g.ID, "b", func() { n++ })
	if _, err := br.Observe(Observation{Owner: "alice", Source: mail, Authenticated: false}); err != nil || n != 2 {
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

	o := NewOAuth(b)
	r := newRig(t)
	u, _ := o.Begin("alice", "acme", r.idp.provider())
	state, code := r.idp.authorize(u)
	o.Cancel("mallory", state)
	o.Cancel("alice", state)
	if _, err := o.Complete("alice", state, code, ""); !errors.Is(err, ErrDenied) {
		t.Fatalf("a cancelled sign-in cannot be completed: %v", err)
	}
	br.Cancel("alice", mail)
	if _, err := br.Observe(Observation{Owner: "alice", Source: mail, PrincipalRef: "pr_one", Authenticated: true}); !errors.Is(err, ErrDenied) {
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
	br, b, cl, g := signedIn(t)
	inner := fixedSessions{"s1": {ID: "s1", Owner: "alice", AppID: "app.mail", RuntimeID: "rt1", Authenticated: true},
		"s2": {ID: "s2", Owner: "bob", AppID: "app.mail", Authenticated: true}}
	s := Sessions{Broker: b, Inner: inner}
	got, ok := s.Lookup("s1")
	if !ok || !got.Authenticated || got.Revoked || got.PrincipalRef != "pr_one" || !got.ExpiresAt.Equal(g.ExpiresAt) || len(got.SourceScopes) != 1 || got.RuntimeID != "rt1" {
		t.Fatalf("%+v", got)
	}
	if other, _ := s.Lookup("s2"); other.Authenticated || other.Revoked || other.PrincipalRef != "" {
		t.Fatalf("another caller's session is untouched by alice's sign-in: %+v", other)
	}
	_, _ = b.Pause(g.ID, "alice", ChallengeMFA)
	if got, _ := s.Lookup("s1"); got.Authenticated || got.Revoked {
		t.Fatalf("a paused sign-in is unauthenticated, not revoked: %+v", got)
	}
	cl.t = cl.t.Add(time.Second)
	_, _ = b.Resume(g.ID, "alice", fresh(cl, nil))
	cl.t = g.ExpiresAt
	if got, _ := s.Lookup("s1"); got.Authenticated {
		t.Fatalf("an expired sign-in no longer authenticates: %+v", got)
	}
	cl.t = epoch
	_, _ = br.Observe(Observation{Owner: "alice", Source: mail, Authenticated: false})
	if got, _ := s.Lookup("s1"); got.Authenticated || !got.Revoked {
		t.Fatalf("signing out revokes the session: %+v", got)
	}
	if _, ok := s.Lookup("missing"); ok {
		t.Fatal("unknown sessions stay unknown")
	}
}
