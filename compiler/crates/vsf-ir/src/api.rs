//! One entry point shared by the native worker and the WASM module.

use crate::doc::{decode_document, document_digest};
use crate::semantics::{validate, violation_values};
use vsf_canon::{Value, to_canonical};

fn text(s: &str) -> Value {
    Value::Str(s.to_string())
}

/// Decodes and validates a document; only non-canonical input is an `Err` (`canon.*`).
pub fn validate_bytes(input: &[u8]) -> Result<Vec<u8>, &'static str> {
    validate_report(input).map(|(bytes, _)| bytes)
}

/// Like [`validate_bytes`], also returning whether the document decoded and passed every invariant.
pub fn validate_report(input: &[u8]) -> Result<(Vec<u8>, bool), &'static str> {
    let (decode, violations, valid) = match decode_document(input) {
        Ok(body) => {
            let found = validate(&body);
            let valid = found.is_empty();
            (
                Value::Object(vec![
                    ("digest".into(), text(&document_digest(&body))),
                    ("family".into(), text(body.family())),
                    ("ok".into(), Value::Bool(true)),
                ]),
                violation_values(&found),
                valid,
            )
        }
        Err(e) if e.code.starts_with("canon.") => return Err(e.code),
        Err(e) => (
            Value::Object(vec![
                ("code".into(), text(e.code)),
                ("ok".into(), Value::Bool(false)),
                ("path".into(), text(&e.path)),
            ]),
            Vec::new(),
            false,
        ),
    };
    let report = Value::Object(vec![
        ("decode".into(), decode),
        ("valid".into(), Value::Bool(valid)),
        ("violations".into(), Value::Array(violations)),
    ]);
    Ok((to_canonical(&report), valid))
}
