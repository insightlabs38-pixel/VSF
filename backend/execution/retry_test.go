package execution

import (
	"fmt"
	"testing"
	"time"
)

func effectEdit(class, idem string) func(map[string]any) {
	return func(b map[string]any) {
		e := b["effect"].(map[string]any)
		e["class"], e["idempotency"] = class, idem
		if class == "non_idempotent_write" || class == "destructive" {
			e["confirmation"] = "always_prompt"
		}
		b["evidence_effects"].([]any)[0].(map[string]any)["effect"] = class
		for _, w := range b["witnesses"].([]any) {
			w.(map[string]any)["observed_effect"] = class
		}
	}
}

func (r *rig) confirmed(id string) Request {
	q := r.request(id)
	c := r.confirmation(q)
	q.Confirmation = &c
	return q
}

func TestThrottledAttemptsRetryWithBackoffAndTheSameDedupToken(t *testing.T) {
	r := newRig(t, nil)
	r.ex.caps.DedupTokens = true
	r.ex.results = []Result{{Outcome: OutcomeThrottled}, {Outcome: OutcomeThrottled}}
	rec, _ := r.run(r.request("req-1"))
	mustState(t, rec, Verified, "")
	if r.ex.applyCount() != 3 {
		t.Fatalf("applies %d", r.ex.applyCount())
	}
	if got := r.clock.Sleeps(); len(got) != 2 || got[0] != 100*time.Millisecond || got[1] != 200*time.Millisecond {
		t.Fatalf("backoff %v", got)
	}
	tok := r.ex.applies[0].DedupToken
	if tok == "" || tok != rec.Key {
		t.Fatalf("token %q for %q", tok, rec.Key)
	}
	for _, a := range r.ex.applies {
		if a.DedupToken != tok {
			t.Fatal("the dedup token changed between attempts")
		}
	}
}

func TestNoDedupTokenIsInventedWhenTheSourceCannotHonourOne(t *testing.T) {
	r := newRig(t, nil)
	if _, err := r.run(r.request("req-1")); err != nil {
		t.Fatal(err)
	}
	if r.ex.applies[0].DedupToken != "" {
		t.Fatal("a token was sent to a source that ignores it")
	}
}

func TestRetriesAreBoundedAndHonourTheSourcesHint(t *testing.T) {
	r := newRig(t, nil)
	r.ex.results = []Result{
		{Outcome: OutcomeThrottled, RetryAfter: 3 * time.Second},
		{Outcome: OutcomeThrottled, RetryAfter: time.Second},
		{Outcome: OutcomeThrottled, RetryAfter: time.Second},
		{Outcome: OutcomeThrottled},
	}
	rec, _ := r.run(r.request("req-1"))
	mustState(t, rec, Failed, "throttled")
	if r.ex.applyCount() != 3 {
		t.Fatalf("applies %d; the bound is 3", r.ex.applyCount())
	}
	if got := r.clock.Sleeps(); got[0] != 3*time.Second {
		t.Fatalf("slept %v; the source asked for 3s", got)
	}
}

func TestUncertainOutcomesNeverRetryWhateverTheContract(t *testing.T) {
	for _, c := range []struct{ class, idem string }{
		{"conditional_write", "conditional"}, {"idempotent_write", "idempotent"}, {"non_idempotent_write", "non_idempotent"}, {"destructive", "non_idempotent"},
	} {
		for _, dedup := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/dedup=%v", c.class, dedup), func(t *testing.T) {
				r := newRig(t, effectEdit(c.class, c.idem))
				r.ex.caps.DedupTokens = dedup
				r.ex.results = []Result{{Outcome: OutcomeUnknown}}
				rec, _ := r.run(r.confirmed("req-1"))
				mustState(t, rec, Unknown, ReasonUnknownCommit)
				if r.ex.applyCount() != 1 {
					t.Fatalf("applies %d; an ambiguous write was retried", r.ex.applyCount())
				}
			})
		}
	}
}

func TestTransportErrorsAfterSendingAreNeverRetried(t *testing.T) {
	r := newRig(t, effectEdit("idempotent_write", "idempotent"))
	r.ex.caps.DedupTokens = true
	r.ex.errs = []error{fmt.Errorf("connection reset by peer")}
	rec, _ := r.run(r.request("req-1"))
	mustState(t, rec, Unknown, ReasonUnknownCommit)
	if r.ex.applyCount() != 1 {
		t.Fatalf("applies %d", r.ex.applyCount())
	}
}

func TestUnsentRequestsRetryOnlyWithEvidenceOfIdempotencyOrADedupToken(t *testing.T) {
	notSent := []error{ErrNotSent, ErrNotSent}
	cases := []struct {
		name         string
		class, idem  string
		dedup        bool
		wantApplies  int
		wantState    State
		wantReasonIs string
	}{
		{"idempotent", "idempotent_write", "idempotent", false, 3, Verified, ""},
		{"non-idempotent without a token", "non_idempotent_write", "non_idempotent", false, 1, Failed, "unavailable"},
		{"non-idempotent with a token", "non_idempotent_write", "non_idempotent", true, 3, Verified, ""},
		{"destructive without a token", "destructive", "non_idempotent", false, 1, Failed, "unavailable"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t, effectEdit(c.class, c.idem))
			r.ex.caps.DedupTokens = c.dedup
			r.ex.errs = notSent
			rec, _ := r.run(r.confirmed("req-1"))
			mustState(t, rec, c.wantState, c.wantReasonIs)
			if r.ex.applyCount() != c.wantApplies {
				t.Fatalf("applies %d want %d", r.ex.applyCount(), c.wantApplies)
			}
		})
	}
}

func TestDefinitiveRefusalsAreNeverRetried(t *testing.T) {
	for _, o := range []Outcome{OutcomeRejected, OutcomeConflict} {
		r := newRig(t, nil)
		r.ex.results = []Result{{Outcome: o}}
		rec, _ := r.run(r.request("req-1"))
		mustState(t, rec, Failed, string(o))
		if r.ex.applyCount() != 1 {
			t.Fatalf("%s: applies %d", o, r.ex.applyCount())
		}
	}
}

func TestThrottlingSlowsTheSourceDownForLaterCalls(t *testing.T) {
	r := newRig(t, nil)
	r.ex.results = []Result{{Outcome: OutcomeThrottled, RetryAfter: time.Second}}
	if _, err := r.run(r.request("req-1")); err != nil {
		t.Fatal(err)
	}
	if iv := r.rt.limiter.Interval(appID); iv <= 0 {
		t.Fatalf("interval %v: the refusal did not slow the source down", iv)
	}
}
