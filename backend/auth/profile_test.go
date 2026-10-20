package auth

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func claim(mut func(*ProfileClaim)) *ProfileClaim {
	c := &ProfileClaim{Roles: []string{"member"}, Capabilities: []string{"mail.read"},
		Env: Env{Locale: "de-CH", Region: "eu", Variant: "standard", AppVersion: "4.2.0", Tenant: "t1", Capability: "cap-1"}}
	if mut != nil {
		mut(c)
	}
	return c
}

// profileRig signs alice in and returns a function that observes a profile one second after the last.
func profileRig(t *testing.T) (*Profiles, *sig, Grant, func(mut func(*ProfileClaim)) (Change, error)) {
	t.Helper()
	r := newSig(t)
	g := r.signIn()
	ps := NewProfiles(r.b)
	observe := func(mut func(*ProfileClaim)) (Change, error) {
		r.tick(time.Second)
		o := r.obs("bs_alice1", func(e *Evidence) { e.Profile = claim(mut) })
		return ps.Observe(o)
	}
	return ps, r, g, observe
}

func TestARolesEnvironmentOrExpiryChangeInvalidatesPlansBuiltEarlier(t *testing.T) {
	ps, r, g, observe := profileRig(t)
	ch, err := observe(nil)
	if err != nil || !ch.New {
		t.Fatalf("%+v %v", ch, err)
	}
	ref, err := ps.Bind("plan-1", "alice", g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ok, why := ps.Valid(ref); !ok || why != ReasonOK {
		t.Fatalf("%v %v", ok, why)
	}
	if ch, _ := observe(func(c *ProfileClaim) { c.Capabilities = []string{"mail.read", "mail.read"} }); ch.Any() {
		t.Fatalf("order, duplicates and observation time are not changes: %+v", ch)
	}
	if ok, _ := ps.Valid(ref); !ok {
		t.Fatal("still valid")
	}
	if ch, _ := observe(func(c *ProfileClaim) { c.Roles = []string{"member", "admin"} }); !ch.Roles {
		t.Fatalf("%+v", ch)
	}
	if ok, why := ps.Valid(ref); ok || why != ReasonRoles {
		t.Fatalf("a role change invalidates: %v %v", ok, why)
	}
	ref2, _ := ps.Bind("plan-2", "alice", g.ID)
	if ok, _ := ps.Valid(ref2); !ok {
		t.Fatal("rebuilt plan is valid")
	}
	if ch, _ := observe(func(c *ProfileClaim) { c.Roles = []string{"member", "admin"}; c.Env.Locale = "fr-CH" }); !ch.Env {
		t.Fatalf("%+v", ch)
	}
	if ok, why := ps.Valid(ref2); ok || why != ReasonEnv {
		t.Fatalf("a locale change invalidates: %v %v", ok, why)
	}
	ref3, _ := ps.Bind("plan-3", "alice", g.ID)
	r.cl.t = g.ExpiresAt
	if ok, why := ps.Valid(ref3); ok || why != ReasonExpired {
		t.Fatalf("session expiry invalidates: %v %v", ok, why)
	}
}

func TestAPlanIsInvalidAfterAnyEnvironmentFieldChanges(t *testing.T) {
	for name, mut := range map[string]func(*ProfileClaim){
		"application version": func(c *ProfileClaim) { c.Env.AppVersion = "4.3.0" },
		"tenant":              func(c *ProfileClaim) { c.Env.Tenant = "t2" },
		"capability manifest": func(c *ProfileClaim) { c.Env.Capability = "cap-2" },
		"variant":             func(c *ProfileClaim) { c.Env.Variant = "enterprise" },
		"region":              func(c *ProfileClaim) { c.Env.Region = "us" },
		"capability removed":  func(c *ProfileClaim) { c.Capabilities = nil },
		"role removed":        func(c *ProfileClaim) { c.Roles = nil },
	} {
		ps, _, g, observe := profileRig(t)
		_, _ = observe(nil)
		ref, _ := ps.Bind("plan", "alice", g.ID)
		if _, err := observe(mut); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if ok, _ := ps.Valid(ref); ok {
			t.Fatalf("a change of %s must invalidate the plan", name)
		}
	}
}

func TestAPlanBuiltBeforeReauthenticationNeedsRebindingEvenWithIdenticalRoles(t *testing.T) {
	ps, r, g, observe := profileRig(t)
	_, _ = observe(nil)
	ref, _ := ps.Bind("plan-1", "alice", g.ID)
	r.tick(time.Second)
	if _, err := r.b.Pause(g.ID, "alice", ChallengeMFA); err != nil {
		t.Fatal(err)
	}
	r.tick(time.Second)
	if _, err := r.br.Accept(r.ev("bs_alice1", nil)); err != nil {
		t.Fatalf("an all-clear after the challenge resumes the grant: %v", err)
	}
	cur, _ := r.b.Store.Get(g.ID)
	if cur.State != StateActive || cur.Epoch != 2 {
		t.Fatalf("%+v", cur)
	}
	if ok, why := ps.Valid(ref); ok || why != ReasonEpoch {
		t.Fatalf("identical roles under a new sign-in epoch are still a different authority: %v %v", ok, why)
	}
	if _, err := ps.Bind("plan-2", "alice", g.ID); !errors.Is(err, ErrReauth) {
		t.Fatalf("no plan binds until the profile is observed in the new epoch: %v", err)
	}
	if _, err := ps.Rebind(ref); !errors.Is(err, ErrReauth) {
		t.Fatalf("nor can the old one be carried over blindly: %v", err)
	}
	if _, err := observe(nil); err != nil {
		t.Fatal(err)
	}
	re, err := ps.Rebind(ref)
	if err != nil || re.Epoch != 2 {
		t.Fatalf("an unchanged profile lets the plan be rebound explicitly: %+v %v", re, err)
	}
	if ok, _ := ps.Valid(re); !ok {
		t.Fatal("the rebound plan is valid")
	}
	if ok, _ := ps.Valid(ref); ok {
		t.Fatal("the old reference stays invalid")
	}
	_, _ = observe(func(c *ProfileClaim) { c.Roles = []string{"guest"} })
	if _, err := ps.Rebind(ref); !errors.Is(err, ErrStale) {
		t.Fatalf("a plan whose authority changed must be built again: %v", err)
	}
}

func TestAPlanBuiltUnderAnEarlierGrantDoesNotSurviveANewSignInAsTheSamePerson(t *testing.T) {
	ps, r, g, observe := profileRig(t)
	_, _ = observe(nil)
	ref, _ := ps.Bind("plan-1", "alice", g.ID)
	r.tick(time.Second)
	_, _ = r.br.Accept(r.ev("bs_alice1", func(e *Evidence) { e.Authenticated = false }))
	r.tick(time.Second)
	_ = r.br.Expect("alice", "acme", mail, []string{"mail.read"})
	g2, err := r.br.Accept(r.ev("bs_alice1", nil))
	if err != nil || g2.ID == g.ID {
		t.Fatalf("%+v %v", g2, err)
	}
	_, _ = observe(nil)
	if ok, why := ps.Valid(ref); ok || why != ReasonRevoked {
		t.Fatalf("%v %v", ok, why)
	}
	if _, err := ps.Rebind(ref); !errors.Is(err, ErrDenied) {
		t.Fatalf("a revoked grant's plan is never rebound to its successor: %v", err)
	}
}

func TestAStaleReplayedOrOutOfOrderProfileCannotReplaceNewerAuthority(t *testing.T) {
	ps, r, _, observe := profileRig(t)
	_, _ = observe(func(c *ProfileClaim) { c.Roles = []string{"admin"} })
	older := r.ev("bs_alice1", func(e *Evidence) {
		e.Profile = claim(func(c *ProfileClaim) { c.Roles = []string{"owner"} })
		e.ObservedAt = r.cl.t.Add(-2 * time.Second)
	})
	r.tick(time.Second)
	newer := r.ev("bs_alice1", func(e *Evidence) { e.Profile = claim(func(c *ProfileClaim) { c.Roles = []string{"guest"} }) })
	on, err := r.br.Verify(newer)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ps.Observe(on); err != nil {
		t.Fatal(err)
	}
	if _, err := r.br.Verify(older); !errors.Is(err, ErrStale) {
		t.Fatalf("an older message arriving late is refused by the authority: %v", err)
	}
	if _, err := ps.Observe(on); !errors.Is(err, ErrStale) {
		t.Fatalf("a verified observation cannot be applied twice: %v", err)
	}
	if _, err := r.br.Verify(newer); !errors.Is(err, ErrStale) {
		t.Fatalf("replaying the message fails too: %v", err)
	}
	for _, s := range ps.byGrant {
		if !sameSet(s.p.Roles, []string{"guest"}) {
			t.Fatalf("newer authority was replaced: %v", s.p.Roles)
		}
	}
}

func TestAProfileNeedsAProvenObservationFromTheGrantsOwnRuntime(t *testing.T) {
	ps, r, _, _ := profileRig(t)
	r.tick(time.Second)
	good := r.obs("bs_alice1", func(e *Evidence) { e.Profile = claim(nil) })
	forged := good
	forged.verified = false
	if _, err := ps.Observe(forged); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a hand-built observation is not evidence: %v", err)
	}
	noProfile := good
	noProfile.Profile = nil
	if _, err := ps.Observe(noProfile); !errors.Is(err, ErrInvalid) {
		t.Fatalf("%v", err)
	}
	other := good
	other.RuntimeID = "rt_other"
	if _, err := ps.Observe(other); !errors.Is(err, ErrReauth) {
		t.Fatalf("a different runtime than the grant is bound to is not heard: %v", err)
	}
	r.tick(2 * time.Minute)
	if _, err := ps.Observe(good); !errors.Is(err, ErrStale) {
		t.Fatalf("an observation that waited too long is stale: %v", err)
	}
	bob := r.obs("bs_bob1", func(e *Evidence) { e.Profile = claim(nil) })
	if _, err := ps.Observe(bob); !errors.Is(err, ErrDenied) {
		t.Fatalf("bob's runtime cannot set alice's profile, and he has no grant: %v", err)
	}
}

