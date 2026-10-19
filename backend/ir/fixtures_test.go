package ir

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/insightlabs38-pixel/vsf/backend/canon"
	"github.com/insightlabs38-pixel/vsf/backend/ir/rt"
)

const fixtureDir = "../../schemas/ir/fixtures"

func validNames(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(fixtureDir, "valid"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, strings.TrimSuffix(e.Name(), ".json"))
	}
	sort.Strings(names)
	return names
}

func validBytes(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(fixtureDir, "valid", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	rest := text[strings.Index(text, `"schema": "vsf.`)+len(`"schema": "vsf.`):]
	family := rest[:strings.Index(rest, "/")]
	verText := rest[strings.Index(rest, "/")+1 : strings.Index(rest, `"`)]
	ver, _ := rt.ParseVersion(verText)
	d, _ := DigestAt(family, ver)
	text = strings.Replace(text, "$DIGEST", d, 1)
	v, err := canon.Parse([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	return canon.ToCanonical(v)
}

func parseFile(t *testing.T, rel string) *canon.Object {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(fixtureDir, rel))
	if err != nil {
		t.Fatal(err)
	}
	v, err := canon.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return v.(*canon.Object)
}

func TestValidFixturesRoundTripToIdenticalBytes(t *testing.T) {
	names := validNames(t)
	if len(names) < 10 {
		t.Fatalf("expected >=10 fixtures, got %d", len(names))
	}
	for _, name := range names {
		b := validBytes(t, name)
		v, perr := canon.Parse(b)
		if perr != nil {
			t.Fatal(perr)
		}
		body, ver, err := DecodeDocumentValueVersioned(v)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		out, ok := EncodeDocumentAt(body, ver)
		if !ok || string(out) != string(b) {
			t.Errorf("%s: re-encoded bytes differ", name)
		}
		if _, err := DecodeDocument(EncodeDocument(body)); err != nil {
			t.Errorf("%s: re-encoding at the current version: %v", name, err)
		}
	}
}

func TestPinnedDigestsMatchTheRustAuthority(t *testing.T) {
	pins := parseFile(t, "pins.json").Fields["entries"].([]any)
	names := validNames(t)
	if len(pins) != len(names) {
		t.Fatalf("pins %d fixtures %d", len(pins), len(names))
	}
	for i, p := range pins {
		e := p.(*canon.Object).Fields
		name := e["name"].(string)
		if name != names[i] {
			t.Fatalf("order: %s vs %s", name, names[i])
		}
		b := validBytes(t, name)
		if int64(len(b)) != e["canonical_bytes"].(int64) || canon.SHA256Hex(b) != e["canonical_sha256"] {
			t.Errorf("%s: canonical bytes or sha256 differ", name)
		}
		pv, perr := canon.Parse(b)
		if perr != nil {
			t.Fatal(perr)
		}
		body, ver, err := DecodeDocumentValueVersioned(pv)
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := DocumentDigestAt(body, ver); got != e["document_digest"] {
			t.Errorf("%s: document digest differs", name)
		}
	}
}

func TestNFCAndNFDTextStayDistinct(t *testing.T) {
	body, err := DecodeDocument(validBytes(t, "semantic-orders"))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, e := range body.Semantic.Entities {
		if e.Label != nil {
			seen[string(*e.Label)] = true
		}
	}
	if !seen["Café"] || !seen["Café"] {
		t.Fatal("both normalization forms must be preserved")
	}
}

func pointerSet(t *testing.T, root any, path string, value any, remove bool) {
	t.Helper()
	segs := strings.Split(path, "/")[1:]
	last := segs[len(segs)-1]
	cur := root
	for _, seg := range segs[:len(segs)-1] {
		switch c := cur.(type) {
		case *canon.Object:
			cur = c.Fields[seg]
		case []any:
			n, _ := strconv.Atoi(seg)
			cur = c[n]
		default:
			t.Fatalf("bad path %s", path)
		}
	}
	switch c := cur.(type) {
	case *canon.Object:
		if remove {
			delete(c.Fields, last)
			keys := c.Keys[:0]
			for _, k := range c.Keys {
				if k != last {
					keys = append(keys, k)
				}
			}
			c.Keys = keys
		} else {
			c.Set(last, value)
		}
	case []any:
		n, _ := strconv.Atoi(last)
		if remove {
			copy(c[n:], c[n+1:])
			// shrink handled by caller replacing the parent slice is not needed for these fixtures
			t.Fatalf("array removal unsupported in fixtures")
		}
		c[n] = value
	default:
		t.Fatalf("bad path %s", path)
	}
}

func TestEveryInvalidCaseFailsClosedWithExpectedCodeAndPath(t *testing.T) {
	cases := parseFile(t, "invalid/cases.json").Fields["cases"].([]any)
	if len(cases) < 70 {
		t.Fatalf("only %d cases", len(cases))
	}
	for _, raw := range cases {
		c := raw.(*canon.Object).Fields
		name := c["name"].(string)
		want := c["expect"].(*canon.Object).Fields
		var input []byte
		if r, ok := c["raw"].(string); ok {
			input = []byte(r)
		} else {
			v, err := canon.Parse(validBytes(t, c["base"].(string)))
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range c["edits"].([]any) {
				ed := e.(*canon.Object).Fields
				pointerSet(t, v, ed["path"].(string), ed["value"], ed["op"] == "remove")
			}
			input = canon.ToCanonical(v)
		}
		_, err := DecodeDocument(input)
		var re *rt.Error
		if !errors.As(err, &re) {
			t.Errorf("%s: expected error, got %v", name, err)
			continue
		}
		if re.Code != want["code"] || re.Path != want["path"] {
			t.Errorf("%s: got %s at %q want %s at %q", name, re.Code, re.Path, want["code"], want["path"])
		}
	}
}

func TestTypedDigestsAreDomainSeparated(t *testing.T) {
	c := []byte("{}")
	if TypedDigest("document:operation", c) == TypedDigest("document:semantic", c) {
		t.Fatal("kinds must separate digests")
	}
	if TypedDigest("a", c) == "sha256:"+canon.SHA256Hex(c) {
		t.Fatal("typed digest must differ from the plain digest")
	}
}
