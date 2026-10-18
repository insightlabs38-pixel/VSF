package ir

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/insightlabs38-pixel/vsf/backend/canon"
	"github.com/insightlabs38-pixel/vsf/backend/ir/rt"
)

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

func DecodeDocumentValue(v any) (Body, error) {
	o, err := rt.NewObj(v, "")
	if err != nil {
		return Body{}, err
	}
	rawSchema, err := o.Req("schema")
	if err != nil {
		return Body{}, err
	}
	schema, err := rt.EnumStr(rawSchema, "/schema")
	if err != nil {
		return Body{}, err
	}
	rawDigest, err := o.Req("schema_digest")
	if err != nil {
		return Body{}, err
	}
	digest, err := rt.EnumStr(rawDigest, "/schema_digest")
	if err != nil {
		return Body{}, err
	}
	body, err := o.Req("body")
	if err != nil {
		return Body{}, err
	}
	if err := o.Finish(); err != nil {
		return Body{}, err
	}
	slash := strings.LastIndex(schema, "/")
	if slash < 0 {
		return Body{}, rt.Fail("/schema", "schema.malformed")
	}
	family, ok := strings.CutPrefix(schema[:slash], "vsf.")
	if !ok {
		return Body{}, rt.Fail("/schema", "schema.unknown")
	}
	expected, known := familyDigest(family)
	if !known {
		return Body{}, rt.Fail("/schema", "schema.unknown")
	}
	got, ok := rt.ParseVersion(schema[slash+1:])
	if !ok {
		return Body{}, rt.Fail("/schema", "schema.malformed")
	}
	if got[0] != IRVersion[0] {
		return Body{}, rt.Fail("/schema", "schema.incompatible_major")
	}
	if got[1] > IRVersion[1] || (got[1] == IRVersion[1] && got[2] > IRVersion[2]) {
		return Body{}, rt.Fail("/schema", "schema.too_new")
	}
	if got != IRVersion {
		return Body{}, rt.Fail("/schema", "schema.unknown_version")
	}
	if digest != expected {
		return Body{}, rt.Fail("/schema_digest", "schema.digest_mismatch")
	}
	return decodeBody(family, body, "/body", rt.Ctx{Version: got})
}

func DecodeDocument(input []byte) (Body, error) {
	v, err := canon.Parse(input)
	if err != nil {
		return Body{}, rt.Fail("", err.Error())
	}
	return DecodeDocumentValue(v)
}

func EncodeDocument(b Body) []byte {
	d, _ := familyDigest(b.Family)
	doc := canon.NewObject()
	doc.Set("schema", "vsf."+b.Family+"/"+IRVersionText)
	doc.Set("schema_digest", d)
	doc.Set("body", b.Encode())
	return canon.ToCanonical(doc)
}

func DocumentDigest(b Body) string {
	return TypedDigest("document:"+b.Family, EncodeDocument(b))
}
