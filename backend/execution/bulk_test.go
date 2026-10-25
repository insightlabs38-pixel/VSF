package execution

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type stepClock struct {
	mu    sync.Mutex
	t     time.Time
	slept []time.Duration
}

func (c *stepClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *stepClock) Sleep(ctx context.Context, d time.Duration) error {
	c.mu.Lock()
	c.slept = append(c.slept, d)
	c.t = c.t.Add(d)
	c.mu.Unlock()
	return ctx.Err()
}

// productSource is a server-backed table: records, a filter, pages of five, and switches for hostile behaviour.
type productSource struct {
	mu        sync.Mutex
	records   []string
	applied   map[string]int // record -> number of times the action reached the source
	committed map[string]bool
	pageSize  int
	filter    string
	version   int
	// script gives, per record, the outcomes of successive attempts; an exhausted script commits.
	script map[string][]Result
	err    map[string]error
	calls  []string
	tokens map[string][]string
}

func newProducts(n int) *productSource {
	p := &productSource{applied: map[string]int{}, committed: map[string]bool{}, pageSize: 5, script: map[string][]Result{}, err: map[string]error{}, tokens: map[string][]string{}}
	for i := 1; i <= n; i++ {
		p.records = append(p.records, "p"+strconv.Itoa(i))
	}
	return p
}

func (p *productSource) tokenFor(filter string, cursor int) string {
	return fmt.Sprintf("f=%s;v=%d;c=%d", filter, p.version, cursor)
}

func (p *productSource) List(_ context.Context, filter, cursor string) (Listing, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	start := 0
	if cursor != "" {
		for _, part := range strings.Split(cursor, ";") {
			if v, ok := strings.CutPrefix(part, "c="); ok {
				start, _ = strconv.Atoi(v)
			}
		}
	}
	end := min(start+p.pageSize, len(p.records))
	l := Listing{Token: p.tokenFor(filter, start), FilterToken: "flt:" + filter + ":" + strconv.Itoa(p.version/100), Total: len(p.records), IDs: append([]string(nil), p.records[start:end]...)}
	if end < len(p.records) {
		l.Next = p.tokenFor(filter, end)
	}
	return l, nil
}

func (p *productSource) Kind() string { return "fake" }
func (p *productSource) Capabilities() Capabilities {
	return Capabilities{AuthoritativeStatus: true, DedupTokens: true, ReadBack: true}
}
func (p *productSource) Read(context.Context, Target) (Snapshot, error) {
	return Snapshot{Fields: map[string]string{}}, nil
}

func (p *productSource) Apply(_ context.Context, c Call) (Result, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	id := c.Target.Record
	p.calls = append(p.calls, id)
	p.tokens[id] = append(p.tokens[id], c.DedupToken)
	if err := p.err[id]; err != nil {
		p.applied[id]++
		return Result{}, err
	}
	if s := p.script[id]; len(s) > 0 {
		r := s[0]
		p.script[id] = s[1:]
		if r.Outcome == OutcomeCommitted {
			p.applied[id]++
			p.committed[id] = true
		}
		return r, nil
	}
	p.applied[id]++
	p.committed[id] = true
	return Result{Outcome: OutcomeCommitted}, nil
}

func (p *productSource) remove(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, r := range p.records {
		if r == id {
			p.records = append(p.records[:i], p.records[i+1:]...)
			p.version++
			return
		}
	}
}

func bulk(p *productSource, edit func(*BulkConfig)) (*BulkExecutor, *stepClock) {
	clk := &stepClock{t: time.Unix(1000, 0)}
	cfg := BulkConfig{Inner: p, Lister: p, Clock: clk, Backoff: time.Second, MaxAttempts: 3}
	if edit != nil {
		edit(&cfg)
	}
	return NewBulkExecutor(cfg), clk
}

func call(params map[string]any) Call {
	return Call{Key: "op1", Target: Target{Params: params}}
}

func TestSelectedIdsApplyToExactlyThoseRecords(t *testing.T) {
	p := newProducts(12)
	e, _ := bulk(p, nil)
	res, err := e.Apply(context.Background(), call(map[string]any{"scope": "selected_ids", "ids": []string{"p2", "p9"}}))
	if err != nil || res.Outcome != OutcomeCommitted || len(p.committed) != 2 || !p.committed["p2"] || !p.committed["p9"] {
		t.Fatalf("%+v %v %v", res, err, p.committed)
	}
}

