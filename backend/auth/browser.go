package auth

import (
	"crypto/sha256"
	"errors"
	"slices"
	"sort"
	"sync"
	"time"
)

const (
	intentTTL      = 15 * time.Minute
	maxSourceLife  = 12 * time.Hour
	maxIntentsEach = 16
	// obsMaxAge bounds how old an observation may be; ordering state older than this is redundant and dropped.
	obsMaxAge  = 30 * time.Second
	obsSkew    = 5 * time.Second
	maxMarks   = 8192
	lockStripe = 32
)

// RuntimeSession is what the bridge vouches for about a live runtime session: the paired caller, the exact source and origin it was registered for, and the principal and runtime identity in its validated manifest.
type RuntimeSession struct {
	ID        string
	Client    string
	Owner     string
	AppID     string
	Origin    string
	Principal string
	Runtime   string
	Expires   time.Time
}

// Runtimes resolves a bridge session for the paired client that presents it. It must fail for an unknown, ended, expired or foreign session.
type Runtimes interface {
	Session(id, client string) (RuntimeSession, error)
}

// Evidence is what a paired runtime reports about a source page. Client is set by the transport from the authenticated pairing, never from message content. VSF accepts nothing else as proof of a sign-in: a page script, a local process without a pairing, or a replayed message cannot produce an Observation.
type Evidence struct {
	Client        string
	SessionID     string
	Source        Source
	PrincipalRef  string
	Authenticated bool
	// Challenge is the step-up the source is asking for right now (multi-factor, passkey, password again), empty when none.
	Challenge Challenge
	// Seq rises with every message of one runtime session; ObservedAt is the runtime's clock reading.
	Seq        uint64
	ObservedAt time.Time
	Expires    time.Time
	Scopes     []string
	// GrantEpoch is the sign-in epoch the runtime last saw for this source, zero when it has none; a sign-out or challenge about an older epoch than the grant's is stale.
	GrantEpoch int
	// Profile, when present, is the roles, capabilities and environment the page showed in this same observation.
	Profile *ProfileClaim
}

// Observation is Evidence that passed the authority. Only this package can build a verified one.
type Observation struct {
	Owner         string
	Source        Source
	PrincipalRef  string
	Authenticated bool
	Challenge     Challenge
	ObservedAt    time.Time
	Expires       time.Time
	Scopes        []string
	RuntimeID     string
	SessionID     string
	Seq           uint64
	GrantEpoch    int
	Profile       *ProfileClaim
	verified      bool
}

type intent struct {
	account string
	scopes  []string
	expires time.Time
}

type mark struct {
	seq uint64
	at  time.Time
}

// Browser tracks sign-ins the source application manages itself. VSF never sees or sends credentials: the person signs in on the page, the paired runtime reports it, the authority checks the report against the bridge and a grant records it without any secret.
type Browser struct {
	Broker   *Broker
	Runtimes Runtimes

	mu      sync.Mutex
	intents map[string]intent
	marks   map[string]mark
	latest  map[string]time.Time
	locks   [lockStripe]sync.Mutex
}

func NewBrowser(b *Broker, rt Runtimes) *Browser {
	return &Browser{Broker: b, Runtimes: rt, intents: map[string]intent{}, marks: map[string]mark{}, latest: map[string]time.Time{}}
}

func ikey(owner string, s Source) string { return owner + "\x00" + s.AppID + "\x00" + s.Origin }

func (br *Browser) lock(k string) *sync.Mutex {
	h := sha256.Sum256([]byte(k))
	return &br.locks[int(h[0])%lockStripe]
}

// Cancel drops an expected sign-in the caller no longer wants.
func (br *Browser) Cancel(owner string, src Source) {
	br.mu.Lock()
	defer br.mu.Unlock()
	delete(br.intents, ikey(owner, src))
}

// Expect records that a caller is about to sign in to a source; an observation of a sign-in that was not expected creates nothing.
func (br *Browser) Expect(owner, account string, src Source, scopes []string) error {
	if !idRe.MatchString(owner) || !idRe.MatchString(account) || !idRe.MatchString(src.AppID) || !originRe.MatchString(src.Origin) || len(scopes) > maxScopes {
		return ErrInvalid
	}
	for _, s := range scopes {
		if !scopeRe.MatchString(s) {
			return ErrInvalid
		}
	}
	sc := slices.Clone(scopes)
	sort.Strings(sc)
	now := br.Broker.Now()
	br.mu.Lock()
	defer br.mu.Unlock()
	count := 0
	for k, x := range br.intents {
		if !now.Before(x.expires) {
			delete(br.intents, k)
		} else {
			count++
		}
	}
	if count >= maxIntentsEach*8 {
		return ErrDenied
	}
	br.intents[ikey(owner, src)] = intent{account: account, scopes: slices.Compact(sc), expires: now.Add(intentTTL)}
	return nil
}

