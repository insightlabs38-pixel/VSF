package execution

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Refusal reasons for bulk plans; each is decided from a fresh listing before any write.
const (
	BulkScopeInvalid   = "bulk.scope_invalid"
	BulkStalePage      = "bulk.stale_page"
	BulkStaleFilter    = "bulk.stale_filter"
	BulkCountMismatch  = "bulk.count_mismatch"
	BulkMissingItems   = "bulk.missing_items"
	BulkTooManyItems   = "bulk.too_many_items"
	BulkNothingToApply = "bulk.nothing_to_apply"
)

// Listing is one page of a source's records as the source reports it now.
type Listing struct {
	// Token names this exact page of this exact filter; a source changes it when either changes.
	Token string
	// FilterToken names the filter and sort alone.
	FilterToken string
	Total       int
	IDs         []string
	Next        string
}

// Lister reads pages of records by stable id; positions are never used to name a record.
type Lister interface {
	List(ctx context.Context, filter, cursor string) (Listing, error)
}

// BulkConfig controls how a bulk action walks and applies records.
type BulkConfig struct {
	Inner  Executor
	Lister Lister
	// MaxItems bounds a plan; larger plans are refused rather than truncated.
	MaxItems int
	// MaxAttempts per item for outcomes the source proved changed nothing.
	MaxAttempts int
	Backoff     time.Duration
	// Idempotent says the action is safe to repeat even where the source cannot prove a no-effect refusal.
	Idempotent bool
	// ContinueAfterFailure applies the remaining items after a refusal; an unknown outcome always stops the run.
	ContinueAfterFailure bool
	Clock                Clock
	Limiter              *Limiter
	App                  string
}

// BulkExecutor applies one action to many records and reports exactly which ones the source changed.
type BulkExecutor struct{ cfg BulkConfig }

func NewBulkExecutor(cfg BulkConfig) *BulkExecutor {
	if cfg.MaxItems <= 0 {
		cfg.MaxItems = 500
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 3
	}
	if cfg.Backoff <= 0 {
		cfg.Backoff = 250 * time.Millisecond
	}
	if cfg.Clock == nil {
		cfg.Clock = realClock{}
	}
	return &BulkExecutor{cfg: cfg}
}

func (e *BulkExecutor) Kind() string { return "bulk_over_" + e.cfg.Inner.Kind() }

func (e *BulkExecutor) Capabilities() Capabilities {
	c := e.cfg.Inner.Capabilities()
	c.ReceiptLookup, c.Fencing = false, false
	return c
}

func (e *BulkExecutor) Read(ctx context.Context, t Target) (Snapshot, error) {
	return e.cfg.Inner.Read(ctx, t)
}

type bulkPlan struct {
	scope, filter, pageToken, filterToken string
	ids                                   []string
	expectedTotal                         int
	perItem                               map[string]any
}

func planOf(p map[string]any) (bulkPlan, error) {
	str := func(k string) string { s, _ := p[k].(string); return s }
	plan := bulkPlan{scope: str("scope"), filter: str("filter"), pageToken: str("page_token"), filterToken: str("filter_token")}
	switch v := p["expected_total"].(type) {
	case int:
		plan.expectedTotal = v
	case int64:
		plan.expectedTotal = int(v)
	case float64:
		plan.expectedTotal = int(v)
	}
	switch v := p["ids"].(type) {
	case []string:
		plan.ids = v
	case []any:
		for _, x := range v {
			s, ok := x.(string)
			if !ok || s == "" {
				return plan, refuse(BulkScopeInvalid)
			}
			plan.ids = append(plan.ids, s)
		}
	}
	plan.perItem, _ = p["item"].(map[string]any)
	switch plan.scope {
	case "selected_ids", "visible_page", "all_matching":
	default:
		return plan, refuse(BulkScopeInvalid)
	}
	return plan, nil
}

