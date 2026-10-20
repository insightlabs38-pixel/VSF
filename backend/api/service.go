package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/insightlabs38-pixel/vsf/backend/api/jsonschema"
	"github.com/insightlabs38-pixel/vsf/backend/execution"
)

// Backend is what every surface is generated from; REST, CLI and MCP only translate to and from it.
type Backend interface {
	Discovery() Discovery
	Namespaces(p Principal) []Namespace
	List(p Principal, q ListQuery) (Page, *Error)
	Describe(p Principal, name, version string) (*Spec, *Error)
	Versions(p Principal, name string) ([]VersionInfo, *Error)
	Invoke(ctx context.Context, p Principal, raw []byte, route Route) Result
	Status(p Principal, key string, withProgress bool) Result
	Cancel(p Principal, key string) Result
	Reconcile(ctx context.Context, p Principal, key string, raw []byte) Result
	Confirm(ctx context.Context, p Principal, raw []byte, route Route) (ConfirmationIn, *Error)
}

// Route carries what a surface knows about the target before the body is read.
type Route struct {
	Operation string
	Version   string
}

// Discovery tells a client which interface versions and limits this service offers.
type Discovery struct {
	Service         string   `json:"service"`
	APIVersions     []string `json:"api_versions"`
	Surfaces        []string `json:"surfaces"`
	MaxRequestBytes int      `json:"max_request_bytes"`
	MaxPageSize     int      `json:"max_page_size"`
	DefaultPageSize int      `json:"default_page_size"`
	MaxWaitMs       int64    `json:"max_wait_ms"`
	Operations      int      `json:"operations,omitempty"`
}

// ServiceConfig wires the catalog and the execution runtime; nothing here decides what an operation may do.
type ServiceConfig struct {
	Catalog  *Catalog
	Runtime  *execution.Runtime
	Callers  execution.Callers
	Sessions execution.Sessions
	Limiter  *CallLimiter
	// Confirmer and Now are the runtime's own, so a confirmation expires by the clock that checks it.
	Confirmer *execution.Confirmer
	Now       func() time.Time
}

// Service is the single implementation of invocation semantics.
type Service struct {
	cfg ServiceConfig
}

// APIVersion is the interface version this build serves; contracts are versioned separately.
const (
	APIVersion = "v1"
	MaxWaitMs  = 120000
	// DefaultWaitMs is how long a call waits for a result when the caller does not say.
	DefaultWaitMs = 10000
)

func NewService(cfg ServiceConfig) (*Service, error) {
	if cfg.Catalog == nil || cfg.Runtime == nil || cfg.Callers == nil || cfg.Sessions == nil {
		return nil, errors.New("catalog, runtime, callers and sessions are required")
	}
	return &Service{cfg: cfg}, nil
}

func (s *Service) Discovery() Discovery {
	return Discovery{
		Service: "vsf", APIVersions: []string{APIVersion}, Surfaces: []string{string(SurfaceREST), string(SurfaceCLI), string(SurfaceMCP)},
		MaxRequestBytes: MaxRequestBytes, MaxPageSize: MaxPageSize, DefaultPageSize: DefaultPageSize, MaxWaitMs: MaxWaitMs,
		Operations: s.cfg.Catalog.Size(),
	}
}

// caller re-checks the principal against the directory on every call, so a revoked caller stops at once.
func (s *Service) caller(p Principal) (execution.Caller, *Error) {
	if p.Caller == "" || p.Surface == "" {
		return execution.Caller{}, newError(CodeUnauthenticated, "")
	}
	c, ok := s.cfg.Callers.Lookup(p.Caller)
	if !ok || c.ID != p.Caller {
		return execution.Caller{}, newError(CodeUnauthenticated, "caller_revoked")
	}
	if s.cfg.Limiter != nil {
		if wait, ok := s.cfg.Limiter.Allow(p.Caller); !ok {
			e := newError(CodeRateLimited, "")
			e.RetryAfterMs = wait.Milliseconds()
			return execution.Caller{}, e
		}
	}
	return c, nil
}

func (s *Service) Namespaces(p Principal) []Namespace {
	if _, err := s.caller(p); err != nil {
		return nil
	}
	return s.cfg.Catalog.Namespaces()
}

func (s *Service) List(p Principal, q ListQuery) (Page, *Error) {
	who, err := s.caller(p)
	if err != nil {
		return Page{}, err
	}
	q.Scopes = who.Scopes
	page, lerr := s.cfg.Catalog.List(q)
	if lerr != nil {
		return Page{}, AsError(lerr)
	}
	return page, nil
}

