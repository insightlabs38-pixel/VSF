package execution

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// APIConfig describes a documented HTTP API: where a record lives and where a change is posted.
type APIConfig struct {
	BaseURL   string
	ReadPath  string
	WritePath string
	Client    *http.Client
	Dedup     bool
}

// APIExecutor drives a documented API and reports results in the shared taxonomy.
type APIExecutor struct{ cfg APIConfig }

func NewAPIExecutor(cfg APIConfig) *APIExecutor {
	c := http.Client{}
	if cfg.Client != nil {
		c = *cfg.Client
	}
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	cfg.Client = &c
	return &APIExecutor{cfg: cfg}
}

func (e *APIExecutor) Kind() string { return "provider_api_authorized" }

func (e *APIExecutor) Capabilities() Capabilities {
	return Capabilities{DedupTokens: e.cfg.Dedup, ReadBack: true}
}

func (e *APIExecutor) endpoint(path, record string) string {
	return e.cfg.BaseURL + fmt.Sprintf(path, url.PathEscape(record))
}

func notSent(err error) bool {
	var op *net.OpError
	return errors.As(err, &op) && op.Op == "dial"
}

func (e *APIExecutor) Read(ctx context.Context, t Target) (Snapshot, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.endpoint(e.cfg.ReadPath, t.Record), nil)
	if err != nil {
		return Snapshot{}, err
	}
	res, err := e.cfg.Client.Do(req)
	if err != nil {
		return Snapshot{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return Snapshot{}, fmt.Errorf("read: status %d", res.StatusCode)
	}
	var doc map[string]any
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&doc); err != nil {
		return Snapshot{}, err
	}
	snap := Snapshot{Fields: map[string]string{}, ObservedAt: time.Now()}
	for k, v := range doc {
		switch k {
		case "version":
			snap.Version, _ = v.(string)
		case "confirmation_required":
			snap.ConfirmationRequired, _ = v.(bool)
		default:
			if s, ok := v.(string); ok {
				snap.Fields[k] = s
			}
		}
	}
	return snap, nil
}

func (e *APIExecutor) Apply(ctx context.Context, c Call) (Result, error) {
	if ctx.Err() != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrNotSent, ctx.Err())
	}
	body, err := json.Marshal(c.Target.Params)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrNotSent, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint(e.cfg.WritePath, c.Target.Record), bytes.NewReader(body))
	if err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrNotSent, err)
	}
	req.GetBody = nil // the transport must never replay a write behind our back
	req.Header.Set("Content-Type", "application/json")
	if c.IfMatch != "" {
		req.Header.Set("If-Match", `"`+c.IfMatch+`"`)
	}
	if c.DedupToken != "" && e.cfg.Dedup {
		req.Header.Set("Idempotency-Key", c.DedupToken)
	}
	res, err := e.cfg.Client.Do(req)
	if err != nil {
		if notSent(err) {
			return Result{}, fmt.Errorf("%w: %v", ErrNotSent, err)
		}
		return Result{}, err
	}
	defer res.Body.Close()
	return classifyStatus(res.StatusCode, res.Header.Get("Retry-After"), res.Body), nil
}

// classifyStatus maps the API's documented statuses; anything undocumented is treated as unknown.
func classifyStatus(status int, retryAfter string, body io.Reader) Result {
	switch {
	case status == http.StatusOK || status == http.StatusCreated || status == http.StatusNoContent:
		echo := map[string]string{}
		var doc map[string]any
		if json.NewDecoder(io.LimitReader(body, 1<<20)).Decode(&doc) == nil {
			for k, v := range doc {
				if s, ok := v.(string); ok {
					echo[k] = s
				}
			}
		}
		return Result{Outcome: OutcomeCommitted, Echo: echo}
	case status == http.StatusConflict || status == http.StatusPreconditionFailed:
		return Result{Outcome: OutcomeConflict, Detail: strconv.Itoa(status)}
	case status == http.StatusTooManyRequests:
		secs, _ := strconv.Atoi(retryAfter)
		return Result{Outcome: OutcomeThrottled, Detail: "429", RetryAfter: time.Duration(secs) * time.Second}
	case status >= 400 && status < 500:
		return Result{Outcome: OutcomeRejected, Detail: strconv.Itoa(status)}
	}
	return Result{Outcome: OutcomeUnknown, Detail: "status " + strconv.Itoa(status)}
}
