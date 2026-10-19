package execution

import (
	"context"
	"errors"
	"testing"
	"time"
)

func reasonFrom(t *testing.T, err error) string {
	t.Helper()
	var ref *Refusal
	if !errors.As(err, &ref) {
		t.Fatalf("not a refusal: %v", err)
	}
	return ref.Reason
}

func TestBoundCallerSessionAndContractRunTheOperation(t *testing.T) {
	r := newRig(t, nil)
	rec, err := r.run(r.request("req-1"))
	if err != nil {
		t.Fatal(err)
	}
	mustState(t, rec, Verified, "")
	if r.auth.calls != 2 {
		t.Fatalf("authorized %d times; want at admission and again before the side effect", r.auth.calls)
	}
	env := r.auth.seenEv[0]
	if env.PrincipalRef != principal || env.RuntimeID != runtimeID || env.VariantID != variantID || env.SourceState != sourceState {
		t.Fatalf("environment %+v", env)
	}
}

func TestEveryBindingFailureStopsBeforeAnySourceContact(t *testing.T) {
	cases := []struct {
		name   string
		reason string
		edit   func(r *rig, q *Request)
	}{
		{"unknown caller", "unknown_caller", func(r *rig, q *Request) { q.Caller = "stranger" }},
		{"caller without the scope", "caller_scope_missing", func(r *rig, q *Request) {
			r.dir.callers[userCaller] = Caller{ID: userCaller, Scopes: []string{"records.read"}}
		}},
		{"unknown session", "unknown_session", func(r *rig, q *Request) { q.SessionID = "nope" }},
		{"unauthenticated session", "session_unauthenticated", func(r *rig, q *Request) {
			s := goodSession(sessionOne, userCaller)
			s.Authenticated = false
			r.dir.setSession(s)
		}},
		{"expired session", "session_expired", func(r *rig, q *Request) {
			s := goodSession(sessionOne, userCaller)
			s.ExpiresAt = r.clock.Now().Add(-time.Second)
			r.dir.setSession(s)
		}},
		{"another caller's session", "session_other_caller", func(r *rig, q *Request) { q.SessionID = "sess-bob" }},
		{"source scope missing", "source_scope_missing", func(r *rig, q *Request) {
			s := goodSession(sessionOne, userCaller)
			s.SourceScopes = []string{"records.read"}
			r.dir.setSession(s)
		}},
		{"another application", "session_other_source", func(r *rig, q *Request) {
			s := goodSession(sessionOne, userCaller)
			s.AppID = "other-console:app.other.test"
			r.dir.setSession(s)
		}},
		{"another variant class", "session_other_source", func(r *rig, q *Request) {
			s := goodSession(sessionOne, userCaller)
			s.VariantClass = "admin"
			r.dir.setSession(s)
		}},
		{"version outside the range", "source_version_out_of_range", func(r *rig, q *Request) {
			s := goodSession(sessionOne, userCaller)
			s.AppVersion = "3.0.0"
			r.dir.setSession(s)
		}},
		{"unparseable version", "source_version_out_of_range", func(r *rig, q *Request) {
			s := goodSession(sessionOne, userCaller)
			s.AppVersion = "latest"
			r.dir.setSession(s)
		}},
		{"pinned version differs", "contract_pin_mismatch", func(r *rig, q *Request) { q.Pin.Version = "1.2.9" }},
		{"pinned digest differs", "contract_pin_mismatch", func(r *rig, q *Request) { q.Pin.Digest = "sha256:" + string(make([]byte, 0)) + "00" }},
		{"environment not authorized", "witness_source_state_changed", func(r *rig, q *Request) { r.auth.setDeny("witness_source_state_changed") }},
		{"strategy not in the contract", "strategy_unavailable", func(r *rig, q *Request) { q.Strategy = "native_companion" }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t, nil)
			q := r.request("req-1")
			c.edit(r, &q)
			h, err := r.rt.Submit(context.Background(), q)
			if got := reasonFrom(t, err); got != c.reason {
				t.Fatalf("reason %q, want %q", got, c.reason)
			}
			if h.State() != Failed || h.Record().Reason != c.reason {
				t.Fatalf("record %+v", h.Record())
			}
			if r.ex.applyCount() != 0 || r.ex.readCount() != 0 {
				t.Fatalf("source touched: %d applies, %d reads", r.ex.applyCount(), r.ex.readCount())
			}
		})
	}
}

