//! Strict reading helpers over the canonical value model.

use std::fmt;
use vsf_canon::Value;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Error {
    pub code: &'static str,
    pub detail: String,
}

impl fmt::Display for Error {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{}: {}", self.code, self.detail)
    }
}

pub type Res<T> = Result<T, Error>;

pub fn err(code: &'static str, detail: impl Into<String>) -> Error {
    Error {
        code,
        detail: detail.into(),
    }
}

pub struct Obj<'a> {
    entries: &'a [(String, Value)],
    used: Vec<bool>,
    path: String,
}

impl<'a> Obj<'a> {
    pub fn new(v: &'a Value, path: &str) -> Res<Self> {
        match v {
            Value::Object(entries) => Ok(Self {
                entries,
                used: vec![false; entries.len()],
                path: path.to_string(),
            }),
            _ => Err(err("registry.not_object", path)),
        }
    }

    fn find(&mut self, key: &str) -> Option<&'a Value> {
        let i = self.entries.iter().position(|(k, _)| k == key)?;
        self.used[i] = true;
        Some(&self.entries[i].1)
    }

    pub fn req(&mut self, key: &str) -> Res<&'a Value> {
        self.find(key)
            .ok_or_else(|| err("registry.missing_field", format!("{}/{key}", self.path)))
    }

    pub fn opt(&mut self, key: &str) -> Option<&'a Value> {
        self.find(key)
    }

    pub fn finish(self) -> Res<()> {
        match self.used.iter().position(|u| !u) {
            Some(i) => Err(err(
                "registry.unknown_field",
                format!("{}/{}", self.path, self.entries[i].0),
            )),
            None => Ok(()),
        }
    }

    pub fn str(&mut self, key: &str) -> Res<String> {
        let p = format!("{}/{key}", self.path);
        text(self.req(key)?, &p)
    }

    pub fn strs(&mut self, key: &str) -> Res<Vec<String>> {
        let p = format!("{}/{key}", self.path);
        texts(self.req(key)?, &p)
    }

    pub fn int(&mut self, key: &str) -> Res<i64> {
        let p = format!("{}/{key}", self.path);
        self.req(key)?
            .as_int()
            .ok_or_else(|| err("registry.wrong_type", p))
    }

    pub fn boolean(&mut self, key: &str) -> Res<bool> {
        let p = format!("{}/{key}", self.path);
        match self.req(key)? {
            Value::Bool(b) => Ok(*b),
            _ => Err(err("registry.wrong_type", p)),
        }
    }
}

pub fn text(v: &Value, path: &str) -> Res<String> {
    v.as_str()
        .map(str::to_string)
        .ok_or_else(|| err("registry.wrong_type", path))
}

pub fn array<'a>(v: &'a Value, path: &str) -> Res<&'a [Value]> {
    match v {
        Value::Array(items) => Ok(items),
        _ => Err(err("registry.not_array", path)),
    }
}

pub fn texts(v: &Value, path: &str) -> Res<Vec<String>> {
    array(v, path)?
        .iter()
        .enumerate()
        .map(|(i, x)| text(x, &format!("{path}/{i}")))
        .collect()
}

pub fn s(x: &str) -> Value {
    Value::Str(x.to_string())
}

pub fn list(items: &[String]) -> Value {
    Value::Array(items.iter().map(|x| s(x)).collect())
}

pub fn object(entries: Vec<(&str, Value)>) -> Value {
    Value::Object(
        entries
            .into_iter()
            .map(|(k, v)| (k.to_string(), v))
            .collect(),
    )
}

/// One top-level array element per line so generated files diff well; the content is canonical.
pub fn pretty(v: &Value) -> String {
    match v {
        Value::Object(entries) => {
            let mut sorted: Vec<&(String, Value)> = entries.iter().collect();
            sorted.sort_by(|a, b| a.0.encode_utf16().cmp(b.0.encode_utf16()));
            let rows: Vec<String> = sorted
                .iter()
                .map(|(k, x)| {
                    let key =
                        String::from_utf8_lossy(&vsf_canon::to_canonical(&Value::Str(k.clone())))
                            .into_owned();
                    match x {
                        Value::Array(items)
                            if items.iter().all(|i| matches!(i, Value::Object(_)))
                                && !items.is_empty() =>
                        {
                            let lines: Vec<String> = items
                                .iter()
                                .map(|i| {
                                    format!(
                                        "    {}",
                                        String::from_utf8_lossy(&vsf_canon::to_canonical(i))
                                    )
                                })
                                .collect();
                            format!("  {key}: [\n{}\n  ]", lines.join(",\n"))
                        }
                        other => format!(
                            "  {key}: {}",
                            String::from_utf8_lossy(&vsf_canon::to_canonical(other))
                        ),
                    }
                })
                .collect();
            format!("{{\n{}\n}}\n", rows.join(",\n"))
        }
        other => format!(
            "{}\n",
            String::from_utf8_lossy(&vsf_canon::to_canonical(other))
        ),
    }
}
