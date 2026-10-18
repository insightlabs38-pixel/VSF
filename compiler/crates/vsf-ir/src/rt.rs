//! Runtime support for generated IR codecs: strict decoding over the canonical `Value` model.

use regex::Regex;
use std::collections::{BTreeMap, HashMap};
use std::sync::{Mutex, OnceLock};
use vsf_canon::Value;

pub type Version = (u32, u32, u32);

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct DecodeError {
    pub path: String,
    pub code: &'static str,
}

impl DecodeError {
    pub fn new(path: &str, code: &'static str) -> Self {
        Self {
            path: path.to_string(),
            code,
        }
    }
}

impl std::fmt::Display for DecodeError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(
            f,
            "{} at {}",
            self.code,
            if self.path.is_empty() {
                "/"
            } else {
                &self.path
            }
        )
    }
}

impl std::error::Error for DecodeError {}

pub type Res<T> = Result<T, DecodeError>;

/// Document schema version being decoded; gates fields introduced by later minor versions.
#[derive(Debug, Clone, Copy)]
pub struct Ctx {
    pub version: Version,
}

pub fn child(path: &str, key: &str) -> String {
    format!("{path}/{key}")
}

pub fn index(path: &str, i: usize) -> String {
    format!("{path}/{i}")
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
            _ => Err(DecodeError::new(path, "ir.not_object")),
        }
    }

    fn find(&mut self, key: &str) -> Option<&'a Value> {
        let i = self.entries.iter().position(|(k, _)| k == key)?;
        self.used[i] = true;
        Some(&self.entries[i].1)
    }

    pub fn path(&self) -> &str {
        &self.path
    }

    pub fn req(&mut self, key: &str) -> Res<&'a Value> {
        self.find(key)
            .ok_or_else(|| DecodeError::new(&child(&self.path, key), "ir.missing_field"))
    }

    pub fn opt(&mut self, key: &str) -> Option<&'a Value> {
        self.find(key)
    }

    /// A field introduced in `since`: forbidden before it, mandatory (when `required`) from it onward.
    pub fn since(
        &mut self,
        key: &str,
        since: Version,
        required: bool,
        ctx: &Ctx,
    ) -> Res<Option<&'a Value>> {
        let found = self.find(key);
        if ctx.version < since {
            return match found {
                Some(_) => Err(DecodeError::new(
                    &child(&self.path, key),
                    "ir.field_from_future",
                )),
                None => Ok(None),
            };
        }
        match (found, required) {
            (None, true) => Err(DecodeError::new(
                &child(&self.path, key),
                "ir.missing_field",
            )),
            (found, _) => Ok(found),
        }
    }

    pub fn finish(self) -> Res<()> {
        for (i, used) in self.used.iter().enumerate() {
            if !used {
                return Err(DecodeError::new(
                    &child(&self.path, &self.entries[i].0),
                    "ir.unknown_field",
                ));
            }
        }
        Ok(())
    }
}

pub fn array<'a>(v: &'a Value, path: &str, min: usize, max: usize) -> Res<&'a [Value]> {
    match v {
        Value::Array(items) if items.len() > max => {
            Err(DecodeError::new(path, "ir.too_many_items"))
        }
        Value::Array(items) if items.len() < min => Err(DecodeError::new(path, "ir.too_few_items")),
        Value::Array(items) => Ok(items),
        _ => Err(DecodeError::new(path, "ir.not_array")),
    }
}

pub fn list<T>(
    v: &Value,
    path: &str,
    min: usize,
    max: usize,
    f: impl Fn(&Value, &str) -> Res<T>,
) -> Res<Vec<T>> {
    let items = array(v, path, min, max)?;
    items
        .iter()
        .enumerate()
        .map(|(i, e)| f(e, &index(path, i)))
        .collect()
}

pub fn map<K: Ord, T>(
    v: &Value,
    path: &str,
    max: usize,
    key: impl Fn(&str) -> Res<K>,
    f: impl Fn(&Value, &str) -> Res<T>,
) -> Res<BTreeMap<K, T>> {
    let mut out = BTreeMap::new();
    for (k, e) in map_entries(v, path, max)? {
        let p = child(path, k);
        let key = key(k).map_err(|_| DecodeError::new(&p, "ir.bad_key"))?;
        out.insert(key, f(e, &p)?);
    }
    Ok(out)
}