// resolve turns a plan into the exact ids to change, from a fresh listing; it never guesses a scope from a visible control.
func (e *BulkExecutor) resolve(ctx context.Context, plan bulkPlan) ([]string, error) {
	switch plan.scope {
	case "selected_ids":
		if len(plan.ids) == 0 {
			return nil, refuse(BulkNothingToApply)
		}
		if len(plan.ids) > e.cfg.MaxItems {
			return nil, refuse(BulkTooManyItems)
		}
		seen := map[string]bool{}
		for _, id := range plan.ids {
			if seen[id] {
				return nil, refuse(BulkScopeInvalid)
			}
			seen[id] = true
		}
		// Selected ids must still exist; a record that vanished since the selection is reported, never skipped.
		present, err := e.present(ctx, plan.filter)
		if err != nil {
			return nil, err
		}
		for _, id := range plan.ids {
			if !present[id] {
				return nil, refuse(BulkMissingItems)
			}
		}
		return plan.ids, nil
	case "visible_page":
		page, err := e.cfg.Lister.List(ctx, plan.filter, plan.pageToken)
		if err != nil {
			return nil, err
		}
		if page.Token != plan.pageToken || (plan.filterToken != "" && page.FilterToken != plan.filterToken) {
			return nil, refuse(BulkStalePage)
		}
		if len(page.IDs) == 0 {
			return nil, refuse(BulkNothingToApply)
		}
		if len(page.IDs) > e.cfg.MaxItems {
			return nil, refuse(BulkTooManyItems)
		}
		return page.IDs, nil
	default: // all_matching
		first, err := e.cfg.Lister.List(ctx, plan.filter, "")
		if err != nil {
			return nil, err
		}
		if plan.filterToken == "" || first.FilterToken != plan.filterToken {
			return nil, refuse(BulkStaleFilter)
		}
		if plan.expectedTotal <= 0 || first.Total != plan.expectedTotal {
			return nil, refuse(BulkCountMismatch)
		}
		if first.Total > e.cfg.MaxItems {
			return nil, refuse(BulkTooManyItems)
		}
		var ids []string
		for page := first; ; {
			if page.FilterToken != plan.filterToken {
				return nil, refuse(BulkStaleFilter)
			}
			ids = append(ids, page.IDs...)
			if page.Next == "" {
				break
			}
			if page, err = e.cfg.Lister.List(ctx, plan.filter, page.Next); err != nil {
				return nil, err
			}
		}
		if len(ids) != plan.expectedTotal {
			return nil, refuse(BulkCountMismatch)
		}
		return ids, nil
	}
}

func (e *BulkExecutor) present(ctx context.Context, filter string) (map[string]bool, error) {
	out := map[string]bool{}
	cursor := ""
	for pages := 0; pages <= e.cfg.MaxItems; pages++ {
		page, err := e.cfg.Lister.List(ctx, filter, cursor)
		if err != nil {
			return nil, err
		}
		for _, id := range page.IDs {
			out[id] = true
		}
		if page.Next == "" {
			return out, nil
		}
		cursor = page.Next
	}
	return nil, refuse(BulkTooManyItems)
}

// retryable is true only where the source proved the attempt changed nothing and repeating it is safe.
func (e *BulkExecutor) retryable(res Result) bool {
	switch res.Outcome {
	case OutcomeThrottled:
		return e.cfg.Inner.Capabilities().AuthoritativeStatus
	case OutcomeUnavailable:
		return e.cfg.Idempotent || e.cfg.Inner.Capabilities().DedupTokens
	}
	return false
}

