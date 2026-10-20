//go:build integration

package interfaces

import (
	"strings"
	"testing"
	"time"

	"github.com/insightlabs38-pixel/vsf/backend/api"
	"github.com/insightlabs38-pixel/vsf/backend/auth"
	st "github.com/insightlabs38-pixel/vsf/backend/internal/stacktest"
)

func TestAPersonCanConfirmForACallerOnlyUnderADelegationAndOnlyForThatCallersExactRequest(t *testing.T) {
	deleg := auth.NewDelegations(nil, nil)
	s := st.New(t, st.Opts{Delegations: deleg})
	x := newSurfaces(t, s)
	n := 0
	bobRequest := func(mut func(map[string]any)) map[string]any {
		return bodyMap(func(m map[string]any) {
			n++
			m["request_id"] = "deleg-" + string(rune('a'+n))
			m["session_id"] = st.SessBob
			m["record"] = "o2"
			m["params"].(map[string]any)["record_id"] = "o2"
			m["seen"] = map[string]any{"version": "v1", "observed_at": 1}
			if mut != nil {
				mut(m)
			}
		})
	}
	ui := func(caller string) *api.Client {
		c, err := api.NewClient(x.rest.URL, s.Token(caller, api.SurfaceUI), api.SurfaceUI, nil)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	agent := func(caller string) *api.Client {
		c, err := api.NewClient(x.rest.URL, s.Token(caller, api.SurfaceREST), api.SurfaceREST, nil)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	ada, bobUI, bobAgent := ui(st.Ada), ui(st.Bob), agent(st.Bob)
	bobTok, adaTok := s.Token(st.Bob, api.SurfaceREST), s.Token(st.Ada, api.SurfaceREST)
	forBob := api.Route{Operation: s.Spec.Name, Version: s.Spec.ContractVersion, Delegate: st.Bob}
	body := marshal(bobRequest(nil))
	denied := func(what string, e *api.Error) {
		t.Helper()
		if e == nil || e.Code != api.CodePermissionDenied {
			t.Fatalf("%s must be denied: %+v", what, e)
		}
	}
	committed := func() int { return s.App.Commits() }

	_, e := ada.Confirm(t.Context(), forBob, body)
	denied("confirming for a caller no delegation names", e)

	if err := deleg.Allow(st.Ada, st.Ada, time.Now().Add(time.Hour)); err == nil {
		t.Fatal("a person cannot delegate to themself")
	}
	if err := deleg.Allow(st.Ada, st.Bob, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	_, e = bobAgent.Confirm(t.Context(), forBob, body)
	denied("an agent credential confirming for itself", e)
	_, e = bobAgent.Confirm(t.Context(), api.Route{Operation: s.Spec.Name, Version: s.Spec.ContractVersion, Delegate: st.Ada}, body)
	denied("an agent credential confirming for someone else", e)
	_, e = bobUI.Confirm(t.Context(), api.Route{Operation: s.Spec.Name, Version: s.Spec.ContractVersion, Delegate: st.Ada},
		marshal(bobRequest(func(m map[string]any) { m["session_id"] = st.SessAda })))
	denied("a person confirming for a caller who never delegated to them", e)

	_, e = ada.Confirm(t.Context(), forBob, marshal(bobRequest(func(m map[string]any) { m["session_id"] = st.SessAda })))
	if e == nil {
		t.Fatal("the session must belong to the caller who will run the request")
	}

	conf, e := ada.Confirm(t.Context(), forBob, body)
	if e != nil {
		t.Fatal(e)
	}
	if conf.By != st.Ada {
		t.Fatalf("the approving person is recorded: %+v", conf)
	}
	with := func(mut func(map[string]any)) []byte {
		return marshal(bobRequest(func(m map[string]any) {
			m["confirmation"] = conf
			if mut != nil {
				mut(m)
			}
		}))
	}
	adaOwn := marshal(bobRequest(func(m map[string]any) { m["session_id"] = st.SessAda; m["confirmation"] = conf }))
	if _, r := x.restResult(adaTok, adaOwn); r.Error == nil || r.Error.Code != api.CodeConfirmationRequired || committed() != 0 {
		t.Fatalf("a confirmation for Bob cannot be spent by the person it came from: %+v", r)
	}
	widened := with(func(m map[string]any) {
		m["params"].(map[string]any)["stage"] = "proposal"
		m["expect"] = map[string]any{"stage": "proposal"}
	})
	if _, r := x.restResult(bobTok, widened); r.Error == nil || r.Error.Code != api.CodeConfirmationRequired || committed() != 0 {
		t.Fatalf("a delegated confirmation covers exactly the request it was given for: %+v", r)
	}
	forged := conf
	forged.ExpiresAt += int64(time.Hour / time.Millisecond)
	if _, r := x.restResult(bobTok, marshal(bobRequest(func(m map[string]any) { m["confirmation"] = forged }))); r.Error == nil || committed() != 0 {
		t.Fatalf("its expiry cannot be extended: %+v", r)
	}
	if _, r := x.restResult(bobTok, with(nil)); r.Category != api.CategorySucceeded {
		t.Fatalf("the delegate runs what the person confirmed: %+v", r)
	}
	if committed() != 1 {
		t.Fatalf("one write: %d", committed())
	}
	s.ResetOrder("o2")
	s.App.Put(orderWithConfirmation("o2"))
	if _, r := x.restResult(bobTok, with(nil)); r.Error == nil || r.Error.Code != api.CodeConfirmationRequired {
		t.Fatalf("a delegated confirmation works once: %+v", r)
	}

	conf, e = ada.Confirm(t.Context(), forBob, marshal(bobRequest(nil)))
	if e != nil {
		t.Fatal(e)
	}
	deleg.Withdraw(st.Ada, st.Bob)
	before := committed()
	if _, r := x.restResult(bobTok, with(nil)); r.Error == nil || r.Error.Code != api.CodeConfirmationRequired || committed() != before {
		t.Fatalf("withdrawing the delegation ends confirmations already given under it: %+v", r)
	}

	if err := deleg.Allow(st.Ada, st.Bob, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	conf, e = ada.Confirm(t.Context(), forBob, marshal(bobRequest(nil)))
	if e != nil {
		t.Fatal(e)
	}
	s.Clock.Advance(api.ConfirmationTTL + time.Second)
	if _, r := x.restResult(bobTok, with(nil)); r.Error == nil || r.Error.Code != api.CodeConfirmationRequired || committed() != before {
		t.Fatalf("a delegated confirmation expires like any other: %+v", r)
	}

	s.Dir.SetScopes(st.Bob, "records.read")
	_, e = ada.Confirm(t.Context(), forBob, marshal(bobRequest(nil)))
	denied("confirming for a caller who lacks the scope the operation needs", e)
	if strings.Contains(e.Error(), st.Bob) {
		t.Fatalf("the refusal names no one: %v", e)
	}
}