func (br *Browser) active(owner string, src Source) []Grant {
	var out []Grant
	for _, g := range br.Broker.Store.List(owner) {
		if g.Method == MethodSourceBrowser && g.Source == src && g.State != StateRevoked {
			out = append(out, g)
		}
	}
	return out
}

// Verify checks evidence against the bridge and the ordering state and, when it holds, returns the verified observation. It records the message so a replay of it, or of anything older, is refused afterwards. Where the infrastructure cannot prove a claim the answer is an error, never an authenticated observation.
func (br *Browser) Verify(ev Evidence) (Observation, error) {
	if br.Runtimes == nil || !idRe.MatchString(ev.Client) || !idRe.MatchString(ev.SessionID) || !idRe.MatchString(ev.Source.AppID) || !originRe.MatchString(ev.Source.Origin) ||
		ev.Seq == 0 || ev.ObservedAt.IsZero() || len(ev.Scopes) > maxScopes || (ev.Challenge != "" && !challenges[ev.Challenge]) || ev.GrantEpoch < 0 {
		return Observation{}, ErrInvalid
	}
	for _, s := range ev.Scopes {
		if !scopeRe.MatchString(s) {
			return Observation{}, ErrInvalid
		}
	}
	rs, err := br.Runtimes.Session(ev.SessionID, ev.Client)
	if err != nil || rs.ID != ev.SessionID || rs.Client != ev.Client || !idRe.MatchString(rs.Owner) || rs.AppID == "" || rs.Runtime == "" {
		br.Broker.note(EventRefused, Grant{Source: Source{Origin: safeOrigin(ev.Source.Origin)}}, safeActor(ev.Client), ReasonRuntime)
		return Observation{}, ErrDenied
	}
	if rs.Origin != ev.Source.Origin || rs.AppID != ev.Source.AppID || (ev.Authenticated && (ev.PrincipalRef == "" || ev.PrincipalRef != rs.Principal)) {
		br.Broker.note(EventRefused, Grant{Source: Source{Origin: safeOrigin(ev.Source.Origin)}}, rs.Owner, ReasonRuntime)
		return Observation{}, ErrDenied
	}
	now := br.Broker.Now()
	if ev.ObservedAt.After(now.Add(obsSkew)) || now.Sub(ev.ObservedAt) > obsMaxAge {
		br.Broker.note(EventRefused, Grant{Source: ev.Source}, rs.Owner, ReasonStaleObs)
		return Observation{}, ErrStale
	}
	sk := ikey(rs.Owner, ev.Source)
	mk := sk + "\x00" + ev.SessionID
	br.mu.Lock()
	defer br.mu.Unlock()
	if len(br.marks) >= maxMarks {
		for k, m := range br.marks {
			if now.Sub(m.at) > 2*obsMaxAge {
				delete(br.marks, k)
			}
		}
		for k, at := range br.latest {
			if now.Sub(at) > 2*obsMaxAge {
				delete(br.latest, k)
			}
		}
		if len(br.marks) >= maxMarks {
			return Observation{}, ErrDenied
		}
	}
	prev, seen := br.marks[mk]
	switch {
	case seen && (ev.Seq <= prev.seq || ev.ObservedAt.Before(prev.at)):
		br.Broker.note(EventRefused, Grant{Source: ev.Source}, rs.Owner, ReasonReplay)
		return Observation{}, ErrStale
	case !ev.ObservedAt.After(br.latest[sk]) && !seen && !br.latest[sk].IsZero():
		br.Broker.note(EventRefused, Grant{Source: ev.Source}, rs.Owner, ReasonStaleObs)
		return Observation{}, ErrStale
	}
	br.marks[mk] = mark{seq: ev.Seq, at: ev.ObservedAt}
	if ev.ObservedAt.After(br.latest[sk]) {
		br.latest[sk] = ev.ObservedAt
	}
	return Observation{Owner: rs.Owner, Source: ev.Source, PrincipalRef: ev.PrincipalRef, Authenticated: ev.Authenticated, Challenge: ev.Challenge, ObservedAt: ev.ObservedAt,
		Expires: ev.Expires, Scopes: slices.Clone(ev.Scopes), RuntimeID: rs.Runtime, SessionID: rs.ID, Seq: ev.Seq, GrantEpoch: ev.GrantEpoch, Profile: cloneClaim(ev.Profile), verified: true}, nil
}

// Accept verifies evidence and applies it: a sign-out ends the grants for that source, a sign-in as someone else ends the old person's grant before anything new is recorded, a challenge pauses, and an all-clear after a challenge resumes.
func (br *Browser) Accept(ev Evidence) (Grant, error) {
	o, err := br.Verify(ev)
	if err != nil {
		return Grant{}, err
	}
	return br.Apply(o)
}

