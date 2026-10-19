//! Small constructors for canonical output values.

use vsf_canon::Value;

pub fn s(x: &str) -> Value {
    Value::Str(x.to_string())
}

pub fn n(x: i64) -> Value {
    Value::Int(x)
}

pub fn count(x: usize) -> Value {
    Value::Int(i64::try_from(x).unwrap_or(i64::MAX))
}

pub fn obj(entries: Vec<(&str, Value)>) -> Value {
    Value::Object(
        entries
            .into_iter()
            .map(|(k, v)| (k.to_string(), v))
            .collect(),
    )
}

pub fn list(items: Vec<Value>) -> Value {
    Value::Array(items)
}

/// One top-level array element per line so generated files diff well; the content is canonical.
pub fn pretty(v: &Value) -> String {
    let canon = |x: &Value| String::from_utf8_lossy(&vsf_canon::to_canonical(x)).into_owned();
    match v {
        Value::Object(entries) => {
            let mut sorted: Vec<&(String, Value)> = entries.iter().collect();
            sorted.sort_by(|a, b| a.0.encode_utf16().cmp(b.0.encode_utf16()));
            let rows: Vec<String> = sorted
                .iter()
                .map(|(k, x)| match x {
                    Value::Array(items)
                        if !items.is_empty()
                            && items.iter().all(|i| matches!(i, Value::Object(_))) =>
                    {
                        let lines: Vec<String> =
                            items.iter().map(|i| format!("    {}", canon(i))).collect();
                        format!(
                            "  {}: [\n{}\n  ]",
                            canon(&Value::Str(k.clone())),
                            lines.join(",\n")
                        )
                    }
                    other => format!("  {}: {}", canon(&Value::Str(k.clone())), canon(other)),
                })
                .collect();
            format!("{{\n{}\n}}\n", rows.join(",\n"))
        }
        other => format!("{}\n", canon(other)),
    }
}
