package execution

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/insightlabs38-pixel/vsf/backend/ir"
)

type RetryPolicy struct {
	MaxAttempts int
	Base        time.Duration
	Max         time.Duration
}

type Config struct {
	Journal      *Journal
	Authorizer   Authorizer
	Callers      Callers
	Sessions     Sessions
	Executors    map[string]Executor
	Confirmer    *Confirmer
	Clock        Clock
	Locks        *Locks
	Limiter      *Limiter
	Retry        RetryPolicy
	ApplyTimeout time.Duration
}

type Runtime struct {
	j       *Journal
	cfg     Config
	clock   Clock
	locks   *Locks
	limiter *Limiter
	mu      sync.Mutex
	handles map[string]*Handle
}

var ErrInconclusive = errors.New("the source does not show whether the change was applied")

// NewRuntime replays the journal and settles whatever a previous process left half done.
func NewRuntime(cfg Config) (*Runtime, error) {
	if cfg.Journal == nil || cfg.Authorizer == nil || cfg.Callers == nil || cfg.Sessions == nil {
		return nil, errors.New("journal, authorizer, callers and sessions are required")
	}
	if cfg.Clock == nil {
		cfg.Clock = realClock{}
	}
	if cfg.Locks == nil {
		cfg.Locks = NewLocks()
	}
	if cfg.Limiter == nil {
		cfg.Limiter = NewLimiter(cfg.Clock, LimiterConfig{MaxInFlight: 4, MaxInterval: 8 * time.Second})
	}
	if cfg.Retry.MaxAttempts <= 0 {
		cfg.Retry = RetryPolicy{MaxAttempts: 3, Base: 100 * time.Millisecond, Max: 2 * time.Second}
	}
	if cfg.ApplyTimeout <= 0 {
		cfg.ApplyTimeout = 30 * time.Second
	}
	r := &Runtime{j: cfg.Journal, cfg: cfg, clock: cfg.Clock, locks: cfg.Locks, limiter: cfg.Limiter, handles: map[string]*Handle{}}
	if err := r.recover(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Runtime) now() int64 { return r.clock.Now().UnixMilli() }

func (r *Runtime) recover() error {
	for _, key := range r.j.Keys() {
		rec, _ := r.j.Get(key)
		var err error
		switch rec.State {
		case Running:
			err = r.j.Move(key, Unknown, ReasonUnknownCommit, "recovered_while_running", r.now())
		case Admitted:
			err = r.j.Move(key, Failed, "abandoned_before_run", "", r.now())
		case Prepared:
			err = r.j.Move(key, Failed, "abandoned_before_admission", "", r.now())
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func keyOf(caller, session, request, digest string) string {
	sum := sha256.Sum256([]byte(caller + "\x00" + session + "\x00" + request + "\x00" + digest))
	return "op_" + hex.EncodeToString(sum[:16])
}

func (r *Runtime) handleFor(key string) *Handle {
	r.mu.Lock()
	defer r.mu.Unlock()
	h := r.handles[key]
	if h == nil {
		h = newHandle(r, key)
		h.finish(nil)
		r.handles[key] = h
	}
	return h
}

func (r *Runtime) unresolved(session, operation, record string) bool {
	for _, k := range r.j.Keys() {
		rec, _ := r.j.Get(k)
		if rec.State == Unknown && rec.Session == session && rec.OperationID == operation && rec.RecordKey == record {
			return true
		}
	}
	return false
}

func lockKey(c ir.OperationContract, session, record string) (string, error) {
	switch c.Effect.Concurrency {
	case ir.ConcurrencyScopeNone:
		return "", nil
	case ir.ConcurrencyScopeSourceSession:
		return "s:" + session, nil
	case ir.ConcurrencyScopeSourceSessionAndRecord:
		if record == "" {
			return "", refuse("record_required")
		}
		return "r:" + session + ":" + record, nil
	case ir.ConcurrencyScopeGlobalSource:
		return "g:" + string(c.Source.AppID), nil
	}
	return "", refuse("concurrency_unknown")
}

func isWrite(c ir.OperationContract) bool { return c.Effect.Class != ir.EffectClassReadOnly }

func (r *Runtime) executorFor(c ir.OperationContract, want string) (Executor, string, error) {
	for _, s := range c.Strategies {
		name := string(s)
		if want != "" && want != name {
			continue
		}
		if e, ok := r.cfg.Executors[name]; ok {
			return e, name, nil
		}
	}
	if want != "" {
		return nil, "", refuse("strategy_unavailable")
	}
	return nil, "", refuse("no_executor")
}

func (r *Runtime) needsConfirmation(c ir.OperationContract) bool {
	return c.Effect.Confirmation == ir.ConfirmationPolicyAlwaysPrompt || c.Effect.Confirmation == ir.ConfirmationPolicyNeverAutomatic
}

func (r *Runtime) confirmed(req Request, digest string) bool {
	return req.Confirmation != nil && r.cfg.Confirmer != nil && r.cfg.Confirmer.Valid(*req.Confirmation, digest, r.clock.Now())
}

func (r *Runtime) refusedOp(key string, h *Handle, err error) (*Handle, error) {
	reason := "refused"
	var ref *Refusal
	if errors.As(err, &ref) {
		reason = ref.Reason
	}
	if jerr := r.j.Move(key, Failed, reason, "", r.now()); jerr != nil {
		return nil, jerr
	}
	h.finish(nil)
	return h, err
}

// Submit records the request, checks everything that can be checked without touching the source, then runs it.
func (r *Runtime) Submit(ctx context.Context, req Request) (*Handle, error) {
	if err := r.j.Err(); err != nil {
		return nil, err
	}
	if req.RequestID == "" || req.Caller == "" || req.SessionID == "" {
		return nil, refuse("request_malformed")
	}
	un, err := ir.DecodeDocument(req.Contract)
	if err != nil {
		return nil, refuse("undecodable")
	}
	body := un.Unsafe()
	if body.Operation == nil {
		return nil, refuse("not_operation_contract")
	}
	c := *body.Operation
	digest := un.Digest()
	key := keyOf(req.Caller, req.SessionID, req.RequestID, digest)
	pd := requestDigest(digest, req.Record, req.Params)
	rec, created, err := r.j.Create(key, r.now(), map[string]string{
		"operation_id": string(c.OperationID), "contract_digest": digest, "params_digest": pd,
		"caller": req.Caller, "session": req.SessionID, "record": req.Record,
	})
	if err != nil {
		return nil, err
	}
	if !created {
		if rec.ParamsDigest != pd {
			return nil, refuse("request_id_reused")
		}
		return r.handleFor(key), nil
	}
	h := newHandle(r, key)
	r.mu.Lock()
	r.handles[key] = h
	r.mu.Unlock()
	h.progress("prepared", 0, "")

	if req.Pin.Version != string(c.ContractVersion) || req.Pin.Digest != digest {
		return r.refusedOp(key, h, refuse("contract_pin_mismatch"))
	}
	exec, strategy, err := r.executorFor(c, req.Strategy)
	if err != nil {
		return r.refusedOp(key, h, err)
	}
	if err := checkParams(c.Inputs, req.Params); err != nil {
		return r.refusedOp(key, h, err)
	}
	if isWrite(c) {
		if r.unresolved(req.SessionID, string(c.OperationID), req.Record) {
			return r.refusedOp(key, h, refuse("unresolved_unknown_commit"))
		}
		if _, err := lockKey(c, req.SessionID, req.Record); err != nil {
			return r.refusedOp(key, h, err)
		}
		if c.Verification.Required && len(req.Expect) == 0 {
			return r.refusedOp(key, h, refuse("expect_required"))
		}
	}
	env, err := bind(c, r.cfg.Callers, r.cfg.Sessions, req.Caller, req.SessionID, r.clock.Now())
	if err != nil {
		return r.refusedOp(key, h, err)
	}
	if isWrite(c) && r.needsConfirmation(c) && !r.confirmed(req, requestDigest(digest, req.Record, req.Params)) {
		return r.refusedOp(key, h, refuse("confirmation_required"))
	}
	auth, err := r.cfg.Authorizer.Authorize(req.Contract, env)
	if err != nil {
		return r.refusedOp(key, h, err)
	}
	if !auth.valid() || auth.Digest() != digest {
		return r.refusedOp(key, h, refuse("authorization_invalid"))
	}
	if err := r.j.Move(key, Admitted, "", "", r.now()); err != nil {
		return nil, err
	}
	h.progress("admitted", 0, "")
	runCtx, cancel := context.WithCancel(context.Background())
	h.mu.Lock()
	h.cancel = cancel
	h.mu.Unlock()
	go r.run(runCtx, h, req, c, digest, exec, strategy)
	return h, nil
}

type pending struct {
	Record     string `json:"record"`
	Strategy   string `json:"strategy"`
	PreVersion string `json:"pre_version"`
	Expect     string `json:"expect_digest"`
}

func digestOf(m map[string]string) string {
	b, _ := json.Marshal(m)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// finishWith records the end of a run and settles what Cancel may truthfully say.
func (r *Runtime) finishWith(h *Handle, to State, reason, detail string, cancel CancelStatus) {
	err := r.j.Move(h.key, to, reason, detail, r.now())
	if cancel != CancelPending {
		h.setCancelStatus(cancel)
	}
	h.progress(string(to), 1, reason)
	h.finish(err)
}

func (r *Runtime) run(ctx context.Context, h *Handle, req Request, c ir.OperationContract, digest string, exec Executor, strategy string) {
	defer h.cancel()
	write := isWrite(c)
	h.progress("queued", 0, "")
	if write {
		if key, _ := lockKey(c, req.SessionID, req.Record); key != "" {
			release, err := r.locks.Acquire(ctx, key)
			if err != nil {
				r.finishWith(h, Failed, "cancelled_before_run", "", CancelBeforeEffect)
				return
			}
			defer release()
		}
	}
	app := string(c.Source.AppID)
	releaseSlot, err := r.limiter.Acquire(ctx, app)
	if err != nil {
		r.finishWith(h, Failed, "cancelled_before_run", "", CancelBeforeEffect)
		return
	}
	defer releaseSlot()

	env, err := bind(c, r.cfg.Callers, r.cfg.Sessions, req.Caller, req.SessionID, r.clock.Now())
	if err == nil {
		var auth Authorized
		if auth, err = r.cfg.Authorizer.Authorize(req.Contract, env); err == nil && (!auth.valid() || auth.Digest() != digest) {
			err = refuse("authorization_invalid")
		}
	}
	if err != nil {
		r.finishWith(h, Failed, reasonOf(err), "", CancelBeforeEffect)
		return
	}
	target := Target{OperationID: string(c.OperationID), Record: req.Record, Params: req.Params}
	snap, ifMatch, why := r.preconditions(ctx, c, req, digest, exec, target, h)
	pd, _ := json.Marshal(pending{Record: req.Record, Strategy: strategy, PreVersion: snap.Version, Expect: digestOf(req.Expect)})
	if mv := r.j.Move(h.key, Running, "", string(pd), r.now()); mv != nil {
		h.finish(mv)
		return
	}
	h.progress("running", 0, "")
	if why != "" {
		r.finishWith(h, Failed, why, "", CancelBeforeEffect)
		return
	}
	if ctx.Err() != nil {
		r.finishWith(h, Failed, "cancelled_before_effect", "", CancelBeforeEffect)
		return
	}
	if !write {
		r.finishWith(h, Verified, "read", snap.Version, CancelTooLate)
		return
	}
	r.apply(ctx, h, req, c, digest, exec, target, ifMatch, snap)
}

func reasonOf(err error) string {
	var ref *Refusal
	if errors.As(err, &ref) {
		return ref.Reason
	}
	return "failed"
}

// preconditions reads the source and blocks the write on stale data or a missing confirmation.
func (r *Runtime) preconditions(ctx context.Context, c ir.OperationContract, req Request, digest string, exec Executor, t Target, h *Handle) (Snapshot, string, string) {
	var snap Snapshot
	if !isWrite(c) || c.Freshness.BeforeWrite != ir.FreshnessRequirementNone || c.Effect.Confirmation == ir.ConfirmationPolicyInheritSourceRequirement {
		h.progress("reading", 0, "")
		s, err := exec.Read(ctx, t)
		if err != nil {
			return snap, "", "read_failed"
		}
		snap = s
	}
	if !isWrite(c) {
		return snap, "", ""
	}
	ifMatch := ""
	switch c.Freshness.BeforeWrite {
	case ir.FreshnessRequirementSourceReadRequired:
		if req.Seen == nil {
			return snap, "", "seen_required"
		}
		if snap.Version != req.Seen.Version {
			return snap, "", "stale_read"
		}
		ifMatch = snap.Version
	case ir.FreshnessRequirementObservationWithin:
		if req.Seen == nil {
			return snap, "", "seen_required"
		}
		limit := time.Duration(0)
		if c.Freshness.MaxAgeMs != nil {
			limit = time.Duration(*c.Freshness.MaxAgeMs) * time.Millisecond
		}
		if r.clock.Now().Sub(req.Seen.ObservedAt) > limit {
			return snap, "", "stale_observation"
		}
		ifMatch = req.Seen.Version
	}
	if c.Effect.Confirmation == ir.ConfirmationPolicyInheritSourceRequirement && snap.ConfirmationRequired &&
		!r.confirmed(req, requestDigest(digest, req.Record, req.Params)) {
		return snap, "", "confirmation_required"
	}
	return snap, ifMatch, ""
}

func (r *Runtime) apply(ctx context.Context, h *Handle, req Request, c ir.OperationContract, digest string, exec Executor, t Target, ifMatch string, pre Snapshot) {
	app := string(c.Source.AppID)
	needs := r.needsConfirmation(c) || (c.Effect.Confirmation == ir.ConfirmationPolicyInheritSourceRequirement && req.Confirmation != nil)
	if needs {
		if req.Confirmation == nil || r.cfg.Confirmer == nil ||
			!r.cfg.Confirmer.Use(*req.Confirmation, requestDigest(digest, req.Record, req.Params), r.clock.Now()) {
			r.finishWith(h, Failed, "confirmation_spent", "", CancelBeforeEffect)
			return
		}
	}
	call := Call{Key: h.key, Target: t, IfMatch: ifMatch, Confirmed: needs}
	if exec.Capabilities().DedupTokens {
		call.DedupToken = h.key
	}
	var res Result
	for attempt := 1; ; attempt++ {
		h.setStage("applying")
		h.progress("applying", 0, fmt.Sprintf("attempt %d", attempt))
		actx, cancel := context.WithTimeout(context.WithValue(ctx, progressKey{}, h.progress), r.cfg.ApplyTimeout)
		var err error
		res, err = exec.Apply(actx, call)
		cancel()
		switch {
		case errors.Is(err, ErrNotSent):
			res = Result{Outcome: OutcomeUnavailable, Detail: err.Error()}
		case err != nil:
			res = Result{Outcome: OutcomeUnknown, Detail: err.Error()}
		}
		if res.Outcome == OutcomeThrottled {
			r.limiter.Slow(app, res.RetryAfter)
		}
		if !r.mayRetry(c, exec, res, attempt) {
			break
		}
		if ctx.Err() != nil {
			break
		}
		if r.clock.Sleep(ctx, r.backoff(attempt, res.RetryAfter)) != nil {
			break
		}
	}
	h.setStage("post")
	switch res.Outcome {
	case OutcomeCommitted:
		r.limiter.Ok(app)
		r.verify(h, req, c, exec, t, res)
	case OutcomeRejected, OutcomeConflict, OutcomeThrottled, OutcomeUnavailable:
		if r.contradicted(exec, t, pre, req.Expect) {
			r.finishWith(h, Unknown, ReasonUnknownCommit, "no_effect_contradicted:"+string(res.Outcome), CancelPending)
			return
		}
		r.finishWith(h, Failed, string(res.Outcome), res.Detail, CancelBeforeEffect)
	default:
		cs := CancelPending
		if h.cancelRequested() {
			cs = CancelEffectUnknown
		}
		r.finishWith(h, Unknown, ReasonUnknownCommit, res.Detail, cs)
	}
}

// contradicted is true when the source now shows our change although an executor claimed it changed nothing.
func (r *Runtime) contradicted(exec Executor, t Target, pre Snapshot, expect map[string]string) bool {
	if pre.Version == "" || len(expect) == 0 {
		return false
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(context.Background()), r.cfg.ApplyTimeout)
	defer cancel()
	now, err := exec.Read(ctx, t)
	if err != nil || now.Version == pre.Version {
		return false
	}
	for k, want := range expect {
		if now.Fields[k] != want {
			return false
		}
	}
	return true
}

func (r *Runtime) backoff(attempt int, hint time.Duration) time.Duration {
	d := r.cfg.Retry.Base << (attempt - 1)
	if d > r.cfg.Retry.Max || d <= 0 {
		d = r.cfg.Retry.Max
	}
	return max(d, hint)
}

// mayRetry allows another attempt only when the source proved the last one changed nothing.
func (r *Runtime) mayRetry(c ir.OperationContract, exec Executor, res Result, attempt int) bool {
	if attempt >= r.cfg.Retry.MaxAttempts {
		return false
	}
	switch res.Outcome {
	case OutcomeThrottled:
		return true
	case OutcomeUnavailable:
		return c.Effect.Idempotency == ir.IdempotencyIdempotent || exec.Capabilities().DedupTokens
	}
	return false
}

// verify never reports success from the source's acknowledgement alone.
func (r *Runtime) verify(h *Handle, req Request, c ir.OperationContract, exec Executor, t Target, res Result) {
	if !c.Verification.Required {
		r.finishWith(h, Verified, "acknowledged", res.Detail, CancelTooLate)
		return
	}
	h.progress("verifying", 0, "")
	vctx, cancel := context.WithTimeout(context.WithoutCancel(context.Background()), r.cfg.ApplyTimeout)
	defer cancel()
	got := res.Echo
	if c.Verification.Strategy != ir.VerificationStrategyResponseEcho {
		snap, err := exec.Read(vctx, t)
		if err != nil {
			r.finishWith(h, Unknown, ReasonUnverified, "read_back_failed", CancelTooLate)
			return
		}
		got = snap.Fields
	}
	for k, want := range req.Expect {
		if got[k] != want {
			r.finishWith(h, Unknown, ReasonUnverified, "mismatch:"+k, CancelTooLate)
			return
		}
	}
	r.finishWith(h, Verified, "verified", string(c.Verification.Strategy), CancelTooLate)
}

// Reconcile settles an Unknown operation from an authoritative read, or says it cannot.
func (r *Runtime) Reconcile(ctx context.Context, caller, key string, expect map[string]string) (Record, error) {
	rec, ok := r.j.Get(key)
	if !ok || rec.State != Unknown {
		return rec, refuse("not_unknown")
	}
	if rec.Caller != caller {
		return rec, refuse("other_caller")
	}
	if c, ok := r.cfg.Callers.Lookup(caller); !ok || c.ID == "" {
		return rec, refuse("unknown_caller")
	}
	var p pending
	for _, e := range rec.Events {
		if e.Kind == "state" && e.To == Running {
			_ = json.Unmarshal([]byte(e.Detail), &p)
		}
	}
	if p.Expect == "" || digestOf(expect) != p.Expect {
		return rec, refuse("expect_mismatch")
	}
	exec, ok := r.cfg.Executors[p.Strategy]
	if !ok {
		return rec, refuse("no_executor")
	}
	snap, err := exec.Read(ctx, Target{OperationID: rec.OperationID, Record: p.Record})
	if err != nil {
		return rec, ErrInconclusive
	}
	applied := true
	for k, want := range expect {
		if snap.Fields[k] != want {
			applied = false
		}
	}
	switch {
	case applied:
		err = r.j.Move(key, Verified, "reconciled_committed", "", r.now())
	case snap.Version == p.PreVersion && p.PreVersion != "":
		err = r.j.Move(key, Failed, "reconciled_not_committed", "", r.now())
	default:
		return rec, ErrInconclusive
	}
	rec, _ = r.j.Get(key)
	return rec, err
}

// Get returns the journal's view of an operation.
func (r *Runtime) Get(key string) (Record, bool) { return r.j.Get(key) }
