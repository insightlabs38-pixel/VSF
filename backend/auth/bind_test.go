package auth

import (
	"sync"
	"testing"
	"time"

	"github.com/insightlabs38-pixel/vsf/backend/execution"
)

func innerSession(id, owner, app, origin, principal, runtime string) execution.Session {
	return execution.Session{ID: id, Owner: owner, AppID: app, Origin: origin, PrincipalRef: principal, RuntimeID: runtime, Authenticated: true,
		SourceScopes: []string{"mail.read", "mail.admin"}}
}

func TestASessionBindsToExactlyOneGrantOrToNone(t *testing.T) {
	r := newSig(t)
	g := r.signIn()
	other := "https://mail2.example.com"
	inner := fixedSessions{
		"exact":        innerSession("exact", "alice", mail.AppID, mail.Origin, "pr_one", "rt_1"),
		"origin":       innerSession("origin", "alice", mail.AppID, other, "pr_one", "rt_1"),
		"app":          innerSession("app", "alice", "app.other", mail.Origin, "pr_one", "rt_1"),
		"principal":    innerSession("principal", "alice", mail.AppID, mail.Origin, "pr_two", "rt_1"),
		"runtime":      innerSession("runtime", "alice", mail.AppID, mail.Origin, "pr_one", "rt_9"),
		"noprincipal":  innerSession("noprincipal", "alice", mail.AppID, mail.Origin, "", "rt_1"),
		"noorigin":     innerSession("noorigin", "alice", mail.AppID, "", "pr_one", "rt_1"),
		"noruntime":    innerSession("noruntime", "alice", mail.AppID, mail.Origin, "pr_one", ""),
		"noowner":      innerSession("noowner", "", mail.AppID, mail.Origin, "pr_one", "rt_1"),
		"anotheraccnt": innerSession("anotheraccnt", "bob", mail.AppID, mail.Origin, "pr_one", "rt_1"),
	}
	s := Sessions{Broker: r.b, Inner: inner}
	got, _ := s.Lookup("exact")
	if !got.Authenticated || got.Generation != g.Generation() || len(got.SourceScopes) != 1 || got.SourceScopes[0] != "mail.read" {
		t.Fatalf("the bound grant's scopes only, not the bridge's wider ones: %+v", got)
	}
	for name := range inner {
		if name == "exact" {
			continue
		}
		if x, _ := s.Lookup(name); x.Authenticated || x.SourceScopes != nil || x.Generation != "" {
			t.Fatalf("%s must not be bound to alice's grant: %+v", name, x)
		}
	}
}

