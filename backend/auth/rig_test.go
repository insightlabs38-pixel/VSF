package auth

import (
	"bytes"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeRuntimes struct {
	mu sync.Mutex
	m  map[string]RuntimeSession
}

func (f *fakeRuntimes) Session(id, client string) (RuntimeSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.m[id]
	if !ok || s.Client != client {
		return RuntimeSession{}, errors.New("session.unknown")
	}
	return s, nil
}

func (f *fakeRuntimes) put(s RuntimeSession) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.m[s.ID] = s
}

func (f *fakeRuntimes) end(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.m, id)
}

// sig is a broker, a browser authority and a set of paired runtime sessions on a controllable clock.
type sig struct {
	t   *testing.T
	b   *Broker
	br  *Browser
	cl  *clock
	rt  *fakeRuntimes
	log *bytes.Buffer
	seq map[string]uint64
}

func newSig(t *testing.T) *sig {
	t.Helper()
	b, cl, buf := newBroker(t)
	b.Vault = NewMemVault()
	rt := &fakeRuntimes{m: map[string]RuntimeSession{}}
	s := &sig{t: t, b: b, br: NewBrowser(b, rt), cl: cl, rt: rt, log: buf, seq: map[string]uint64{}}
	s.session("bs_alice1", "alice", "pr_one", "rt_1", mail)
	s.session("bs_bob1", "bob", "pr_bob", "rt_b", mail)
	return s
}

func (s *sig) session(id, owner, principal, runtime string, src Source) {
	s.rt.put(RuntimeSession{ID: id, Client: "cl_" + owner, Owner: owner, AppID: src.AppID, Origin: src.Origin, Principal: principal, Runtime: runtime, Expires: s.cl.t.Add(time.Hour)})
}

// ev builds evidence from a session at the current clock reading with the next sequence number.
func (s *sig) ev(session string, mut func(*Evidence)) Evidence {
	s.t.Helper()
	rs, err := s.rt.Session(session, "cl_"+ownerOf(session, s.rt))
	if err != nil {
		rs = RuntimeSession{AppID: mail.AppID, Origin: mail.Origin}
	}
	s.seq[session]++
	e := Evidence{Client: "cl_" + rs.Owner, SessionID: session, Source: Source{AppID: rs.AppID, Origin: rs.Origin}, PrincipalRef: rs.Principal, Authenticated: true,
		Seq: s.seq[session], ObservedAt: s.cl.t, Scopes: []string{"mail.read"}}
	if mut != nil {
		mut(&e)
	}
	return e
}

func ownerOf(session string, f *fakeRuntimes) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.m[session].Owner
}

// signIn expects and accepts alice's sign-in through her runtime.
func (s *sig) signIn() Grant {
	s.t.Helper()
	if err := s.br.Expect("alice", "acme", mail, []string{"mail.read"}); err != nil {
		s.t.Fatal(err)
	}
	g, err := s.br.Accept(s.ev("bs_alice1", nil))
	if err != nil {
		s.t.Fatal(err)
	}
	return g
}

// obs verifies evidence into an observation for Resume or Profiles.
func (s *sig) obs(session string, mut func(*Evidence)) Observation {
	s.t.Helper()
	o, err := s.br.Verify(s.ev(session, mut))
	if err != nil {
		s.t.Fatal(err)
	}
	return o
}

func (s *sig) tick(d time.Duration) { s.cl.t = s.cl.t.Add(d) }