func (s *Service) Describe(p Principal, name, version string) (*Spec, *Error) {
	who, err := s.caller(p)
	if err != nil {
		return nil, err
	}
	spec, rerr := s.cfg.Catalog.Resolve(name, version)
	if rerr != nil {
		return nil, AsError(rerr)
	}
	if !holds(who.Scopes, spec.Effect.RequiredScopes) {
		return nil, newError(CodeOperationNotFound, "")
	}
	return spec, nil
}

func (s *Service) Versions(p Principal, name string) ([]VersionInfo, *Error) {
	if _, err := s.caller(p); err != nil {
		return nil, err
	}
	v := s.cfg.Catalog.Versions(name)
	if len(v) == 0 {
		return nil, newError(CodeOperationNotFound, "")
	}
	return v, nil
}

func violations(vs []jsonschema.Violation) map[string]any {
	list := make([]any, 0, len(vs))
	for _, v := range vs {
		list = append(list, map[string]any{"path": v.Path, "keyword": v.Keyword})
	}
	return map[string]any{"violations": list}
}

func objectOf(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	return m, ok
}

func stringMap(v any) map[string]string {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, x := range m {
		out[k], _ = x.(string)
	}
	return out
}

func intOf(v any) int64 {
	n, ok := v.(json.Number)
	if !ok {
		return 0
	}
	i, _ := n.Int64()
	return i
}

func strOf(v any) string {
	s, _ := v.(string)
	return s
}

var schemas = newSchemaCache(256)

// DecodeInvocation checks a body against the operation's own schema, then reads it into the envelope.
func DecodeInvocation(spec *Spec, raw []byte) (Invocation, *Error) {
	if len(raw) > MaxRequestBytes {
		return Invocation{}, newError(CodeTooLarge, "")
	}
	v, err := DecodeStrict(raw)
	if err != nil {
		return Invocation{}, newError(CodeInvalidRequest, "malformed_json")
	}
	m, ok := objectOf(v)
	if !ok {
		return Invocation{}, newError(CodeInvalidRequest, "not_an_object")
	}
	sch, cerr := schemas.get(spec.ContractDigest, spec.InputSchema)
	if cerr != nil {
		return Invocation{}, newError(CodeInternal, "")
	}
	if vs := sch.Validate(m); len(vs) > 0 {
		return Invocation{}, errWith(CodeInvalidArgument, "schema", violations(vs))
	}
	inv := Invocation{
		Operation: spec.Name, ContractVer: strOf(m["contract_version"]), ContractDigest: strOf(m["contract_digest"]),
		RequestID: strOf(m["request_id"]), SessionID: strOf(m["session_id"]), Record: strOf(m["record"]),
		Strategy: strOf(m["strategy"]), Expect: stringMap(m["expect"]),
	}
	inv.Params, _ = objectOf(m["params"])
	if w, ok := m["wait_ms"]; ok {
		n := intOf(w)
		inv.WaitMs = &n
	}
	inv.DryRun, _ = m["dry_run"].(bool)
	if sm, ok := objectOf(m["seen"]); ok {
		inv.Seen = &SeenIn{Version: strOf(sm["version"]), ObservedAt: intOf(sm["observed_at"])}
	}
	if cm, ok := objectOf(m["confirmation"]); ok {
		inv.Confirmation = &ConfirmationIn{Token: strOf(cm["token"]), By: strOf(cm["by"]), Digest: strOf(cm["digest"]), ExpiresAt: intOf(cm["expires_at"])}
	}
	return inv, nil
}

func freshID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return "rd_" + hex.EncodeToString(b[:])
}

// sessionFor gives one answer whether a session is missing or belongs to someone else.
func (s *Service) sessionFor(p Principal, id string) *Error {
	sess, ok := s.cfg.Sessions.Lookup(id)
	if !ok || sess.Owner != p.Caller {
		return newError(CodeSessionInvalid, "session_unavailable")
	}
	return nil
}

// resolve finds the served spec; it returns the spec with the error only when the caller may know it exists.
func (s *Service) resolve(p Principal, who execution.Caller, r Route, pinVersion, digest string) (*Spec, *Error) {
	version := r.Version
	if version == "" {
		version = pinVersion
	} else if pinVersion != "" && pinVersion != r.Version {
		return nil, errWith(CodeInvalidRequest, "contract_version_conflict", nil)
	}
	spec, err := s.cfg.Catalog.Resolve(r.Operation, version)
	if err != nil {
		return nil, AsError(err)
	}
	if !holds(who.Scopes, spec.Effect.RequiredScopes) {
		return nil, newError(CodeOperationNotFound, "")
	}
	if digest != "" && digest != spec.ContractDigest {
		return spec, errWith(CodeContractPinMismatch, "", map[string]any{"contract_version": spec.ContractVersion})
	}
	return spec, nil
}

