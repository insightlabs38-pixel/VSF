package execution

import (
	"context"
	"testing"
	"time"
)

func TestStaleReadBlocksTheWrite(t *testing.T) {
	r := newRig(t, nil)
	r.ex.version = "v2"
	rec, _ := r.run(r.request("req-1"))
	mustState(t, rec, Failed, "stale_read")
	if r.ex.applyCount() != 0 {
		t.Fatal("wrote on a stale read")
	}
}

func TestRevalidatedReadAllowsTheWrite(t *testing.T) {
	r := newRig(t, nil)
	r.ex.version = "v2"
	q := r.request("req-1")
	q.Seen = &Seen{Version: "v2", ObservedAt: r.clock.Now()}
	rec, _ := r.run(q)
	mustState(t, rec, Verified, "")
	if got := r.ex.applies[0].IfMatch; got != "v2" {
		t.Fatalf("if-match %q; the write must be conditional on what was revalidated", got)
	}
}

func TestAWriteWithoutAnObservationIsRefusedWhenFreshnessIsRequired(t *testing.T) {
	r := newRig(t, nil)
	q := r.request("req-1")
	q.Seen = nil
	rec, _ := r.run(q)
	mustState(t, rec, Failed, "seen_required")
	if r.ex.applyCount() != 0 {
		t.Fatal("applied")
	}
}

func TestObservationWithinBoundsTheAgeOfWhatTheCallerSaw(t *testing.T) {
	edit := func(b map[string]any) {
		b["freshness"] = map[string]any{"before_write": "observation_within", "max_age_ms": 60000}
	}
	r := newRig(t, edit)
	q := r.request("req-1")
	q.Seen = &Seen{Version: "v1", ObservedAt: r.clock.Now().Add(-2 * time.Minute)}
	rec, _ := r.run(q)
	mustState(t, rec, Failed, "stale_observation")
	if r.ex.applyCount() != 0 {
		t.Fatal("wrote on an old observation")
	}
	q = r.request("req-2")
	q.Seen = &Seen{Version: "v1", ObservedAt: r.clock.Now().Add(-30 * time.Second)}
	rec, _ = r.run(q)
	mustState(t, rec, Verified, "")
	if r.ex.applies[0].IfMatch != "v1" {
		t.Fatalf("if-match %q", r.ex.applies[0].IfMatch)
	}
}

func TestSourceRequiredConfirmationIsPropagatedAndBlocksUntilGiven(t *testing.T) {
	r := newRig(t, nil)
	r.ex.confirm = true
	q := r.request("req-1")
	rec, _ := r.run(q)
	mustState(t, rec, Failed, "confirmation_required")
	if r.ex.applyCount() != 0 {
		t.Fatal("wrote without the source's confirmation")
	}
	q2 := r.request("req-2")
	c := r.confirmation(q2)
	q2.Confirmation = &c
	rec, _ = r.run(q2)
	mustState(t, rec, Verified, "")
	if !r.ex.applies[0].Confirmed {
		t.Fatal("the call did not carry the confirmation")
	}
}

func TestConfirmationIsBoundExpiringAndUsableOnce(t *testing.T) {
	r := newRig(t, nil)
	r.ex.confirm = true
	q := r.request("req-1")
	c := r.confirmation(q)

	other := q
	other.RequestID = "req-other"
	other.Params = map[string]any{"record_id": "o1", "stage": "proposal"}
	other.Confirmation = &c
	rec, _ := r.run(other)
	mustState(t, rec, Failed, "confirmation_required")

	expired := q
	expired.RequestID = "req-expired"
	old := r.conf.Issue(requestDigest(q.Pin.Digest, q.Record, q.Params), "ada", r.clock.Now().Add(-time.Second))
	expired.Confirmation = &old
	rec, _ = r.run(expired)
	mustState(t, rec, Failed, "confirmation_required")

	forged := q
	forged.RequestID = "req-forged"
	f := c
	f.Token = "00" + f.Token[2:]
	forged.Confirmation = &f
	rec, _ = r.run(forged)
	mustState(t, rec, Failed, "confirmation_required")

	first := q
	first.RequestID = "req-first"
	first.Confirmation = &c
	rec, _ = r.run(first)
	mustState(t, rec, Verified, "")

	replay := q
	replay.RequestID = "req-replay"
	replay.Confirmation = &c
	r.ex.mu.Lock()
	r.ex.fields["stage"], r.ex.version = "new", "v1"
	r.ex.mu.Unlock()
	rec, _ = r.run(replay)
	mustState(t, rec, Failed, "confirmation_required")
	if r.ex.applyCount() != 1 {
		t.Fatalf("applies %d", r.ex.applyCount())
	}
}

