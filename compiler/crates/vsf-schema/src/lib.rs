#![cfg_attr(test, allow(clippy::unwrap_used, clippy::expect_used, clippy::panic))]

#[rustfmt::skip]
mod generated;

use vsf_canon::{CanonError, Value};

pub use generated::{CURRENT_VERSION, KNOWN_VERSIONS, SCHEMA_NAME};

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum SchemaError {
    Malformed,
    Unknown,
    IncompatibleMajor,
    TooNew,
    UnknownVersion,
    DigestMismatch,
    FieldMissing,
    FieldInvalid,
}

impl SchemaError {
    pub fn code(self) -> &'static str {
        match self {
            Self::Malformed => "schema.malformed",
            Self::Unknown => "schema.unknown",
            Self::IncompatibleMajor => "schema.incompatible_major",
            Self::TooNew => "schema.too_new",
            Self::UnknownVersion => "schema.unknown_version",
            Self::DigestMismatch => "schema.digest_mismatch",
            Self::FieldMissing => "schema.field_missing",
            Self::FieldInvalid => "schema.field_invalid",
        }
    }
}

impl std::fmt::Display for SchemaError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(self.code())
    }
}

impl std::error::Error for SchemaError {}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Error {
    Canon(CanonError),
    Schema(SchemaError),
}

impl Error {
    pub fn code(self) -> &'static str {
        match self {
            Self::Canon(e) => e.code(),
            Self::Schema(e) => e.code(),
        }
    }
}

impl From<CanonError> for Error {
    fn from(e: CanonError) -> Self {
        Self::Canon(e)
    }
}

impl From<SchemaError> for Error {
    fn from(e: SchemaError) -> Self {
        Self::Schema(e)
    }
}

impl std::fmt::Display for Error {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(self.code())
    }
}

impl std::error::Error for Error {}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Envelope {
    pub seq: i64,
    pub payload: Value,
}

fn parse_version(text: &str) -> Result<(u64, u64, u64), SchemaError> {
    let mut parts = text.split('.');
    let mut next = || -> Result<u64, SchemaError> {
        let part = parts.next().ok_or(SchemaError::Malformed)?;
        let canonical = part == "0" || (!part.starts_with('0') && !part.is_empty());
        if !canonical || !part.bytes().all(|b| b.is_ascii_digit()) {
            return Err(SchemaError::Malformed);
        }
        part.parse().map_err(|_| SchemaError::Malformed)
    };
    let version = (next()?, next()?, next()?);
    if parts.next().is_some() {
        return Err(SchemaError::Malformed);
    }
    Ok(version)
}

pub fn validate_envelope(value: &Value) -> Result<Envelope, SchemaError> {
    let Value::Object(entries) = value else {
        return Err(SchemaError::Malformed);
    };
    let schema = match value.get("schema") {
        None => return Err(SchemaError::FieldMissing),
        Some(Value::Str(s)) => s,
        Some(_) => return Err(SchemaError::FieldInvalid),
    };
    let (name, version) = schema.rsplit_once('/').ok_or(SchemaError::Malformed)?;
    if name != SCHEMA_NAME {
        return Err(SchemaError::Unknown);
    }
    let got = parse_version(version)?;
    let want = parse_version(CURRENT_VERSION)?;
    if got.0 != want.0 {
        return Err(SchemaError::IncompatibleMajor);
    }
    if (got.1, got.2) > (want.1, want.2) {
        return Err(SchemaError::TooNew);
    }
    let (_, known_digest) = KNOWN_VERSIONS
        .iter()
        .find(|(v, _)| *v == version)
        .ok_or(SchemaError::UnknownVersion)?;
    match value.get("schema_digest") {
        None => return Err(SchemaError::FieldMissing),
        Some(Value::Str(d)) if d == known_digest => {}
        Some(Value::Str(_)) => return Err(SchemaError::DigestMismatch),
        Some(_) => return Err(SchemaError::FieldInvalid),
    }
    let seq = match value.get("seq") {
        None => return Err(SchemaError::FieldMissing),
        Some(Value::Int(n)) if *n >= 0 => *n,
        Some(_) => return Err(SchemaError::FieldInvalid),
    };
    let payload = value
        .get("payload")
        .ok_or(SchemaError::FieldMissing)?
        .clone();
    if entries
        .iter()
        .any(|(k, _)| !matches!(k.as_str(), "schema" | "schema_digest" | "seq" | "payload"))
    {
        return Err(SchemaError::FieldInvalid);
    }
    Ok(Envelope { seq, payload })
}

