// Package meta is the durable metadata of the artifact store: tenants, artifact records, signed manifests, version roots, pins and holds.
package meta

import (
	"context"
	"errors"
	"regexp"
)

var (
	ErrNotFound       = errors.New("not found")
	ErrConflict       = errors.New("the root moved; read it again")
	ErrInvalid        = errors.New("invalid argument")
	ErrMissingRef     = errors.New("a referenced artifact or manifest is not recorded for this tenant")
	ErrSchemaTooNew   = errors.New("the database was written by a newer version")
	ErrMigrationDrift = errors.New("an applied migration differs from the one shipped")
	ErrLeaseHeld      = errors.New("the lease is held by another owner")
)

// Tenant names whose data a call may touch; every method takes one and no method can cross it.
type Tenant string

var (
	tenantRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)
	digestRe = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	nameRe   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,63}$`)
)

func ValidTenant(t Tenant) bool { return tenantRe.MatchString(string(t)) }
func ValidDigest(d string) bool { return digestRe.MatchString(d) }

// Class decides whether an artifact may be shared between tenants.
type Class string

const (
	Public  Class = "public"
	Private Class = "private"
)

type ArtifactRecord struct {
	Digest     string
	Size       int64
	Class      Class
	Provenance string
	CreatedAt  int64
}

type ManifestRecord struct {
	ID        string
	Kind      string
	Doc       []byte
	KeyID     string
	ParentID  string
	CreatedAt int64
}

type RootRecord struct {
	Channel    string
	ManifestID string
	Seq        int64
	UpdatedAt  int64
}

type Pin struct {
	ManifestID  string
	Reason      string
	Entitlement string
	CreatedAt   int64
}

type Hold struct {
	ManifestID string
	Holder     string
	Reason     string
	CreatedAt  int64
}

type Lease struct {
	ID         string
	ManifestID string
	Owner      string
	ExpiresAt  int64
}

type IntentState string

const (
	IntentPending   IntentState = "pending"
	IntentDone      IntentState = "done"
	IntentAbandoned IntentState = "abandoned"
)

type Intent struct {
	ID        int64
	Kind      string
	Payload   string
	State     IntentState
	CreatedAt int64
}

// Store is the metadata contract; SQLite implements it locally and the PostgreSQL schema carries the same model for hosting.
type Store interface {
	Close() error
	SchemaVersion(ctx context.Context) (int, error)
	EnsureTenant(ctx context.Context, t Tenant) error
	Tenants(ctx context.Context) ([]Tenant, error)

	PutArtifact(ctx context.Context, t Tenant, a ArtifactRecord) (created bool, err error)
	Artifact(ctx context.Context, t Tenant, digest string) (ArtifactRecord, error)
	Artifacts(ctx context.Context, t Tenant, after string, limit int) ([]ArtifactRecord, error)
	DeleteArtifact(ctx context.Context, t Tenant, digest string) error

	// PublishManifest records a manifest and its artifact links in one transaction or not at all.
	PublishManifest(ctx context.Context, t Tenant, m ManifestRecord, artifacts []string) error
	Manifest(ctx context.Context, t Tenant, id string) (ManifestRecord, error)
	ManifestArtifacts(ctx context.Context, t Tenant, id string) ([]string, error)
	Manifests(ctx context.Context, t Tenant, after string, limit int) ([]ManifestRecord, error)
	DeleteManifest(ctx context.Context, t Tenant, id string) error

	// SetRoot moves a channel to a manifest only if it is still at expectSeq (0 for a new channel).
	SetRoot(ctx context.Context, t Tenant, channel, manifestID string, expectSeq int64) (RootRecord, error)
	Root(ctx context.Context, t Tenant, channel string) (RootRecord, error)
	Roots(ctx context.Context, t Tenant) ([]RootRecord, error)
	RootHistory(ctx context.Context, t Tenant, channel string) ([]RootRecord, error)
	// PruneRootHistory drops all but the newest keep positions of a channel, which frees older manifests to be collected.
	PruneRootHistory(ctx context.Context, t Tenant, channel string, keep int) (int, error)

	Pin(ctx context.Context, t Tenant, p Pin) error
	Unpin(ctx context.Context, t Tenant, manifestID, reason string) error
	Pins(ctx context.Context, t Tenant) ([]Pin, error)
	Hold(ctx context.Context, t Tenant, h Hold) error
	Release(ctx context.Context, t Tenant, manifestID, holder string) error
	Holds(ctx context.Context, t Tenant) ([]Hold, error)

	Annotate(ctx context.Context, t Tenant, manifestID, key, value string) error
	Annotations(ctx context.Context, t Tenant, manifestID string) (map[string]string, error)

	Acquire(ctx context.Context, t Tenant, l Lease) error
	Renew(ctx context.Context, t Tenant, id string, expiresAt int64) error
	ReleaseLease(ctx context.Context, t Tenant, id string) error
	Leases(ctx context.Context, t Tenant) ([]Lease, error)

	BeginIntent(ctx context.Context, t Tenant, kind, payload string) (int64, error)
	FinishIntent(ctx context.Context, t Tenant, id int64, state IntentState) error
	PendingIntents(ctx context.Context, t Tenant) ([]Intent, error)
}

func validName(s string) bool { return nameRe.MatchString(s) }

// Maintenance is the cross-tenant view a sweep needs; only maintenance code may hold it, never a request handler.
type Maintenance interface {
	// PublicDigestInUse reports whether any tenant still records a public artifact with this digest.
	PublicDigestInUse(ctx context.Context, digest string) (bool, error)
}