// Apply resolves the plan from fresh listings, then applies it one record at a time.
func (e *BulkExecutor) Apply(ctx context.Context, c Call) (Result, error) {
	plan, err := planOf(c.Target.Params)
	if err != nil {
		return Result{Outcome: OutcomeRejected, Detail: reasonOf(err)}, nil
	}
	ids, err := e.resolve(ctx, plan)
	if err != nil {
		if _, ok := err.(*Refusal); ok {
			return Result{Outcome: OutcomeRejected, Detail: reasonOf(err)}, nil
		}
		return Result{}, fmt.Errorf("%w: %v", ErrNotSent, err)
	}
	state := map[string]string{}
	var firstRetry time.Duration
	for n, id := range ids {
		Progress(ctx, float64(n)/float64(len(ids)), "record "+id)
		res := e.one(ctx, c, id, plan.perItem)
		state[id] = string(res.Outcome)
		if res.RetryAfter > firstRetry {
			firstRetry = res.RetryAfter
		}
		if res.Outcome == OutcomeUnknown {
			for _, rest := range ids[n+1:] {
				state[rest] = "not_attempted"
			}
			break
		}
		if res.Outcome != OutcomeCommitted && !e.cfg.ContinueAfterFailure {
			for _, rest := range ids[n+1:] {
				state[rest] = "not_attempted"
			}
			break
		}
	}
	return summarize(ids, state, firstRetry), nil
}

// one applies the action to one record, repeating it only after outcomes that provably changed nothing.
func (e *BulkExecutor) one(ctx context.Context, c Call, id string, item map[string]any) Result {
	params := map[string]any{}
	for k, v := range item {
		params[k] = v
	}
	params["record"] = id
	call := Call{Key: c.Key + ":" + id, Target: Target{OperationID: c.Target.OperationID, Record: id, Params: params}, IfMatch: "", Confirmed: c.Confirmed}
	if e.cfg.Inner.Capabilities().DedupTokens {
		call.DedupToken = c.Key + ":" + id
	}
	var res Result
	for attempt := 1; ; attempt++ {
		release := func() {}
		if e.cfg.Limiter != nil {
			var err error
			if release, err = e.cfg.Limiter.Acquire(ctx, e.cfg.App); err != nil {
				return Result{Outcome: OutcomeUnavailable, Detail: err.Error()}
			}
		}
		var err error
		res, err = e.cfg.Inner.Apply(ctx, call)
		release()
		switch {
		case err != nil && errors.Is(err, ErrNotSent):
			res = Result{Outcome: OutcomeUnavailable, Detail: err.Error()}
		case err != nil:
			res = Result{Outcome: OutcomeUnknown, Detail: err.Error()}
		}
		if res.Outcome == OutcomeThrottled && e.cfg.Limiter != nil {
			e.cfg.Limiter.Slow(e.cfg.App, res.RetryAfter)
		}
		if !e.retryable(res) || attempt >= e.cfg.MaxAttempts || ctx.Err() != nil {
			return res
		}
		wait := e.cfg.Backoff << (attempt - 1)
		if res.RetryAfter > wait {
			wait = res.RetryAfter
		}
		if e.cfg.Clock.Sleep(ctx, wait) != nil {
			return res
		}
	}
}

// summarize reports every record's state; a run that changed some records and not others is never called failed.
func summarize(ids []string, state map[string]string, retryAfter time.Duration) Result {
	counts := map[string]int{}
	echo := map[string]string{}
	for _, id := range ids {
		s := state[id]
		counts[s]++
		echo["item:"+id] = s
	}
	committed := counts[string(OutcomeCommitted)]
	switch {
	case committed == len(ids):
		return Result{Outcome: OutcomeCommitted, Echo: echo}
	case committed == 0 && counts[string(OutcomeUnknown)] == 0:
		first := ""
		for _, id := range ids {
			if s := state[id]; s != "not_attempted" {
				first = s
				break
			}
		}
		return Result{Outcome: Outcome(first), Detail: describe(counts), Echo: echo, RetryAfter: retryAfter}
	default:
		return Result{Outcome: OutcomeUnknown, Detail: "partial: " + describe(counts), Echo: echo}
	}
}

func describe(counts map[string]int) string {
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", k, counts[k]))
	}
	return strings.Join(parts, " ")
}
