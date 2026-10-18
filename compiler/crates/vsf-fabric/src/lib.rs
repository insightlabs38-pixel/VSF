#![cfg_attr(test, allow(clippy::unwrap_used, clippy::expect_used, clippy::panic))]

use vsf_canon::Value;
use vsf_schema::Error;

pub const COMPILER_ID: &str = "vsf-fabric-stub/0.0.0";

/// Deterministic B00 stand-in for the semantic compiler: verify, redact, summarize.
pub fn compile_envelope(input: &[u8]) -> Result<Vec<u8>, Error> {
    let (envelope, canonical_input) = vsf_schema::verify_envelope_bytes(input)?;
    let node_count = match envelope.payload.get("nodes") {
        Some(Value::Array(nodes)) => nodes.len() as i64,
        _ => 0,
    };
    let payload = Value::Object(vec![
        ("compiler".to_string(), Value::Str(COMPILER_ID.to_string())),
        (
            "input_sha256".to_string(),
            Value::Str(vsf_canon::sha256_hex(&canonical_input)),
        ),
        ("node_count".to_string(), Value::Int(node_count)),
        (
            "payload".to_string(),
            vsf_redact::redact_value(&envelope.payload),
        ),
    ]);
    Ok(vsf_canon::to_canonical(&vsf_schema::make_envelope(
        envelope.seq,
        payload,
    )))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn compile_is_deterministic_and_redacts() {
        let input = vsf_fixtures::generate_envelope_bytes(42);
        let a = compile_envelope(&input).unwrap();
        assert_eq!(a, compile_envelope(&input).unwrap());
        let text = String::from_utf8(a).unwrap();
        assert!(text.contains("[REDACTED:keyed_secret]"));
        assert!(!text.contains("api_key="));
    }

    #[test]
    fn compile_output_is_itself_a_valid_envelope() {
        let out = compile_envelope(&vsf_fixtures::generate_envelope_bytes(1)).unwrap();
        assert_eq!(vsf_schema::verify_envelope_bytes(&out).unwrap().1, out);
    }

    #[test]
    fn compile_rejects_incompatible_schema() {
        let bad = br#"{"schema":"vsf.smoke.envelope/2.0.0","schema_digest":"sha256:0","seq":1,"payload":null}"#;
        assert_eq!(
            compile_envelope(bad).unwrap_err().code(),
            "schema.incompatible_major"
        );
    }
}
