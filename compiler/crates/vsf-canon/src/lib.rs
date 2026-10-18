#![cfg_attr(test, allow(clippy::unwrap_used, clippy::expect_used, clippy::panic))]

use sha2::{Digest, Sha256};
use std::collections::BTreeSet;
use std::fmt::Write as _;

pub const MAX_INPUT_BYTES: usize = 16 * 1024 * 1024;
pub const MAX_DEPTH: usize = 64;
pub const MAX_SAFE_INT: i64 = 9_007_199_254_740_991;

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Value {
    Null,
    Bool(bool),
    Int(i64),
    Str(String),
    Array(Vec<Value>),
    Object(Vec<(String, Value)>),
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum CanonError {
    TooLarge,
    InvalidUtf8,
    Syntax,
    DuplicateKey,
    NonInteger,
    IntegerOutOfRange,
    DepthExceeded,
    TrailingData,
    InvalidEscape,
    LoneSurrogate,
}

impl CanonError {
    pub fn code(self) -> &'static str {
        match self {
            Self::TooLarge => "canon.too_large",
            Self::InvalidUtf8 => "canon.invalid_utf8",
            Self::Syntax => "canon.syntax",
            Self::DuplicateKey => "canon.duplicate_key",
            Self::NonInteger => "canon.non_integer",
            Self::IntegerOutOfRange => "canon.integer_out_of_range",
            Self::DepthExceeded => "canon.depth_exceeded",
            Self::TrailingData => "canon.trailing_data",
            Self::InvalidEscape => "canon.invalid_escape",
            Self::LoneSurrogate => "canon.lone_surrogate",
        }
    }
}

impl std::fmt::Display for CanonError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(self.code())
    }
}

impl std::error::Error for CanonError {}

impl Value {
    pub fn get(&self, key: &str) -> Option<&Value> {
        match self {
            Value::Object(entries) => entries.iter().find(|(k, _)| k == key).map(|(_, v)| v),
            _ => None,
        }
    }

    pub fn as_str(&self) -> Option<&str> {
        match self {
            Value::Str(s) => Some(s),
            _ => None,
        }
    }

    pub fn as_int(&self) -> Option<i64> {
        match self {
            Value::Int(n) => Some(*n),
            _ => None,
        }
    }
}

pub fn parse(input: &[u8]) -> Result<Value, CanonError> {
    if input.len() > MAX_INPUT_BYTES {
        return Err(CanonError::TooLarge);
    }
    let text = std::str::from_utf8(input).map_err(|_| CanonError::InvalidUtf8)?;
    let mut parser = Parser {
        src: text.as_bytes(),
        pos: 0,
    };
    parser.skip_ws();
    let value = parser.value(0)?;
    parser.skip_ws();
    if parser.pos != parser.src.len() {
        return Err(CanonError::TrailingData);
    }
    Ok(value)
}

pub fn canonicalize(input: &[u8]) -> Result<Vec<u8>, CanonError> {
    Ok(to_canonical(&parse(input)?))
}

pub fn to_canonical(value: &Value) -> Vec<u8> {
    let mut out = Vec::new();
    write_value(value, &mut out);
    out
}

pub fn sha256_hex(bytes: &[u8]) -> String {
    let digest = Sha256::digest(bytes);
    let mut hex = String::with_capacity(64);
    for byte in digest {
        let _ = write!(hex, "{byte:02x}");
    }
    hex
}

fn write_value(value: &Value, out: &mut Vec<u8>) {
    match value {
        Value::Null => out.extend_from_slice(b"null"),
        Value::Bool(true) => out.extend_from_slice(b"true"),
        Value::Bool(false) => out.extend_from_slice(b"false"),
        Value::Int(n) => out.extend_from_slice(n.to_string().as_bytes()),
        Value::Str(s) => write_string(s, out),
        Value::Array(items) => {
            out.push(b'[');
            for (i, item) in items.iter().enumerate() {
                if i > 0 {
                    out.push(b',');
                }
                write_value(item, out);
            }
            out.push(b']');
        }
        Value::Object(entries) => {
            let mut sorted: Vec<&(String, Value)> = entries.iter().collect();
            sorted.sort_by(|a, b| a.0.encode_utf16().cmp(b.0.encode_utf16()));
            out.push(b'{');
            for (i, (key, val)) in sorted.into_iter().enumerate() {
                if i > 0 {
                    out.push(b',');
                }
                write_string(key, out);
                out.push(b':');
                write_value(val, out);
            }
            out.push(b'}');
        }
    }
}

