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

// Revocations lets a store of published versions stop a contract that was revoked after it was pinned.
type Revocations interface {
	RevokedContract(documentDigest, operation, version string) (reason string, revoked bool)
}

type Config struct {
	Revocations  Revocations
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
	// reconciling keeps two reconciliations of one operation from racing each other.
	reconciling map[string]bool
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
	r := &Runtime{j: cfg.Journal, cfg: cfg, clock: cfg.Clock, locks: cfg.Locks, limiter: cfg.Limiter, handles: map[string]*Handle{}, reconciling: map[string]bool{}}
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

func validID(v string, max int) bool {
	if v == "" || len(v) > max {
		return false
	}
	for _, c := range v {
		if c < 0x20 || c == 0x7f {
			return false
		}
	}
	return true
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
	if !validID(req.RequestID, 256) || !validID(req.Caller, 256) || !validID(req.SessionID, 256) ||
		(req.Record != "" && !validID(req.Record, 1024)) {
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
	var fp Footprint
	var fpErr error
	if isWrite(c) {
		fp, fpErr = footprintOf(c, req.SessionID, req.Record)
	}
	meta := footprintMeta(fp)
	if fp.Level == "" {
		meta = footprintMeta(Footprint{App: string(c.Source.AppID), Level: LevelSession})
	}
	for k, v := range map[string]string{
		"operation_id": string(c.OperationID), "contract_digest": digest, "params_digest": pd,
		"caller": req.Caller, "session": req.SessionID, "record": req.Record,
	} {
		meta[k] = v
	}
	rec, created, err := r.j.Create(key, r.now(), meta)
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
	if why, revoked := r.revoked(digest, c); revoked {
		return r.refusedOp(key, h, refuse("contract_revoked:"+why))
	}
	exec, strategy, err := r.executorFor(c, req.Strategy)
	if err != nil {
		return r.refusedOp(key, h, err)
	}
	if err := checkParams(c.Inputs, req.Params); err != nil {
		return r.refusedOp(key, h, err)
	}
	if isWrite(c) {
		if fpErr != nil {
			return r.refusedOp(key, h, fpErr)
		}
		if _, blocked := r.j.Blocking(fp, key); blocked {
			return r.refusedOp(key, h, refuse("unresolved_unknown_commit"))
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
	adm, _ := json.Marshal(Admission{Env: env, AppID: string(c.Source.AppID), VariantClass: string(c.Source.VariantClass)})
	if err := r.j.Move(key, Admitted, "", string(adm), r.now()); err != nil {
		return nil, err
	}
	h.progress("admitted", 0, "")
	runCtx, cancel := context.WithCancel(context.Background())
	h.mu.Lock()
	h.cancel = cancel
	h.mu.Unlock()
	go r.run(runCtx, h, req, c, digest, exec, strategy, fp, env)
	return h, nil
}

type pending struct {
	Record     string `json:"record"`
	Strategy   string `json:"strategy"`
	PreVersion string `json:"pre_version"`
	PreMatched bool   `json:"pre_matched,omitempty"`
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

// authorize reruns caller, session, environment and Rust validation; drift from the admitted environment is refused.
func (r *Runtime) revoked(digest string, c ir.OperationContract) (string, bool) {
	if r.cfg.Revocations == nil {
		return "", false
	}
	return r.cfg.Revocations.RevokedContract(digest, string(c.OperationID), string(c.ContractVersion))
}

func (r *Runtime) authorize(req Request, c ir.OperationContract, digest string, admitted *Env) error {
	if err := r.j.Err(); err != nil {
		return err
	}
	if why, revoked := r.revoked(digest, c); revoked {
		return refuse("contract_revoked:" + why)
	}
	env, err := bind(c, r.cfg.Callers, r.cfg.Sessions, req.Caller, req.SessionID, r.clock.Now())
	if err != nil {
		return err
	}
	if admitted != nil && !sameEnvironment(*admitted, env) {
		return refuse("environment_changed")
	}
	auth, err := r.cfg.Authorizer.Authorize(req.Contract, env)
	if err != nil {
		return err
	}
	if !auth.valid() || auth.Digest() != digest {
		return refuse("authorization_invalid")
	}
	return nil
}

func sameEnvironment(a, b Env) bool {
	return a.RuntimeID == b.RuntimeID && a.CapabilityDigest == b.CapabilityDigest && a.PrincipalRef == b.PrincipalRef && a.VariantID == b.VariantID
}

func (r *Runtime) run(ctx context.Context, h *Handle, req Request, c ir.OperationContract, digest string, exec Executor, strategy string, fp Footprint, admitted Env) {
	defer h.cancel()
	write := isWrite(c)
	h.progress("queued", 0, "")
	if write {
		release, err := r.locks.Acquire(ctx, fp)
		if err != nil {
			r.finishWith(h, Failed, "cancelled_before_run", "", CancelBeforeEffect)
			return
		}
		defer release()
	}
	app := string(c.Source.AppID)
	releaseSlot, err := r.limiter.Acquire(ctx, app)
	if err != nil {
		r.finishWith(h, Failed, "cancelled_before_run", "", CancelBeforeEffect)
		return
	}
	defer releaseSlot()

	if write {
		if _, blocked := r.j.Blocking(fp, h.key); blocked {
			r.finishWith(h, Failed, "unresolved_unknown_commit", "", CancelBeforeEffect)
			return
		}
	}
	if err := r.authorize(req, c, digest, &admitted); err != nil {
		r.finishWith(h, Failed, reasonOf(err), "", CancelBeforeEffect)
		return
	}
	target := Target{OperationID: string(c.OperationID), Record: req.Record, Params: req.Params}
	snap, ifMatch, why := r.preconditions(ctx, c, req, digest, exec, target, h)
	if why == "" {
		// The source may have been read for seconds; nothing admitted earlier is trusted at the moment of the effect.
		if err := r.authorize(req, c, digest, &admitted); err != nil {
			why = reasonOf(err)
		}
	}
	if why == "" && ctx.Err() != nil {
		why = "cancelled_before_effect"
	}
	pd, _ := json.Marshal(pending{Record: req.Record, Strategy: strategy, PreVersion: snap.Version, PreMatched: snap.Fields != nil && matches(snap.Fields, req.Expect), Expect: digestOf(req.Expect)})
	var mv error
	if write {
		mv = r.j.Start(h.key, string(pd), r.now())
	} else {
		mv = r.j.Move(h.key, Running, "", string(pd), r.now())
	}
	if errors.Is(mv, ErrBlocked) {
		r.finishWith(h, Failed, "unresolved_unknown_commit", "", CancelBeforeEffect)
		return
	}
	if mv != nil {
		h.finish(mv)
		return
	}
	h.progress("running", 0, "")
	if why != "" {
		r.finishWith(h, Failed, why, "", CancelBeforeEffect)
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
		if why := r.noEffectDoubt(exec, t, pre, req.Expect); why != "" {
			r.finishWith(h, Unknown, ReasonUnknownCommit, why+":"+string(res.Outcome), CancelPending)
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

func matches(fields, want map[string]string) bool {
	for k, v := range want {
		if got, ok := fields[k]; !ok || got != v {
			return false
		}
	}
	return true
}

// noEffectDoubt re-reads after a "nothing changed" claim; non-authoritative sources must show their state untouched.
func (r *Runtime) noEffectDoubt(exec Executor, t Target, pre Snapshot, expect map[string]string) string {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(context.Background()), r.cfg.ApplyTimeout)
	defer cancel()
	authoritative := exec.Capabilities().AuthoritativeStatus
	now, err := exec.Read(ctx, t)
	if err != nil {
		if authoritative {
			return ""
		}
		return "no_effect_unproven"
	}
	if len(expect) > 0 && matches(now.Fields, expect) && !(pre.Fields != nil && matches(pre.Fields, expect)) {
		return "no_effect_contradicted"
	}
	if !authoritative && (len(expect) == 0 || pre.Version == "" || now.Version != pre.Version) {
		return "no_effect_unproven"
	}
	return ""
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
		return exec.Capabilities().AuthoritativeStatus
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

// Get returns the journal's view of an operation.
func (r *Runtime) Get(key string) (Record, bool) { return r.j.Get(key) }
