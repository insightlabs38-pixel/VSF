// Package gc removes what nothing needs any more: unprotected old versions, unreferenced artifacts and orphaned objects.
package gc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/insightlabs38-pixel/vsf/backend/storage/artifact"
	"github.com/insightlabs38-pixel/vsf/backend/storage/blob"
	"github.com/insightlabs38-pixel/vsf/backend/storage/meta"
)

var ErrCrashed = errors.New("simulated crash")

// Policy says what may go. Anything protected by a root, a pin, a hold or a lease is never touched, whatever the ages say.
type Policy struct {
	// KeepRootHistory is how many earlier positions of each channel stay live besides the current one.
	KeepRootHistory int
	// RetainFor is how long an unprotected version is kept after it was published; a plan's history retention lands here.
	RetainFor time.Duration
	// MinAge shields everything younger from every phase, so work in flight is never swept.
	MinAge time.Duration
	// OrphanGrace keeps objects without a record for at least this long, since an object is written before its record.
	OrphanGrace time.Duration
	DryRun      bool
}

func (p Policy) withDefaults() Policy {
	if p.MinAge == 0 {
		p.MinAge = time.Hour
	}
	if p.OrphanGrace == 0 {
		p.OrphanGrace = time.Hour
	}
	return p
}

// Reason is why a version stayed.
type Reason string

const (
	ByRoot    Reason = "root"
	ByHistory Reason = "root_history"
	ByPin     Reason = "pin"
	ByHold    Reason = "hold"
	ByLease   Reason = "lease"
	ByParent  Reason = "ancestor_of_live"
	ByAge     Reason = "younger_than_retention"
	ByMinAge  Reason = "younger_than_min_age"
)

// Report is what one collection did or, in a dry run, would do.
type Report struct {
	Tenant            meta.Tenant    `json:"tenant,omitempty"`
	Kept              map[Reason]int `json:"kept"`
	ManifestsDeleted  []string       `json:"manifests_deleted"`
	ArtifactsDeleted  int            `json:"artifacts_deleted"`
	ObjectsDeleted    int            `json:"objects_deleted"`
	OrphansDeleted    int            `json:"orphans_deleted"`
	BytesReclaimed    int64          `json:"bytes_reclaimed"`
	SharedObjectsKept int            `json:"shared_objects_kept"`
}

type Collector struct {
	Meta  meta.Store
	Maint meta.Maintenance
	Blobs blob.Backend
	// Layout must be an artifact.KeyParser to sweep orphans; the default layout is.
	Layout artifact.Layout
	// Exclusive keeps a publish from landing between deleting a record and its object; wire it to artifact.Store.Exclusive.
	Exclusive func(func() error) error
	Now       func() int64
	// CrashAt is for tests: returning true abandons the collection at that phase after done steps, as a dead process would.
	CrashAt func(phase string, done int) bool
}

const page = 500

func (c *Collector) now() int64 {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now().UnixMilli()
}

func (c *Collector) layout() artifact.Layout {
	if c.Layout != nil {
		return c.Layout
	}
	return artifact.DefaultLayout{}
}

func (c *Collector) exclusive(fn func() error) error {
	if c.Exclusive != nil {
		return c.Exclusive(fn)
	}
	return fn()
}

func (c *Collector) crash(phase string, done int) bool {
	return c.CrashAt != nil && c.CrashAt(phase, done)
}

func (c *Collector) manifests(ctx context.Context, t meta.Tenant) ([]meta.ManifestRecord, error) {
	var out []meta.ManifestRecord
	for after := ""; ; {
		ps, err := c.Meta.Manifests(ctx, t, after, page)
		if err != nil {
			return nil, err
		}
		out = append(out, ps...)
		if len(ps) < page {
			return out, nil
		}
		after = ps[len(ps)-1].ID
	}
}

func (c *Collector) artifacts(ctx context.Context, t meta.Tenant) ([]meta.ArtifactRecord, error) {
	var out []meta.ArtifactRecord
	for after := ""; ; {
		ps, err := c.Meta.Artifacts(ctx, t, after, page)
		if err != nil {
			return nil, err
		}
		out = append(out, ps...)
		if len(ps) < page {
			return out, nil
		}
		after = ps[len(ps)-1].Digest
	}
}

