//! Versioned document envelope: `{"schema":"vsf.<family>/<ver>","schema_digest":..,"body":..}`.

use crate::generated::{ACCEPTED, Body, FAMILIES, IR_VERSION};
use crate::rt::{self, Ctx, DecodeError, Res};
use vsf_canon::Value;

fn err(path: &str, code: &'static str) -> DecodeError {
    DecodeError::new(path, code)
}

pub fn family_digest(family: &str) -> Option<&'static str> {
    FAMILIES
        .iter()
        .find(|(f, _, _)| *f == family)
        .map(|(_, _, d)| *d)
}

/// The digest a document of this family and version must carry; every released version stays readable.
pub fn digest_at(family: &str, version: (u32, u32, u32)) -> Option<&'static str> {
    ACCEPTED
        .iter()
        .find(|(v, f, _)| *v == version && *f == family)
        .map(|(_, _, d)| *d)
}

/// Domain-separated digest so a digest of one kind of artifact can never validate as another.
pub fn typed_digest(kind: &str, canonical: &[u8]) -> String {
    let mut input = Vec::with_capacity(canonical.len() + kind.len() + 16);
    input.extend_from_slice(b"vsf.digest.v1\0");
    input.extend_from_slice(kind.as_bytes());
    input.push(0);
    input.extend_from_slice(canonical);
    format!("sha256:{}", vsf_canon::sha256_hex(&input))
}

pub fn decode_value(v: &Value) -> Res<(Body, (u32, u32, u32))> {
    let mut o = rt::Obj::new(v, "")?;
    let schema = rt::enum_str(o.req("schema")?, "/schema")?;
    let digest = rt::enum_str(o.req("schema_digest")?, "/schema_digest")?;
    let body = o.req("body")?;
    o.finish()?;
    let (name, version) = schema
        .rsplit_once('/')
        .ok_or_else(|| err("/schema", "schema.malformed"))?;
    let family = name
        .strip_prefix("vsf.")
        .ok_or_else(|| err("/schema", "schema.unknown"))?;
    family_digest(family).ok_or_else(|| err("/schema", "schema.unknown"))?;
    let got = rt::parse_version(version).ok_or_else(|| err("/schema", "schema.malformed"))?;
    if got.0 != IR_VERSION.0 {
        return Err(err("/schema", "schema.incompatible_major"));
    }
    if (got.1, got.2) > (IR_VERSION.1, IR_VERSION.2) {
        return Err(err("/schema", "schema.too_new"));
    }
    let expected =
        digest_at(family, got).ok_or_else(|| err("/schema", "schema.unknown_version"))?;
    if digest != expected {
        return Err(err("/schema_digest", "schema.digest_mismatch"));
    }
    let ctx = Ctx { version: got };
    Ok((Body::decode(family, body, "/body", &ctx)?, got))
}

pub fn decode_document(bytes: &[u8]) -> Res<Body> {
    let v = vsf_canon::parse(bytes).map_err(|e| err("", e.code()))?;
    Ok(decode_value(&v)?.0)
}

pub fn encode_document(body: &Body) -> Vec<u8> {
    encode_document_at(body, IR_VERSION).unwrap_or_default()
}

/// Encodes under a released version's envelope; `None` when that version is not one this reader knows.
pub fn encode_document_at(body: &Body, version: (u32, u32, u32)) -> Option<Vec<u8>> {
    let family = body.family();
    let digest = digest_at(family, version)?;
    let schema = format!("vsf.{family}/{}.{}.{}", version.0, version.1, version.2);
    let doc = rt::object(vec![
        ("schema", rt::text(&schema)),
        ("schema_digest", rt::text(digest)),
        ("body", body.encode()),
    ]);
    Some(vsf_canon::to_canonical(&doc))
}

/// Typed digest of a document as it was written under `version`.
pub fn document_digest_at(body: &Body, version: (u32, u32, u32)) -> Option<String> {
    Some(typed_digest(
        &format!("document:{}", body.family()),
        &encode_document_at(body, version)?,
    ))
}

/// Canonical document bytes and the typed artifact digest of a decoded document.
pub fn document_digest(body: &Body) -> String {
    typed_digest(
        &format!("document:{}", body.family()),
        &encode_document(body),
    )
}
