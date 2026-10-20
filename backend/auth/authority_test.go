package auth

import (
	"errors"
	"testing"
	"time"
)

func TestForgedMismatchedOrUnprovenEvidenceMintsNothing(t *testing.T) {
	cases := map[string]struct {
		mut  func(*Evidence)
		want error
	}{
		"unknown runtime session":                  {func(e *Evidence) { e.SessionID = "bs_nobody" }, ErrDenied},
		"another client's pairing":                 {func(e *Evidence) { e.Client = "cl_bob" }, ErrDenied},
		"an unpaired local caller":                 {func(e *Evidence) { e.Client = "cl_unpaired" }, ErrDenied},
		"another origin":                           {func(e *Evidence) { e.Source.Origin = "https://evil.example.com" }, ErrDenied},
		"another application":                      {func(e *Evidence) { e.Source.AppID = "app.other" }, ErrDenied},
		"a principal the runtime never registered": {func(e *Evidence) { e.PrincipalRef = "pr_admin" }, ErrDenied},
		"no principal":                             {func(e *Evidence) { e.PrincipalRef = "" }, ErrDenied},
		"no sequence number":                       {func(e *Evidence) { e.Seq = 0 }, ErrInvalid},
		"no observation time":                      {func(e *Evidence) { e.ObservedAt = time.Time{} }, ErrInvalid},
		"a negative epoch":                         {func(e *Evidence) { e.GrantEpoch = -1 }, ErrInvalid},
		"an unknown challenge":                     {func(e *Evidence) { e.Challenge = "otp-entered" }, ErrInvalid},
		"a malformed scope":                        {func(e *Evidence) { e.Scopes = []string{"mail read"} }, ErrInvalid},
		"an old message":                           {func(e *Evidence) { e.ObservedAt = e.ObservedAt.Add(-time.Minute) }, ErrStale},
		"a message from the future":                {func(e *Evidence) { e.ObservedAt = e.ObservedAt.Add(time.Minute) }, ErrStale},
	}
	for name, c := range cases {
		r := newSig(t)
		_ = r.br.Expect("alice", "acme", mail, []string{"mail.read"})
		if g, err := r.br.Accept(r.ev("bs_alice1", c.mut)); !errors.Is(err, c.want) {
			t.Fatalf("%s: %v %+v", name, err, g)
		}
		if len(r.b.List("alice")) != 0 || r.br.Authenticated("alice", mail) {
			t.Fatalf("%s minted a grant", name)
		}
		// the expected sign-in survives a refused message
		if _, err := r.br.Accept(r.ev("bs_alice1", nil)); err != nil {
			t.Fatalf("%s: the real message still works: %v", name, err)
		}
	}
}

func TestWithoutABridgeOrAManifestAppIdentityNothingIsProven(t *testing.T) {
	r := newSig(t)
	_ = r.br.Expect("alice", "acme", mail, []string{"mail.read"})
	noBridge := NewBrowser(r.b, nil)
	if _, err := noBridge.Accept(r.ev("bs_alice1", nil)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("%v", err)
	}
	r.rt.put(RuntimeSession{ID: "bs_noapp", Client: "cl_alice", Owner: "alice", Origin: mail.Origin, Principal: "pr_one", Runtime: "rt_x"})
	if _, err := r.br.Accept(r.ev("bs_noapp", func(e *Evidence) { e.Client = "cl_alice"; e.Source = mail })); !errors.Is(err, ErrDenied) {
		t.Fatalf("a session whose manifest names no application proves nothing about one: %v", err)
	}
	if _, err := r.br.Apply(Observation{Owner: "alice", Source: mail, PrincipalRef: "pr_one", Authenticated: true}); !errors.Is(err, ErrDenied) {
		t.Fatalf("a hand-built observation is not evidence: %v", err)
	}
	if len(r.b.List("alice")) != 0 {
		t.Fatal("nothing minted")
	}
}

func TestReplayedAndOutOfOrderMessagesCannotMintRestoreOrEndAGrant(t *testing.T) {
	r := newSig(t)
	_ = r.br.Expect("alice", "acme", mail, []string{"mail.read"})
	in := r.ev("bs_alice1", nil)
	g, err := r.br.Accept(in)
	if err != nil {
		t.Fatal(err)
	}
	r.tick(time.Second)
	out := r.ev("bs_alice1", func(e *Evidence) { e.Authenticated = false })
	if _, err := r.br.Accept(out); err != nil {
		t.Fatal(err)
	}
	if cur, _ := r.b.Store.Get(g.ID); cur.State != StateRevoked {
		t.Fatal("signed out")
	}
	_ = r.br.Expect("alice", "acme", mail, []string{"mail.read"})
	if _, err := r.br.Accept(in); !errors.Is(err, ErrStale) {
		t.Fatalf("replaying the old sign-in restores nothing: %v", err)
	}
	if _, err := r.br.Accept(out); !errors.Is(err, ErrStale) {
		t.Fatalf("replaying the sign-out is refused too: %v", err)
	}
	late := r.ev("bs_alice1", func(e *Evidence) { e.ObservedAt = r.cl.t.Add(-2 * time.Second) })
	if _, err := r.br.Accept(late); !errors.Is(err, ErrStale) {
		t.Fatalf("a sign-in observed before the sign-out cannot undo it, even with a fresh sequence number: %v", err)
	}
	if r.br.Authenticated("alice", mail) {
		t.Fatal("still signed out")
	}
	r.tick(time.Second)
	if _, err := r.br.Accept(r.ev("bs_alice1", nil)); err != nil {
		t.Fatalf("a genuinely new sign-in works: %v", err)
	}
}

