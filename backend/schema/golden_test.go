package schema

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/insightlabs38-pixel/vsf/backend/canon"
)

func TestGoldenFixturesAreCanonicalValidEnvelopes(t *testing.T) {
	dir := filepath.Join("..", "..", "fixtures", "golden")
	raw, err := os.ReadFile(filepath.Join(dir, "MANIFEST.json"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := canon.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	entries := doc.(*canon.Object).Fields["entries"].([]any)
	if len(entries) < 3 {
		t.Fatalf("want >=3 golden entries, got %d", len(entries))
	}
	for _, e := range entries {
		entry := e.(*canon.Object)
		file := entry.Fields["compiled_file"].(string)
		bytes, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil {
			t.Fatal(err)
		}
		if got := canon.SHA256Hex(bytes); got != entry.Fields["compiled_sha256"] {
			t.Errorf("%s: digest %s != manifest %v", file, got, entry.Fields["compiled_sha256"])
		}
		_, canonical, err := VerifyBytes(bytes)
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		if string(canonical) != string(bytes) {
			t.Errorf("%s: Go canonical form differs from Rust output", file)
		}
	}
}

func TestTamperedGoldenIsRejected(t *testing.T) {
	bytes, err := os.ReadFile(filepath.Join("..", "..", "fixtures", "golden", "compiled-seed-1.json"))
	if err != nil {
		t.Fatal(err)
	}
	tampered := append([]byte(nil), bytes...)
	tampered = append(tampered[:len(tampered)-1], []byte(`,"extra":1}`)...)
	if _, _, err := VerifyBytes(tampered); ErrorCode(err) != "schema.field_invalid" {
		t.Fatalf("got %v", err)
	}
}
