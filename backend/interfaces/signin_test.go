//go:build integration

package interfaces

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/insightlabs38-pixel/vsf/backend/api"
	"github.com/insightlabs38-pixel/vsf/backend/auth"
	"github.com/insightlabs38-pixel/vsf/backend/execution"
	st "github.com/insightlabs38-pixel/vsf/backend/internal/stacktest"
)

type oneRuntime struct{ rs auth.RuntimeSession }

func (o oneRuntime) Session(id, client string) (auth.RuntimeSession, error) {
	if id != o.rs.ID || client != o.rs.Client {
		return auth.RuntimeSession{}, errors.New("session.unknown")
	}
	return o.rs, nil
}

func TestTheRuntimeRefusesWorkWhileASignInIsPausedEndedOrSomeoneElses(t *testing.T) {
	var audit bytes.Buffer
	probe := st.GoodSession("probe", st.Ada)
	now := time.Now()
	broker := auth.NewBroker(nil, auth.NewAudit(&audit), func() time.Time { return now })
	src := auth.Source{AppID: st.TestApp, Origin: "https://app.example.com"}
	browser := auth.NewBrowser(broker, oneRuntime{auth.RuntimeSession{ID: "bs_ada", Client: "cl_ada", Owner: st.Ada, AppID: src.AppID, Origin: src.Origin,
		Principal: probe.PrincipalRef, Runtime: probe.RuntimeID}})
	s := st.New(t, st.Opts{WrapSessions: func(in execution.Sessions) execution.Sessions { return auth.Sessions{Broker: broker, Inner: in} }})
	x := newSurfaces(t, s)
	scopes := []string{"records.read", "records.write"}
	n, seq := 0, uint64(0)
	run := func(session string) api.Result {
		t.Helper()
		n++
		body := bodyMap(func(m map[string]any) {
			m["request_id"] = "signin-" + string(rune('a'+n))
			m["session_id"] = session
			m["record"] = "o1"
			m["params"].(map[string]any)["record_id"] = "o1"
		})
		_, r := x.restResult(s.Token(st.Ada, api.SurfaceREST), marshal(body))
		return r
	}
	refused := func(what string, r api.Result) {
		t.Helper()
		if r.Error == nil || r.Error.Code != api.CodeSessionInvalid {
			t.Fatalf("%s must be refused as a source-session problem: %+v", what, r)
		}
	}
	ev := func(mut func(*auth.Evidence)) auth.Evidence {
		seq++
		e := auth.Evidence{Client: "cl_ada", SessionID: "bs_ada", Source: src, PrincipalRef: probe.PrincipalRef, Authenticated: true, Seq: seq, ObservedAt: now, Scopes: scopes}
		if mut != nil {
			mut(&e)
		}
		return e
	}

	refused("work before any sign-in was recorded", run(st.SessAda))
	if err := browser.Expect(st.Ada, "acme", src, scopes); err != nil {
		t.Fatal(err)
	}
	if _, err := browser.Accept(ev(func(e *auth.Evidence) { e.Client = "cl_mallory" })); err == nil {
		t.Fatal("an observation from a client that is not the runtime's own pairing creates nothing")
	}
	g, err := browser.Accept(ev(nil))
	if err != nil {
		t.Fatal(err)
	}
	if r := run(st.SessAda); r.Category != api.CategorySucceeded {
		t.Fatalf("a recorded sign-in lets the work through: %+v", r)
	}
	if _, err := broker.Pause(g.ID, st.Ada, auth.ChallengeMFA); err != nil {
		t.Fatal(err)
	}
	before := s.App.Commits()
	refused("work while the source is asking for a second factor", run(st.SessAda))
	if s.App.Commits() != before {
		t.Fatal("a paused sign-in reached the source")
	}
	now = now.Add(time.Second)
	if _, err := browser.Accept(ev(nil)); err != nil {
		t.Fatal(err)
	}
	s.ResetOrder("o1")
	if r := run(st.SessAda); r.Category != api.CategorySucceeded {
		t.Fatalf("answering the challenge at the source resumes the work: %+v", r)
	}
	now = now.Add(time.Second)
	if _, err := browser.Accept(ev(func(e *auth.Evidence) { e.Authenticated = false })); err != nil {
		t.Fatal(err)
	}
	s.ResetOrder("o1")
	before = s.App.Commits()
	refused("work after signing out", run(st.SessAda))
	if s.App.Commits() != before {
		t.Fatal("a signed-out session reached the source")
	}
	if n, err := auth.VerifyAudit(bytes.NewReader(audit.Bytes())); err != nil || n < 4 {
		t.Fatalf("the audit chain holds: %d %v", n, err)
	}
}
