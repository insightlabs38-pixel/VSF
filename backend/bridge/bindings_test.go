package bridge

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/insightlabs38-pixel/vsf/backend/ir"
)

const (
	srcOrigin  = "http://127.0.0.1:4173"
	principalA = "prin_b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2"
	principalB = "prin_c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3"
)

// decodeValidator accepts what the Go decoder can read; the real validator is exercised in the integration test.
type decodeValidator struct{ valid bool }

func (d decodeValidator) Validate(doc []byte) (Validation, error) {
	u, err := ir.DecodeDocument(doc)
	if err != nil {
		return Validation{}, nil
	}
	return Validation{Valid: d.valid, Family: u.Unsafe().Family, Digest: u.Digest()}, nil
}

func manifest(t *testing.T, at time.Time, edit func(body map[string]any)) []byte {
	t.Helper()
	digest, ok := ir.DigestAt("environment", [3]uint32{1, 2, 0})
	if !ok {
		t.Fatal("no digest for environment 1.2.0")
	}
	body := map[string]any{
		"runtime_id": "rt_ext01", "mode": "extension", "runtime_version": "0.0.1", "compiler_version": "0.1.0+c3c3c3c3c3c3",
		"generated_at": at.UnixMilli(), "principal_ref": principalA,
		"permissions": []any{map[string]any{"origin": srcOrigin, "scopes": []any{"page.observe"}}},
		"observation": []any{map[string]any{"id": "dom.snapshot", "state": "available"}},
		"execution":   []any{map[string]any{"id": "ui.present", "state": "available"}},
		"persistence": "local", "credential_isolation": "browser_profile_bound", "egress": "allowlist",
		"limits": map[string]any{"max_payload_bytes": 16777216, "max_nodes": 100000, "max_concurrency": 1, "max_wallclock_ms": 30000},
	}
	if edit != nil {
		edit(body)
	}
	raw, err := json.Marshal(map[string]any{"schema": "vsf.environment/1.2.0", "schema_digest": digest, "body": body})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func newBindings(valid bool) (*Bindings, *fakeClock) {
	clock := &fakeClock{t: time.Unix(1_800_000_000, 0)}
	return NewBindings(decodeValidator{valid: valid}, clock.now), clock
}

func TestManifestBindsRuntimeToOriginDocumentAndPrincipal(t *testing.T) {
	b, clock := newBindings(true)
	bd, err := b.Register("cli_a", srcOrigin, "doc-0001-aaaa", manifest(t, clock.now(), nil))
	if err != nil {
		t.Fatal(err)
	}
	if bd.Principal != principalA || bd.Runtime != "rt_ext01" || bd.Origin != srcOrigin || len(bd.Scopes) != 1 || !strings.HasPrefix(bd.ID, "bs_") {
		t.Fatalf("binding: %+v", bd)
	}
	if _, err := b.Check(bd.ID, "cli_a", srcOrigin, "doc-0001-aaaa"); err != nil {
		t.Fatalf("the owner at the bound origin and document: %v", err)
	}
	for _, c := range []struct{ client, origin, doc string }{
		{"cli_b", srcOrigin, "doc-0001-aaaa"},
		{"cli_a", "http://127.0.0.1:4174", "doc-0001-aaaa"},
		{"cli_a", srcOrigin, "doc-0002-bbbb"},
	} {
		if _, err := b.Check(bd.ID, c.client, c.origin, c.doc); err == nil {
			t.Errorf("%+v must not pass", c)
		}
	}
}

func TestRegistrationRefusesWhatPairingAloneDoesNotJustify(t *testing.T) {
	b, clock := newBindings(true)
	now := clock.now()
	cases := []struct {
		name     string
		client   string
		origin   string
		document string
		edit     func(map[string]any)
		at       time.Time
	}{
		{"no client", "", srcOrigin, "doc-0001-aaaa", nil, now},
		{"origin the manifest grants nothing for", "c", "http://127.0.0.1:9999", "doc-0001-aaaa", nil, now},
		{"origin not canonical", "c", "http://EXAMPLE.test", "doc-0001-aaaa", nil, now},
		{"bad document id", "c", srcOrigin, "x", nil, now},
		{"not an extension", "c", srcOrigin, "doc-0001-aaaa", func(m map[string]any) { m["mode"] = "desktop" }, now},
		{"grant without scopes", "c", srcOrigin, "doc-0001-aaaa", func(m map[string]any) {
			m["permissions"] = []any{map[string]any{"origin": srcOrigin, "scopes": []any{}}}
		}, now},
		{"stale manifest", "c", srcOrigin, "doc-0001-aaaa", nil, now.Add(-manifestSkew - time.Minute)},
		{"manifest from the future", "c", srcOrigin, "doc-0001-aaaa", nil, now.Add(manifestSkew + time.Minute)},
	}
	for _, c := range cases {
		if _, err := b.Register(c.client, c.origin, c.document, manifest(t, c.at, c.edit)); err == nil {
			t.Errorf("%s must be refused", c.name)
		}
	}
	if _, err := b.Register("c", srcOrigin, "doc-0001-aaaa", []byte(`{"not":"a document"}`)); err == nil {
		t.Error("garbage must be refused")
	}
	bad, _ := newBindings(false)
	if _, err := bad.Register("c", srcOrigin, "doc-0001-aaaa", manifest(t, now, nil)); err == nil {
		t.Error("a manifest the validator rejects must be refused")
	}
}

func TestOneClientKeepsOnePrincipalAndRuntime(t *testing.T) {
	b, clock := newBindings(true)
	if _, err := b.Register("cli_a", srcOrigin, "doc-0001-aaaa", manifest(t, clock.now(), nil)); err != nil {
		t.Fatal(err)
	}
	other := manifest(t, clock.now(), func(m map[string]any) { m["principal_ref"] = principalB })
	if _, err := b.Register("cli_a", srcOrigin, "doc-0002-bbbb", other); err == nil {
		t.Fatal("a paired client cannot start acting for another principal")
	}
	if _, err := b.Register("cli_b", srcOrigin, "doc-0003-cccc", other); err != nil {
		t.Fatalf("another client may have its own: %v", err)
	}
}

func TestRegistrationIsIdempotentRenewsExpiresAndEnds(t *testing.T) {
	b, clock := newBindings(true)
	m := manifest(t, clock.now(), nil)
	first, err := b.Register("cli_a", srcOrigin, "doc-0001-aaaa", m)
	if err != nil {
		t.Fatal(err)
	}
	same, err := b.Register("cli_a", srcOrigin, "doc-0001-aaaa", m)
	if err != nil || same.ID != first.ID {
		t.Fatalf("the same manifest for the same document is the same binding: %v", err)
	}
	clock.advance(DefaultBindingTTL / 2)
	renewed, err := b.Renew(first.ID, "cli_a")
	if err != nil || renewed.ExpiresAt <= first.ExpiresAt {
		t.Fatalf("renew: %v %d %d", err, renewed.ExpiresAt, first.ExpiresAt)
	}
	if _, err := b.Renew(first.ID, "cli_b"); err != ErrBindingMissing {
		t.Fatalf("only the owner renews: %v", err)
	}
	next, err := b.Register("cli_a", srcOrigin, "doc-0001-aaaa", manifest(t, clock.now(), nil))
	if err != nil || next.ID == first.ID {
		t.Fatalf("a different manifest for the same document replaces the binding: %v", err)
	}
	if _, err := b.Check(first.ID, "cli_a", srcOrigin, "doc-0001-aaaa"); err == nil {
		t.Fatal("the replaced binding must be gone")
	}
	clock.advance(DefaultBindingTTL + time.Second)
	if _, err := b.Check(next.ID, "cli_a", srcOrigin, "doc-0001-aaaa"); err != ErrBindingStale {
		t.Fatalf("expired: %v", err)
	}
	fresh, _ := b.Register("cli_a", srcOrigin, "doc-0009-zzzz", manifest(t, clock.now(), nil))
	for i := 0; i < int(maxBindingAge/(DefaultBindingTTL/2))+2; i++ {
		clock.advance(DefaultBindingTTL / 2)
		if _, err := b.Renew(fresh.ID, "cli_a"); err != nil {
			if err != ErrBindingStale {
				t.Fatalf("renew until the age limit: %v", err)
			}
			break
		}
		if i > 100 {
			t.Fatal("a binding must not be renewable forever")
		}
	}
	again, _ := b.Register("cli_a", srcOrigin, "doc-0010-yyyy", manifest(t, clock.now(), nil))
	if b.End(again.ID, "cli_b") {
		t.Fatal("only the owner can end a binding")
	}
	if !b.End(again.ID, "cli_a") {
		t.Fatal("owner ends")
	}
	if _, err := b.Get(again.ID, "cli_a"); err != ErrBindingMissing {
		t.Fatalf("ended: %v", err)
	}
}

func TestRevokingTheClientEndsEveryBinding(t *testing.T) {
	b, clock := newBindings(true)
	var ids []string
	for _, d := range []string{"doc-0001-aaaa", "doc-0002-bbbb"} {
		bd, err := b.Register("cli_a", srcOrigin, d, manifest(t, clock.now(), nil))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, bd.ID)
	}
	b.RevokeClient("cli_a")
	for _, id := range ids {
		if _, err := b.Get(id, "cli_a"); err == nil {
			t.Fatal("binding survived revoke")
		}
	}
	if _, err := b.Register("cli_a", srcOrigin, "doc-0003-cccc", manifest(t, clock.now(), func(m map[string]any) { m["principal_ref"] = principalB })); err != nil {
		t.Fatalf("a client that starts over may bind a new principal: %v", err)
	}
}

func TestBindingCapPerClient(t *testing.T) {
	b, clock := newBindings(true)
	for i := 0; i < maxBindings; i++ {
		if _, err := b.Register("cli_a", srcOrigin, "doc-"+strings.Repeat(string(rune('a'+i)), 8), manifest(t, clock.now(), nil)); err != nil {
			t.Fatalf("binding %d: %v", i, err)
		}
	}
	if _, err := b.Register("cli_a", srcOrigin, "doc-overflow-1", manifest(t, clock.now(), nil)); err == nil {
		t.Fatal("the cap must hold")
	}
}
