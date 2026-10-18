package schema

import (
	"fmt"
	"testing"
)

func envelope(schema, digest string, extra string) []byte {
	return []byte(fmt.Sprintf(`{"schema":%q,"schema_digest":%q,"seq":1,"payload":null%s}`, schema, digest, extra))
}

func TestAcceptsCurrentEnvelopeAndIsCanonical(t *testing.T) {
	in := envelope(SchemaName+"/"+CurrentVersion, KnownVersions[0][1], "")
	_, out, err := VerifyBytes(in)
	if err != nil {
		t.Fatal(err)
	}
	_, again, err := VerifyBytes(out)
	if err != nil || string(again) != string(out) {
		t.Fatalf("not canonical: %v", err)
	}
}

func TestRejectsIncompatibleSchemasWithSpecificCodes(t *testing.T) {
	d := KnownVersions[0][1]
	cases := []struct{ schema, digest, want string }{
		{"vsf.smoke.envelope/2.0.0", d, "schema.incompatible_major"},
		{"vsf.smoke.envelope/1.1.0", d, "schema.too_new"},
		{"vsf.smoke.envelope/1.0.1", d, "schema.too_new"},
		{"vsf.other/1.0.0", d, "schema.unknown"},
		{"vsf.smoke.envelope", d, "schema.malformed"},
		{"vsf.smoke.envelope/1.0", d, "schema.malformed"},
		{"vsf.smoke.envelope/01.0.0", d, "schema.malformed"},
		{"vsf.smoke.envelope/1.0.0", "sha256:00", "schema.digest_mismatch"},
	}
	for _, c := range cases {
		_, _, err := VerifyBytes(envelope(c.schema, c.digest, ""))
		if got := ErrorCode(err); got != c.want {
			t.Errorf("%s: got %s want %s", c.schema, got, c.want)
		}
	}
}

func TestRejectsMissingInvalidAndUnknownFields(t *testing.T) {
	ok := SchemaName + "/" + CurrentVersion
	d := KnownVersions[0][1]
	cases := []struct{ in, want string }{
		{`[]`, "schema.malformed"},
		{`{"seq":1}`, "schema.field_missing"},
		{`{"schema":1}`, "schema.field_invalid"},
		{fmt.Sprintf(`{"schema":%q,"seq":1,"payload":1}`, ok), "schema.field_missing"},
		{fmt.Sprintf(`{"schema":%q,"schema_digest":%q,"seq":-1,"payload":1}`, ok, d), "schema.field_invalid"},
		{fmt.Sprintf(`{"schema":%q,"schema_digest":%q,"seq":1}`, ok, d), "schema.field_missing"},
		{fmt.Sprintf(`{"schema":%q,"schema_digest":%q,"seq":1,"payload":1,"x":1}`, ok, d), "schema.field_invalid"},
		{fmt.Sprintf(`{"schema":%q,"schema_digest":%q,"seq":1.5,"payload":1}`, ok, d), "canon.non_integer"},
	}
	for _, c := range cases {
		_, _, err := VerifyBytes([]byte(c.in))
		if got := ErrorCode(err); got != c.want {
			t.Errorf("%s: got %s want %s", c.in, got, c.want)
		}
	}
}
