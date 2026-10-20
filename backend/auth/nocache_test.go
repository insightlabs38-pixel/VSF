package auth

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/insightlabs38-pixel/vsf/backend/storage/artifact"
	"github.com/insightlabs38-pixel/vsf/backend/storage/blob"
	"github.com/insightlabs38-pixel/vsf/backend/storage/meta"
	"github.com/insightlabs38-pixel/vsf/backend/storage/secret"
	"github.com/insightlabs38-pixel/vsf/backend/storage/tcache"
)

func TestNoCredentialEverReachesTheArtifactStoreItsCacheOrAnythingPersistedWithAGrant(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	m, err := meta.OpenSQLite(ctx, filepath.Join(dir, "meta.db"), meta.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Close() })
	mem := blob.NewMem()
	keys := secret.Generate()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	store, err := artifact.New(artifact.Config{Meta: m, Blobs: mem, Sealer: keys.Sealer(), Signer: &artifact.Signer{KeyID: "k1", Key: priv},
		Trust: artifact.Trust{Keys: map[string]ed25519.PublicKey{"k1": pub}, Revoked: map[string]bool{}}})
	if err != nil {
		t.Fatal(err)
	}
	cache := &tcache.Cache{Blobs: mem, Keys: keys, Quota: 1 << 20, TTL: time.Hour}

	r := newRig(t)
	r.b.Vault, _ = NewFileVault(filepath.Join(dir, "vault"), keys)
	var audit bytes.Buffer
	r.b.Audit = NewAudit(&audit)
	g, err := r.run(t, "alice")
	if err != nil {
		t.Fatal(err)
	}
	creds := &Credentials{Broker: r.b, Redact: NewRedactor()}
	l, err := creds.Lease(Check{GrantID: g.ID, Owner: "alice", Origin: g.Source.Origin, Audience: g.Audience, Scopes: []string{"mail.read"}}, "access", time.Minute, 1)
	if err != nil {
		t.Fatal(err)
	}
	var used string
	_ = l.Inject(g.Source.Origin, func(v []byte) error { used = string(v); return nil })
	if used == "" {
		t.Fatal("the lease handed out nothing")
	}

	// What a caller would naturally persist about a sign-in: the grant, a plan note, a profile and the audit record.
	ps := NewProfiles(r.b)
	ref := PlanRef{PlanID: "plan-1", Owner: "alice", Grant: g.ID, Epoch: g.Epoch, Digest: strings.Repeat("a", 64), Content: strings.Repeat("b", 64)}
	_ = ps
	gj, _ := json.Marshal(g)
	rj, _ := json.Marshal(ref)
	d, err := store.PutArtifact(ctx, "acme", append(gj, rj...), artifact.ArtifactInput{Class: meta.Private})
	if err != nil {
		t.Fatal(err)
	}
	if err := cache.Put(ctx, "acme", "sign-in", append(gj, audit.Bytes()...)); err != nil {
		t.Fatal(err)
	}
	if got, err := store.Artifact(ctx, "acme", d); err != nil || !bytes.Contains(got, []byte(g.ID)) {
		t.Fatalf("the grant is storable: %v", err)
	}
	var everything []byte
	objs, _ := mem.List(ctx, "", "", 0)
	for _, o := range objs {
		b, _ := mem.Get(ctx, o.Key)
		everything = append(everything, b...)
	}
	arts, _ := m.Artifacts(ctx, "acme", "", 0)
	for _, a := range arts {
		everything = append(everything, a.Provenance...)
	}
	plain := append(append(gj, rj...), audit.Bytes()...)
	for name, blobOfData := range map[string][]byte{"stored objects and records": everything, "grant, plan reference and audit": plain} {
		if strings.Contains(string(blobOfData), used) {
			t.Fatalf("the credential reached the %s", name)
		}
	}
}
