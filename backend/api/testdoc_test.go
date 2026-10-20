package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/insightlabs38-pixel/vsf/backend/canon"
	"github.com/insightlabs38-pixel/vsf/backend/ir"
	"github.com/insightlabs38-pixel/vsf/backend/ir/rt"
)

const (
	testApp       = "vendor-console:app.vendor.test"
	testOperation = "app.records.update_stage"
)

// contractDoc loads the witnessed update_stage contract, optionally edited, as a canonical document.
func contractDoc(t testing.TB, edit func(body map[string]any)) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "schemas", "ir", "fixtures", "valid", "operation-witnessed-1-1.json"))
	if err != nil {
		t.Fatal(err)
	}
	digest, ok := ir.DigestAt("operation", rt.Version{1, 1, 0})
	if !ok {
		t.Fatal("no digest for operation 1.1.0")
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(strings.ReplaceAll(string(raw), "$DIGEST", digest)), &doc); err != nil {
		t.Fatal(err)
	}
	if edit != nil {
		edit(doc["body"].(map[string]any))
	}
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	v, perr := canon.Parse(out)
	if perr != nil {
		t.Fatal(perr)
	}
	return canon.ToCanonical(v)
}

func specOfDoc(t testing.TB, doc []byte) *Spec {
	t.Helper()
	s, err := BuildSpec(doc)
	if err != nil {
		t.Fatal(err)
	}
	return &s
}