// Invoke runs one operation; the result is always a Result, never a transport-specific failure.
func (s *Service) Invoke(ctx context.Context, p Principal, raw []byte, route Route) Result {
	who, err := s.caller(p)
	if err != nil {
		return RefusedFor(route, nil, err)
	}
	if len(raw) > MaxRequestBytes {
		return RefusedFor(route, nil, newError(CodeTooLarge, ""))
	}
	var head struct {
		Version string `json:"contract_version"`
		Digest  string `json:"contract_digest"`
	}
	if v, derr := DecodeStrict(raw); derr == nil {
		if m, ok := objectOf(v); ok {
			head.Version, head.Digest = strOf(m["contract_version"]), strOf(m["contract_digest"])
		}
	}
	if route.Version == "" {
		route.Version = head.Version
	}
	spec, err := s.resolve(p, who, route, head.Version, head.Digest)
	if err != nil {
		return RefusedFor(route, spec, err)
	}
	inv, err := DecodeInvocation(spec, raw)
	if err != nil {
		return RefusedResult(spec, err)
	}
	if inv.RequestID == "" {
		inv.RequestID = freshID()
	}
	return withRequestID(s.invoke(ctx, p, spec, inv), inv.RequestID)
}

func withRequestID(r Result, id string) Result {
	r.RequestID = id
	return r
}

func (s *Service) invoke(ctx context.Context, p Principal, spec *Spec, inv Invocation) Result {
	if err := s.sessionFor(p, inv.SessionID); err != nil {
		return RefusedResult(spec, err)
	}
	if inv.DryRun {
		d := spec.Effect
		return Result{Operation: spec.Name, ContractVersion: spec.ContractVersion, ContractDigest: spec.ContractDigest,
			Category: CategoryValidated, State: "validated", Effect: EffectNone, Retry: RetryNever, Disclosure: &d}
	}
	doc, ok := s.cfg.Catalog.Contract(spec.ContractDigest)
	if !ok {
		return RefusedResult(spec, newError(CodeInternal, ""))
	}
	req := execution.Request{
		RequestID: inv.RequestID, Caller: p.Caller, SessionID: inv.SessionID, Contract: doc,
		Pin:    execution.Pin{Version: spec.ContractVersion, Digest: spec.ContractDigest},
		Params: inv.Params, Record: inv.Record, Expect: inv.Expect, Strategy: inv.Strategy,
	}
	if inv.Seen != nil {
		req.Seen = &execution.Seen{Version: inv.Seen.Version, ObservedAt: time.UnixMilli(inv.Seen.ObservedAt)}
	}
	if c := inv.Confirmation; c != nil {
		req.Confirmation = &execution.Confirmation{Token: c.Token, By: c.By, Digest: c.Digest, ExpiresAt: time.UnixMilli(c.ExpiresAt)}
	}
	h, serr := s.cfg.Runtime.Submit(ctx, req)
	if h == nil {
		return RefusedResult(spec, submitError(serr))
	}
	if serr != nil {
		res := ResultOf(spec, h, false)
		if res.Error == nil {
			res.Error = submitError(serr)
		}
		return res
	}
	return s.await(ctx, spec, h, inv.WaitMs)
}

func submitError(err error) *Error {
	var ref *execution.Refusal
	if errors.As(err, &ref) {
		return errWith(ClassifyReason(ref.Reason), reasonText(ref.Reason), nil)
	}
	return newError(CodeInternal, "")
}

// await waits at most wait for the run; a longer run is reported in progress and keeps going.
func (s *Service) await(ctx context.Context, spec *Spec, h *execution.Handle, wait *int64) Result {
	waitMs := int64(DefaultWaitMs)
	if wait != nil {
		waitMs = *wait
	}
	if waitMs > MaxWaitMs {
		waitMs = MaxWaitMs
	}
	if waitMs > 0 {
		wctx, cancel := context.WithTimeout(ctx, time.Duration(waitMs)*time.Millisecond)
		_, _ = h.Wait(wctx)
		cancel()
	}
	return ResultOf(spec, h, true)
}

// own finds an invocation only for the caller that made it; anyone else gets the same answer as for no invocation.
func (s *Service) own(p Principal, key string) (*Spec, *execution.Handle, *Error) {
	if _, err := s.caller(p); err != nil {
		return nil, nil, err
	}
	h, ok := s.cfg.Runtime.Handle(key)
	if !ok || h.Record().Caller != p.Caller {
		return nil, nil, newError(CodeInvocationNotFound, "")
	}
	spec, ok := s.cfg.Catalog.ByDigest(h.Record().ContractDigest)
	if !ok {
		return nil, nil, newError(CodeInternal, "")
	}
	return spec, h, nil
}

func (s *Service) Status(p Principal, key string, withProgress bool) Result {
	spec, h, err := s.own(p, key)
	if err != nil {
		return RefusedResult(nil, err)
	}
	return ResultOf(spec, h, withProgress)
}

