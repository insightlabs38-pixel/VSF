//go:build integration

package versions

import (
	"encoding/json"
	"strings"
	"testing"
)

func artifactDigest(t *testing.T, r *rig, manifest, role string) string {
	t.Helper()
	var m struct {
		Body struct {
			Artifacts []struct{ Role, Digest string } `json:"artifacts"`
		} `json:"body"`
	}
	if err := json.Unmarshal(r.sample.docs[manifest], &m); err != nil {
		t.Fatal(err)
	}
	for _, a := range m.Body.Artifacts {
		if a.Role == role {
			return a.Digest
		}
	}
	t.Fatalf("no %s artifact in %s", role, manifest)
	return ""
}

func TestARevokedHeadCannotExecuteEvenThoughItIsPromotedAndPinned(t *testing.T) {
	r := newRig(t)
	r.promote("m1")
	if err := r.store.Executable("stable"); err != nil {
		t.Fatal(err)
	}
	if err := r.store.Revoke(Revocations{Artifacts: []string{artifactDigest(t, r, "m1", "operation_contract")}}); err != nil {
		t.Fatal(err)
	}
	if err := r.store.Executable("stable"); err == nil || !strings.HasPrefix(reason(t, err), "revoked:artifact") {
		t.Fatalf("a revoked artifact still executes: %v", err)
	}
	r.reopen()
	if err := r.store.Executable("stable"); err == nil {
		t.Fatal("a restart forgot the revocation")
	}
}

func TestARevokedArtifactBlocksEveryLaterManifestThatReusesIt(t *testing.T) {
	r := newRig(t)
	r.promote("m1")
	shared := artifactDigest(t, r, "m2", "semantic")
	if err := r.store.Revoke(Revocations{Artifacts: []string{shared}}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.store.Promote(r.req("m2")); err == nil || !strings.HasPrefix(reason(t, err), "revoked:artifact") {
		t.Fatalf("promotion reused a revoked artifact: %v", err)
	}
	if head, _ := r.store.Head("stable"); head != r.sample.ids["m1"] {
		t.Fatal("the head moved")
	}
}

func TestARevokedManifestCannotBePromotedAndRevocationsOnlyGrow(t *testing.T) {
	r := newRig(t)
	if err := r.store.Revoke(Revocations{Manifests: []string{r.sample.ids["m1"]}}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.store.Promote(r.req("m1")); err == nil || !strings.HasPrefix(reason(t, err), "revoked:manifest") {
		t.Fatalf("%v", err)
	}
	if err := r.store.Revoke(Revocations{Operations: []string{"app.records.update_stage@1.3.0"}}); err != nil {
		t.Fatal(err)
	}
	if _, bad := r.store.RevokedContract("sha256:none", "app.records.update_stage", "1.3.0"); !bad {
		t.Fatal("the operation version is not revoked")
	}
	if _, bad := r.store.RevokedContract("sha256:none", "app.records.update_stage", "1.2.0"); bad {
		t.Fatal("another version was revoked")
	}
	r.reopen()
	if _, err := r.store.Promote(r.req("m1")); err == nil {
		t.Fatal("a revoked manifest was promoted after restart")
	}
	if _, bad := r.store.RevokedContract("sha256:none", "app.records.update_stage", "1.3.0"); !bad {
		t.Fatal("an earlier revocation was dropped when another was added")
	}
}