pub fn map_entries<'a>(v: &'a Value, path: &str, max: usize) -> Res<&'a [(String, Value)]> {
    match v {
        Value::Object(entries) if entries.len() > max => {
            Err(DecodeError::new(path, "ir.too_many_items"))
        }
        Value::Object(entries) => Ok(entries),
        _ => Err(DecodeError::new(path, "ir.not_object")),
    }
}

pub fn boolean(v: &Value, path: &str) -> Res<bool> {
    match v {
        Value::Bool(b) => Ok(*b),
        _ => Err(DecodeError::new(path, "ir.wrong_type")),
    }
}

pub fn integer(v: &Value, path: &str, min: i64, max: i64) -> Res<i64> {
    match v {
        Value::Int(n) if *n < min || *n > max => Err(DecodeError::new(path, "ir.out_of_range")),
        Value::Int(n) => Ok(*n),
        _ => Err(DecodeError::new(path, "ir.wrong_type")),
    }
}

pub const INSTANT_MIN: i64 = -62_135_596_800_000;
pub const INSTANT_MAX: i64 = 253_402_300_799_999;

pub fn instant(v: &Value, path: &str) -> Res<i64> {
    match v {
        Value::Int(n) if *n < INSTANT_MIN || *n > INSTANT_MAX => {
            Err(DecodeError::new(path, "ir.bad_instant"))
        }
        Value::Int(n) => Ok(*n),
        _ => Err(DecodeError::new(path, "ir.wrong_type")),
    }
}