func (s *Service) Cancel(p Principal, key string) Result {
	spec, h, err := s.own(p, key)
	if err != nil {
		return RefusedResult(nil, err)
	}
	ack := h.Cancel()
	res := ResultOf(spec, h, true)
	if ack.Requested && res.Cancel == "" {
		res.Cancel = "requested"
	}
	if !ack.Requested && res.Cancel == "" {
		res.Cancel = string(ack.Status)
	}
	return res
}

// Reconcile asks the source what happened to an unknown outcome and reports only what evidence settled.
func (s *Service) Reconcile(ctx context.Context, p Principal, key string, raw []byte) Result {
	spec, h, err := s.own(p, key)
	if err != nil {
		return RefusedResult(nil, err)
	}
	v, derr := DecodeStrict(raw)
	m, ok := objectOf(v)
	if derr != nil || !ok {
		return RefusedResult(spec, newError(CodeInvalidRequest, "malformed_json"))
	}
	for k := range m {
		if k != "session_id" && k != "expect" {
			return RefusedResult(spec, errWith(CodeInvalidRequest, "unknown_field", nil))
		}
	}
	sessionID, expect := strOf(m["session_id"]), stringMap(m["expect"])
	if sessionID == "" || len(expect) == 0 {
		return RefusedResult(spec, errWith(CodeInvalidArgument, "session_and_expect_required", nil))
	}
	if serr := s.sessionFor(p, sessionID); serr != nil {
		return RefusedResult(spec, serr)
	}
	doc, ok := s.cfg.Catalog.Contract(spec.ContractDigest)
	if !ok {
		return RefusedResult(spec, newError(CodeInternal, ""))
	}
	rec, rerr := s.cfg.Runtime.Reconcile(ctx, execution.ReconcileRequest{Caller: p.Caller, SessionID: sessionID, Key: key, Contract: doc, Expect: expect})
	res := ResultOf(spec, h, true)
	res.Reconciliation = &Reconciliation{Attempt: rec.Attempt, Level: string(rec.Evidence.Level), Basis: rec.Evidence.Basis, Version: rec.Evidence.Version}
	switch {
	case rerr == nil:
		res.Reconciliation.Settled = true
	case errors.Is(rerr, execution.ErrInconclusive):
		res.Error = errWith(CodeStillUnknown, string(rec.Evidence.Level), nil)
		res.Retry = RetryReconcile
	default:
		res.Category = CategoryRefused
		res.Error = submitError(rerr)
		res.Retry = res.Error.Retry
	}
	return res
}

// Reconciliation says what a reconcile attempt found, separately from the invocation's own state.
type Reconciliation struct {
	Attempt int    `json:"attempt"`
	Level   string `json:"evidence_level,omitempty"`
	Basis   string `json:"basis,omitempty"`
	Version string `json:"version,omitempty"`
	Settled bool   `json:"settled"`
}

// ConfirmationTTL is how long a person has to use a confirmation they gave.
const ConfirmationTTL = 5 * time.Minute

// Confirm records a person's yes to exactly this request, for this caller in this session, and to nothing else.
func (s *Service) Confirm(_ context.Context, p Principal, raw []byte, route Route) (ConfirmationIn, *Error) {
	if _, err := s.caller(p); err != nil {
		return ConfirmationIn{}, err
	}
	if p.Surface != SurfaceUI || s.cfg.Confirmer == nil || s.cfg.Now == nil {
		return ConfirmationIn{}, errWith(CodePermissionDenied, "confirmation_requires_a_person", nil)
	}
	var head struct{ Version, Digest string }
	if v, derr := DecodeStrict(raw); derr == nil {
		if m, ok := objectOf(v); ok {
			head.Version, head.Digest = strOf(m["contract_version"]), strOf(m["contract_digest"])
		}
	}
	who, _ := s.cfg.Callers.Lookup(p.Caller)
	spec, err := s.resolve(p, who, route, head.Version, head.Digest)
	if err != nil {
		return ConfirmationIn{}, err
	}
	inv, err := DecodeInvocation(spec, raw)
	if err != nil {
		return ConfirmationIn{}, err
	}
	if err := s.sessionFor(p, inv.SessionID); err != nil {
		return ConfirmationIn{}, err
	}
	if !spec.Effect.Write {
		return ConfirmationIn{}, errWith(CodeInvalidRequest, "nothing_to_confirm", nil)
	}
	digest := execution.ConfirmationDigest(spec.ContractDigest, inv.Record, inv.Params, p.Caller, inv.SessionID)
	c := s.cfg.Confirmer.Issue(digest, p.Caller, s.cfg.Now().Add(ConfirmationTTL))
	return ConfirmationIn{Token: c.Token, By: c.By, Digest: c.Digest, ExpiresAt: c.ExpiresAt.UnixMilli()}, nil
}