pub fn make_envelope(seq: i64, payload: Value) -> Value {
    let digest = KNOWN_VERSIONS
        .iter()
        .find(|(v, _)| *v == CURRENT_VERSION)
        .map_or("", |(_, d)| *d);
    Value::Object(vec![
        (
            "schema".to_string(),
            Value::Str(format!("{SCHEMA_NAME}/{CURRENT_VERSION}")),
        ),
        ("schema_digest".to_string(), Value::Str(digest.to_string())),
        ("seq".to_string(), Value::Int(seq)),
        ("payload".to_string(), payload),
    ])
}

pub fn verify_envelope_bytes(input: &[u8]) -> Result<(Envelope, Vec<u8>), Error> {
    let value = vsf_canon::parse(input)?;
    let envelope = validate_envelope(&value)?;
    Ok((envelope, vsf_canon::to_canonical(&value)))
}

#[cfg(test)]
mod tests {
    use super::*;

    fn bytes_with_schema(schema: &str, digest: &str) -> Vec<u8> {
        format!(r#"{{"schema":"{schema}","schema_digest":"{digest}","seq":1,"payload":null}}"#)
            .into_bytes()
    }

    fn current_digest() -> &'static str {
        KNOWN_VERSIONS[0].1
    }

    #[test]
    fn accepts_current_envelope_and_roundtrips() {
        let value = make_envelope(7, Value::Array(vec![Value::Int(1)]));
        let bytes = vsf_canon::to_canonical(&value);
        let (env, canonical) = verify_envelope_bytes(&bytes).unwrap();
        assert_eq!(env.seq, 7);
        assert_eq!(canonical, bytes);
    }

    #[test]
    fn rejects_incompatible_schemas_with_specific_codes() {
        let d = current_digest();
        let cases = [
            ("vsf.smoke.envelope/2.0.0", d, "schema.incompatible_major"),
            ("vsf.smoke.envelope/1.1.0", d, "schema.too_new"),
            ("vsf.smoke.envelope/1.0.1", d, "schema.too_new"),
            ("vsf.other/1.0.0", d, "schema.unknown"),
            ("vsf.smoke.envelope", d, "schema.malformed"),
            ("vsf.smoke.envelope/1.0", d, "schema.malformed"),
            ("vsf.smoke.envelope/01.0.0", d, "schema.malformed"),
            (
                "vsf.smoke.envelope/1.0.0",
                "sha256:00",
                "schema.digest_mismatch",
            ),
        ];
        for (schema, digest, want) in cases {
            let err = verify_envelope_bytes(&bytes_with_schema(schema, digest)).unwrap_err();
            assert_eq!(err.code(), want, "{schema}");
        }
    }

    #[test]
    fn rejects_missing_invalid_and_unknown_fields() {
        let d = current_digest();
        let ok = format!("vsf.smoke.envelope/{CURRENT_VERSION}");
        let cases: Vec<(String, &str)> = vec![
            ("[]".to_string(), "schema.malformed"),
            (r#"{"seq":1}"#.to_string(), "schema.field_missing"),
            (r#"{"schema":1}"#.to_string(), "schema.field_invalid"),
            (
                format!(r#"{{"schema":"{ok}","seq":1,"payload":1}}"#),
                "schema.field_missing",
            ),
            (
                format!(r#"{{"schema":"{ok}","schema_digest":"{d}","seq":-1,"payload":1}}"#),
                "schema.field_invalid",
            ),
            (
                format!(r#"{{"schema":"{ok}","schema_digest":"{d}","seq":1}}"#),
                "schema.field_missing",
            ),
            (
                format!(r#"{{"schema":"{ok}","schema_digest":"{d}","seq":1,"payload":1,"x":1}}"#),
                "schema.field_invalid",
            ),
            (
                format!(r#"{{"schema":"{ok}","schema_digest":"{d}","seq":1.5,"payload":1}}"#),
                "canon.non_integer",
            ),
        ];
        for (input, want) in cases {
            assert_eq!(
                verify_envelope_bytes(input.as_bytes()).unwrap_err().code(),
                want,
                "{input}"
            );
        }
    }
}
