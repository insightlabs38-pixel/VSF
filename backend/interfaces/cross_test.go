//go:build integration

package interfaces

import (
	"testing"

	"github.com/insightlabs38-pixel/vsf/backend/api"
	"github.com/insightlabs38-pixel/vsf/backend/internal/fixtureapp"
	st "github.com/insightlabs38-pixel/vsf/backend/internal/stacktest"
)

func allSame(t *testing.T, label string, results map[string]api.Result) core {
	t.Helper()
	var ref core
	first := ""
	for name, r := range results {
		c := coreOf(r)
		if first == "" {
			ref, first = c, name
			continue
		}
		sameCore(t, label+": "+first+" vs "+name, results[first], r)
	}
	return ref
}

func TestAVerifiedWriteHasTheSameObservableEffectOnUIRESTCLIAndMCP(t *testing.T) {
	s := st.New(t, st.Opts{})
	x := newSurfaces(t, s)
	got := map[string]api.Result{}
	status := map[string]api.Result{}
	commits := 0
	for _, o := range x.all(st.Ada) {
		s.ResetOrder("o1")
		r := o.write("w", nil)
		commits++
		if r.Category != api.CategorySucceeded || s.Stage("o1") != "won" || s.App.Commits() != commits {
			t.Fatalf("%s: %+v stage %s commits %d", o.name, r, s.Stage("o1"), s.App.Commits())
		}
		got[o.name] = r
		status[o.name] = o.status(r.Invocation)
		sameCore(t, o.name+": status after the call", r, status[o.name])
		if o.exit != nil && o.exit() != 0 {
			t.Fatalf("%s exit %d", o.name, o.exit())
		}
	}
	c := allSame(t, "a verified write", got)
	if c.Category != api.CategorySucceeded || c.Effect != api.EffectApplied || c.Fields["stage"] != "won" {
		t.Fatalf("%+v", c)
	}
	allSame(t, "status of a verified write", status)
}

func TestAnUnknownCommitMeansTheSameOnEverySurface(t *testing.T) {
	for _, name := range []string{"rest", "ui", "cli", "mcp"} {
		t.Run(name, func(t *testing.T) {
			s := st.New(t, st.Opts{NoDedup: true})
			x := newSurfaces(t, s)
			var o *ops
			for _, c := range x.all(st.Ada) {
				if c.name == name {
					o = c
				}
			}
			s.App.SetFaults(fixtureapp.Faults{DropAfterCommit: 1})
			r := o.write("u", nil)
			c := coreOf(r)
			want := core{Category: api.CategoryUnknownCommit, Effect: api.EffectUnknown, Retry: api.RetryReconcile, Code: api.CodeUnknownCommit,
				State: "unknown", Digest: s.Spec.ContractDigest}
			if c.Category != want.Category || c.Effect != want.Effect || c.Retry != want.Retry || c.Code != want.Code || c.State != want.State {
				t.Fatalf("%+v", c)
			}
			if o.exit != nil && o.exit() != api.ExitUnknown {
				t.Fatalf("exit %d", o.exit())
			}
			if s.App.WriteAttempts() != 1 {
				t.Fatalf("an ambiguous write was attempted %d times", s.App.WriteAttempts())
			}
			sameCore(t, "status", r, o.status(r.Invocation))
			rec := o.reconcile(r.Invocation, st.SessAda, map[string]string{"stage": "won"})
			if rec.Error == nil || rec.Error.Code != api.CodeStillUnknown || rec.Category != api.CategoryUnknownCommit || rec.Effect != api.EffectUnknown {
				t.Fatalf("a matching state alone must not settle it: %+v", rec)
			}
			next := o.write("u2", nil)
			if next.Error == nil || next.Error.Code != api.CodeBlockedByUnknownCommit || next.Retry != api.RetryReconcile {
				t.Fatalf("%+v", next)
			}
			if s.App.WriteAttempts() != 1 {
				t.Fatal("the blocked write reached the source")
			}
		})
	}
}

func TestAnUnverifiedEffectMeansTheSameOnEverySurface(t *testing.T) {
	results := map[string]api.Result{}
	for _, name := range []string{"rest", "ui", "cli", "mcp"} {
		s := st.New(t, st.Opts{})
		x := newSurfaces(t, s)
		var o *ops
		for _, c := range x.all(st.Ada) {
			if c.name == name {
				o = c
			}
		}
		s.App.SetStale("o1", &fixtureapp.Order{ID: "o1", Stage: "new", Version: 1})
		r := o.write("v", nil)
		if r.Category != api.CategoryUnverifiedEffect || r.Effect != api.EffectUnknown || r.Retry != api.RetryReconcile || r.Error.Code != api.CodeUnverifiedEffect {
			t.Fatalf("%s: %+v", name, r)
		}
		if o.exit != nil && o.exit() != api.ExitUnverified {
			t.Fatalf("exit %d", o.exit())
		}
		if s.App.WriteAttempts() != 1 {
			t.Fatalf("%s: %d attempts", name, s.App.WriteAttempts())
		}
		results[name] = r
	}
	allSame(t, "unverified effect", results)
}