func TestAStaleSignOutFromAnOlderEpochCannotEndANewerSignIn(t *testing.T) {
	r := newSig(t)
	g := r.signIn()
	r.tick(time.Second)
	_, _ = r.b.Pause(g.ID, "alice", ChallengeMFA)
	r.tick(time.Second)
	if _, err := r.br.Accept(r.ev("bs_alice1", nil)); err != nil {
		t.Fatal(err)
	}
	cur, _ := r.b.Store.Get(g.ID)
	if cur.Epoch != 2 {
		t.Fatalf("%+v", cur)
	}
	r.tick(time.Second)
	if _, err := r.br.Accept(r.ev("bs_alice1", func(e *Evidence) { e.Authenticated = false; e.GrantEpoch = 1 })); !errors.Is(err, ErrStale) {
		t.Fatalf("%v", err)
	}
	if cur, _ := r.b.Store.Get(g.ID); cur.State != StateActive {
		t.Fatal("the newer sign-in stands")
	}
	r.tick(time.Second)
	if _, err := r.br.Accept(r.ev("bs_alice1", func(e *Evidence) { e.Authenticated = false; e.GrantEpoch = 2 })); err != nil {
		t.Fatal(err)
	}
	if cur, _ := r.b.Store.Get(g.ID); cur.State != StateRevoked {
		t.Fatal("a current sign-out ends it")
	}
}

func TestARuntimeRestartIsANewGenerationOfTheSameSignIn(t *testing.T) {
	r := newSig(t)
	g := r.signIn()
	cancelled := 0
	r.b.Track(g.ID, "run", func() { cancelled++ })
	r.tick(time.Second)
	r.rt.end("bs_alice1")
	if _, err := r.br.Accept(r.ev("bs_alice1", nil)); !errors.Is(err, ErrDenied) {
		t.Fatalf("an ended runtime session proves nothing: %v", err)
	}
	if cur, _ := r.b.Store.Get(g.ID); !cur.ExpiresAt.Equal(g.ExpiresAt) {
		t.Fatal("and extends nothing")
	}
	r.session("bs_alice1b", "alice", "pr_one", "rt_1b", mail)
	g2, err := r.br.Accept(r.ev("bs_alice1b", nil))
	if err != nil || g2.ID != g.ID || g2.Epoch != 2 || g2.RuntimeID != "rt_1b" || g2.SessionID != "bs_alice1b" {
		t.Fatalf("the restarted runtime continues the same grant under a new epoch: %+v %v", g2, err)
	}
	if cancelled != 1 {
		t.Fatal("work started under the old runtime generation is cancelled")
	}
	s := Sessions{Broker: r.b, Inner: fixedSessions{
		"old": innerSession("old", "alice", mail.AppID, mail.Origin, "pr_one", "rt_1"),
		"new": innerSession("new", "alice", mail.AppID, mail.Origin, "pr_one", "rt_1b"),
	}}
	if o, _ := s.Lookup("old"); o.Authenticated {
		t.Fatal("the old runtime is no longer bound")
	}
	if n, _ := s.Lookup("new"); !n.Authenticated {
		t.Fatal("the new one is")
	}
}

func TestReplacingThePersonWithoutAnExpectedSignInEndsTheOldGrantAndCreatesNone(t *testing.T) {
	r := newSig(t)
	g := r.signIn()
	r.session("bs_alice2", "alice", "pr_two", "rt_2", mail)
	r.tick(time.Second)
	if _, err := r.br.Accept(r.ev("bs_alice2", nil)); !errors.Is(err, ErrDenied) {
		t.Fatalf("%v", err)
	}
	if cur, _ := r.b.Store.Get(g.ID); cur.State != StateRevoked {
		t.Fatal("someone else on the page ends the earlier person's sign-in")
	}
	if r.br.Authenticated("alice", mail) {
		t.Fatal("and nobody is signed in on VSF's say-so")
	}
}

func TestVSFNeverAnswersAChallengeItJustWaits(t *testing.T) {
	r := newSig(t)
	g := r.signIn()
	r.tick(time.Second)
	if got, err := r.br.Accept(r.ev("bs_alice1", func(e *Evidence) { e.Challenge = ChallengePasskey })); err != nil || got.State != StatePaused {
		t.Fatalf("%+v %v", got, err)
	}
	for i := 0; i < 3; i++ {
		r.tick(time.Second)
		got, err := r.br.Accept(r.ev("bs_alice1", func(e *Evidence) { e.Challenge = ChallengeMFA }))
		if err != nil || got.State != StatePaused || got.Epoch != g.Epoch {
			t.Fatalf("repeated prompts keep waiting: %+v %v", got, err)
		}
	}
}
