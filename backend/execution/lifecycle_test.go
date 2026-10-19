package execution

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestHappyPathWalksPreparedAdmittedRunningVerified(t *testing.T) {
	r := newRig(t, nil)
	rec, err := r.run(r.request("req-1"))
	if err != nil {
		t.Fatal(err)
	}
	mustState(t, rec, Verified, "verified")
	var path []State
	for _, e := range rec.Events {
		if e.To != "" {
			path = append(path, e.To)
		}
	}
	want := []State{Prepared, Admitted, Running, Verified}
	if len(path) != len(want) {
		t.Fatalf("path %v", path)
	}
	for i := range want {
		if path[i] != want[i] {
			t.Fatalf("path %v", path)
		}
	}
	if r.ex.applyCount() != 1 {
		t.Fatalf("applies %d", r.ex.applyCount())
	}
	if got := r.ex.applies[0]; got.IfMatch != "v1" || got.Target.Record != "o1" {
		t.Fatalf("call %+v", got)
	}
}

func TestEveryIllegalTransitionIsRefused(t *testing.T) {
	states := []State{Prepared, Admitted, Running, Verified, Failed, Unknown}
	legal := map[[2]State]bool{}
	for from, tos := range transitions {
		for _, to := range tos {
			legal[[2]State{from, to}] = true
		}
	}
	for _, from := range states {
		for _, to := range states {
			if got := canMove(from, to); got != legal[[2]State{from, to}] {
				t.Errorf("%s -> %s: canMove=%v", from, to, got)
			}
		}
	}
	if canMove(Verified, Failed) || canMove(Failed, Verified) || canMove(Prepared, Verified) || canMove(Admitted, Verified) {
		t.Fatal("a terminal state or a shortcut to success is reachable")
	}
	r := newRig(t, nil)
	h, _ := r.rt.Submit(t.Context(), r.request("req-x"))
	if _, err := h.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := r.cfg.Journal.Move(h.Key(), Failed, "late", "", 1); err == nil {
		t.Fatal("a verified operation moved to failed")
	}
	if err := r.cfg.Journal.Move(h.Key(), Verified, "again", "", 1); err == nil {
		t.Fatal("a verified operation was verified twice")
	}
}

func TestAcknowledgementAloneIsNotSuccess(t *testing.T) {
	r := newRig(t, nil)
	r.ex.applyFn = func(_ context.Context, _ Call) (Result, error) {
		return Result{Outcome: OutcomeCommitted, Echo: map[string]string{"stage": "won"}}, nil
	}
	rec, err := r.run(r.request("req-1"))
	if err != nil {
		t.Fatal(err)
	}
	mustState(t, rec, Unknown, ReasonUnverified)
	if !strings.Contains(rec.Detail, "mismatch:stage") {
		t.Fatalf("detail %q", rec.Detail)
	}
}

func TestReadBackFailureLeavesTheOutcomeUnverified(t *testing.T) {
	r := newRig(t, nil)
	r.ex.applyFn = func(_ context.Context, _ Call) (Result, error) {
		r.ex.mu.Lock()
		r.ex.readErr = errors.New("source offline")
		r.ex.mu.Unlock()
		return Result{Outcome: OutcomeCommitted}, nil
	}
	rec, _ := r.run(r.request("req-1"))
	mustState(t, rec, Unknown, ReasonUnverified)
}

func TestSourceRejectionAndConflictAreFailuresWithoutSideEffect(t *testing.T) {
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

func TestJournalSurvivesRestartAndKeepsEveryState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	r := newRig(t, nil)
	r.build(NewFileStore(path))
	rec, err := r.run(r.request("req-1"))
	if err != nil {
		t.Fatal(err)
	}
	mustState(t, rec, Verified, "")
	j2, err := OpenJournal(NewFileStore(path))
	if err != nil {
		t.Fatal(err)
	}
	again, ok := j2.Get(rec.Key)
	if !ok || again.State != Verified || len(again.Events) != len(rec.Events) {
		t.Fatalf("reopened %+v", again)
	}
}

