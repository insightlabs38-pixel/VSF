//go:build integration

package bridge

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/insightlabs38-pixel/vsf/backend/execution"
)

func nativeValidator(t *testing.T) NativeValidator {
	t.Helper()
	p := os.Getenv("VSF_FABRIC")
	if p == "" {
		p = filepath.Join("..", "..", "artifacts", "bin", "vsf-fabric")
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("native worker not found at %s (run scripts/build-native.sh or set VSF_FABRIC): %v", p, err)
	}
	w, err := execution.StartWorker(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return NativeValidator{Worker: w}
}

func TestRustValidatorDecidesWhichManifestsBind(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_800_000_000, 0)}
	b := NewBindings(nativeValidator(t), clock.now)
	if _, err := b.Register("cli_a", srcOrigin, "doc-0001-aaaa", manifest(t, clock.now(), nil)); err != nil {
		t.Fatalf("a valid manifest must bind: %v", err)
	}
	invalid := map[string]func(map[string]any){
		"zero limit": func(m map[string]any) { m["limits"].(map[string]any)["max_nodes"] = 0 },
		"duplicate capability": func(m map[string]any) {
			m["observation"] = []any{map[string]any{"id": "dom.snapshot", "state": "available"}, map[string]any{"id": "dom.snapshot", "state": "unknown"}}
		},
		"extension with managed persistence": func(m map[string]any) { m["persistence"] = "managed" },
		"origin not canonical": func(m map[string]any) {
			m["permissions"] = []any{map[string]any{"origin": "http://127.0.0.1:04173", "scopes": []any{"page.observe"}}}
		},
		"variant without app": func(m map[string]any) { m["variant_id"] = "var_00112233445566778899aabbccddeeff" },
	}
	for name, edit := range invalid {
		if _, err := b.Register("cli_"+name[:3], srcOrigin, "doc-0002-bbbb", manifest(t, clock.now(), edit)); err == nil {
			t.Errorf("%s: the Rust validator's verdict must be final", name)
		}
	}
	if _, err := b.Register("cli_z", srcOrigin, "doc-0003-cccc", []byte(`{"schema":"vsf.environment/1.2.0"}`)); err == nil {
		t.Error("a document that does not decode must not bind")
	}
}
