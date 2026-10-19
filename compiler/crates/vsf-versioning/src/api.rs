//! Worker-facing entry points: Rust decides whether a promoted manifest may be trusted, Go only stores the verdict.

use crate::promotion;
use vsf_canon::{Value, to_canonical};
use vsf_ir::Body;
use vsf_ir::doc::{decode_value, document_digest_at};
use vsf_ir::semantics::validate_version;
use vsf_registry::manifest::{Accepted, Freshness, Trust};

fn text(s: &str) -> Value {
    Value::Str(s.to_string())
}

fn num(n: i64) -> Value {
    Value::Int(n)
}

fn context(json: &[u8]) -> Result<(Trust, Freshness), &'static str> {
    let v = vsf_canon::parse(json).map_err(|_| "context_malformed")?;
    let now_ms = v
        .get("now_ms")
        .and_then(Value::as_int)
        .ok_or("context_malformed")?;
    let trust =
        Trust::decode(v.get("trust").ok_or("context_malformed")?).map_err(|_| "trust_malformed")?;
    let last = match v.get("last") {
        None | Some(Value::Null) => None,
        Some(l) => Some(Accepted {
            sequence: l
                .get("sequence")
                .and_then(Value::as_int)
                .ok_or("context_malformed")?,
            digest: l
                .get("digest")
                .and_then(Value::as_str)
                .ok_or("context_malformed")?
                .to_string(),
        }),
    };
    Ok((trust, Freshness { now_ms, last }))
}

/// Frame: u32-LE context length, context JSON, manifest document; only a malformed frame is an `Err`.
pub fn promotion_report(frame: &[u8]) -> Result<Vec<u8>, &'static str> {
    let (len, rest) = frame.split_at_checked(4).ok_or("promotion.truncated")?;
    let len = u32::from_le_bytes([len[0], len[1], len[2], len[3]]) as usize;
    let (ctx, doc) = rest.split_at_checked(len).ok_or("promotion.truncated")?;
    let (trust, fresh) = context(ctx)?;
    let mut fields: Vec<(String, Value)> = Vec::new();
    let mut reason = String::new();
    let parsed = vsf_canon::parse(doc);
    let decoded = match &parsed {
        Ok(v) => decode_value(v).map_err(|_| "undecodable"),
        Err(e) => {
            reason = e.code().to_string();
            Err("undecodable")
        }
    };
    match decoded {
        Ok((Body::Version(m), version)) => {
            let body = Body::Version(m.clone());
            let id = document_digest_at(&body, version).unwrap_or_default();
            fields.push(("id".into(), text(&id)));
            fields.push(("channel".into(), text(m.channel.as_str())));
            fields.push((
                "lineage".into(),
                Value::Array(m.lineage.iter().map(|d| text(d.as_str())).collect()),
            ));
            fields.push((
                "artifacts".into(),
                Value::Array(
                    m.artifacts
                        .iter()
                        .map(|a| {
                            Value::Object(vec![
                                ("bytes".into(), num(a.bytes.0)),
                                ("digest".into(), text(a.digest.as_str())),
                                ("role".into(), text(a.role.as_str())),
                            ])
                        })
                        .collect(),
                ),
            ));
            fields.push((
                "previous".into(),
                m.promotion
                    .as_ref()
                    .and_then(|p| p.previous.as_ref())
                    .map_or(Value::Null, |d| text(d.as_str())),
            ));
            fields.push(("promoted".into(), Value::Bool(m.promotion.is_some())));
            let invalid = validate_version(&m);
            fields.push(("valid".into(), Value::Bool(invalid.is_empty())));
            if let Some(v) = invalid.first() {
                reason = format!("invalid:{}", v.code);
            } else {
                match promotion::verify(&m, &trust, &fresh) {
                    Ok(ok) => {
                        fields.push(("key_id".into(), text(&ok.key_id)));
                        fields.push(("purpose".into(), text(&ok.purpose)));
                        fields.push(("sequence".into(), num(ok.sequence)));
                        fields.push(("not_after".into(), num(ok.not_after_ms)));
                        fields.push(("signed_digest".into(), text(&ok.digest)));
                    }
                    Err(e) => reason = e.code.to_string(),
                }
            }
        }
        Ok(_) => reason = "not_a_version_manifest".into(),
        Err(code) => {
            if reason.is_empty() {
                reason = code.into();
            }
        }
    }
    if !fields.iter().any(|(k, _)| k == "valid") {
        fields.push(("valid".into(), Value::Bool(false)));
        fields.push(("promoted".into(), Value::Bool(false)));
    }
    fields.push(("verified".into(), Value::Bool(reason.is_empty())));
    fields.push(("reason".into(), text(&reason)));
    Ok(to_canonical(&Value::Object(fields)))
}