func TestRefusalsAndSourceFailuresMeanTheSameOnEverySurface(t *testing.T) {
	type scenario struct {
		name  string
		setup func(s *st.Stack)
		mut   func(m map[string]any)
		want  api.Code
		cat   api.Category
		exit  int
	}
	scenarios := []scenario{
		{"stale read", func(s *st.Stack) { s.App.Change("o1", "qualified") }, nil, api.CodePreconditionFailed, api.CategoryRefused, api.ExitRefused},
		{"no freshness precondition", nil, func(m map[string]any) { delete(m, "seen") }, api.CodePreconditionRequired, api.CategoryRefused, api.ExitRefused},
		{"no expectation", nil, func(m map[string]any) { delete(m, "expect") }, api.CodeInvalidArgument, api.CategoryRefused, api.ExitUsage},
		{"throttled source", func(s *st.Stack) { s.App.SetFaults(fixtureapp.Faults{ThrottleNext: 50}) }, nil, api.CodeSourceThrottled, api.CategoryFailed, api.ExitFailed},
		{"session of another source version", func(s *st.Stack) {
			sess := st.GoodSession(st.SessAda, st.Ada)
			sess.AppVersion = "3.0.0"
			s.Dir.SetSession(sess)
		}, nil, api.CodeSourceVersionUnsupported, api.CategoryRefused, api.ExitRefused},
		{"revoked source session", func(s *st.Stack) {
			sess := st.GoodSession(st.SessAda, st.Ada)
			sess.Revoked = true
			s.Dir.SetSession(sess)
		}, nil, api.CodeSessionInvalid, api.CategoryRefused, api.ExitRefused},
		{"source scope lost", func(s *st.Stack) {
			sess := st.GoodSession(st.SessAda, st.Ada)
			sess.SourceScopes = []string{"records.read"}
			s.Dir.SetSession(sess)
		}, nil, api.CodePermissionDenied, api.CategoryRefused, api.ExitAuth},
	}
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			results := map[string]api.Result{}
			for _, name := range []string{"rest", "ui", "cli", "mcp"} {
				s := st.New(t, st.Opts{})
				x := newSurfaces(t, s)
				var o *ops
				for _, c := range x.all(st.Ada) {
					if c.name == name {
						o = c
					}
				}
				if sc.setup != nil {
					sc.setup(s)
				}
				r := o.write("s", sc.mut)
				if r.Error == nil || r.Error.Code != sc.want || r.Category != sc.cat || r.Effect != api.EffectNone {
					t.Fatalf("%s: %+v", name, r)
				}
				if o.exit != nil && o.exit() != sc.exit {
					t.Fatalf("%s: exit %d, want %d", name, o.exit(), sc.exit)
				}
				if s.App.Commits() != 0 {
					t.Fatalf("%s: the source changed", name)
				}
				results[name] = r
			}
			allSame(t, sc.name, results)
		})
	}
}

func TestAnUnknownCommitOnOneSurfaceBlocksTheOthersAndAnyOfThemMayReconcileIt(t *testing.T) {
	s := st.New(t, st.Opts{Receipts: true})
	x := newSurfaces(t, s)
	surfaces := x.all(st.Ada)
	by := map[string]*ops{}
	for _, o := range surfaces {
		by[o.name] = o
	}
	s.App.SetFaults(fixtureapp.Faults{DropAfterCommit: 1})
	first := by["rest"].write("a", nil)
	if first.Category != api.CategoryUnknownCommit {
		t.Fatalf("%+v", first)
	}
	attempts := s.App.WriteAttempts()
	blocked := map[string]api.Result{}
	for _, name := range []string{"ui", "cli", "mcp"} {
		blocked[name] = by[name].write("b", nil)
		if blocked[name].Error == nil || blocked[name].Error.Code != api.CodeBlockedByUnknownCommit {
			t.Fatalf("%s: %+v", name, blocked[name])
		}
	}
	allSame(t, "blocked", blocked)
	if s.App.WriteAttempts() != attempts {
		t.Fatal("a blocked write reached the source")
	}
	for _, name := range []string{"ui", "cli", "mcp"} {
		sameCore(t, name+" sees the unknown outcome", first, by[name].status(first.Invocation))
	}
	got := by["mcp"].reconcile(first.Invocation, st.SessAda, map[string]string{"stage": "won"})
	if got.Category != api.CategorySucceeded || got.Reconciliation == nil || !got.Reconciliation.Settled || got.Reason != "reconciled_committed" {
		t.Fatalf("a source receipt must settle it: %+v", got)
	}
	for _, name := range []string{"rest", "ui", "cli", "mcp"} {
		if c := by[name].status(first.Invocation); c.Category != api.CategorySucceeded || c.Effect != api.EffectApplied {
			t.Fatalf("%s: %+v", name, c)
		}
	}
	again := by["cli"].reconcile(first.Invocation, st.SessAda, map[string]string{"stage": "won"})
	if again.Error == nil || again.Error.Code != api.CodeNotReconcilable {
		t.Fatalf("only an unknown outcome can be reconciled: %+v", again)
	}
	later := by["ui"].write("c", nil)
	if later.Error != nil && later.Error.Code == api.CodeBlockedByUnknownCommit {
		t.Fatal("a settled outcome must lift the block")
	}
}

func TestCancellingAQueuedWriteIsReportedTheSameOnEverySurface(t *testing.T) {
	results := map[string]api.Result{}
	for _, name := range []string{"rest", "ui", "cli", "mcp"} {
		s := st.New(t, st.Opts{})
		x := newSurfaces(t, s)
		var o *ops
		for _, c := range x.all(st.Ada) {
			if c.name == name {
				o = c
			}
		}
		r := o.write("done", nil)
		if r.Category != api.CategorySucceeded {
			t.Fatalf("%+v", r)
		}
		c := o.cancel(r.Invocation)
		if c.Cancel != "already_finished" && c.Cancel != "too_late" {
			t.Fatalf("%s: a finished write cannot be cancelled: %+v", name, c)
		}
		results[name] = c
	}
	allSame(t, "cancel after completion", results)
}
