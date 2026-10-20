// Package artifact stores immutable, content-addressed artifacts and the signed manifests that name them.
package artifact

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/insightlabs38-pixel/vsf/backend/canon"
	"github.com/insightlabs38-pixel/vsf/backend/storage/blob"
	"github.com/insightlabs38-pixel/vsf/backend/storage/meta"
)

var (
	ErrNotFound        = errors.New("not found")
	ErrCorrupt         = errors.New("the stored bytes do not match their digest")
	ErrMissingObject   = errors.New("the artifact is recorded but its object is gone")
	ErrMissingArtifact = errors.New("a manifest names an artifact that is not stored for this tenant")
	ErrBadSignature    = errors.New("the manifest signature does not verify")
	ErrUntrustedKey    = errors.New("the manifest is signed by a key that is not trusted")
	ErrRevokedKey      = errors.New("the manifest is signed by a revoked key")
	ErrWrongTenant     = errors.New("the manifest belongs to another tenant")
	ErrInvalid         = errors.New("invalid argument")
	ErrCrashed         = blob.ErrCrashed
)

const (
	ManifestSchema = "vsf.storage.manifest/1"
	maxProvenance  = 4096
	maxArtifacts   = 4096
)

// Signer holds the private half of a manifest signing key.
type Signer struct {
	KeyID string
	Key   ed25519.PrivateKey
}

// Trust is the set of public keys manifests may be signed by, and the keys withdrawn since.
type Trust struct {
	Keys    map[string]ed25519.PublicKey
	Revoked map[string]bool
}

// Layout maps an artifact to the key of its object; the default shares public objects and separates private ones.
type Layout interface {
	Key(t meta.Tenant, class meta.Class, digest string) string
}

type DefaultLayout struct{}

func (DefaultLayout) Key(t meta.Tenant, class meta.Class, digest string) string {
	hexa := digest[len("sha256:"):]
	if class == meta.Public {
		return "pub/" + hexa[:2] + "/" + hexa
	}
	return "ten/" + string(t) + "/" + hexa[:2] + "/" + hexa
}

type Config struct {
	Meta   meta.Store
	Blobs  blob.Backend
	Signer *Signer
	Trust  Trust
	Layout Layout
	Now    func() int64
	// MaxArtifact bounds one artifact; the default is the backend's object limit.
	MaxArtifact int
	// SkipVerifyOnPublish trades the read-back of every artifact at publish time for speed; the default checks them.
	SkipVerifyOnPublish bool
	// CrashAt is for tests: returning true at a named point abandons the operation as a dead process would.
	CrashAt func(point string) bool
}

type Store struct{ cfg Config }

func New(cfg Config) (*Store, error) {
	if cfg.Meta == nil || cfg.Blobs == nil {
		return nil, errors.New("metadata and blob backends are required")
	}
	if cfg.Layout == nil {
		cfg.Layout = DefaultLayout{}
	}
	if cfg.Now == nil {
		cfg.Now = func() int64 { return time.Now().UnixMilli() }
	}
	if cfg.MaxArtifact <= 0 || cfg.MaxArtifact > blob.MaxObject {
		cfg.MaxArtifact = blob.MaxObject
	}
	return &Store{cfg: cfg}, nil
}

func (s *Store) crash(point string) bool { return s.cfg.CrashAt != nil && s.cfg.CrashAt(point) }

