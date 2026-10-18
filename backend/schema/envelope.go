// Package schema validates the B00 smoke envelope contract.
package schema

import (
	"strconv"
	"strings"

	"github.com/insightlabs38-pixel/vsf/backend/canon"
)

type Error struct{ Code string }

func (e *Error) Error() string { return e.Code }

func fail(code string) error { return &Error{Code: code} }

type Envelope struct {
	Seq     int64
	Payload any
}

func parseVersion(s string) ([3]int, error) {
	parts := strings.Split(s, ".")
	var out [3]int
	if len(parts) != 3 {
		return out, fail("schema.malformed")
	}
	for i, p := range parts {
		if p == "" || (len(p) > 1 && p[0] == '0') {
			return out, fail("schema.malformed")
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, fail("schema.malformed")
		}
		out[i] = n
	}
	return out, nil
}

func Validate(v any) (*Envelope, error) {
	obj, ok := v.(*canon.Object)
	if !ok {
		return nil, fail("schema.malformed")
	}
	rawSchema, present := obj.Fields["schema"]
	if !present {
		return nil, fail("schema.field_missing")
	}
	schema, ok := rawSchema.(string)
	if !ok {
		return nil, fail("schema.field_invalid")
	}
	slash := strings.LastIndex(schema, "/")
	if slash < 0 {
		return nil, fail("schema.malformed")
	}
	if schema[:slash] != SchemaName {
		return nil, fail("schema.unknown")
	}
	version := schema[slash+1:]
	got, err := parseVersion(version)
	if err != nil {
		return nil, err
	}
	want, err := parseVersion(CurrentVersion)
	if err != nil {
		return nil, err
	}
	if got[0] != want[0] {
		return nil, fail("schema.incompatible_major")
	}
	if got[1] > want[1] || (got[1] == want[1] && got[2] > want[2]) {
		return nil, fail("schema.too_new")
	}
	var knownDigest string
	for _, kv := range KnownVersions {
		if kv[0] == version {
			knownDigest = kv[1]
		}
	}
	if knownDigest == "" {
		return nil, fail("schema.unknown_version")
	}
	rawDigest, present := obj.Fields["schema_digest"]
	if !present {
		return nil, fail("schema.field_missing")
	}
	digest, ok := rawDigest.(string)
	if !ok {
		return nil, fail("schema.field_invalid")
	}
	if digest != knownDigest {
		return nil, fail("schema.digest_mismatch")
	}
	rawSeq, present := obj.Fields["seq"]
	if !present {
		return nil, fail("schema.field_missing")
	}
	seq, ok := rawSeq.(int64)
	if !ok || seq < 0 {
		return nil, fail("schema.field_invalid")
	}
	payload, present := obj.Fields["payload"]
	if !present {
		return nil, fail("schema.field_missing")
	}
	for _, k := range obj.Keys {
		switch k {
		case "schema", "schema_digest", "seq", "payload":
		default:
			return nil, fail("schema.field_invalid")
		}
	}
	return &Envelope{Seq: seq, Payload: payload}, nil
}

// VerifyBytes validates an envelope and returns its canonical bytes.
func VerifyBytes(input []byte) (*Envelope, []byte, error) {
	v, err := canon.Parse(input)
	if err != nil {
		return nil, nil, err
	}
	env, err := Validate(v)
	if err != nil {
		return nil, nil, err
	}
	return env, canon.ToCanonical(v), nil
}

func ErrorCode(err error) string {
	switch e := err.(type) {
	case *Error:
		return e.Code
	case *canon.Error:
		return e.Code
	}
	return "internal"
}
