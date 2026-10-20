package execution

import (
	"context"
	"encoding/json"
	"fmt"
)

const maxReconcileNote = 512

type ReconcileRequest struct {
	Caller    string
	SessionID string
	Key       string
	Contract  []byte
	Expect    map[string]string
}

type Reconciliation struct {
	Record   Record
	Evidence Evidence
	Attempt  int
}

// attempt is what the journal keeps of one reconciliation, whether or not it settled anything.
type attempt struct {
	N        int           `json:"n"`
	By       string        `json:"by"`
	Session  string        `json:"session"`
	Level    EvidenceLevel `json:"level,omitempty"`
	Basis    string        `json:"basis,omitempty"`
	Version  string        `json:"version,omitempty"`
	Matches  bool          `json:"matches"`
	Outcome  string        `json:"outcome"`
	Detail   string        `json:"detail,omitempty"`
	Observed bool          `json:"observed"`
}

func (r *Runtime) noteAttempt(key string, a attempt) {
	b, _ := json.Marshal(a)
	if len(b) > maxReconcileNote {
		b = b[:maxReconcileNote]
	}
	_ = r.j.Note(key, "reconcile:"+string(b), r.now())
}

func attemptsOf(rec Record) int {
	n := 0
	for _, e := range rec.Events {
		if e.Kind == "note" && len(e.Detail) > 10 && e.Detail[:10] == "reconcile:" {
			n++
		}
	}
	return n
}

// Reconcile settles an Unknown operation only on evidence of how this operation ended, never on a matching state.
func (r *Runtime) Reconcile(ctx context.Context, q ReconcileRequest) (Reconciliation, error) {
	rec, ok := r.j.Get(q.Key)
	if !ok {
		return Reconciliation{}, refuse("not_unknown")
	}
	out := Reconciliation{Record: rec, Attempt: attemptsOf(rec) + 1}
	refused := func(reason string) (Reconciliation, error) {
		if rec.State == Unknown {
			r.noteAttempt(q.Key, attempt{N: out.Attempt, By: q.Caller, Session: q.SessionID, Outcome: "refused:" + reason})
		}
		return out, refuse(reason)
	}
	if rec.State != Unknown {
		return out, refuse("not_unknown")
	}
	if rec.Caller != q.Caller {
		return refused("other_caller")
	}
	un, digest, err := decodeDocument(q.Contract)
	if err != nil || un.Unsafe().Operation == nil {
		return refused("undecodable")
	}
	if digest != rec.ContractDigest {
		return refused("contract_mismatch")
	}
	c := *un.Unsafe().Operation
	if rec.Admitted == nil {
		return refused("identity_unrecorded")
	}
	sess, ok := r.cfg.Sessions.Lookup(q.SessionID)
	if !ok {
		return refused("unknown_session")
	}
	adm := rec.Admitted
	if sess.AppID != adm.AppID || sess.VariantClass != adm.VariantClass || sess.PrincipalRef != adm.Env.PrincipalRef ||
		sess.VariantID != adm.Env.VariantID {
		return refused("other_source")
	}
	req := Request{Caller: q.Caller, SessionID: q.SessionID, Contract: q.Contract}
	env, err := bind(c, r.cfg.Callers, r.cfg.Sessions, q.Caller, q.SessionID, r.clock.Now())
	if err == nil && (env.PrincipalRef != adm.Env.PrincipalRef || env.VariantID != adm.Env.VariantID) {
		err = refuse("environment_changed")
	}
	if err == nil {
		err = r.authorize(req, c, digest, nil)
	}
	if err != nil {
		return refused(reasonOf(err))
	}
	var p pending
	for _, e := range rec.Events {
		if e.Kind == "state" && e.To == Running {
			_ = json.Unmarshal([]byte(e.Detail), &p)
		}
	}
	if p.Expect == "" || digestOf(q.Expect) != p.Expect {
		return refused("expect_mismatch")
	}
	exec, ok := r.cfg.Executors[p.Strategy]
	if !ok {
		return refused("no_executor")
	}
	r.mu.Lock()
	if r.reconciling[q.Key] {
		r.mu.Unlock()
		return refused("reconcile_in_progress")
	}
	r.reconciling[q.Key] = true
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		delete(r.reconciling, q.Key)
		r.mu.Unlock()
	}()

	t := Target{OperationID: rec.OperationID, Record: p.Record}
	a := attempt{N: out.Attempt, By: q.Caller, Session: q.SessionID}
	snap, rerr := exec.Read(ctx, t)
	a.Observed = rerr == nil
	ev := Evidence{Level: Inconclusive, Basis: "state_read", Detail: "source_unreadable"}
	if rerr == nil {
		a.Matches = matches(snap.Fields, q.Expect)
		ev = Evidence{Level: Inconclusive, Basis: "state_read", Version: snap.Version}
		if a.Matches {
			ev.Level = StateMatchesUnproven
		}
	}
	caps := exec.Capabilities()
	if rc, ok := exec.(Reconciler); ok && caps.ReceiptLookup && caps.DedupTokens {
		got, perr := rc.Reconcile(ctx, Probe{Key: q.Key, DedupToken: q.Key, Target: t, PreVersion: p.PreVersion, Expect: q.Expect})
		if perr == nil {
			if got = admit(caps, got); got.Level == ProvenCommitted || got.Level == ProvenNotCommitted || ev.Level == Inconclusive {
				ev = got
			}
		} else {
			ev.Detail = "receipt_lookup_failed"
		}
	}
	a.Level, a.Basis, a.Version, a.Detail = ev.Level, ev.Basis, ev.Version, ev.Detail
	out.Evidence = ev

	to, reason := Unknown, ""
	switch {
	case ev.Level == ProvenNotCommitted:
		to, reason = Failed, "reconciled_not_committed"
	case ev.Level == ProvenCommitted && a.Matches:
		to, reason = Verified, "reconciled_committed"
	case ev.Level == ProvenCommitted:
		a.Detail = "committed_but_expected_values_not_visible"
	}
	a.Outcome = "unresolved"
	if to != Unknown {
		a.Outcome = "resolved:" + string(to)
	}
	r.noteAttempt(q.Key, a)
	if to != Unknown {
		if err := r.j.Move(q.Key, to, reason, ev.Basis, r.now()); err != nil {
			return out, err
		}
	}
	out.Record, _ = r.j.Get(q.Key)
	if to == Unknown {
		return out, fmt.Errorf("%w (%s)", ErrInconclusive, ev.Level)
	}
	return out, nil
}
