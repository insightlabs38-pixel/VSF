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

func init() {
	addReasons(ReasonRoles, ReasonEnv, ReasonProfileExpired, ReasonNoProfile)
}

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
	Tenant     string `json:"tenant"`
	// Capability is the digest of the environment capability manifest the page was observed under.
	Capability string `json:"capability"`
}

// ProfileClaim is the profile the source page showed; it travels inside Evidence, so it has the provenance, freshness and ordering of the observation that carries it.
type ProfileClaim struct {
	Roles        []string
	Capabilities []string
	Env          Env
}

// Profile is what a signed-in account may do at its source right now.
type Profile struct {
	GrantID      string
	Epoch        int
	Roles        []string
	Capabilities []string
	Env          Env
	RuntimeID    string
	ObservedAt   time.Time
	ExpiresAt    time.Time
}

type authority struct {
	V         int      `json:"v"`
	Roles     []string `json:"roles"`
	Caps      []string `json:"capabilities"`
	Env       Env      `json:"env"`
	Principal string   `json:"principal"`
	Source    Source   `json:"source"`
}

type binding struct {
	Content authority `json:"content"`
	Grant   string    `json:"grant"`
	Epoch   int       `json:"epoch"`
	Runtime string    `json:"runtime"`
}

func canonical(p Profile, g Grant) (content, full string) {
	a := authority{V: 1, Roles: slices.Compact(slices.Sorted(slices.Values(p.Roles))), Caps: slices.Compact(slices.Sorted(slices.Values(p.Capabilities))), Env: p.Env, Principal: g.PrincipalRef, Source: g.Source}
	b, _ := json.Marshal(a)
	cs := sha256.Sum256(b)
	fb, _ := json.Marshal(binding{Content: a, Grant: g.ID, Epoch: p.Epoch, Runtime: p.RuntimeID})
	fs := sha256.Sum256(fb)
	return hex.EncodeToString(cs[:]), hex.EncodeToString(fs[:])
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
	p       Profile
	digest  string
	content string
}

// PlanRef ties a plan to the profile and the exact sign-in it was built under.
type PlanRef struct {
	PlanID  string
	Owner   string
	Grant   string
	Epoch   int
	Digest  string
	Content string
}

// Profiles tracks each signed-in account's profile and tells whether a plan built earlier is still safe to run.
type Profiles struct {
	Broker *Broker

	mu      sync.Mutex
	byGrant map[string]stored
	flagged map[string]bool
	history map[string]Profile
	order   []string
}

const (
	maxHistory  = 1024
	maxProfiles = 4096
)

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
	for _, s := range []string{e.Locale, e.Region, e.Variant, e.AppVersion, e.Tenant, e.Capability} {
		if len(s) > 128 {
			return false
		}
	}
	return true
}

// grantFor finds the one live source-managed grant an observation speaks for: same caller, source, principal, runtime and bridge session. Anything else is refused rather than guessed.
func (b *Broker) grantFor(o Observation) (Grant, bool) {
	var found []Grant
	for _, g := range b.Store.List(o.Owner) {
		if g.Method == MethodSourceBrowser && g.IdentityVerified && g.Source == o.Source && g.PrincipalRef == o.PrincipalRef && g.State != StateRevoked {
			found = append(found, g)
		}
	}
	if len(found) != 1 {
		return Grant{}, false
	}
	return found[0], true
}

