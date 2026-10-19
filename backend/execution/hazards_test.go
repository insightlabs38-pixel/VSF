package execution

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestTheHTTPTransportNeverReplaysAWriteCarryingADedupKey(t *testing.T) {
	var posts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"version":"v1","stage":"new"}`))
			return
		}
		posts.Add(1)
		c, _, _ := w.(http.Hijacker).Hijack()
		_ = c.Close()
	}))
	defer srv.Close()
	e := NewAPIExecutor(APIConfig{BaseURL: srv.URL, ReadPath: "/o/%s", WritePath: "/o/%s", Dedup: true, Client: &http.Client{Timeout: 2 * time.Second}})
	ctx := context.Background()
	if _, err := e.Read(ctx, Target{Record: "o1"}); err != nil {
		t.Fatal(err)
	}
	_, err := e.Apply(ctx, Call{Key: "k", DedupToken: "k", IfMatch: "v1", Target: Target{Record: "o1", Params: map[string]any{"stage": "won"}}})
	if err == nil {
		t.Fatal("a dropped connection was reported as success")
	}
	if n := posts.Load(); n != 1 {
		t.Fatalf("the source received %d writes; the transport replayed one", n)
	}
}

func TestRedirectsOnWritesAreNotFollowed(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path == "/o/o1" {
			http.Redirect(w, r, "/elsewhere", http.StatusTemporaryRedirect)
		}
	}))
	defer srv.Close()
	e := NewAPIExecutor(APIConfig{BaseURL: srv.URL, ReadPath: "/o/%s", WritePath: "/o/%s"})
	res, err := e.Apply(context.Background(), Call{Target: Target{Record: "o1", Params: map[string]any{"stage": "won"}}})
	if err != nil || res.Outcome != OutcomeUnknown {
		t.Fatalf("%+v %v", res, err)
	}
	if hits.Load() != 1 {
		t.Fatalf("%d requests", hits.Load())
	}
}

func TestAConnectionRefusedBeforeSendingIsReportedAsNotSent(t *testing.T) {
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := l.Addr().String()
	l.Close()
	e := NewAPIExecutor(APIConfig{BaseURL: "http://" + addr, ReadPath: "/o/%s", WritePath: "/o/%s"})
	_, err := e.Apply(context.Background(), Call{Target: Target{Record: "o1"}})
	if !errorsIs(err, ErrNotSent) {
		t.Fatalf("err %v", err)
	}
}

func TestANoEffectClaimTheSourceNowContradictsBecomesUnknown(t *testing.T) {
	cases := []struct {
		name    string
		version string
		fields  map[string]string
		state   State
		reason  string
	}{
		{"our change is visible under a new version", "v2", map[string]string{"stage": "won"}, Unknown, ReasonUnknownCommit},
		{"nothing changed", "v1", map[string]string{"stage": "new"}, Failed, "conflict"},
		{"someone else changed it to something else", "v2", map[string]string{"stage": "qualified"}, Failed, "conflict"},
		{"the version is unchanged though the value matches", "v1", map[string]string{"stage": "won"}, Failed, "conflict"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t, nil)
			r.ex.applyFn = func(context.Context, Call) (Result, error) {
				r.ex.mu.Lock()
				r.ex.version, r.ex.fields = c.version, c.fields
				r.ex.mu.Unlock()
				return Result{Outcome: OutcomeConflict, Detail: "409"}, nil
			}
			rec, _ := r.run(r.request("req-1"))
			mustState(t, rec, c.state, c.reason)
			if c.state == Unknown && !strings.Contains(rec.Detail, "no_effect_contradicted:conflict") {
				t.Fatalf("detail %q", rec.Detail)
			}
		})
	}
}

func TestAnUnreadableSourceKeepsTheExecutorsNoEffectClaim(t *testing.T) {
	r := newRig(t, nil)
	r.ex.applyFn = func(context.Context, Call) (Result, error) {
		r.ex.mu.Lock()
		r.ex.readErr = context.DeadlineExceeded
		r.ex.mu.Unlock()
		return Result{Outcome: OutcomeRejected}, nil
	}
	rec, _ := r.run(r.request("req-1"))
	mustState(t, rec, Failed, "rejected")
}