func TestMalformedRequestsLeaveNoOperation(t *testing.T) {
	r := newRig(t, nil)
	for name, edit := range map[string]func(*Request){
		"no request id": func(q *Request) { q.RequestID = "" },
		"no caller":     func(q *Request) { q.Caller = "" },
		"no session":    func(q *Request) { q.SessionID = "" },
		"garbage contract": func(q *Request) {
			q.Contract = []byte("{}")
		},
	} {
		q := r.request("req-1")
		edit(&q)
		if _, err := r.rt.Submit(context.Background(), q); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if len(r.cfg.Journal.Keys()) != 0 {
		t.Fatal("malformed requests were journaled")
	}
}

func TestParametersMustMatchTheContract(t *testing.T) {
	cases := map[string]struct {
		reason string
		edit   func(map[string]any)
	}{
		"missing":      {"param_missing:stage", func(p map[string]any) { delete(p, "stage") }},
		"unknown":      {"param_unknown:force", func(p map[string]any) { p["force"] = true }},
		"wrong type":   {"param_invalid:record_id", func(p map[string]any) { p["record_id"] = 7 }},
		"not a choice": {"param_invalid:stage", func(p map[string]any) { p["stage"] = "bogus" }},
		"nil required": {"param_missing:record_id", func(p map[string]any) { p["record_id"] = nil }},
	}
	for name, c := range cases {
		r := newRig(t, nil)
		q := r.request("req-1")
		c.edit(q.Params)
		_, err := r.rt.Submit(context.Background(), q)
		if got := reasonFrom(t, err); got != c.reason {
			t.Errorf("%s: %q want %q", name, got, c.reason)
		}
		if r.ex.applyCount() != 0 {
			t.Errorf("%s: source touched", name)
		}
	}
}

func TestARoleRevokedWhileQueuedNeverReachesTheSource(t *testing.T) {
	r := newRig(t, nil)
	started, release := make(chan struct{}), make(chan struct{})
	r.ex.applyFn = func(ctx context.Context, c Call) (Result, error) {
		close(started)
		<-release
		return Result{Outcome: OutcomeCommitted, Echo: map[string]string{}}, nil
	}
	first, err := r.rt.Submit(context.Background(), r.request("first"))
	if err != nil {
		t.Fatal(err)
	}
	<-started
	second, err := r.rt.Submit(context.Background(), r.request("second"))
	if err != nil {
		t.Fatal(err)
	}
	r.dir.revoke(userCaller)
	close(release)
	if _, err := first.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	rec, _ := second.Wait(context.Background())
	mustState(t, rec, Failed, "unknown_caller")
	if r.ex.applyCount() != 1 {
		t.Fatalf("applies %d; the revoked caller's queued operation ran", r.ex.applyCount())
	}
}

func TestSessionLostWhileQueuedIsRecheckedBeforeTheSideEffect(t *testing.T) {
	r := newRig(t, nil)
	started, release := make(chan struct{}), make(chan struct{})
	r.ex.applyFn = func(ctx context.Context, c Call) (Result, error) {
		close(started)
		<-release
		return Result{Outcome: OutcomeCommitted, Echo: map[string]string{}}, nil
	}
	first, _ := r.rt.Submit(context.Background(), r.request("first"))
	<-started
	second, _ := r.rt.Submit(context.Background(), r.request("second"))
	s := goodSession(sessionOne, userCaller)
	s.Authenticated = false
	r.dir.setSession(s)
	close(release)
	first.Wait(context.Background())
	rec, _ := second.Wait(context.Background())
	mustState(t, rec, Failed, "session_unauthenticated")
}

func TestAuthorizationFromAnotherDocumentIsRejected(t *testing.T) {
	r := newRig(t, nil)
	other := contractDoc(t, func(b map[string]any) { b["contract_version"] = "1.4.0" })
	liar := &mismatchAuth{doc: other}
	r.cfg.Authorizer = liar
	rt2, err := NewRuntime(r.cfg)
	if err != nil {
		t.Fatal(err)
	}
	h, err := rt2.Submit(context.Background(), r.request("req-1"))
	if reasonFrom(t, err) != "authorization_invalid" || h.State() != Failed {
		t.Fatalf("%v %+v", err, h.Record())
	}
	if r.ex.applyCount() != 0 {
		t.Fatal("applied on a foreign authorization")
	}
}

type mismatchAuth struct{ doc []byte }

func (m *mismatchAuth) Authorize(_ []byte, env Env) (Authorized, error) {
	return (&fakeAuth{}).Authorize(m.doc, env)
}

func TestAZeroAuthorizedIsNeverAccepted(t *testing.T) {
	r := newRig(t, nil)
	r.cfg.Authorizer = zeroAuth{}
	rt2, _ := NewRuntime(r.cfg)
	h, err := rt2.Submit(context.Background(), r.request("req-1"))
	if reasonFrom(t, err) != "authorization_invalid" || h.State() != Failed || r.ex.applyCount() != 0 {
		t.Fatalf("%v %+v", err, h.Record())
	}
}

type zeroAuth struct{}

func (zeroAuth) Authorize([]byte, Env) (Authorized, error) { return Authorized{}, nil }