fn compiled(pattern: &'static str) -> &'static Regex {
    static CACHE: OnceLock<Mutex<HashMap<&'static str, &'static Regex>>> = OnceLock::new();
    let cache = CACHE.get_or_init(|| Mutex::new(HashMap::new()));
    let mut guard = cache
        .lock()
        .unwrap_or_else(std::sync::PoisonError::into_inner);
    if let Some(r) = guard.get(pattern) {
        return r;
    }
    // Patterns come from the checked-in IDL; a bad one is caught by the generator tests.
    let leaked: &'static Regex =
        Box::leak(Box::new(Regex::new(pattern).unwrap_or_else(|_| {
            Regex::new("^$").unwrap_or_else(|_| unreachable!())
        })));
    guard.insert(pattern, leaked);
    leaked
}

#[derive(Clone, Copy, PartialEq, Eq)]
pub enum TextMode {
    None,
    Label,
    Multiline,
}

pub struct StrRule {
    pub pattern: Option<&'static str>,
    pub max_len: usize,
    pub text: TextMode,
}

fn forbidden_char(c: char, multiline: bool) -> bool {
    let u = c as u32;
    if multiline && (c == '\n' || c == '\t') {
        return false;
    }
    u < 0x20
        || (0x7f..=0x9f).contains(&u)
        || u == 0x2028
        || u == 0x2029
        || (0x202a..=0x202e).contains(&u)
        || (0x2066..=0x2069).contains(&u)
        || u == 0xfffe
        || u == 0xffff
}

pub fn string(v: &Value, path: &str, rule: &StrRule) -> Res<String> {
    let Value::Str(s) = v else {
        return Err(DecodeError::new(path, "ir.wrong_type"));
    };
    check_string(s, path, rule)?;
    Ok(s.clone())
}

pub fn check_string(s: &str, path: &str, rule: &StrRule) -> Res<()> {
    if s.chars().count() > rule.max_len {
        return Err(DecodeError::new(path, "ir.too_long"));
    }
    if rule.text != TextMode::None
        && s.chars()
            .any(|c| forbidden_char(c, rule.text == TextMode::Multiline))
    {
        return Err(DecodeError::new(path, "ir.bad_text"));
    }
    if let Some(p) = rule.pattern
        && !compiled(p).is_match(s)
    {
        return Err(DecodeError::new(path, "ir.bad_pattern"));
    }
    Ok(())
}

pub fn decimal(v: &Value, path: &str, scale: Option<u32>) -> Res<String> {
    let Value::Str(s) = v else {
        return Err(DecodeError::new(path, "ir.wrong_type"));
    };
    if !valid_decimal(s, scale) {
        return Err(DecodeError::new(path, "ir.bad_decimal"));
    }
    Ok(s.clone())
}

pub fn valid_decimal(s: &str, scale: Option<u32>) -> bool {
    if s.is_empty() || s.len() > 64 {
        return false;
    }
    let (neg, body) = s.strip_prefix('-').map_or((false, s), |r| (true, r));
    let (int, frac) = match body.split_once('.') {
        Some((i, f)) => (i, Some(f)),
        None => (body, None),
    };
    if int.is_empty()
        || !int.bytes().all(|b| b.is_ascii_digit())
        || (int.len() > 1 && int.starts_with('0'))
    {
        return false;
    }
    let frac_ok = match (frac, scale) {
        (None, None) | (None, Some(0)) => true,
        (Some(f), Some(n)) if n > 0 => {
            f.len() == n as usize && f.bytes().all(|b| b.is_ascii_digit())
        }
        (Some(f), None) => {
            !f.is_empty() && f.bytes().all(|b| b.is_ascii_digit()) && !f.ends_with('0')
        }
        _ => false,
    };
    if !frac_ok {
        return false;
    }
    let zero = body.bytes().all(|b| b == b'0' || b == b'.');
    !(neg && zero)
}

pub fn date(v: &Value, path: &str) -> Res<String> {
    let Value::Str(s) = v else {
        return Err(DecodeError::new(path, "ir.wrong_type"));
    };
    if !valid_date(s) {
        return Err(DecodeError::new(path, "ir.bad_date"));
    }
    Ok(s.clone())
}

pub fn valid_date(s: &str) -> bool {
    let b = s.as_bytes();
    if b.len() != 10 || b[4] != b'-' || b[7] != b'-' {
        return false;
    }
    let digits = |r: std::ops::Range<usize>| b[r].iter().all(u8::is_ascii_digit);
    if !digits(0..4) || !digits(5..7) || !digits(8..10) {
        return false;
    }
    let num = |r: std::ops::Range<usize>| s[r].parse::<u32>().unwrap_or(0);
    let (y, m, d) = (num(0..4), num(5..7), num(8..10));
    let leap = (y % 4 == 0 && y % 100 != 0) || y % 400 == 0;
    let days = match m {
        1 | 3 | 5 | 7 | 8 | 10 | 12 => 31,
        4 | 6 | 9 | 11 => 30,
        2 if leap => 29,
        2 => 28,
        _ => return false,
    };
    y >= 1 && (1..=days).contains(&d)
}

pub fn enum_str<'a>(v: &'a Value, path: &str) -> Res<&'a str> {
    match v {
        Value::Str(s) => Ok(s),
        _ => Err(DecodeError::new(path, "ir.wrong_type")),
    }
}

pub fn text(s: &str) -> Value {
    Value::Str(s.to_string())
}

pub fn int(n: i64) -> Value {
    Value::Int(n)
}

pub fn object(entries: Vec<(&str, Value)>) -> Value {
    Value::Object(
        entries
            .into_iter()
            .map(|(k, v)| (k.to_string(), v))
            .collect(),
    )
}