func TestAPlanIsInvalidWhileThePersonIsPausedAndAfterTheSignInEnds(t *testing.T) {
	ps, r, g, observe := profileRig(t)
	_, _ = observe(nil)
	ref, _ := ps.Bind("plan-1", "alice", g.ID)
	_, _ = r.b.Pause(g.ID, "alice", ChallengeMFA)
	if ok, why := ps.Valid(ref); ok || why != ReasonPaused {
		t.Fatalf("%v %v", ok, why)
	}
	_ = r.b.Revoke(g.ID, "alice")
	if ok, why := ps.Valid(ref); ok || why != ReasonRevoked {
		t.Fatalf("%v %v", ok, why)
	}
	if _, err := observe(nil); !errors.Is(err, ErrDenied) {
		t.Fatalf("a profile cannot outlive its grant: %v", err)
	}
}

func TestProfilesAreScopedToTheirOwnerGrantAndAccount(t *testing.T) {
	ps, r, g, observe := profileRig(t)
	_, _ = observe(nil)
	if _, err := ps.Bind("p", "mallory", g.ID); !errors.Is(err, ErrDenied) {
		t.Fatalf("%v", err)
	}
	ref, _ := ps.Bind("plan-1", "alice", g.ID)
	r.tick(time.Second)
	_ = r.br.Expect("bob", "globex", mail, []string{"mail.read"})
	if _, err := r.br.Accept(r.ev("bs_bob1", nil)); err != nil {
		t.Fatal(err)
	}
	r.tick(time.Second)
	if _, err := ps.Observe(r.obs("bs_bob1", func(e *Evidence) { e.Profile = claim(func(c *ProfileClaim) { c.Roles = []string{"owner"} }) })); err != nil {
		t.Fatal(err)
	}
	if ok, _ := ps.Valid(ref); !ok {
		t.Fatal("another caller's profile change does not touch alice's plan")
	}
	stolen := ref
	stolen.Owner = "bob"
	if ok, why := ps.Valid(stolen); ok || why != ReasonUnknownGrant {
		t.Fatalf("a plan reference cannot be replayed under another caller: %v %v", ok, why)
	}
	if _, err := ps.Bind("plan-9", "bob", "gr_unknown"); !errors.Is(err, ErrDenied) {
		t.Fatalf("%v", err)
	}
	if _, err := observe(func(c *ProfileClaim) { c.Roles = []string{"bad role"} }); !errors.Is(err, ErrInvalid) {
		t.Fatalf("%v", err)
	}
}