func TestSameApplicationOnTwoOriginsAndTwoPeopleKeepTheirOwnGrants(t *testing.T) {
	r := newSig(t)
	g1 := r.signIn()
	other := Source{AppID: mail.AppID, Origin: "https://mail2.example.com"}
	r.session("bs_alice_o2", "alice", "pr_one", "rt_o2", other)
	_ = r.br.Expect("alice", "acme", other, []string{"mail.admin"})
	r.tick(time.Second)
	g2, err := r.br.Accept(r.ev("bs_alice_o2", func(e *Evidence) { e.Scopes = []string{"mail.admin"} }))
	if err != nil || g2.ID == g1.ID {
		t.Fatalf("%+v %v", g2, err)
	}
	_ = r.br.Expect("bob", "globex", mail, []string{"mail.read"})
	r.tick(time.Second)
	gb, err := r.br.Accept(r.ev("bs_bob1", nil))
	if err != nil {
		t.Fatal(err)
	}
	s := Sessions{Broker: r.b, Inner: fixedSessions{
		"a1": innerSession("a1", "alice", mail.AppID, mail.Origin, "pr_one", "rt_1"),
		"a2": innerSession("a2", "alice", mail.AppID, other.Origin, "pr_one", "rt_o2"),
		"b1": innerSession("b1", "bob", mail.AppID, mail.Origin, "pr_bob", "rt_b"),
	}}
	a1, _ := s.Lookup("a1")
	a2, _ := s.Lookup("a2")
	b1, _ := s.Lookup("b1")
	if a1.Generation != g1.Generation() || a2.Generation != g2.Generation() || b1.Generation != gb.Generation() {
		t.Fatalf("each session is bound to its own grant: %q %q %q", a1.Generation, a2.Generation, b1.Generation)
	}
	if len(a1.SourceScopes) != 1 || a1.SourceScopes[0] != "mail.read" || len(a2.SourceScopes) != 1 || a2.SourceScopes[0] != "mail.admin" {
		t.Fatalf("scopes are never combined across grants: %v %v", a1.SourceScopes, a2.SourceScopes)
	}
	if err := r.b.Revoke(g2.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	a1, _ = s.Lookup("a1")
	a2, _ = s.Lookup("a2")
	if !a1.Authenticated || a2.Authenticated || !a2.Revoked || a1.Revoked {
		t.Fatalf("revoking one origin's grant touches no other: %+v %+v", a1, a2)
	}
}

func TestTwoLiveGrantsForOneSessionFailClosedWhateverTheStoreOrder(t *testing.T) {
	r := newSig(t)
	r.signIn()
	dup := Grant{Account: "acme", Owner: "alice", Source: mail, Audience: mail.Origin, Scopes: []string{"mail.admin", "mail.read"}, Method: MethodSourceBrowser,
		PrincipalRef: "pr_one", IdentityVerified: true, RuntimeID: "rt_1", SessionID: "bs_alice1", ObservedAt: r.cl.t, ExpiresAt: r.cl.t.Add(time.Hour)}
	if _, err := r.b.Issue(dup); err != nil {
		t.Fatal(err)
	}
	s := Sessions{Broker: r.b, Inner: fixedSessions{"s": innerSession("s", "alice", mail.AppID, mail.Origin, "pr_one", "rt_1")}}
	for range 50 {
		if x, _ := s.Lookup("s"); x.Authenticated || x.SourceScopes != nil {
			t.Fatalf("an ambiguous binding grants nothing, and never the union or the first found: %+v", x)
		}
	}
}

func TestAnOAuthGrantAndAnUnverifiedIdentityNeverAuthenticateASourceSession(t *testing.T) {
	r := newSig(t)
	unverified := Grant{Account: "acme", Owner: "alice", Source: mail, Audience: mail.Origin, Scopes: []string{"mail.read"}, Method: MethodSourceBrowser,
		RuntimeID: "rt_1", SessionID: "bs_alice1", ExpiresAt: r.cl.t.Add(time.Hour)}
	if _, err := r.b.Issue(unverified); err != nil {
		t.Fatal(err)
	}
	oauth := unverified
	oauth.Method, oauth.PrincipalRef, oauth.IdentityVerified = MethodOAuthPKCE, "pr_one", true
	if _, err := r.b.Issue(oauth); err != nil {
		t.Fatal(err)
	}
	s := Sessions{Broker: r.b, Inner: fixedSessions{"s": innerSession("s", "alice", mail.AppID, mail.Origin, "pr_one", "rt_1")}}
	if x, _ := s.Lookup("s"); x.Authenticated {
		t.Fatalf("%+v", x)
	}
}

func TestConcurrentSignInsOfOnePersonLeaveExactlyOneLiveGrant(t *testing.T) {
	r := newSig(t)
	r.session("bs_alice3", "alice", "pr_one", "rt_3", mail)
	_ = r.br.Expect("alice", "acme", mail, []string{"mail.read"})
	e1 := r.ev("bs_alice1", func(e *Evidence) { e.ObservedAt = r.cl.t })
	e2 := r.ev("bs_alice3", func(e *Evidence) { e.ObservedAt = r.cl.t.Add(time.Millisecond) })
	var wg sync.WaitGroup
	for _, e := range []Evidence{e1, e2} {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = r.br.Accept(e) }()
	}
	wg.Wait()
	live := 0
	for _, g := range r.b.List("alice") {
		if g.State != StateRevoked {
			live++
		}
	}
	if live != 1 {
		t.Fatalf("%d live grants", live)
	}
	s := Sessions{Broker: r.b, Inner: fixedSessions{
		"s1": innerSession("s1", "alice", mail.AppID, mail.Origin, "pr_one", "rt_1"),
		"s3": innerSession("s3", "alice", mail.AppID, mail.Origin, "pr_one", "rt_3"),
	}}
	a, _ := s.Lookup("s1")
	c, _ := s.Lookup("s3")
	if a.Authenticated == c.Authenticated {
		t.Fatalf("exactly one runtime is bound to the sign-in at a time: %v %v", a.Authenticated, c.Authenticated)
	}
}