// live computes which manifests of a tenant are protected and why; the first reason found is the strongest.
func (c *Collector) live(ctx context.Context, t meta.Tenant, ms []meta.ManifestRecord, p Policy) (map[string]Reason, []meta.RootRecord, error) {
	by := make(map[string]meta.ManifestRecord, len(ms))
	for _, m := range ms {
		by[m.ID] = m
	}
	live := map[string]Reason{}
	mark := func(id string, r Reason) {
		if _, ok := by[id]; ok && live[id] == "" {
			live[id] = r
		}
	}
	roots, err := c.Meta.Roots(ctx, t)
	if err != nil {
		return nil, nil, err
	}
	for _, r := range roots {
		mark(r.ManifestID, ByRoot)
	}
	for _, r := range roots {
		hist, err := c.Meta.RootHistory(ctx, t, r.Channel)
		if err != nil {
			return nil, nil, err
		}
		for i := len(hist) - 1; i >= 0 && len(hist)-1-i <= p.KeepRootHistory; i-- {
			mark(hist[i].ManifestID, ByHistory)
		}
	}
	pins, err := c.Meta.Pins(ctx, t)
	if err != nil {
		return nil, nil, err
	}
	for _, x := range pins {
		mark(x.ManifestID, ByPin)
	}
	holds, err := c.Meta.Holds(ctx, t)
	if err != nil {
		return nil, nil, err
	}
	for _, x := range holds {
		mark(x.ManifestID, ByHold)
	}
	leases, err := c.Meta.Leases(ctx, t)
	if err != nil {
		return nil, nil, err
	}
	for _, x := range leases {
		mark(x.ManifestID, ByLease)
	}
	now := c.now()
	for _, m := range ms {
		switch {
		case now-m.CreatedAt < p.MinAge.Milliseconds():
			mark(m.ID, ByMinAge)
		case p.RetainFor > 0 && now-m.CreatedAt < p.RetainFor.Milliseconds():
			mark(m.ID, ByAge)
		}
	}
	for changed := true; changed; {
		changed = false
		for id := range live {
			if par := by[id].ParentID; par != "" && live[par] == "" {
				mark(par, ByParent)
				changed = live[par] != ""
			}
		}
	}
	return live, roots, nil
}

// Collect sweeps one tenant in repeatable steps, so a collection that dies half way is finished by running it again.
func (c *Collector) Collect(ctx context.Context, t meta.Tenant, p Policy) (Report, error) {
	p = p.withDefaults()
	rep := Report{Tenant: t, Kept: map[Reason]int{}, ManifestsDeleted: []string{}}
	ms, err := c.manifests(ctx, t)
	if err != nil {
		return rep, err
	}
	live, roots, err := c.live(ctx, t, ms, p)
	if err != nil {
		return rep, err
	}
	var garbage []meta.ManifestRecord
	for _, m := range ms {
		if r := live[m.ID]; r != "" {
			rep.Kept[r]++
		} else {
			garbage = append(garbage, m)
		}
	}
	var intent int64
	if !p.DryRun {
		payload, _ := json.Marshal(p)
		if intent, err = c.Meta.BeginIntent(ctx, t, "gc", string(payload)); err != nil {
			return rep, err
		}
		for _, r := range roots {
			if _, err := c.Meta.PruneRootHistory(ctx, t, r.Channel, p.KeepRootHistory+1); err != nil {
				return rep, err
			}
		}
	}
	removed, err := c.sweepManifests(ctx, t, garbage, p, &rep)
	if err != nil {
		return rep, err
	}
	linked := map[string]bool{}
	for _, m := range ms {
		if removed[m.ID] {
			continue
		}
		ds, err := c.Meta.ManifestArtifacts(ctx, t, m.ID)
		if err != nil {
			return rep, err
		}
		for _, d := range ds {
			linked[d] = true
		}
	}
	if err := c.sweepArtifacts(ctx, t, linked, p, &rep); err != nil {
		return rep, err
	}
	if !p.DryRun {
		if err := c.Meta.FinishIntent(ctx, t, intent, meta.IntentDone); err != nil {
			return rep, err
		}
	}
	return rep, nil
}

// sweepManifests deletes newest first, so a parent goes after its children; whatever the store refuses stays.
func (c *Collector) sweepManifests(ctx context.Context, t meta.Tenant, garbage []meta.ManifestRecord, p Policy, rep *Report) (map[string]bool, error) {
	sort.Slice(garbage, func(i, j int) bool {
		if garbage[i].CreatedAt != garbage[j].CreatedAt {
			return garbage[i].CreatedAt > garbage[j].CreatedAt
		}
		return garbage[i].ID < garbage[j].ID
	})
	removed := map[string]bool{}
	for progressed := true; progressed && len(garbage) > 0; {
		progressed = false
		var rest []meta.ManifestRecord
		for _, m := range garbage {
			if c.crash("manifests", len(removed)) {
				return removed, ErrCrashed
			}
			if p.DryRun {
				removed[m.ID] = true
				rep.ManifestsDeleted = append(rep.ManifestsDeleted, m.ID)
				progressed = true
				continue
			}
			switch err := c.Meta.DeleteManifest(ctx, t, m.ID); {
			case err == nil, errors.Is(err, meta.ErrNotFound):
				removed[m.ID] = true
				rep.ManifestsDeleted = append(rep.ManifestsDeleted, m.ID)
				progressed = true
			case errors.Is(err, meta.ErrConflict):
				rest = append(rest, m)
			default:
				return removed, err
			}
		}
		garbage = rest
	}
	rep.Kept[ByParent] += len(garbage)
	return removed, nil
}