func TestProfileExpiryIsCappedByTheGrantAndAuditRecordsDescribeNothing(t *testing.T) {
	ps, r, g, _ := profileRig(t)
	r.tick(time.Second)
	long := r.obs("bs_alice1", func(e *Evidence) { e.Profile = claim(nil); e.Expires = g.ExpiresAt.Add(24 * time.Hour) })
	if _, err := ps.Observe(long); err != nil {
		t.Fatal(err)
	}
	if s := ps.byGrant[g.ID]; !s.p.ExpiresAt.Equal(g.ExpiresAt) {
		t.Fatalf("a profile never outlives the sign-in: %v", s.p.ExpiresAt)
	}
	ref, _ := ps.Bind("plan-1", "alice", g.ID)
	r.tick(time.Second)
	_, err := ps.Observe(r.obs("bs_alice1", func(e *Evidence) {
		e.Profile = claim(func(c *ProfileClaim) { c.Roles = []string{"confidential-role-name"}; c.Env.Locale = "xx-secret-locale" })
	}))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		ps.Valid(ref)
	}
	log := r.log.String()
	for _, leak := range []string{"confidential-role-name", "xx-secret-locale", "member", "de-CH", "mail.read"} {
		if strings.Contains(log, leak) {
			t.Fatalf("the audit log describes the profile (%q): %s", leak, log)
		}
	}
	if strings.Count(log, `"event":"plan_invalidated"`) != 1 {
		t.Fatalf("a plan is flagged once however often it is checked: %s", log)
	}
	if !strings.Contains(log, `"reason":"roles_changed"`) || !strings.Contains(log, `"reason":"environment_changed"`) || !strings.Contains(log, `"ref":"`) {
		t.Fatalf("%s", log)
	}
	if n, err := VerifyAudit(strings.NewReader(log)); err != nil || n < 4 {
		t.Fatalf("%d %v", n, err)
	}
}

func TestProfileDigestsAreCanonicalAndDeterministic(t *testing.T) {
	g := Grant{ID: "gr_x", PrincipalRef: "pr_one", Source: mail}
	a := Profile{GrantID: "gr_x", Epoch: 1, Roles: []string{"b", "a", "a"}, Capabilities: []string{"y", "x"}, RuntimeID: "rt_1"}
	b := Profile{GrantID: "gr_x", Epoch: 1, Roles: []string{"a", "b"}, Capabilities: []string{"x", "y", "x"}, RuntimeID: "rt_1", ObservedAt: epoch}
	c1, f1 := canonical(a, g)
	c2, f2 := canonical(b, g)
	if c1 != c2 || f1 != f2 {
		t.Fatal("order, duplicates and observation time do not change a digest")
	}
	a.Epoch = 2
	if c, f := canonical(a, g); c != c1 || f == f1 {
		t.Fatal("the epoch changes the sign-in digest but not the authority content")
	}
	a.Epoch, a.RuntimeID = 1, "rt_2"
	if _, f := canonical(a, g); f == f1 {
		t.Fatal("the runtime is part of the sign-in digest")
	}
}
