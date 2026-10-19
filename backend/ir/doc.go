package ir

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/insightlabs38-pixel/vsf/backend/canon"
	"github.com/insightlabs38-pixel/vsf/backend/ir/rt"
)

// DigestAt is the digest a document of this family and version must carry.
func DigestAt(family string, v rt.Version) (string, bool) {
	return acceptedDigest(family, v)
}

func acceptedDigest(family string, v rt.Version) (string, bool) {
	for _, a := range Accepted {
		if a.Family == family && a.Version == [3]uint32(v) {
			return a.Digest, true
		}
	}
	return "", false
}

func familyDigest(family string) (string, bool) {
	for _, f := range Families {
		if f[0] == family {
			return f[2], true
		}
	}
	return "", false
}

// TypedDigest domain-separates digests so one artifact kind can never validate as another.
func TypedDigest(kind string, canonical []byte) string {
	h := sha256.New()
	h.Write([]byte("vsf.digest.v1\x00"))
	h.Write([]byte(kind))
	h.Write([]byte{0})
	h.Write(canonical)
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// Unvalidated is a decoded document no validation has accepted; it can never be executed.
type Unvalidated struct {
	body    Body
	version rt.Version
}

// Unsafe returns the decoded body, checked for shape only.
func (u Unvalidated) Unsafe() Body { return u.body }

// Version is the schema version the document was written under.
func (u Unvalidated) Version() rt.Version { return u.version }

// Digest is the typed digest of the document as it was written.
func (u Unvalidated) Digest() string {
	d, _ := DocumentDigestAt(u.body, u.version)
	return d
}

func DecodeDocumentValue(v any) (Unvalidated, error) {
	b, ver, err := decodeEnvelope(v)
	if err != nil {
		return Unvalidated{}, err
	}
	return Unvalidated{body: b, version: ver}, nil
}

func decodeEnvelope(v any) (Body, rt.Version, error) {
	o, err := rt.NewObj(v, "")
	if err != nil {
		return Body{}, rt.Version{}, err
	}
	rawSchema, err := o.Req("schema")
	if err != nil {
		return Body{}, rt.Version{}, err
	}
	schema, err := rt.EnumStr(rawSchema, "/schema")
	if err != nil {
		return Body{}, rt.Version{}, err
	}
	rawDigest, err := o.Req("schema_digest")
	if err != nil {
		return Body{}, rt.Version{}, err
	}
	digest, err := rt.EnumStr(rawDigest, "/schema_digest")
	if err != nil {
		return Body{}, rt.Version{}, err
	}
	body, err := o.Req("body")
	if err != nil {
		return Body{}, rt.Version{}, err
	}
	if err := o.Finish(); err != nil {
		return Body{}, rt.Version{}, err
	}
	slash := strings.LastIndex(schema, "/")
	if slash < 0 {
		return Body{}, rt.Version{}, rt.Fail("/schema", "schema.malformed")
	}
	family, ok := strings.CutPrefix(schema[:slash], "vsf.")
	if !ok {
		return Body{}, rt.Version{}, rt.Fail("/schema", "schema.unknown")
	}
	if _, known := familyDigest(family); !known {
		return Body{}, rt.Version{}, rt.Fail("/schema", "schema.unknown")
	}
	got, ok := rt.ParseVersion(schema[slash+1:])
	if !ok {
		return Body{}, rt.Version{}, rt.Fail("/schema", "schema.malformed")
	}
	if got[0] != IRVersion[0] {
		return Body{}, rt.Version{}, rt.Fail("/schema", "schema.incompatible_major")
	}
	if got[1] > IRVersion[1] || (got[1] == IRVersion[1] && got[2] > IRVersion[2]) {
		return Body{}, rt.Version{}, rt.Fail("/schema", "schema.too_new")
	}
	expected, released := acceptedDigest(family, got)
	if !released {
		return Body{}, rt.Version{}, rt.Fail("/schema", "schema.unknown_version")
	}
	if digest != expected {
		return Body{}, rt.Version{}, rt.Fail("/schema_digest", "schema.digest_mismatch")
	}
	out, err := decodeBody(family, body, "/body", rt.Ctx{Version: got})
	return out, got, err
}

func DecodeDocument(input []byte) (Unvalidated, error) {
	v, err := canon.Parse(input)
	if err != nil {
		return Unvalidated{}, rt.Fail("", err.Error())
	}
	return DecodeDocumentValue(v)
}

func EncodeDocument(b Body) []byte {
	out, _ := EncodeDocumentAt(b, IRVersion)
	return out
}

// EncodeDocumentAt writes the envelope of a released version; false when that version is not known.
func EncodeDocumentAt(b Body, v rt.Version) ([]byte, bool) {
	d, ok := DigestAt(b.Family, v)
	if !ok {
		return nil, false
	}
	doc := canon.NewObject()
	doc.Set("schema", fmt.Sprintf("vsf.%s/%d.%d.%d", b.Family, v[0], v[1], v[2]))
	doc.Set("schema_digest", d)
	doc.Set("body", b.Encode())
	return canon.ToCanonical(doc), true
}

func DocumentDigest(b Body) string {
	return TypedDigest("document:"+b.Family, EncodeDocument(b))
}

// DocumentDigestAt is the typed digest of a document as it was written under version v.
func DocumentDigestAt(b Body, v rt.Version) (string, bool) {
	out, ok := EncodeDocumentAt(b, v)
	if !ok {
		return "", false
	}
	return TypedDigest("document:"+b.Family, out), true
}