// Observe records the profile a verified observation carried and returns what changed. The observation must be authenticated, challenge-free, newer than the previous profile, fresh, and from the runtime session the grant is bound to; the profile cannot outlive its grant or its epoch.
func (ps *Profiles) Observe(o Observation) (Change, error) {
	if !o.verified || o.Profile == nil || !o.Authenticated || o.Challenge != "" {
		return Change{}, ErrInvalid
	}
	g, ok := ps.Broker.grantFor(o)
	if !ok {
		ps.Broker.note(EventRefused, Grant{Source: o.Source}, safeActor(o.Owner), ReasonUnknownGrant)
		return Change{}, ErrDenied
	}
	pc := o.Profile
	if !validList(pc.Roles) || !validList(pc.Capabilities) || !pc.Env.valid() {
		return Change{}, ErrInvalid
	}
	now := ps.Broker.Now()
	switch {
	case g.RuntimeID != o.RuntimeID || g.SessionID != o.SessionID:
		return Change{}, ErrReauth
	case now.Sub(o.ObservedAt) > 2*obsMaxAge || o.ObservedAt.Before(g.ObservedAt):
		ps.Broker.note(EventRefused, g, o.Owner, ReasonStaleObs)
		return Change{}, ErrStale
	case g.State != StateActive:
		return Change{}, ErrReauth
	}
	p := Profile{GrantID: g.ID, Epoch: g.Epoch, Roles: pc.Roles, Capabilities: pc.Capabilities, Env: pc.Env, RuntimeID: o.RuntimeID, ObservedAt: o.ObservedAt, ExpiresAt: o.Expires}
	if p.ExpiresAt.IsZero() || p.ExpiresAt.After(g.ExpiresAt) {
		p.ExpiresAt = g.ExpiresAt
	}
	content, full := canonical(p, g)
	ps.mu.Lock()
	defer ps.mu.Unlock()
	prev, had := ps.byGrant[g.ID]
	if had && prev.p.Epoch == p.Epoch && !p.ObservedAt.After(prev.p.ObservedAt) {
		ps.Broker.note(EventRefused, g, o.Owner, ReasonStaleObs)
		return Change{}, ErrStale
	}
	if !had && len(ps.byGrant) >= maxProfiles {
		for id := range ps.byGrant {
			if cur, ok := ps.Broker.Store.Get(id); !ok || cur.State == StateRevoked {
				delete(ps.byGrant, id)
			}
		}
		if len(ps.byGrant) >= maxProfiles {
			return Change{}, ErrDenied
		}
	}
	ps.byGrant[g.ID] = stored{p: p, digest: full, content: content}
	if _, seen := ps.history[full]; !seen {
		if len(ps.order) >= maxHistory {
			delete(ps.history, ps.order[0])
			ps.order = ps.order[1:]
		}
		ps.order = append(ps.order, full)
	}
	ps.history[full] = p
	ch := Change{New: !had}
	if had {
		ch.Roles = !sameSet(prev.p.Roles, p.Roles)
		ch.Capabilities = !sameSet(prev.p.Capabilities, p.Capabilities)
		ch.Env = prev.p.Env != p.Env
		ch.Expiry = !prev.p.ExpiresAt.Equal(p.ExpiresAt)
	}
	if ch.Roles || ch.Capabilities {
		ps.Broker.noteRef(EventProfile, g, o.Owner, ReasonRoles, full)
	}
	if ch.Env {
		ps.Broker.noteRef(EventProfile, g, o.Owner, ReasonEnv, full)
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
	if err := b.Audit.Append(AuditRecord{At: b.Now(), Event: ev, Grant: g.ID, Account: g.Account, Actor: actor, Origin: g.Source.Origin, Reason: r, Ref: ref}); err != nil {
		b.dropped.Add(1)
	}
}

// Bind records the profile and sign-in a plan is being built under. It needs a profile observed in the grant's current epoch.
func (ps *Profiles) Bind(planID, owner, grantID string) (PlanRef, error) {
	if !idRe.MatchString(planID) {
		return PlanRef{}, ErrInvalid
	}
	g, ok := ps.Broker.Store.Get(grantID)
	if !ok || g.Owner != owner || g.State == StateRevoked {
		return PlanRef{}, ErrDenied
	}
	ps.mu.Lock()
	defer ps.mu.Unlock()
	s, ok := ps.byGrant[grantID]
	if !ok || s.p.Epoch != g.Epoch || g.State != StateActive {
		ps.Broker.note(EventRefused, g, owner, ReasonNoProfile)
		return PlanRef{}, ErrReauth
	}
	return PlanRef{PlanID: planID, Owner: owner, Grant: grantID, Epoch: g.Epoch, Digest: s.digest, Content: s.content}, nil
}

// Valid says whether a plan may still run: the same person and sign-in epoch, the same roles, capabilities and environment, a sign-in that is live and not expired. The first time a plan fails, the reason is audited.
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
	case !now.Before(g.ExpiresAt):
		why = ReasonExpired
	case g.Epoch != ref.Epoch:
		why = ReasonEpoch
	case !have || s.p.Epoch != g.Epoch:
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
	first := !ps.flagged[ref.PlanID+"\x00"+ref.Digest+"\x00"+string(why)]
	if first && len(ps.flagged) < maxHistory*8 {
		ps.flagged[ref.PlanID+"\x00"+ref.Digest+"\x00"+string(why)] = true
	}
	ps.mu.Unlock()
	if first {
		ps.Broker.noteRef(EventPlanStale, g, ref.Owner, why, ref.Digest)
	}
	return false, why
}

// Rebind carries a plan to the grant's current sign-in epoch, and only when what the profile authorizes is exactly what the plan was built under. A plan whose roles, capabilities or environment moved must be built again.
func (ps *Profiles) Rebind(ref PlanRef) (PlanRef, error) {
	g, ok := ps.Broker.Store.Get(ref.Grant)
	if !ok || g.Owner != ref.Owner || g.State != StateActive || !ps.Broker.Now().Before(g.ExpiresAt) {
		return PlanRef{}, ErrDenied
	}
	ps.mu.Lock()
	defer ps.mu.Unlock()
	s, have := ps.byGrant[ref.Grant]
	switch {
	case !have || s.p.Epoch != g.Epoch || !ps.Broker.Now().Before(s.p.ExpiresAt):
		return PlanRef{}, ErrReauth
	case s.content != ref.Content:
		return PlanRef{}, ErrStale
	}
	return PlanRef{PlanID: ref.PlanID, Owner: ref.Owner, Grant: ref.Grant, Epoch: g.Epoch, Digest: s.digest, Content: s.content}, nil
}

// changeReason names the broadest cause when the digest differs: roles and capabilities first, then environment.
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