func (c *Collector) sweepArtifacts(ctx context.Context, t meta.Tenant, linked map[string]bool, p Policy, rep *Report) error {
	recs, err := c.artifacts(ctx, t)
	if err != nil {
		return err
	}
	now := c.now()
	done := 0
	for _, a := range recs {
		if linked[a.Digest] || now-a.CreatedAt < p.MinAge.Milliseconds() {
			continue
		}
		if c.crash("artifacts", done) {
			return ErrCrashed
		}
		if p.DryRun {
			rep.ArtifactsDeleted++
			rep.BytesReclaimed += a.Size
			continue
		}
		err := c.exclusive(func() error {
			if err := c.Meta.DeleteArtifact(ctx, t, a.Digest); err != nil {
				return err
			}
			rep.ArtifactsDeleted++
			if c.crash("objects", done) {
				return ErrCrashed
			}
			return c.removeObject(ctx, t, a, rep)
		})
		switch {
		case err == nil:
			done++
		case errors.Is(err, meta.ErrConflict), errors.Is(err, meta.ErrNotFound):
		default:
			return err
		}
	}
	return nil
}

func (c *Collector) removeObject(ctx context.Context, t meta.Tenant, a meta.ArtifactRecord, rep *Report) error {
	if a.Class == meta.Public {
		if c.Maint == nil {
			return errors.New("a public object can only be removed with the cross-tenant view")
		}
		used, err := c.Maint.PublicDigestInUse(ctx, a.Digest)
		if err != nil {
			return err
		}
		if used {
			rep.SharedObjectsKept++
			return nil
		}
	}
	switch err := c.Blobs.Delete(ctx, c.layout().Key(t, a.Class, a.Digest)); {
	case err == nil:
		rep.ObjectsDeleted++
		rep.BytesReclaimed += a.Size
	case !errors.Is(err, blob.ErrNotFound):
		return err
	}
	return nil
}

// claimed reports whether a record anywhere still names the object; a miss on an unknown tenant is a miss.
func (c *Collector) claimed(ctx context.Context, t meta.Tenant, class meta.Class, digest string) (bool, error) {
	if class == meta.Public {
		if c.Maint == nil {
			return true, errors.New("the cross-tenant view is required")
		}
		return c.Maint.PublicDigestInUse(ctx, digest)
	}
	if _, err := c.Meta.Artifact(ctx, t, digest); err != nil {
		if errors.Is(err, meta.ErrNotFound) {
			return false, nil
		}
		return true, err
	}
	return true, nil
}

// SweepOrphans removes unclaimed objects of the store's own layout past the grace and leaves foreign keys alone.
func (c *Collector) SweepOrphans(ctx context.Context, p Policy) (Report, error) {
	p = p.withDefaults()
	rep := Report{Kept: map[Reason]int{}, ManifestsDeleted: []string{}}
	parser, ok := c.layout().(artifact.KeyParser)
	if !ok {
		return rep, errors.New("the layout cannot tell its own objects from foreign ones")
	}
	now := c.now()
	done := 0
	for after := ""; ; {
		objs, err := c.Blobs.List(ctx, "", after, 1000)
		if err != nil {
			return rep, err
		}
		for _, o := range objs {
			after = o.Key
			t, class, digest, ours := parser.ParseKey(o.Key)
			if !ours || now-o.MTime < p.OrphanGrace.Milliseconds() {
				continue
			}
			held, err := c.claimed(ctx, t, class, digest)
			if err != nil {
				return rep, err
			}
			if held {
				continue
			}
			if c.crash("orphans", done) {
				return rep, ErrCrashed
			}
			if p.DryRun {
				rep.OrphansDeleted++
				rep.BytesReclaimed += o.Size
				continue
			}
			err = c.exclusive(func() error {
				if held, err := c.claimed(ctx, t, class, digest); err != nil || held {
					return err
				}
				switch err := c.Blobs.Delete(ctx, o.Key); {
				case err == nil:
					rep.OrphansDeleted++
					rep.BytesReclaimed += o.Size
				case !errors.Is(err, blob.ErrNotFound):
					return err
				}
				return nil
			})
			if err != nil {
				return rep, err
			}
			done++
		}
		if len(objs) < 1000 {
			return rep, nil
		}
	}
}

// Recover finishes collections a dead process left pending by running them again with the policy they recorded.
func (c *Collector) Recover(ctx context.Context, t meta.Tenant) (int, error) {
	pending, err := c.Meta.PendingIntents(ctx, t)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, in := range pending {
		if in.Kind != "gc" {
			continue
		}
		var p Policy
		if err := json.Unmarshal([]byte(in.Payload), &p); err != nil {
			return n, fmt.Errorf("gc intent %d: %w", in.ID, err)
		}
		if _, err := c.Collect(ctx, t, p); err != nil {
			return n, err
		}
		if err := c.Meta.FinishIntent(ctx, t, in.ID, meta.IntentDone); err != nil && !errors.Is(err, meta.ErrNotFound) {
			return n, err
		}
		n++
	}
	return n, nil
}