func TestPolicyThatAlwaysPromptsNeedsAConfirmationAtAdmission(t *testing.T) {
	r := newRig(t, func(b map[string]any) { b["effect"].(map[string]any)["confirmation"] = "always_prompt" })
	q := r.request("req-1")
	h, err := r.rt.Submit(context.Background(), q)
	if reasonFrom(t, err) != "confirmation_required" || h.State() != Failed {
		t.Fatalf("%v %+v", err, h.Record())
	}
	if r.ex.readCount() != 0 || r.auth.calls != 0 {
		t.Fatal("work happened before the person confirmed")
	}
	q2 := r.request("req-2")
	c := r.confirmation(q2)
	q2.Confirmation = &c
	rec, _ := r.run(q2)
	mustState(t, rec, Verified, "")
}

func TestAmbiguousEffectsStayUnverified(t *testing.T) {
	for _, o := range []Outcome{OutcomeUnknown} {
		r := newRig(t, nil)
		r.ex.results = []Result{{Outcome: o, Detail: "gateway timeout"}}
		rec, _ := r.run(r.request("req-1"))
		mustState(t, rec, Unknown, ReasonUnknownCommit)
	}
	r := newRig(t, nil)
	r.ex.applyFn = func(context.Context, Call) (Result, error) {
		return Result{}, context.DeadlineExceeded
	}
	rec, _ := r.run(r.request("req-1"))
	mustState(t, rec, Unknown, ReasonUnknownCommit)
}

func TestReadOnlyOperationsNeedNoWriteChecks(t *testing.T) {
	edit := func(b map[string]any) {
		b["effect"] = map[string]any{"class": "read_only", "idempotency": "idempotent", "confirmation": "none", "concurrency": "none"}
		b["freshness"] = map[string]any{"before_write": "none"}
		b["verification"] = map[string]any{"required": false, "strategy": "none", "suite": "update_stage_v2"}
		b["evidence_effects"] = []any{map[string]any{"evidence": "ev_run", "effect": "read_only"}}
	}
	r := newRig(t, edit)
	q := r.request("req-1")
	q.Expect, q.Seen, q.Record = nil, nil, ""
	rec, _ := r.run(q)
	mustState(t, rec, Verified, "read")
	if r.ex.applyCount() != 0 || r.ex.readCount() != 1 {
		t.Fatalf("applies %d reads %d", r.ex.applyCount(), r.ex.readCount())
	}
}

func TestAWriteThatChecksNothingAfterwardsIsRefusedWithoutExpectations(t *testing.T) {
	r := newRig(t, nil)
	q := r.request("req-1")
	q.Expect = nil
	_, err := r.rt.Submit(context.Background(), q)
	if reasonFrom(t, err) != "expect_required" {
		t.Fatal(err)
	}
}

func TestAConfirmationTokenWorksExactlyOnce(t *testing.T) {
	c := NewConfirmer([]byte("k"))
	now := time.UnixMilli(t0Millis)
	conf := c.Issue("sha256:abc", "ada", now.Add(time.Hour))
	if !c.Valid(conf, "sha256:abc", now) {
		t.Fatal("a fresh token was not valid")
	}
	if c.Use(conf, "sha256:other", now) {
		t.Fatal("used against another request")
	}
	if !c.Use(conf, "sha256:abc", now) {
		t.Fatal("a fresh token could not be used")
	}
	if c.Use(conf, "sha256:abc", now) || c.Valid(conf, "sha256:abc", now) {
		t.Fatal("a spent token still works")
	}
	other := NewConfirmer([]byte("another key"))
	if other.Use(conf, "sha256:abc", now) {
		t.Fatal("a token from another key was accepted")
	}
	if c.Use(c.Issue("sha256:abc", "ada", now.Add(-time.Second)), "sha256:abc", now) {
		t.Fatal("an expired token was accepted")
	}
}
