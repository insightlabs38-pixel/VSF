package canon

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func loadVectors(t *testing.T) []*Object {
	t.Helper()
	raw, err := os.ReadFile("../../schemas/smoke/canon-vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	items := doc.(*Object).Fields["cases"].([]any)
	out := make([]*Object, 0, len(items))
	for _, it := range items {
		out = append(out, it.(*Object))
	}
	return out
}

func TestSharedVectors(t *testing.T) {
	cases := loadVectors(t)
	if len(cases) < 20 {
		t.Fatalf("expected shared vectors, got %d", len(cases))
	}
	for _, c := range cases {
		id := c.Fields["id"].(string)
		input := c.Fields["input"].(string)
		if want, ok := c.Fields["canonical"].(string); ok {
			got, err := Canonicalize([]byte(input))
			if err != nil || string(got) != want {
				t.Errorf("%s: got %q err %v want %q", id, got, err, want)
			}
			continue
		}
		want := c.Fields["error"].(string)
		_, err := Parse([]byte(input))
		var ce *Error
		if !errors.As(err, &ce) || ce.Code != want {
			t.Errorf("%s: got %v want %s", id, err, want)
		}
	}
}

func TestInvalidUTF8AndSize(t *testing.T) {
	if _, err := Parse([]byte{'"', 0xff, '"'}); err == nil || err.Error() != "canon.invalid_utf8" {
		t.Fatalf("got %v", err)
	}
	if _, err := Parse([]byte(strings.Repeat(" ", MaxInputBytes+1))); err == nil || err.Error() != "canon.too_large" {
		t.Fatalf("got %v", err)
	}
}

func TestCanonicalIsIdempotent(t *testing.T) {
	once, err := Canonicalize([]byte(`{"k":"aé😀","a":[3,{"z":1,"y":2}]}`))
	if err != nil {
		t.Fatal(err)
	}
	twice, err := Canonicalize(once)
	if err != nil || string(once) != string(twice) {
		t.Fatalf("not idempotent: %q vs %q (%v)", once, twice, err)
	}
}

func TestSHA256KnownVector(t *testing.T) {
	if got := SHA256Hex([]byte("abc")); got != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Fatal(got)
	}
}