pub fn parse_version(s: &str) -> Option<Version> {
    let mut it = s.split('.');
    let mut next = || -> Option<u32> {
        let part = it.next()?;
        if part.is_empty()
            || (part.len() > 1 && part.starts_with('0'))
            || !part.bytes().all(|b| b.is_ascii_digit())
        {
            return None;
        }
        part.parse().ok()
    };
    let v = (next()?, next()?, next()?);
    it.next().is_none().then_some(v)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn decimals_are_exact_and_canonical() {
        for ok in ["0", "-1", "10", "1.5", "-0.5", "123456.789"] {
            assert!(valid_decimal(ok, None), "{ok}");
        }
        for bad in [
            "", "-0", "00", "01", "1.", ".5", "1.50", "1e3", "+1", "1,5", " 1", "-0.0", "0.0",
            "1.0",
        ] {
            assert!(!valid_decimal(bad, None), "{bad}");
        }
        assert!(
            valid_decimal("1.10", Some(2))
                && valid_decimal("0.00", Some(2))
                && valid_decimal("-3.07", Some(2))
        );
        for bad in ["1.1", "1", "1.100", "-0.00", "01.00"] {
            assert!(!valid_decimal(bad, Some(2)), "{bad}");
        }
        assert!(valid_decimal("12", Some(0)) && !valid_decimal("12.0", Some(0)));
    }

    #[test]
    fn dates_follow_the_gregorian_calendar() {
        for ok in ["2024-02-29", "2000-02-29", "0001-01-01", "9999-12-31"] {
            assert!(valid_date(ok), "{ok}");
        }
        for bad in [
            "2023-02-29",
            "1900-02-29",
            "2024-13-01",
            "2024-00-10",
            "2024-04-31",
            "0000-01-01",
            "2024-1-01",
            "2024/01/01",
            "20240101",
            "2024-01-011",
        ] {
            assert!(!valid_date(bad), "{bad}");
        }
    }

    #[test]
    fn text_rules_reject_controls_and_bidi_overrides_but_keep_normalization_untouched() {
        let rule = StrRule {
            pattern: None,
            max_len: 20,
            text: TextMode::Label,
        };
        assert!(check_string("caf\u{e9}", "", &rule).is_ok());
        assert!(check_string("cafe\u{301}", "", &rule).is_ok());
        for bad in [
            "a\nb",
            "a\u{0}b",
            "a\u{202e}b",
            "a\u{2066}b",
            "a\u{85}b",
            "a\u{ffff}b",
            "x\u{7f}",
        ] {
            assert_eq!(
                check_string(bad, "p", &rule).unwrap_err().code,
                "ir.bad_text",
                "{bad:?}"
            );
        }
        let multi = StrRule {
            pattern: None,
            max_len: 20,
            text: TextMode::Multiline,
        };
        assert!(check_string("a\nb\tc", "", &multi).is_ok());
        assert_eq!(
            check_string(&"x".repeat(21), "", &multi).unwrap_err().code,
            "ir.too_long"
        );
        assert!(
            check_string(&"\u{1f600}".repeat(20), "", &multi).is_ok(),
            "length counts scalar values, not bytes"
        );
    }

    #[test]
    fn versions_parse_strictly() {
        assert_eq!(parse_version("1.20.3"), Some((1, 20, 3)));
        for bad in ["1.2", "1.2.3.4", "01.2.3", "1.2.x", "", "1..3", "-1.0.0"] {
            assert_eq!(parse_version(bad), None, "{bad}");
        }
    }

    #[test]
    fn since_gates_fields_by_declared_version() {
        let v = vsf_canon::parse(br#"{"a":1,"b":2}"#).unwrap();
        let old = Ctx { version: (1, 0, 0) };
        let new = Ctx { version: (1, 1, 0) };
        let mut o = Obj::new(&v, "").unwrap();
        o.req("a").unwrap();
        assert_eq!(
            o.since("b", (1, 1, 0), true, &old).unwrap_err().code,
            "ir.field_from_future"
        );
        let mut o = Obj::new(&v, "").unwrap();
        o.req("a").unwrap();
        assert!(o.since("b", (1, 1, 0), true, &new).unwrap().is_some());
        let v2 = vsf_canon::parse(br#"{"a":1}"#).unwrap();
        let mut o = Obj::new(&v2, "").unwrap();
        o.req("a").unwrap();
        assert_eq!(
            o.since("b", (1, 1, 0), true, &new).unwrap_err().code,
            "ir.missing_field"
        );
        let mut o = Obj::new(&v2, "").unwrap();
        o.req("a").unwrap();
        assert_eq!(o.since("b", (1, 1, 0), true, &old).unwrap(), None);
    }
}