// Digest is the identity of some bytes.
func Digest(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

type ArtifactInput struct {
	Class      meta.Class
	Provenance json.RawMessage
}

func validProvenance(p json.RawMessage) (string, error) {
	if len(p) == 0 {
		return "{}", nil
	}
	if len(p) > maxProvenance {
		return "", fmt.Errorf("%w: provenance too large", ErrInvalid)
	}
	var obj map[string]any
	if err := json.Unmarshal(p, &obj); err != nil || obj == nil {
		return "", fmt.Errorf("%w: provenance must be a json object", ErrInvalid)
	}
	return string(p), nil
}

// PutArtifact stores bytes and records them for a tenant; the object is written before the record, so a record never lacks its object.
func (s *Store) PutArtifact(ctx context.Context, t meta.Tenant, data []byte, in ArtifactInput) (string, error) {
	if !meta.ValidTenant(t) || (in.Class != meta.Public && in.Class != meta.Private) {
		return "", fmt.Errorf("%w: tenant or class", ErrInvalid)
	}
	if len(data) > s.cfg.MaxArtifact {
		return "", blob.ErrTooLarge
	}
	prov, err := validProvenance(in.Provenance)
	if err != nil {
		return "", err
	}
	digest := Digest(data)
	if err := s.cfg.Meta.EnsureTenant(ctx, t); err != nil {
		return "", err
	}
	if s.crash("before-object") {
		return "", ErrCrashed
	}
	if err := s.cfg.Blobs.Put(ctx, s.cfg.Layout.Key(t, in.Class, digest), data); err != nil {
		if errors.Is(err, blob.ErrOverwrite) {
			return "", ErrCorrupt
		}
		return "", err
	}
	if s.crash("after-object") {
		return "", ErrCrashed
	}
	if _, err := s.cfg.Meta.PutArtifact(ctx, t, meta.ArtifactRecord{Digest: digest, Size: int64(len(data)), Class: in.Class, Provenance: prov}); err != nil {
		return "", err
	}
	return digest, nil
}

// Artifact returns an artifact's bytes after proving they hash to the digest asked for.
func (s *Store) Artifact(ctx context.Context, t meta.Tenant, digest string) ([]byte, error) {
	rec, err := s.cfg.Meta.Artifact(ctx, t, digest)
	if err != nil {
		if errors.Is(err, meta.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	b, err := s.cfg.Blobs.Get(ctx, s.cfg.Layout.Key(t, rec.Class, digest))
	if err != nil {
		if errors.Is(err, blob.ErrNotFound) {
			return nil, ErrMissingObject
		}
		return nil, err
	}
	if Digest(b) != digest || int64(len(b)) != rec.Size {
		return nil, ErrCorrupt
	}
	return b, nil
}

// ArtifactRef names one artifact in a manifest and the role it plays.
type ArtifactRef struct {
	Role   string     `json:"role"`
	Digest string     `json:"digest"`
	Size   int64      `json:"size"`
	Class  meta.Class `json:"class"`
}

type ManifestInput struct {
	Kind       string
	Version    string
	Parent     string
	Artifacts  []ArtifactRef
	Provenance json.RawMessage
}

type manifestDoc struct {
	Schema         string          `json:"schema"`
	Tenant         string          `json:"tenant"`
	Kind           string          `json:"kind"`
	Version        string          `json:"version"`
	Parent         string          `json:"parent,omitempty"`
	Classification meta.Class      `json:"classification"`
	Artifacts      []ArtifactRef   `json:"artifacts"`
	Provenance     json.RawMessage `json:"provenance"`
	CreatedAt      int64           `json:"created_at"`
	KeyID          string          `json:"key_id"`
}

type envelope struct {
	Doc json.RawMessage `json:"doc"`
	Sig string          `json:"sig"`
}

// Manifest is a verified manifest: its signature, tenant and identity have been checked.
type Manifest struct {
	ID             string
	Kind           string
	Version        string
	Parent         string
	Classification meta.Class
	Artifacts      []ArtifactRef
	Provenance     json.RawMessage
	CreatedAt      int64
	KeyID          string
}

// PublishManifest signs and records a manifest once every artifact it names is stored and intact; it becomes visible all at once.
func (s *Store) PublishManifest(ctx context.Context, t meta.Tenant, in ManifestInput) (string, error) {
	if s.cfg.Signer == nil || s.cfg.Signer.KeyID == "" || len(s.cfg.Signer.Key) != ed25519.PrivateKeySize {
		return "", errors.New("a signing key is required")
	}
	if !meta.ValidTenant(t) || in.Kind == "" || len(in.Kind) > 64 || len(in.Version) > 64 || len(in.Artifacts) == 0 || len(in.Artifacts) > maxArtifacts {
		return "", fmt.Errorf("%w: manifest", ErrInvalid)
	}
	if in.Parent != "" && !meta.ValidDigest(in.Parent) {
		return "", fmt.Errorf("%w: parent", ErrInvalid)
	}
	prov, err := validProvenance(in.Provenance)
	if err != nil {
		return "", err
	}
	class := meta.Public
	seen := map[string]bool{}
	refs := make([]ArtifactRef, 0, len(in.Artifacts))
	for _, a := range in.Artifacts {
		if !meta.ValidDigest(a.Digest) || a.Role == "" || len(a.Role) > 64 || seen[a.Role] {
			return "", fmt.Errorf("%w: artifact reference", ErrInvalid)
		}
		seen[a.Role] = true
		rec, err := s.cfg.Meta.Artifact(ctx, t, a.Digest)
		if err != nil {
			if errors.Is(err, meta.ErrNotFound) {
				return "", fmt.Errorf("%w: %s", ErrMissingArtifact, a.Digest)
			}
			return "", err
		}
		if s.cfg.SkipVerifyOnPublish {
			if info, err := s.cfg.Blobs.Stat(ctx, s.cfg.Layout.Key(t, rec.Class, a.Digest)); err != nil || info.Size != rec.Size {
				return "", fmt.Errorf("%w: %s", ErrMissingObject, a.Digest)
			}
		} else if _, err := s.Artifact(ctx, t, a.Digest); err != nil {
			return "", fmt.Errorf("%s: %w", a.Digest, err)
		}
		if rec.Class == meta.Private {
			class = meta.Private
		}
		refs = append(refs, ArtifactRef{Role: a.Role, Digest: a.Digest, Size: rec.Size, Class: rec.Class})
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].Role < refs[j].Role })
	doc := manifestDoc{Schema: ManifestSchema, Tenant: string(t), Kind: in.Kind, Version: in.Version, Parent: in.Parent, Classification: class,
		Artifacts: refs, Provenance: json.RawMessage(prov), CreatedAt: s.cfg.Now(), KeyID: s.cfg.Signer.KeyID}
	raw, err := json.Marshal(doc)
	if err != nil {
		return "", err
	}
	canonDoc, err := canon.Canonicalize(raw)
	if err != nil {
		return "", err
	}
	sig := ed25519.Sign(s.cfg.Signer.Key, canonDoc)
	env, err := json.Marshal(envelope{Doc: canonDoc, Sig: base64.StdEncoding.EncodeToString(sig)})
	if err != nil {
		return "", err
	}
	stored, err := canon.Canonicalize(env)
	if err != nil {
		return "", err
	}
	id := Digest(stored)
	if s.crash("before-manifest") {
		return "", ErrCrashed
	}
	links := make([]string, len(refs))
	for i, r := range refs {
		links[i] = r.Digest
	}
	if err := s.cfg.Meta.PublishManifest(ctx, t, meta.ManifestRecord{ID: id, Kind: in.Kind, Doc: stored, KeyID: doc.KeyID, ParentID: in.Parent}, links); err != nil {
		return "", err
	}
	if s.crash("after-manifest") {
		return "", ErrCrashed
	}
	return id, nil
}

// Manifest loads a manifest and refuses it unless its identity, signature, key and tenant all check out.
func (s *Store) Manifest(ctx context.Context, t meta.Tenant, id string) (Manifest, error) {
	rec, err := s.cfg.Meta.Manifest(ctx, t, id)
	if err != nil {
		if errors.Is(err, meta.ErrNotFound) {
			return Manifest{}, ErrNotFound
		}
		return Manifest{}, err
	}
	return s.verifyManifest(t, rec)
}

func (s *Store) verifyManifest(t meta.Tenant, rec meta.ManifestRecord) (Manifest, error) {
	if Digest(rec.Doc) != rec.ID {
		return Manifest{}, ErrCorrupt
	}
	var env envelope
	if err := json.Unmarshal(rec.Doc, &env); err != nil {
		return Manifest{}, ErrCorrupt
	}
	var doc manifestDoc
	if err := json.Unmarshal(env.Doc, &doc); err != nil || doc.Schema != ManifestSchema {
		return Manifest{}, ErrCorrupt
	}
	if s.cfg.Trust.Revoked[doc.KeyID] {
		return Manifest{}, ErrRevokedKey
	}
	pub, ok := s.cfg.Trust.Keys[doc.KeyID]
	if !ok || len(pub) != ed25519.PublicKeySize {
		return Manifest{}, ErrUntrustedKey
	}
	sig, err := base64.StdEncoding.DecodeString(env.Sig)
	canonDoc, cerr := canon.Canonicalize(env.Doc)
	if err != nil || cerr != nil || !bytes.Equal(canonDoc, env.Doc) || !ed25519.Verify(pub, canonDoc, sig) {
		return Manifest{}, ErrBadSignature
	}
	if doc.Tenant != string(t) {
		return Manifest{}, ErrWrongTenant
	}
	if doc.KeyID != rec.KeyID {
		return Manifest{}, ErrCorrupt
	}
	return Manifest{ID: rec.ID, Kind: doc.Kind, Version: doc.Version, Parent: doc.Parent, Classification: doc.Classification,
		Artifacts: doc.Artifacts, Provenance: doc.Provenance, CreatedAt: doc.CreatedAt, KeyID: doc.KeyID}, nil
}

// VerifyManifest checks the manifest and then every artifact it names, byte for byte.
func (s *Store) VerifyManifest(ctx context.Context, t meta.Tenant, id string) error {
	m, err := s.Manifest(ctx, t, id)
	if err != nil {
		return err
	}
	for _, a := range m.Artifacts {
		if _, err := s.Artifact(ctx, t, a.Digest); err != nil {
			return fmt.Errorf("%s: %w", a.Digest, err)
		}
	}
	return nil
}