func TestTamperedJournalIsRefusedAtOpen(t *testing.T) {
	r := newRig(t, nil)
	if _, err := r.run(r.request("req-1")); err != nil {
		t.Fatal(err)
	}
	lines, _ := r.store.ReadAll()
	clone := func(edit func([][]byte) [][]byte) error {
		s := &MemStore{}
		for _, l := range edit(append([][]byte(nil), lines...)) {
			_ = s.Append(l)
		}
		_, err := OpenJournal(s)
		return err
	}
	cases := map[string]func([][]byte) [][]byte{
		"altered": func(l [][]byte) [][]byte {
			l[2] = []byte(strings.Replace(string(l[2]), `"to":"running"`, `"to":"verified"`, 1))
			return l
		},
		"altered detail only": func(l [][]byte) [][]byte {
			l[2] = []byte(strings.Replace(string(l[2]), `"at":`, `"detail":"x","at":`, 1))
			return l
		},
		"deleted middle": func(l [][]byte) [][]byte { return append(l[:1], l[2:]...) },
		"reordered":      func(l [][]byte) [][]byte { l[1], l[2] = l[2], l[1]; return l },
		"garbage":        func(l [][]byte) [][]byte { return append(l, []byte("{not json")) },
		"duplicated":     func(l [][]byte) [][]byte { return append(l, l[len(l)-1]) },
	}
	for name, edit := range cases {
		if err := clone(edit); !errors.Is(err, ErrCorrupt) {
			t.Errorf("%s: err %v", name, err)
		}
	}
	if err := clone(func(l [][]byte) [][]byte { return l }); err != nil {
		t.Fatalf("untouched journal: %v", err)
	}
}

func TestRestartSettlesWhatWasHalfDone(t *testing.T) {
	r := newRig(t, nil)
	j := r.cfg.Journal
	for key, to := range map[string]State{"a": Prepared, "b": Admitted, "c": Running} {
		if _, _, err := j.Create(key, 1, map[string]string{"operation_id": "x"}); err != nil {
			t.Fatal(err)
		}
		for _, s := range []State{Admitted, Running} {
			if to == Prepared || (to == Admitted && s == Running) {
				break
			}
			if err := j.Move(key, s, "", "", 1); err != nil {
				t.Fatal(err)
			}
		}
	}
	rt2, err := NewRuntime(r.cfg)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][2]string{"a": {"failed", "abandoned_before_admission"}, "b": {"failed", "abandoned_before_run"}, "c": {"unknown", ReasonUnknownCommit}}
	for k, w := range want {
		rec, _ := rt2.Get(k)
		if string(rec.State) != w[0] || rec.Reason != w[1] {
			t.Errorf("%s: %s/%s", k, rec.State, rec.Reason)
		}
	}
}

func TestBrokenJournalStopsEverythingInsteadOfRunningUnrecorded(t *testing.T) {
	r := newRig(t, nil)
	fs := &failingStore{inner: r.store}
	r.build(fs)
	fs.failAfter = 2
	h, err := r.rt.Submit(t.Context(), r.request("req-1"))
	if err == nil {
		_, err = h.Wait(t.Context())
	}
	if err == nil {
		t.Fatal("a journal write failure was ignored")
	}
	if _, err := r.rt.Submit(t.Context(), r.request("req-2")); err == nil {
		t.Fatal("submissions continue after the journal broke")
	}
	if r.ex.applyCount() != 0 {
		t.Fatalf("applied %d times with an unrecorded Running state", r.ex.applyCount())
	}
}

type failingStore struct {
	inner     Store
	n         int
	failAfter int
}

func (f *failingStore) Append(l []byte) error {
	f.n++
	if f.failAfter > 0 && f.n > f.failAfter {
		return errors.New("disk full")
	}
	return f.inner.Append(l)
}

func (f *failingStore) ReadAll() ([][]byte, error) { return f.inner.ReadAll() }