// Apply acts on an observation Verify returned; any other value is refused.
func (br *Browser) Apply(o Observation) (Grant, error) {
	if !o.verified {
		return Grant{}, ErrDenied
	}
	mu := br.lock(ikey(o.Owner, o.Source))
	mu.Lock()
	defer mu.Unlock()
	now := br.Broker.Now()
	if !o.Authenticated {
		var firstErr error
		for _, g := range br.active(o.Owner, o.Source) {
			if o.ObservedAt.Before(g.ObservedAt) || (o.GrantEpoch != 0 && o.GrantEpoch < g.Epoch) {
				br.Broker.note(EventRefused, g, o.Owner, ReasonStaleObs)
				firstErr = errors.Join(firstErr, ErrStale)
				continue
			}
			br.Broker.note(EventSignedOut, g, o.Owner, ReasonRevoked)
			if _, err := br.Broker.endGrant(g.ID, o.Owner, ReasonRevoked); err != nil && !errors.Is(err, ErrWipePending) && !errors.Is(err, ErrAudit) {
				return Grant{}, err
			}
		}
		return Grant{}, firstErr
	}
	if o.PrincipalRef == "" || !idRe.MatchString(o.PrincipalRef) {
		return Grant{}, ErrInvalid
	}
	expires := now.Add(maxSourceLife)
	if !o.Expires.IsZero() && o.Expires.Before(expires) {
		expires = o.Expires
	}
	if !expires.After(now) {
		return Grant{}, ErrExpired
	}
	for _, g := range br.active(o.Owner, o.Source) {
		if g.PrincipalRef != o.PrincipalRef {
			if _, err := br.Broker.endGrant(g.ID, o.Owner, ReasonSwitched); err != nil && !errors.Is(err, ErrWipePending) && !errors.Is(err, ErrAudit) {
				return Grant{}, err
			}
			continue
		}
		if !o.ObservedAt.After(g.ObservedAt) {
			return g, ErrStale
		}
		if o.Challenge != "" {
			if o.GrantEpoch != 0 && o.GrantEpoch < g.Epoch {
				return g, ErrStale
			}
			return br.Broker.Pause(g.ID, o.Owner, o.Challenge)
		}
		if g.State == StatePaused {
			return br.Broker.Resume(g.ID, o.Owner, o)
		}
		return br.refresh(g, o, expires)
	}
	k := ikey(o.Owner, o.Source)
	br.mu.Lock()
	in, ok := br.intents[k]
	delete(br.intents, k)
	br.mu.Unlock()
	if !ok || !now.Before(in.expires) {
		return Grant{}, ErrDenied
	}
	var scopes []string
	for _, s := range in.scopes {
		if slices.Contains(o.Scopes, s) {
			scopes = append(scopes, s)
		}
	}
	g, err := br.Broker.Issue(Grant{Account: in.account, Owner: o.Owner, Source: o.Source, Audience: o.Source.Origin, Scopes: scopes,
		Method: MethodSourceBrowser, PrincipalRef: o.PrincipalRef, IdentityVerified: true, RuntimeID: o.RuntimeID, SessionID: o.SessionID, ObservedAt: o.ObservedAt, ExpiresAt: expires})
	if err != nil {
		return Grant{}, err
	}
	br.Broker.note(EventSignedIn, g, o.Owner, ReasonOK)
	return g, nil
}

// refresh extends a live sign-in. A different runtime session is a new generation of the sign-in: the epoch rises and work from the old one is cancelled.
func (br *Browser) refresh(g Grant, o Observation, expires time.Time) (Grant, error) {
	next, changed, err := br.Broker.update(g.ID, func(x *Grant) error {
		if x.State == StatePaused || x.State == StateRevoked || !o.ObservedAt.After(x.ObservedAt) {
			return ErrStale
		}
		if x.RuntimeID != o.RuntimeID || x.SessionID != o.SessionID {
			x.Epoch++
			x.RuntimeID, x.SessionID = o.RuntimeID, o.SessionID
		}
		x.ObservedAt, x.ExpiresAt = o.ObservedAt, expires
		return nil
	})
	if err != nil {
		return Grant{}, err
	}
	if changed && next.Epoch > g.Epoch {
		br.Broker.cancelDependents(next, o.Owner, next.Epoch)
	}
	return next, nil
}

// Authenticated reports whether the source itself has a signed-in session for the caller. A grant made another way, such as OAuth, does not make it true.
func (br *Browser) Authenticated(owner string, src Source) bool {
	now := br.Broker.Now()
	for _, g := range br.active(owner, src) {
		if g.State == StateActive && now.Before(g.ExpiresAt) {
			return true
		}
	}
	return false
}

func cloneClaim(c *ProfileClaim) *ProfileClaim {
	if c == nil {
		return nil
	}
	return &ProfileClaim{Roles: slices.Clone(c.Roles), Capabilities: slices.Clone(c.Capabilities), Env: c.Env}
}
