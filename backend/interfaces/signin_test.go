//go:build integration

package interfaces

import (
	"bytes"
	"testing"
	"time"

	"github.com/insightlabs38-pixel/vsf/backend/api"
	"github.com/insightlabs38-pixel/vsf/backend/auth"
	"github.com/insightlabs38-pixel/vsf/backend/execution"
	st "github.com/insightlabs38-pixel/vsf/backend/internal/stacktest"
)

func TestTheRuntimeRefusesWorkWhileASignInIsPausedEndedOrSomeoneElses(t *testing.T) {
	var audit bytes.Buffer
	ada := st.GoodSession("probe", st.Ada).PrincipalRef
	broker := auth.NewBroker(nil, auth.NewAudit(&audit), nil)
	browser := auth.NewBrowser(broker)
	s := st.New(t, st.Opts{WrapSessions: func(in execution.Sessions) execution.Sessions { return auth.Sessions{Broker: broker, Inner: in} }})
	x := newSurfaces(t, s)
	src := auth.Source{AppID: st.TestApp, Origin: "https://app.example.com"}
	scopes := []string{"records.read", "records.write"}
	n := 0
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

	refused("work before any sign-in was recorded", run(st.SessAda))
	if err := browser.Expect(st.Ada, "acme", src, scopes); err != nil {
		t.Fatal(err)
	}
	g, err := browser.Observe(auth.Observation{Owner: st.Ada, Source: src, PrincipalRef: ada, Authenticated: true, Scopes: scopes})
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
	if _, err := broker.Resume(g.ID, st.Ada, auth.Observation{Owner: st.Ada, Source: src, PrincipalRef: ada, Authenticated: true, ObservedAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	s.ResetOrder("o1")
	if r := run(st.SessAda); r.Category != api.CategorySucceeded {
		t.Fatalf("answering the challenge at the source resumes the work: %+v", r)
	}
	if _, err := browser.Observe(auth.Observation{Owner: st.Ada, Source: src, Authenticated: false}); err != nil {
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
