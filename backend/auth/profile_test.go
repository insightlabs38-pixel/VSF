package auth

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

func profileRig(t *testing.T) (*Profiles, *Broker, *clock, Grant, *bytes.Buffer) {
	t.Helper()
	b, cl, buf := newBroker(t)
	b.Vault = NewMemVault()
	br := NewBrowser(b)
	_ = br.Expect("alice", "acme", mail, []string{"mail.read"})
	g, err := br.Observe(Observation{Owner: "alice", Source: mail, PrincipalRef: "pr_one", Authenticated: true, Scopes: []string{"mail.read"}})
	if err != nil {
		t.Fatal(err)
	}
	return NewProfiles(b), b, cl, g, buf
}

func prof(g Grant, cl *clock, mut func(*Profile)) Profile {
	p := Profile{GrantID: g.ID, Roles: []string{"member"}, Capabilities: []string{"mail.read"}, Env: Env{Locale: "de-CH", Region: "eu", Variant: "standard", AppVersion: "4.2.0"}, ObservedAt: cl.t}
	if mut != nil {
		mut(&p)
	}
	return p
}

func TestARolesEnvironmentOrExpiryChangeInvalidatesPlansBuiltEarlier(t *testing.T) {
	ps, b, cl, g, _ := profileRig(t)
	ch, err := ps.Observe("alice", prof(g, cl, nil))
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
	same := prof(g, cl, func(p *Profile) {
		p.Roles = []string{"member"}
		p.Capabilities = []string{"mail.read", "mail.read"}
		p.ObservedAt = cl.t.Add(time.Minute)
	})
	if ch, _ := ps.Observe("alice", same); ch.Any() {
		t.Fatalf("order, duplicates and observation time are not changes: %+v", ch)
	}
	if ok, _ := ps.Valid(ref); !ok {
		t.Fatal("still valid")
	}
	if ch, _ := ps.Observe("alice", prof(g, cl, func(p *Profile) { p.Roles = []string{"member", "admin"} })); !ch.Roles {
		t.Fatalf("%+v", ch)
	}
	if ok, why := ps.Valid(ref); ok || why != ReasonRoles {
		t.Fatalf("a role change invalidates: %v %v", ok, why)
	}
	// A plan built under the new profile is fine until the environment changes.
	ref2, _ := ps.Bind("plan-2", "alice", g.ID)
	if ok, _ := ps.Valid(ref2); !ok {
		t.Fatal("rebuilt plan is valid")
	}
	if ch, _ := ps.Observe("alice", prof(g, cl, func(p *Profile) { p.Roles = []string{"member", "admin"}; p.Env.Locale = "fr-CH" })); !ch.Env {
		t.Fatalf("%+v", ch)
	}
	if ok, why := ps.Valid(ref2); ok || why != ReasonEnv {
		t.Fatalf("a locale change invalidates: %v %v", ok, why)
	}
	ref3, _ := ps.Bind("plan-3", "alice", g.ID)
	cl.t = g.ExpiresAt
	if ok, why := ps.Valid(ref3); ok || why != ReasonProfileExpired {
		t.Fatalf("session expiry invalidates: %v %v", ok, why)
	}
	_ = b
}

func TestAPlanIsInvalidWhileThePersonIsPausedAndAfterTheSignInEnds(t *testing.T) {
	ps, b, cl, g, _ := profileRig(t)
	_, _ = ps.Observe("alice", prof(g, cl, nil))
	ref, _ := ps.Bind("plan-1", "alice", g.ID)
	_, _ = b.Pause(g.ID, "alice", ChallengeMFA)
	if ok, why := ps.Valid(ref); ok || why != ReasonPaused {
		t.Fatalf("%v %v", ok, why)
	}
	_ = b.Revoke(g.ID, "alice")
	if ok, why := ps.Valid(ref); ok || why != ReasonRevoked {
		t.Fatalf("%v %v", ok, why)
	}
	if _, err := ps.Observe("alice", prof(g, cl, nil)); !errors.Is(err, ErrDenied) {
		t.Fatalf("a profile cannot outlive its grant: %v", err)
	}
}

func TestProfilesAreScopedToTheirOwnerGrantAndAccount(t *testing.T) {
	ps, b, cl, g, _ := profileRig(t)
	if _, err := ps.Observe("mallory", prof(g, cl, nil)); !errors.Is(err, ErrDenied) {
		t.Fatalf("another caller cannot set alice's profile: %v", err)
	}
	_, _ = ps.Observe("alice", prof(g, cl, nil))
	if _, err := ps.Bind("p", "mallory", g.ID); !errors.Is(err, ErrDenied) {
		t.Fatalf("%v", err)
	}
	ref, _ := ps.Bind("plan-1", "alice", g.ID)
	bob := grant("bob")
	gb, _ := b.Issue(bob)
	_, _ = ps.Observe("bob", prof(gb, cl, func(p *Profile) { p.Roles = []string{"owner"} }))
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
	g2, _ := b.Issue(grant("alice"))
	if _, err := ps.Bind("plan-2", "alice", g2.ID); !errors.Is(err, ErrReauth) {
		t.Fatalf("a plan cannot be bound before a profile is known: %v", err)
	}
	if _, err := ps.Observe("alice", prof(g, cl, func(p *Profile) { p.Roles = []string{"bad role"} })); !errors.Is(err, ErrInvalid) {
		t.Fatalf("%v", err)
	}
}

func TestProfileExpiryIsCappedByTheGrantAndAuditRecordsDescribeNothing(t *testing.T) {
	ps, _, cl, g, audit := profileRig(t)
	long := prof(g, cl, func(p *Profile) { p.ExpiresAt = g.ExpiresAt.Add(24 * time.Hour) })
	_, _ = ps.Observe("alice", long)
	if s := ps.byGrant[g.ID]; !s.p.ExpiresAt.Equal(g.ExpiresAt) {
		t.Fatalf("a profile never outlives the sign-in: %v", s.p.ExpiresAt)
	}
	ref, _ := ps.Bind("plan-1", "alice", g.ID)
	_, _ = ps.Observe("alice", prof(g, cl, func(p *Profile) {
		p.Roles = []string{"confidential-role-name"}
		p.Env.Locale = "xx-secret-locale"
	}))
	for i := 0; i < 3; i++ {
		ps.Valid(ref)
	}
	log := audit.String()
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