func TestVisiblePageSelectionIsNotSelectAll(t *testing.T) {
	p := newProducts(12)
	e, _ := bulk(p, nil)
	first, _ := p.List(context.Background(), "", "")
	res, _ := e.Apply(context.Background(), call(map[string]any{"scope": "visible_page", "page_token": first.Token}))
	if res.Outcome != OutcomeCommitted || len(p.committed) != 5 {
		t.Fatalf("a page of five changed %d records: %+v", len(p.committed), res)
	}
	if p.committed["p6"] {
		t.Fatal("a record off the page was changed")
	}
}

func TestAllMatchingNeedsTheCountAndFilterThePersonSaw(t *testing.T) {
	p := newProducts(12)
	e, _ := bulk(p, nil)
	first, _ := p.List(context.Background(), "", "")
	for _, c := range []struct {
		why    string
		params map[string]any
	}{
		{BulkCountMismatch, map[string]any{"scope": "all_matching", "filter_token": first.FilterToken, "expected_total": 47}},
		{BulkCountMismatch, map[string]any{"scope": "all_matching", "filter_token": first.FilterToken}},
		{BulkStaleFilter, map[string]any{"scope": "all_matching", "filter_token": "flt::99", "expected_total": 12}},
		{BulkStaleFilter, map[string]any{"scope": "all_matching", "expected_total": 12}},
	} {
		res, _ := e.Apply(context.Background(), call(c.params))
		if res.Outcome != OutcomeRejected || res.Detail != c.why {
			t.Errorf("%s: %+v", c.why, res)
		}
	}
	if len(p.calls) != 0 {
		t.Fatalf("%d writes happened before the plan was confirmed", len(p.calls))
	}
	res, _ := e.Apply(context.Background(), call(map[string]any{"scope": "all_matching", "filter_token": first.FilterToken, "expected_total": 12}))
	if res.Outcome != OutcomeCommitted || len(p.committed) != 12 {
		t.Fatalf("%+v %d", res, len(p.committed))
	}
}

func TestRecordsAddedOrRemovedSinceThePersonLookedRefuseThePlanBeforeAnyWrite(t *testing.T) {
	p := newProducts(12)
	e, _ := bulk(p, nil)
	first, _ := p.List(context.Background(), "", "")
	p.remove("p3") // another user deleted a row; the page the person saw is stale
	res, _ := e.Apply(context.Background(), call(map[string]any{"scope": "visible_page", "page_token": first.Token}))
	if res.Outcome != OutcomeRejected || res.Detail != BulkStalePage {
		t.Fatalf("%+v", res)
	}
	res, _ = e.Apply(context.Background(), call(map[string]any{"scope": "selected_ids", "ids": []string{"p3", "p4"}}))
	if res.Outcome != OutcomeRejected || res.Detail != BulkMissingItems {
		t.Fatalf("%+v", res)
	}
	if len(p.calls) != 0 {
		t.Fatalf("writes happened: %v", p.calls)
	}
}

func TestPlansAreBoundedAndMalformedScopesAreRefused(t *testing.T) {
	p := newProducts(30)
	e, _ := bulk(p, func(c *BulkConfig) { c.MaxItems = 10 })
	first, _ := p.List(context.Background(), "", "")
	for _, c := range []struct {
		why    string
		params map[string]any
	}{
		{BulkTooManyItems, map[string]any{"scope": "all_matching", "filter_token": first.FilterToken, "expected_total": 30}},
		{BulkScopeInvalid, map[string]any{"scope": "everything"}},
		{BulkScopeInvalid, map[string]any{"scope": "selected_ids", "ids": []string{"p1", "p1"}}},
		{BulkNothingToApply, map[string]any{"scope": "selected_ids", "ids": []string{}}},
		{BulkTooManyItems, map[string]any{"scope": "selected_ids", "ids": []string{"p1", "p2", "p3", "p4", "p5", "p6", "p7", "p8", "p9", "p10", "p11"}}},
	} {
		res, _ := e.Apply(context.Background(), call(c.params))
		if res.Outcome != OutcomeRejected || res.Detail != c.why {
			t.Errorf("%s: %+v", c.why, res)
		}
	}
	if len(p.calls) != 0 {
		t.Fatal("a refused plan wrote")
	}
}

func TestAThrottledRecordWaitsForTheHintAndIsRetriedOnceWithTheSameToken(t *testing.T) {
	p := newProducts(6)
	p.script["p2"] = []Result{{Outcome: OutcomeThrottled, RetryAfter: 7 * time.Second}}
	e, clk := bulk(p, nil)
	res, _ := e.Apply(context.Background(), call(map[string]any{"scope": "selected_ids", "ids": []string{"p1", "p2", "p3"}}))
	if res.Outcome != OutcomeCommitted || p.applied["p2"] != 1 {
		t.Fatalf("%+v applied=%v", res, p.applied)
	}
	if len(clk.slept) != 1 || clk.slept[0] != 7*time.Second {
		t.Fatalf("waited %v", clk.slept)
	}
	if got := p.tokens["p2"]; len(got) != 2 || got[0] != got[1] || got[0] != "op1:p2" {
		t.Fatalf("tokens %v", got)
	}
}

