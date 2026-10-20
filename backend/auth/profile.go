package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"sort"
	"sync"
	"time"
)

func init() { addReasons(ReasonRoles, ReasonEnv, ReasonProfileExpired, ReasonNoProfile) }

const (
	ReasonRoles          Reason = "roles_changed"
	ReasonEnv            Reason = "environment_changed"
	ReasonProfileExpired Reason = "profile_expired"
	ReasonNoProfile      Reason = "no_profile"
)

// Env is what differs between sign-ins of the same person that can change what an operation does.
type Env struct {
	Locale     string `json:"locale"`
	Region     string `json:"region"`
	Variant    string `json:"variant"`
	AppVersion string `json:"app_version"`
}

// Profile is what a signed-in account may do at its source right now.
type Profile struct {
	GrantID      string
	Roles        []string
	Capabilities []string
	Env          Env
	ObservedAt   time.Time
	ExpiresAt    time.Time
}

type profileKey struct {
	Roles        []string `json:"roles"`
	Capabilities []string `json:"capabilities"`
	Env          Env      `json:"env"`
	Principal    string   `json:"principal"`
	Source       Source   `json:"source"`
}

// Digest identifies the profile's content: roles, capabilities, environment, person and source, in canonical order and without times.
func digestOf(p Profile, g Grant) string {
	k := profileKey{Roles: slices.Clone(p.Roles), Capabilities: slices.Clone(p.Capabilities), Env: p.Env, Principal: g.PrincipalRef, Source: g.Source}
	sort.Strings(k.Roles)
	sort.Strings(k.Capabilities)
	k.Roles, k.Capabilities = slices.Compact(k.Roles), slices.Compact(k.Capabilities)
	b, _ := json.Marshal(k)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Change says what differs from the previous observation of the same sign-in.
type Change struct {
	New          bool
	Roles        bool
	Capabilities bool
	Env          bool
	Expiry       bool
}

// Any reports whether something that can invalidate a plan changed.
func (c Change) Any() bool { return c.Roles || c.Capabilities || c.Env }

type stored struct {
	p      Profile
	digest string
}

// PlanRef ties a plan to the profile it was built under.
type PlanRef struct {
	PlanID string
	Owner  string
	Grant  string
	Digest string
}

// Profiles tracks each signed-in account's profile and tells whether a plan built earlier is still safe to run.
type Profiles struct {
	Broker *Broker

	mu      sync.Mutex
	byGrant map[string]stored
	flagged map[string]bool
	history map[string]Profile
}

const maxHistory = 1024

func NewProfiles(b *Broker) *Profiles {
	return &Profiles{Broker: b, byGrant: map[string]stored{}, flagged: map[string]bool{}, history: map[string]Profile{}}
}

func validList(in []string) bool {
	if len(in) > maxScopes {
		return false
	}
	for _, s := range in {
		if !scopeRe.MatchString(s) {
			return false
		}
	}
	return true
}

func (e Env) valid() bool {
	for _, s := range []string{e.Locale, e.Region, e.Variant, e.AppVersion} {
		if len(s) > 64 {
			return false
		}
	}
	return true
}

// Observe records what the source showed for a live grant and returns what changed. The profile cannot outlive its grant.
func (ps *Profiles) Observe(owner string, p Profile) (Change, error) {
	g, ok := ps.Broker.Store.Get(p.GrantID)
	if !ok || g.Owner != owner || g.State == StateRevoked {
		ps.Broker.note(EventRefused, Grant{ID: p.GrantID}, safeActor(owner), ReasonUnknownGrant)
		return Change{}, ErrDenied
	}
	if !validList(p.Roles) || !validList(p.Capabilities) || !p.Env.valid() || p.ObservedAt.IsZero() {
		return Change{}, ErrInvalid
	}
	if p.ExpiresAt.IsZero() || p.ExpiresAt.After(g.ExpiresAt) {
		p.ExpiresAt = g.ExpiresAt
	}
	d := digestOf(p, g)
	ps.mu.Lock()
	defer ps.mu.Unlock()
	prev, had := ps.byGrant[g.ID]
	ps.byGrant[g.ID] = stored{p: p, digest: d}
	if len(ps.history) >= maxHistory {
		for k := range ps.history {
			delete(ps.history, k)
			break
		}
	}
	ps.history[d] = p
	ch := Change{New: !had}
	if had {
		ch.Roles = !sameSet(prev.p.Roles, p.Roles)
		ch.Capabilities = !sameSet(prev.p.Capabilities, p.Capabilities)
		ch.Env = prev.p.Env != p.Env
		ch.Expiry = !prev.p.ExpiresAt.Equal(p.ExpiresAt)
	}
	if ch.Roles || ch.Capabilities {
		ps.Broker.noteRef(EventProfile, g, owner, ReasonRoles, d)
	}
	if ch.Env {
		ps.Broker.noteRef(EventProfile, g, owner, ReasonEnv, d)
	}
	return ch, nil
}

func sameSet(a, b []string) bool {
	x, y := slices.Clone(a), slices.Clone(b)
	sort.Strings(x)
	sort.Strings(y)
	return slices.Equal(slices.Compact(x), slices.Compact(y))
}

func (b *Broker) noteRef(ev Event, g Grant, actor string, r Reason, digest string) {
	if b.Audit == nil {
		return
	}
	ref := digest
	if len(ref) > 16 {
		ref = ref[:16]
	}
	_ = b.Audit.Append(AuditRecord{At: b.Now(), Event: ev, Grant: g.ID, Account: g.Account, Actor: actor, Origin: g.Source.Origin, Reason: r, Ref: ref})
}

// Bind records the profile a plan is being built under.
func (ps *Profiles) Bind(planID, owner, grantID string) (PlanRef, error) {
	if !idRe.MatchString(planID) {
		return PlanRef{}, ErrInvalid
	}
	g, ok := ps.Broker.Store.Get(grantID)
	if !ok || g.Owner != owner {
		return PlanRef{}, ErrDenied
	}
	ps.mu.Lock()
	defer ps.mu.Unlock()
	s, ok := ps.byGrant[grantID]
	if !ok {
		ps.Broker.note(EventRefused, g, owner, ReasonNoProfile)
		return PlanRef{}, ErrReauth
	}
	return PlanRef{PlanID: planID, Owner: owner, Grant: grantID, Digest: s.digest}, nil
}

// Valid says whether a plan may still run: the same person, the same roles, capabilities and environment, a sign-in that is live and not expired. The first time a plan fails, the reason is audited.
func (ps *Profiles) Valid(ref PlanRef) (bool, Reason) {
	g, ok := ps.Broker.Store.Get(ref.Grant)
	if !ok || g.Owner != ref.Owner {
		return false, ReasonUnknownGrant
	}
	why := Reason("")
	ps.mu.Lock()
	s, have := ps.byGrant[ref.Grant]
	ps.mu.Unlock()
	now := ps.Broker.Now()
	switch {
	case g.State == StateRevoked:
		why = ReasonRevoked
	case g.State == StatePaused:
		why = ReasonPaused
	case !have:
		why = ReasonNoProfile
	case !now.Before(s.p.ExpiresAt):
		why = ReasonProfileExpired
	case s.digest != ref.Digest:
		why = changeReason(ps, ref, s)
	}
	if why == "" {
		return true, ReasonOK
	}
	ps.mu.Lock()
	first := !ps.flagged[ref.PlanID+"\x00"+ref.Digest]
	ps.flagged[ref.PlanID+"\x00"+ref.Digest] = true
	ps.mu.Unlock()
	if first {
		ps.Broker.noteRef(EventPlanStale, g, ref.Owner, why, ref.Digest)
	}
	return false, why
}

// changeReason names the broadest cause when the digest differs: a different person or source first, then roles, then environment.
func changeReason(ps *Profiles, ref PlanRef, cur stored) Reason {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if old, ok := ps.history[ref.Digest]; ok {
		switch {
		case !sameSet(old.Roles, cur.p.Roles) || !sameSet(old.Capabilities, cur.p.Capabilities):
			return ReasonRoles
		case old.Env != cur.p.Env:
			return ReasonEnv
		}
	}
	return ReasonRoles
}
