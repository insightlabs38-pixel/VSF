//! One entry point shared by the native worker and the WASM module.

use crate::doc::{decode_value, document_digest_at};
use crate::generated::Body;
use crate::semantics::{ExecEnv, authorizes_execution, validate, violation_values};
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
    let v = vsf_canon::parse(input).map_err(|e| e.code())?;
    let (decode, violations, valid) = match decode_value(&v) {
        Ok((body, version)) => {
            let found = validate(&body);
            let valid = found.is_empty();
            let digest = document_digest_at(&body, version).unwrap_or_default();
            (
                Value::Object(vec![
                    ("digest".into(), text(&digest)),
                    ("family".into(), text(body.family())),
                    ("ok".into(), Value::Bool(true)),
                    (
                        "version".into(),
                        text(&format!("{}.{}.{}", version.0, version.1, version.2)),
                    ),
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

fn env_from(v: &Value) -> Option<ExecEnv> {
    let Value::Object(fields) = v else {
        return None;
    };
    let get = |k: &str| fields.iter().find(|(n, _)| n == k).map(|(_, v)| v);
    let text = |k: &str| match get(k) {
        Some(Value::Str(s)) if !s.is_empty() => Some(s.clone()),
        _ => None,
    };
    let Some(Value::Int(now)) = get("now") else {
        return None;
    };
    Some(ExecEnv {
        now: *now,
        runtime_id: text("runtime_id")?,
        source_state: text("source_state")?,
        capability_digest: text("capability_digest")?,
        principal_ref: text("principal_ref")?,
        variant_id: text("variant_id")?,
    })
}

/// Decides whether this contract document may run in this environment; the only source of that decision.
pub fn authorize_report(doc: &[u8], env_json: &[u8]) -> Result<Vec<u8>, &'static str> {
    let v = vsf_canon::parse(doc).map_err(|e| e.code())?;
    let env = vsf_canon::parse(env_json).ok().as_ref().and_then(env_from);
    let mut digest = String::new();
    let mut operation = String::new();
    let mut version = String::new();
    let mut effect = String::new();
    let verdict = match (decode_value(&v), env) {
        (Err(_), _) => Err("undecodable"),
        (Ok(_), None) => Err("env_malformed"),
        (Ok((Body::Operation(c), ver)), Some(env)) => {
            digest = document_digest_at(&Body::Operation(c.clone()), ver).unwrap_or_default();
            operation = c.operation_id.as_str().to_string();
            version = c.contract_version.as_str().to_string();
            effect = c.effect.class.as_str().to_string();
            if validate(&Body::Operation(c.clone())).is_empty() {
                authorizes_execution(&c, &env)
            } else {
                Err("invalid_contract")
            }
        }
        (Ok(_), Some(_)) => Err("not_operation_contract"),
    };
    let report = Value::Object(vec![
        ("authorized".into(), Value::Bool(verdict.is_ok())),
        ("contract_version".into(), text(&version)),
        ("digest".into(), text(&digest)),
        ("effect_class".into(), text(&effect)),
        ("operation_id".into(), text(&operation)),
        ("reason".into(), text(verdict.err().unwrap_or(""))),
    ]);
    Ok(to_canonical(&report))
}