func TestAnUnknownOutcomeIsNeverRetriedAndStopsTheRun(t *testing.T) {
	p := newProducts(6)
	p.err["p2"] = errors.New("connection reset after the request was sent")
	e, _ := bulk(p, func(c *BulkConfig) { c.ContinueAfterFailure = true; c.Idempotent = true })
	res, _ := e.Apply(context.Background(), call(map[string]any{"scope": "selected_ids", "ids": []string{"p1", "p2", "p3", "p4"}}))
	if res.Outcome != OutcomeUnknown {
		t.Fatalf("%+v", res)
	}
	if p.applied["p2"] != 1 {
		t.Fatalf("an ambiguous write was sent %d times", p.applied["p2"])
	}
	if p.committed["p3"] || p.committed["p4"] {
		t.Fatal("records after an unknown outcome were attempted")
	}
	if res.Echo["item:p1"] != "committed" || res.Echo["item:p2"] != "unknown" || res.Echo["item:p3"] != "not_attempted" {
		t.Fatalf("%v", res.Echo)
	}
}

func TestPartialSuccessIsReportedPerRecordAndNeverCalledAFailure(t *testing.T) {
	p := newProducts(6)
	p.script["p2"] = []Result{{Outcome: OutcomeConflict, Detail: "412"}}
	e, _ := bulk(p, func(c *BulkConfig) { c.ContinueAfterFailure = true })
	res, _ := e.Apply(context.Background(), call(map[string]any{"scope": "selected_ids", "ids": []string{"p1", "p2", "p3"}}))
	if res.Outcome != OutcomeUnknown || res.Detail == "" {
		t.Fatalf("some records changed, so the whole is not a clean failure: %+v", res)
	}
	if res.Echo["item:p1"] != "committed" || res.Echo["item:p2"] != "conflict" || res.Echo["item:p3"] != "committed" {
		t.Fatalf("%v", res.Echo)
	}
}

func TestWhenNothingChangedTheWholeRunReportsTheSourcesOwnRefusal(t *testing.T) {
	p := newProducts(6)
	p.script["p1"] = []Result{{Outcome: OutcomeRejected, Detail: "forbidden"}}
	e, _ := bulk(p, nil)
	res, _ := e.Apply(context.Background(), call(map[string]any{"scope": "selected_ids", "ids": []string{"p1", "p2"}}))
	if res.Outcome != OutcomeRejected || p.committed["p2"] {
		t.Fatalf("%+v", res)
	}
	if !res.Outcome.NoEffect() {
		t.Fatal("a run that changed nothing must count as no effect")
	}
}

func TestRetriesStopAtTheBound(t *testing.T) {
	p := newProducts(3)
	p.script["p1"] = []Result{{Outcome: OutcomeThrottled}, {Outcome: OutcomeThrottled}, {Outcome: OutcomeThrottled}, {Outcome: OutcomeThrottled}}
	e, clk := bulk(p, nil)
	res, _ := e.Apply(context.Background(), call(map[string]any{"scope": "selected_ids", "ids": []string{"p1"}}))
	if res.Outcome != OutcomeThrottled || len(clk.slept) != 2 || p.applied["p1"] != 0 {
		t.Fatalf("%+v slept=%v", res, clk.slept)
	}
	if clk.slept[1] != 2*clk.slept[0] {
		t.Fatalf("backoff did not grow: %v", clk.slept)
	}
}

func TestLargeRunsKeepEveryRecordAccountedFor(t *testing.T) {
	p := newProducts(400)
	p.pageSize = 37
	e, _ := bulk(p, func(c *BulkConfig) { c.MaxItems = 500 })
	first, _ := p.List(context.Background(), "", "")
	res, _ := e.Apply(context.Background(), call(map[string]any{"scope": "all_matching", "filter_token": first.FilterToken, "expected_total": 400}))
	if res.Outcome != OutcomeCommitted || len(res.Echo) != 400 || len(p.committed) != 400 {
		t.Fatalf("%v %d %d", res.Outcome, len(res.Echo), len(p.committed))
	}
	for id, n := range p.applied {
		if n != 1 {
			t.Fatalf("%s applied %d times", id, n)
		}
	}
}
