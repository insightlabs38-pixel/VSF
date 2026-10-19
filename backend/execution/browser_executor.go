package execution

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// BrowserPlan is a verified plan for one screen: what to read, what to fill and what to press.
type BrowserPlan struct {
	BaseURL    string
	PagePath   string
	VersionSel string
	Fields     map[string]string
	Inputs     map[string]string
	Submit     string
	Result     string
	Timeout    time.Duration
}

// BrowserExecutor performs a plan in a real browser and reports results in the shared taxonomy.
type BrowserExecutor struct {
	b    *Browser
	plan BrowserPlan
	mu   sync.Mutex
}

func NewBrowserExecutor(b *Browser, plan BrowserPlan) *BrowserExecutor {
	if plan.Timeout <= 0 {
		plan.Timeout = 5 * time.Second
	}
	return &BrowserExecutor{b: b, plan: plan}
}

func (e *BrowserExecutor) Kind() string { return "verified_browser_plan" }

func (e *BrowserExecutor) Capabilities() Capabilities { return Capabilities{ReadBack: true} }

func js(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func (e *BrowserExecutor) open(ctx context.Context, record string) (*Page, error) {
	p, err := e.b.NewPage(ctx)
	if err != nil {
		return nil, err
	}
	if err := p.Navigate(ctx, e.plan.BaseURL+fmt.Sprintf(e.plan.PagePath, record)); err != nil {
		p.Close(context.WithoutCancel(ctx))
		return nil, err
	}
	return p, nil
}

func (e *BrowserExecutor) read(ctx context.Context, p *Page) (Snapshot, error) {
	snap := Snapshot{Fields: map[string]string{}, ObservedAt: time.Now()}
	if err := p.Eval(ctx, fmt.Sprintf(`document.querySelector(%s)?.dataset.version ?? ""`, js(e.plan.VersionSel)), &snap.Version); err != nil {
		return snap, err
	}
	for name, sel := range e.plan.Fields {
		var v string
		if err := p.Eval(ctx, fmt.Sprintf(`document.querySelector(%s)?.textContent ?? ""`, js(sel)), &v); err != nil {
			return snap, err
		}
		snap.Fields[name] = strings.TrimSpace(v)
	}
	return snap, nil
}

func (e *BrowserExecutor) Read(ctx context.Context, t Target) (Snapshot, error) {
	p, err := e.open(ctx, t.Record)
	if err != nil {
		return Snapshot{}, err
	}
	defer p.Close(context.WithoutCancel(ctx))
	return e.read(ctx, p)
}

func (e *BrowserExecutor) Apply(ctx context.Context, c Call) (Result, error) {
	if ctx.Err() != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrNotSent, ctx.Err())
	}
	p, err := e.open(ctx, c.Target.Record)
	if err != nil {
		return Result{}, err
	}
	defer p.Close(context.WithoutCancel(ctx))
	cur, err := e.read(ctx, p)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrNotSent, err)
	}
	if c.IfMatch != "" && cur.Version != c.IfMatch {
		return Result{Outcome: OutcomeConflict, Detail: "screen shows " + cur.Version}, nil
	}
	for name, sel := range e.plan.Inputs {
		v, ok := c.Target.Params[name]
		if !ok {
			continue
		}
		val, _ := json.Marshal(v)
		script := fmt.Sprintf(`(() => { const el = document.querySelector(%s); if (!el) return false; el.value = %s; el.dispatchEvent(new Event("input", {bubbles:true})); el.dispatchEvent(new Event("change", {bubbles:true})); return true; })()`, js(sel), val)
		var done bool
		if err := p.Eval(ctx, script, &done); err != nil || !done {
			return Result{}, fmt.Errorf("%w: control %s is missing", ErrNotSent, sel)
		}
	}
	if ctx.Err() != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrNotSent, ctx.Err())
	}
	var clicked bool
	if err := p.Eval(ctx, fmt.Sprintf(`(() => { const el = document.querySelector(%s); if (!el) return false; el.click(); return true; })()`, js(e.plan.Submit)), &clicked); err != nil || !clicked {
		return Result{}, fmt.Errorf("%w: submit control is missing", ErrNotSent)
	}
	deadline := time.Now().Add(e.plan.Timeout)
	for {
		var status, retry string
		if err := p.Eval(ctx, fmt.Sprintf(`document.querySelector(%s)?.dataset.status ?? ""`, js(e.plan.Result)), &status); err != nil {
			return Result{}, err
		}
		if status != "" && status != "pending" {
			_ = p.Eval(ctx, fmt.Sprintf(`document.querySelector(%s)?.dataset.retryAfter ?? ""`, js(e.plan.Result)), &retry)
			return e.classify(ctx, p, status, retry)
		}
		if time.Now().After(deadline) {
			return Result{}, errors.New("the screen never reported a result")
		}
		select {
		case <-ctx.Done():
			return Result{}, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func (e *BrowserExecutor) classify(ctx context.Context, p *Page, status, retry string) (Result, error) {
	switch status {
	case "committed":
		snap, err := e.read(ctx, p)
		if err != nil {
			return Result{Outcome: OutcomeCommitted}, nil
		}
		echo := snap.Fields
		echo["version"] = snap.Version
		return Result{Outcome: OutcomeCommitted, Echo: echo}, nil
	case "conflict":
		return Result{Outcome: OutcomeConflict, Detail: status}, nil
	case "rejected":
		return Result{Outcome: OutcomeRejected, Detail: status}, nil
	case "throttled":
		secs, _ := strconv.Atoi(retry)
		return Result{Outcome: OutcomeThrottled, Detail: status, RetryAfter: time.Duration(secs) * time.Second}, nil
	}
	return Result{}, fmt.Errorf("the screen reported %q", status)
}