fn write_string(s: &str, out: &mut Vec<u8>) {
    out.push(b'"');
    for ch in s.chars() {
        match ch {
            '"' => out.extend_from_slice(b"\\\""),
            '\\' => out.extend_from_slice(b"\\\\"),
            '\u{08}' => out.extend_from_slice(b"\\b"),
            '\u{0c}' => out.extend_from_slice(b"\\f"),
            '\n' => out.extend_from_slice(b"\\n"),
            '\r' => out.extend_from_slice(b"\\r"),
            '\t' => out.extend_from_slice(b"\\t"),
            c if (c as u32) < 0x20 => {
                let _ = write!(Utf8Sink(out), "\\u{:04x}", c as u32);
            }
            c => {
                let mut buf = [0u8; 4];
                out.extend_from_slice(c.encode_utf8(&mut buf).as_bytes());
            }
        }
    }
    out.push(b'"');
}

struct Utf8Sink<'a>(&'a mut Vec<u8>);

impl std::fmt::Write for Utf8Sink<'_> {
    fn write_str(&mut self, s: &str) -> std::fmt::Result {
        self.0.extend_from_slice(s.as_bytes());
        Ok(())
    }
}

struct Parser<'a> {
    src: &'a [u8],
    pos: usize,
}

impl Parser<'_> {
    fn peek(&self) -> Option<u8> {
        self.src.get(self.pos).copied()
    }

    fn skip_ws(&mut self) {
        while matches!(self.peek(), Some(b' ' | b'\t' | b'\n' | b'\r')) {
            self.pos += 1;
        }
    }

    fn expect(&mut self, byte: u8) -> Result<(), CanonError> {
        if self.peek() == Some(byte) {
            self.pos += 1;
            Ok(())
        } else {
            Err(CanonError::Syntax)
        }
    }

    fn literal(&mut self, word: &[u8], value: Value) -> Result<Value, CanonError> {
        if self.src[self.pos..].starts_with(word) {
            self.pos += word.len();
            Ok(value)
        } else {
            Err(CanonError::Syntax)
        }
    }

    fn value(&mut self, depth: usize) -> Result<Value, CanonError> {
        if depth > MAX_DEPTH {
            return Err(CanonError::DepthExceeded);
        }
        match self.peek() {
            Some(b'n') => self.literal(b"null", Value::Null),
            Some(b't') => self.literal(b"true", Value::Bool(true)),
            Some(b'f') => self.literal(b"false", Value::Bool(false)),
            Some(b'"') => Ok(Value::Str(self.string()?)),
            Some(b'[') => self.array(depth),
            Some(b'{') => self.object(depth),
            Some(b'-' | b'0'..=b'9') => self.number(),
            _ => Err(CanonError::Syntax),
        }
    }

    fn array(&mut self, depth: usize) -> Result<Value, CanonError> {
        self.expect(b'[')?;
        let mut items = Vec::new();
        self.skip_ws();
        if self.peek() == Some(b']') {
            self.pos += 1;
            return Ok(Value::Array(items));
        }
        loop {
            self.skip_ws();
            items.push(self.value(depth + 1)?);
            self.skip_ws();
            match self.peek() {
                Some(b',') => self.pos += 1,
                Some(b']') => {
                    self.pos += 1;
                    return Ok(Value::Array(items));
                }
                _ => return Err(CanonError::Syntax),
            }
        }
    }

    fn object(&mut self, depth: usize) -> Result<Value, CanonError> {
        self.expect(b'{')?;
        let mut entries: Vec<(String, Value)> = Vec::new();
        let mut seen: BTreeSet<String> = BTreeSet::new();
        self.skip_ws();
        if self.peek() == Some(b'}') {
            self.pos += 1;
            return Ok(Value::Object(entries));
        }
        loop {
            self.skip_ws();
            if self.peek() != Some(b'"') {
                return Err(CanonError::Syntax);
            }
            let key = self.string()?;
            self.skip_ws();
            self.expect(b':')?;
            self.skip_ws();
            let val = self.value(depth + 1)?;
            if !seen.insert(key.clone()) {
                return Err(CanonError::DuplicateKey);
            }
            entries.push((key, val));
            self.skip_ws();
            match self.peek() {
                Some(b',') => self.pos += 1,
                Some(b'}') => {
                    self.pos += 1;
                    return Ok(Value::Object(entries));
                }
                _ => return Err(CanonError::Syntax),
            }
        }
    }

    fn number(&mut self) -> Result<Value, CanonError> {
        let negative = self.peek() == Some(b'-');
        if negative {
            self.pos += 1;
        }
        let start = self.pos;
        match self.peek() {
            Some(b'0') => self.pos += 1,
            Some(b'1'..=b'9') => {
                while matches!(self.peek(), Some(b'0'..=b'9')) {
                    self.pos += 1;
                }
            }
            _ => return Err(CanonError::Syntax),
        }
        if matches!(self.peek(), Some(b'.' | b'e' | b'E')) {
            return Err(CanonError::NonInteger);
        }
        if matches!(self.peek(), Some(b'0'..=b'9')) {
            return Err(CanonError::Syntax);
        }
        let digits = &self.src[start..self.pos];
        if digits.len() > 16 {
            return Err(CanonError::IntegerOutOfRange);
        }
        let magnitude: i64 = digits
            .iter()
            .fold(0i64, |acc, d| acc * 10 + i64::from(d - b'0'));
        if magnitude > MAX_SAFE_INT {
            return Err(CanonError::IntegerOutOfRange);
        }
        Ok(Value::Int(if negative { -magnitude } else { magnitude }))
    }

    fn hex4(&mut self) -> Result<u32, CanonError> {
        let slice = self
            .src
            .get(self.pos..self.pos + 4)
            .ok_or(CanonError::InvalidEscape)?;
        let mut n = 0u32;
        for &b in slice {
            let d = match b {
                b'0'..=b'9' => b - b'0',
                b'a'..=b'f' => b - b'a' + 10,
                b'A'..=b'F' => b - b'A' + 10,
                _ => return Err(CanonError::InvalidEscape),
            };
            n = n * 16 + u32::from(d);
        }
        self.pos += 4;
        Ok(n)
    }

    fn string(&mut self) -> Result<String, CanonError> {
        self.expect(b'"')?;
        let mut out = String::new();
        loop {
            let run_start = self.pos;
            while let Some(b) = self.peek() {
                if b == b'"' || b == b'\\' || b < 0x20 {
                    break;
                }
                self.pos += 1;
            }
            if self.pos > run_start {
                let run = std::str::from_utf8(&self.src[run_start..self.pos])
                    .map_err(|_| CanonError::InvalidUtf8)?;
                out.push_str(run);
            }
            match self.peek() {
                Some(b'"') => {
                    self.pos += 1;
                    return Ok(out);
                }
                Some(b'\\') => {
                    self.pos += 1;
                    let esc = self.peek().ok_or(CanonError::InvalidEscape)?;
                    self.pos += 1;
                    match esc {
                        b'"' => out.push('"'),
                        b'\\' => out.push('\\'),
                        b'/' => out.push('/'),
                        b'b' => out.push('\u{08}'),
                        b'f' => out.push('\u{0c}'),
                        b'n' => out.push('\n'),
                        b'r' => out.push('\r'),
                        b't' => out.push('\t'),
                        b'u' => {
                            let hi = self.hex4()?;
                            let code = if (0xD800..0xDC00).contains(&hi) {
                                if self.src.get(self.pos..self.pos + 2) != Some(b"\\u") {
                                    return Err(CanonError::LoneSurrogate);
                                }
                                self.pos += 2;
                                let lo = self.hex4()?;
                                if !(0xDC00..0xE000).contains(&lo) {
                                    return Err(CanonError::LoneSurrogate);
                                }
                                0x10000 + ((hi - 0xD800) << 10) + (lo - 0xDC00)
                            } else if (0xDC00..0xE000).contains(&hi) {
                                return Err(CanonError::LoneSurrogate);
                            } else {
                                hi
                            };
                            out.push(char::from_u32(code).ok_or(CanonError::InvalidEscape)?);
                        }
                        _ => return Err(CanonError::InvalidEscape),
                    }
                }
                _ => return Err(CanonError::Syntax),
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn canon(s: &str) -> String {
        String::from_utf8(canonicalize(s.as_bytes()).unwrap()).unwrap()
    }

    #[test]
    fn sorts_keys_and_strips_whitespace() {
        assert_eq!(
            canon(" { \"b\" : 1 ,\n\"a\":[true,null, \"x\"] } "),
            r#"{"a":[true,null,"x"],"b":1}"#
        );
    }

    #[test]
    fn orders_keys_by_utf16_code_units() {
        let out = canon("{\"\u{ffff}\":1,\"\u{10000}\":2}");
        assert!(out.find('\u{10000}').unwrap() < out.find('\u{ffff}').unwrap());
    }

    #[test]
    fn escapes_minimally_and_roundtrips() {
        assert_eq!(canon(r#""\u0001\u007f\/é\n""#), "\"\\u0001\u{7f}/é\\n\"");
        let once = canon(r#"{"k":"a\u00e9\ud83d\ude00"}"#);
        assert_eq!(canon(&once), once);
    }

    #[test]
    fn normalizes_negative_zero() {
        assert_eq!(canon("[-0,0,-9007199254740991]"), "[0,0,-9007199254740991]");
    }

    #[test]
    fn rejects_malformed_and_ambiguous_inputs() {
        let cases: &[(&str, CanonError)] = &[
            (r#"{"a":1,"a":2}"#, CanonError::DuplicateKey),
            ("1.5", CanonError::NonInteger),
            ("1e3", CanonError::NonInteger),
            ("9007199254740992", CanonError::IntegerOutOfRange),
            ("01", CanonError::Syntax),
            ("[1,]", CanonError::Syntax),
            ("{} x", CanonError::TrailingData),
            (r#""\ud800""#, CanonError::LoneSurrogate),
            (r#""\udc00""#, CanonError::LoneSurrogate),
            (r#""\x""#, CanonError::InvalidEscape),
            ("\"a\u{1}\"", CanonError::Syntax),
            ("\u{feff}1", CanonError::Syntax),
            ("", CanonError::Syntax),
        ];
        for (input, want) in cases {
            assert_eq!(parse(input.as_bytes()).unwrap_err(), *want, "{input:?}");
        }
        assert_eq!(
            parse(&[b'"', 0xff, b'"']).unwrap_err(),
            CanonError::InvalidUtf8
        );
    }

    #[test]
    fn bounds_depth_and_size() {
        let ok = format!("{}1{}", "[".repeat(MAX_DEPTH), "]".repeat(MAX_DEPTH));
        assert!(parse(ok.as_bytes()).is_ok());
        let deep = format!(
            "{}1{}",
            "[".repeat(MAX_DEPTH + 1),
            "]".repeat(MAX_DEPTH + 1)
        );
        assert_eq!(
            parse(deep.as_bytes()).unwrap_err(),
            CanonError::DepthExceeded
        );
        assert_eq!(
            parse(&vec![b' '; MAX_INPUT_BYTES + 1]).unwrap_err(),
            CanonError::TooLarge
        );
    }

    #[test]
    fn sha256_known_vector() {
        assert_eq!(
            sha256_hex(b"abc"),
            "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
        );
    }

    #[test]
    fn shared_vectors_match_every_binding() {
        let doc = parse(include_bytes!(
            "../../../../schemas/smoke/canon-vectors.json"
        ))
        .unwrap();
        let Some(Value::Array(cases)) = doc.get("cases") else {
            panic!("cases")
        };
        assert!(cases.len() >= 20);
        for case in cases {
            let id = case.get("id").unwrap().as_str().unwrap();
            let input = case.get("input").unwrap().as_str().unwrap();
            match (case.get("canonical"), case.get("error")) {
                (Some(Value::Str(want)), None) => {
                    assert_eq!(
                        canonicalize(input.as_bytes()).unwrap(),
                        want.as_bytes(),
                        "{id}"
                    );
                }
                (None, Some(Value::Str(code))) => {
                    assert_eq!(parse(input.as_bytes()).unwrap_err().code(), code, "{id}");
                }
                _ => panic!("malformed vector {id}"),
            }
        }
    }
}
