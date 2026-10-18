#![cfg_attr(test, allow(clippy::unwrap_used, clippy::expect_used, clippy::panic))]

use regex::Regex;
use std::sync::OnceLock;
use vsf_canon::Value;

const PATTERNS_JSON: &str = include_str!("../../../../schemas/redaction/patterns.v1.json");

struct Pattern {
    kind: String,
    regex: Regex,
}

fn patterns() -> &'static [Pattern] {
    static PATTERNS: OnceLock<Vec<Pattern>> = OnceLock::new();
    PATTERNS.get_or_init(|| load(PATTERNS_JSON).unwrap_or_default())
}

fn load(text: &str) -> Option<Vec<Pattern>> {
    let doc = vsf_canon::parse(text.as_bytes()).ok()?;
    let Some(Value::Array(list)) = doc.get("patterns") else {
        return None;
    };
    let mut out = Vec::with_capacity(list.len());
    for item in list {
        let kind = item.get("kind")?.as_str()?.to_string();
        let body = item.get("regex")?.as_str()?;
        let insensitive = item
            .get("flags")
            .and_then(Value::as_str)
            .is_some_and(|f| f.contains('i'));
        let source = if insensitive {
            format!("(?i){body}")
        } else {
            body.to_string()
        };
        out.push(Pattern {
            kind,
            regex: Regex::new(&source).ok()?,
        });
    }
    Some(out)
}

pub fn pattern_count() -> usize {
    patterns().len()
}

pub fn redact(text: &str) -> String {
    let mut current = text.to_string();
    for pattern in patterns() {
        let marker = format!("[REDACTED:{}]", pattern.kind);
        if pattern.regex.is_match(&current) {
            current = pattern
                .regex
                .replace_all(&current, marker.as_str())
                .into_owned();
        }
    }
    current
}

pub fn redact_value(value: &Value) -> Value {
    match value {
        Value::Str(s) => Value::Str(redact(s)),
        Value::Array(items) => Value::Array(items.iter().map(redact_value).collect()),
        Value::Object(entries) => Value::Object(
            entries
                .iter()
                .map(|(k, v)| (redact(k), redact_value(v)))
                .collect(),
        ),
        other => other.clone(),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn cases() -> Value {
        vsf_canon::parse(include_bytes!(
            "../../../../schemas/redaction/cases.v1.json"
        ))
        .unwrap()
    }

    fn join_parts(case: &Value) -> String {
        let Some(Value::Array(parts)) = case.get("parts") else {
            panic!("parts")
        };
        parts.iter().map(|p| p.as_str().unwrap()).collect()
    }

    #[test]
    fn loads_every_pattern() {
        assert_eq!(pattern_count(), 11);
    }

    #[test]
    fn redacts_every_positive_case_with_its_kind() {
        let doc = cases();
        let Some(Value::Array(positive)) = doc.get("positive") else {
            panic!("positive")
        };
        for case in positive {
            let planted = join_parts(case);
            let kind = case.get("kind").unwrap().as_str().unwrap();
            let out = redact(&format!("before {planted} after"));
            assert!(out.contains(&format!("[REDACTED:{kind}]")), "{kind}: {out}");
            assert!(!out.contains(&planted), "{kind} leaked");
        }
    }

    #[test]
    fn leaves_non_secrets_untouched() {
        let doc = cases();
        let Some(Value::Array(negative)) = doc.get("negative") else {
            panic!("negative")
        };
        for case in negative {
            let text = case.get("text").unwrap().as_str().unwrap();
            assert_eq!(redact(text), text);
        }
    }

    #[test]
    fn redacts_nested_values_and_keys_deterministically() {
        let secret = ["gh", "p_", "abcdefghijklmnopqrstuvwxyz0123456789"].concat();
        let input = Value::Object(vec![(
            secret.clone(),
            Value::Array(vec![Value::Str(format!("x {secret}")), Value::Int(3)]),
        )]);
        let out = redact_value(&input);
        assert_eq!(out, redact_value(&input));
        let bytes = vsf_canon::to_canonical(&out);
        assert!(!String::from_utf8(bytes).unwrap().contains(&secret));
    }
}
