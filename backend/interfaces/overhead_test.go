//go:build integration

package interfaces

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/insightlabs38-pixel/vsf/backend/api"
	"github.com/insightlabs38-pixel/vsf/backend/execution"
	st "github.com/insightlabs38-pixel/vsf/backend/internal/stacktest"
)

// instant is a source that answers at once, so what is measured is only what VSF itself spends.
type instant struct {
	mu     sync.Mutex
	stages map[string]string
	ver    map[string]int
}

func newInstant() *instant { return &instant{stages: map[string]string{}, ver: map[string]int{}} }

func (*instant) Kind() string { return "provider_api_authorized" }
func (*instant) Capabilities() execution.Capabilities {
	return execution.Capabilities{ReadBack: true, AuthoritativeStatus: true, DedupTokens: true}
}

func (i *instant) Read(_ context.Context, t execution.Target) (execution.Snapshot, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if _, ok := i.stages[t.Record]; !ok {
		i.stages[t.Record], i.ver[t.Record] = "new", 1
	}
	return execution.Snapshot{Version: fmt.Sprintf("v%d", i.ver[t.Record]), Fields: map[string]string{"stage": i.stages[t.Record]}, ObservedAt: time.UnixMilli(1)}, nil
}

func (i *instant) Apply(_ context.Context, c execution.Call) (execution.Result, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.stages[c.Target.Record] = c.Target.Params["stage"].(string)
	i.ver[c.Target.Record]++
	return execution.Result{Outcome: execution.OutcomeCommitted}, nil
}

type stats struct {
	N                       int
	P50, P95, P99, Max, Avg time.Duration
}

func summarize(d []time.Duration) stats {
	s := append([]time.Duration(nil), d...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	var sum time.Duration
	for _, v := range s {
		sum += v
	}
	at := func(q float64) time.Duration { return s[min(len(s)-1, int(q*float64(len(s))))] }
	return stats{len(s), at(.5), at(.95), at(.99), s[len(s)-1], sum / time.Duration(len(s))}
}

func (s stats) String() string {
	return fmt.Sprintf("n=%d p50=%v p95=%v p99=%v max=%v avg=%v", s.N, s.P50, s.P95, s.P99, s.Max, s.Avg)
}

const (
	overheadWarmup = 30
	overheadRuns   = 400
)

func envelopeFor(i int, layer string) []byte {
	rec := fmt.Sprintf("%s-%d", layer, i)
	return st.Envelope(func(m map[string]any) {
		m["request_id"] = "ovh-" + rec
		m["record"] = rec
		m["params"].(map[string]any)["record_id"] = rec
	})
}

type layer struct {
	name string
	run  func(i int) error
}

func measure(t *testing.T, l layer) stats {
	t.Helper()
	for i := 0; i < overheadWarmup; i++ {
		if err := l.run(-1 - i); err != nil {
			t.Fatalf("%s warmup: %v", l.name, err)
		}
	}
	d := make([]time.Duration, 0, overheadRuns)
	for i := 0; i < overheadRuns; i++ {
		t0 := time.Now()
		if err := l.run(i); err != nil {
			t.Fatalf("%s run %d: %v", l.name, i, err)
		}
		d = append(d, time.Since(t0))
	}
	return summarize(d)
}

// budgets are documented in docs/deep/policy/dispatch-budget.md; each is a ceiling above the measured layer cost.
type budget struct{ p50, p99 time.Duration }

var layerBudget = map[string]budget{
	"http-floor": {2 * time.Millisecond, 10 * time.Millisecond},
	"runtime":    {4 * time.Millisecond, 20 * time.Millisecond},
	"service":    {5 * time.Millisecond, 20 * time.Millisecond},
	"rest":       {6 * time.Millisecond, 25 * time.Millisecond},
	"ui-client":  {6 * time.Millisecond, 25 * time.Millisecond},
	"mcp":        {6 * time.Millisecond, 25 * time.Millisecond},
	"cli":        {8 * time.Millisecond, 30 * time.Millisecond},
}

// layerCost is what one interface adds on top of the layer it wraps, by median.
var layerCost = []struct {
	layer, over string
	max         time.Duration
}{
	{"service", "runtime", 2 * time.Millisecond},
	{"rest", "service", 3 * time.Millisecond},
	{"ui-client", "service", 3 * time.Millisecond},
	{"mcp", "service", 3 * time.Millisecond},
	{"cli", "service", 6 * time.Millisecond},
}

func TestDispatchOverheadPerLayerExcludesTheSourceAndStaysInBudget(t *testing.T) {
	if testing.Short() {
		t.Skip("measurement")
	}
	s := st.New(t, st.Opts{WrapExecutor: func(execution.Executor) execution.Executor { return newInstant() }})
	x := newSurfaces(t, s)
	adaTok := s.Principal(st.Ada, api.SurfaceREST)
	route := api.Route{Operation: s.Spec.Name, Version: s.Spec.ContractVersion}
	restTok := s.Token(st.Ada, api.SurfaceREST)
	uiClient, _ := api.NewClient(x.rest.URL, s.Token(st.Ada, api.SurfaceUI), api.SurfaceUI, nil)
	cliClient, _ := api.NewClient(x.rest.URL, s.Token(st.Ada, api.SurfaceCLI), api.SurfaceCLI, nil)
	mc := &mcpClient{t: t, url: x.mcp.URL, token: s.Token(st.Ada, api.SurfaceMCP)}
	hc := &http.Client{Transport: &http.Transport{MaxIdleConnsPerHost: 8}}

	succeeded := func(r api.Result) error {
		if r.Category != api.CategorySucceeded {
			return fmt.Errorf("%s %+v", r.Category, r.Error)
		}
		return nil
	}
	layers := []layer{
		{"http-floor", func(int) error {
			req, _ := http.NewRequest("GET", x.rest.URL+"/v1/discover", nil)
			resp, err := hc.Do(req)
			if err != nil {
				return err
			}
			resp.Body.Close()
			return nil
		}},
		{"runtime", func(i int) error {
			rec := fmt.Sprintf("runtime-%d", i)
			h, err := s.RT.Submit(t.Context(), execution.Request{
				RequestID: "ovh-" + rec, Caller: st.Ada, SessionID: st.SessAda, Contract: s.Doc,
				Pin:    execution.Pin{Version: s.Spec.ContractVersion, Digest: s.Spec.ContractDigest},
				Params: map[string]any{"record_id": rec, "stage": "won"}, Record: rec, Expect: map[string]string{"stage": "won"},
				Seen: &execution.Seen{Version: "v1", ObservedAt: time.UnixMilli(1)},
			})
			if err != nil {
				return err
			}
			r, err := h.Wait(t.Context())
			if err == nil && r.State != execution.Verified {
				err = fmt.Errorf("state %s", r.State)
			}
			return err
		}},
		{"service", func(i int) error {
			return succeeded(s.Svc.Invoke(t.Context(), adaTok, envelopeFor(i, "service"), route))
		}},
		{"rest", func(i int) error {
			req, _ := http.NewRequest("POST", x.rest.URL+invokePath(s.Spec, s.Spec.ContractVersion), bytes.NewReader(envelopeFor(i, "rest")))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+restTok)
			resp, err := hc.Do(req)
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			if resp.StatusCode != 200 {
				return fmt.Errorf("status %d", resp.StatusCode)
			}
			return nil
		}},
		{"ui-client", func(i int) error {
			return succeeded(uiClient.Invoke(t.Context(), route, envelopeFor(i, "ui")))
		}},
		{"mcp", func(i int) error {
			r := mc.call("tools/call", map[string]any{"name": s.Spec.Name, "arguments": bodyFromEnvelope(envelopeFor(i, "mcp"))})
			if r.Status != 200 || r.Result == nil || r.Result["isError"] != false {
				return fmt.Errorf("%d %s", r.Status, r.Raw)
			}
			return nil
		}},
		{"cli", func(i int) error {
			rec := fmt.Sprintf("cli-%d", i)
			o := runCLI(t, cliClient, "run", s.Spec.Name, "--session", st.SessAda, "--request-id", "ovh-"+rec, "--record", rec,
				"--param", "record_id="+rec, "--param", "stage=won", "--expect", "stage=won", "--seen-version", "v1", "--seen-at", "1")
			if o.Code != 0 {
				return fmt.Errorf("exit %d %s", o.Code, o.Stdout)
			}
			return nil
		}},
	}
	got := map[string]stats{}
	for _, l := range layers {
		got[l.name] = measure(t, l)
		t.Logf("%-10s %s", l.name, got[l.name])
	}
	if os.Getenv("VSF_OVERHEAD_REPORT") != "" {
		writeOverheadReport(t, os.Getenv("VSF_OVERHEAD_REPORT"), layers, got)
	}
	scale := time.Duration(1)
	if raceEnabled {
		scale = 10
	}
	for _, c := range layerCost {
		if d := got[c.layer].P50 - got[c.over].P50; d > c.max*scale {
			t.Errorf("%s adds %v over %s at the median, budget %v", c.layer, d, c.over, c.max*scale)
		}
	}
	for name, b := range layerBudget {
		g := got[name]
		if g.P50 > b.p50*scale || g.P99 > b.p99*scale {
			t.Errorf("%s: p50 %v (budget %v) p99 %v (budget %v)", name, g.P50, b.p50*scale, g.P99, b.p99*scale)
		}
	}
}

func bodyFromEnvelope(b []byte) map[string]any {
	var m map[string]any
	_ = jsonUnmarshal(string(b), &m)
	return m
}

func writeOverheadReport(t *testing.T, path string, layers []layer, got map[string]stats) {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "Dispatch overhead per layer, source answering instantly (n=%d after %d warm-up), race detector: %v\n", overheadRuns, overheadWarmup, raceEnabled)
	fmt.Fprintf(&buf, "%-11s %9s %9s %9s %9s %9s\n", "layer", "p50", "p95", "p99", "max", "avg")
	for _, l := range layers {
		g := got[l.name]
		fmt.Fprintf(&buf, "%-11s %9v %9v %9v %9v %9v\n", l.name, g.P50, g.P95, g.P99, g.Max, g.Avg)
	}
	fmt.Fprintln(&buf, "per-layer cost = layer p50 minus the layer below: service over runtime; rest, ui-client, mcp over service; cli is two round trips (describe, run) over service.")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}
