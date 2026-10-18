#![cfg_attr(test, allow(clippy::unwrap_used, clippy::expect_used, clippy::panic))]

//! Compact observation pack: a canonical, bounds-checked binary snapshot of a DOM-like tree.

use std::collections::BTreeMap;
use vsf_canon::Value;

pub const MAGIC: &[u8; 4] = b"VSFS";
pub const VERSION: u16 = 1;
pub const HEADER_LEN: usize = 24;
pub const NONE: u32 = u32::MAX;
pub const MAX_PACK_BYTES: usize = 64 * 1024 * 1024;
pub const MAX_NODES: u32 = 2_000_000;
pub const MAX_STRINGS: u32 = 4_000_000;
pub const FLAG_MASK: u32 = 0b1111;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PackError {
    Empty,
    Truncated,
    BadMagic,
    BadVersion,
    BadFlags,
    ReservedNonzero,
    TooLarge,
    LengthMismatch,
    OffsetsInvalid,
    InvalidUtf8,
    PaddingNonzero,
    ParentInvalid,
    StringIndexOutOfRange,
    NodeFlagsInvalid,
    NotCanonical,
}

impl PackError {
    pub fn code(self) -> &'static str {
        match self {
            Self::Empty => "pack.empty",
            Self::Truncated => "pack.truncated",
            Self::BadMagic => "pack.bad_magic",
            Self::BadVersion => "pack.bad_version",
            Self::BadFlags => "pack.bad_flags",
            Self::ReservedNonzero => "pack.reserved_nonzero",
            Self::TooLarge => "pack.too_large",
            Self::LengthMismatch => "pack.length_mismatch",
            Self::OffsetsInvalid => "pack.offsets_invalid",
            Self::InvalidUtf8 => "pack.invalid_utf8",
            Self::PaddingNonzero => "pack.padding_nonzero",
            Self::ParentInvalid => "pack.parent_invalid",
            Self::StringIndexOutOfRange => "pack.string_index_out_of_range",
            Self::NodeFlagsInvalid => "pack.node_flags_invalid",
            Self::NotCanonical => "pack.not_canonical",
        }
    }
}

impl std::fmt::Display for PackError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(self.code())
    }
}

impl std::error::Error for PackError {}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct Node {
    pub parent: u32,
    pub tag: u32,
    pub role: u32,
    pub name: u32,
    pub text: u32,
    pub flags: u32,
}

#[derive(Debug, Clone, PartialEq, Eq, Default)]
pub struct Snapshot {
    pub strings: Vec<String>,
    pub nodes: Vec<Node>,
}

/// Interns strings in first-use order so every logical snapshot has exactly one encoding.
#[derive(Default)]
pub struct Builder {
    snapshot: Snapshot,
    index: BTreeMap<String, u32>,
}

impl Builder {
    pub fn new() -> Self {
        Self::default()
    }

    fn intern(&mut self, s: &str) -> u32 {
        if let Some(&i) = self.index.get(s) {
            return i;
        }
        let i = self.snapshot.strings.len() as u32;
        self.snapshot.strings.push(s.to_string());
        self.index.insert(s.to_string(), i);
        i
    }

    fn optional(&mut self, s: Option<&str>) -> u32 {
        s.map_or(NONE, |s| self.intern(s))
    }

    pub fn node(
        &mut self,
        parent: u32,
        tag: &str,
        role: Option<&str>,
        name: Option<&str>,
        text: Option<&str>,
        flags: u32,
    ) -> u32 {
        let tag = self.intern(tag);
        let role = self.optional(role);
        let name = self.optional(name);
        let text = self.optional(text);
        self.snapshot.nodes.push(Node {
            parent,
            tag,
            role,
            name,
            text,
            flags,
        });
        (self.snapshot.nodes.len() - 1) as u32
    }

    pub fn finish(self) -> Snapshot {
        self.snapshot
    }
}

fn pad_len(n: usize) -> usize {
    (4 - n % 4) % 4
}

pub fn encode(snapshot: &Snapshot) -> Vec<u8> {
    let blob_len: usize = snapshot.strings.iter().map(String::len).sum();
    let mut out = Vec::with_capacity(
        HEADER_LEN + 4 * (snapshot.strings.len() + 1) + blob_len + 3 + 24 * snapshot.nodes.len(),
    );
    out.extend_from_slice(MAGIC);
    out.extend_from_slice(&VERSION.to_le_bytes());
    out.extend_from_slice(&0u16.to_le_bytes());
    out.extend_from_slice(&(snapshot.strings.len() as u32).to_le_bytes());
    out.extend_from_slice(&(blob_len as u32).to_le_bytes());
    out.extend_from_slice(&(snapshot.nodes.len() as u32).to_le_bytes());
    out.extend_from_slice(&0u32.to_le_bytes());
    let mut offset = 0u32;
    out.extend_from_slice(&offset.to_le_bytes());
    for s in &snapshot.strings {
        offset += s.len() as u32;
        out.extend_from_slice(&offset.to_le_bytes());
    }
    for s in &snapshot.strings {
        out.extend_from_slice(s.as_bytes());
    }
    out.resize(out.len() + pad_len(blob_len), 0);
    for n in &snapshot.nodes {
        for v in [n.parent, n.tag, n.role, n.name, n.text, n.flags] {
            out.extend_from_slice(&v.to_le_bytes());
        }
    }
    out
}

fn u32_at(b: &[u8], at: usize) -> u32 {
    u32::from_le_bytes([b[at], b[at + 1], b[at + 2], b[at + 3]])
}

pub fn decode(bytes: &[u8]) -> Result<Snapshot, PackError> {
    if bytes.is_empty() {
        return Err(PackError::Empty);
    }
    if bytes.len() > MAX_PACK_BYTES {
        return Err(PackError::TooLarge);
    }
    if bytes.len() < HEADER_LEN {
        return Err(PackError::Truncated);
    }
    if &bytes[0..4] != MAGIC {
        return Err(PackError::BadMagic);
    }
    if u16::from_le_bytes([bytes[4], bytes[5]]) != VERSION {
        return Err(PackError::BadVersion);
    }
    if u16::from_le_bytes([bytes[6], bytes[7]]) != 0 {
        return Err(PackError::BadFlags);
    }
    let string_count = u32_at(bytes, 8);
    let blob_len = u32_at(bytes, 12) as usize;
    let node_count = u32_at(bytes, 16);
    if u32_at(bytes, 20) != 0 {
        return Err(PackError::ReservedNonzero);
    }
    if string_count > MAX_STRINGS || node_count > MAX_NODES || blob_len > MAX_PACK_BYTES {
        return Err(PackError::TooLarge);
    }
    let offsets_len = (string_count as usize + 1) * 4;
    let pad = pad_len(blob_len);
    let expected = HEADER_LEN + offsets_len + blob_len + pad + node_count as usize * 24;
    match bytes.len().cmp(&expected) {
        std::cmp::Ordering::Less => return Err(PackError::Truncated),
        std::cmp::Ordering::Greater => return Err(PackError::LengthMismatch),
        std::cmp::Ordering::Equal => {}
    }
    let offsets_at = HEADER_LEN;
    let blob_at = offsets_at + offsets_len;
    let nodes_at = blob_at + blob_len + pad;
    if bytes[blob_at + blob_len..nodes_at].iter().any(|&b| b != 0) {
        return Err(PackError::PaddingNonzero);
    }
    let offset = |i: usize| u32_at(bytes, offsets_at + 4 * i) as usize;
    if offset(0) != 0 || offset(string_count as usize) != blob_len {
        return Err(PackError::OffsetsInvalid);
    }
    let mut strings = Vec::with_capacity(string_count as usize);
    for i in 0..string_count as usize {
        let (a, b) = (offset(i), offset(i + 1));
        if a > b || b > blob_len {
            return Err(PackError::OffsetsInvalid);
        }
        let s = std::str::from_utf8(&bytes[blob_at + a..blob_at + b])
            .map_err(|_| PackError::InvalidUtf8)?;
        strings.push(s.to_string());
    }
    let mut nodes = Vec::with_capacity(node_count as usize);
    for i in 0..node_count as usize {
        let at = nodes_at + i * 24;
        let node = Node {
            parent: u32_at(bytes, at),
            tag: u32_at(bytes, at + 4),
            role: u32_at(bytes, at + 8),
            name: u32_at(bytes, at + 12),
            text: u32_at(bytes, at + 16),
            flags: u32_at(bytes, at + 20),
        };
        if node.parent != NONE && node.parent as usize >= i {
            return Err(PackError::ParentInvalid);
        }
        if node.tag == NONE
            || [node.tag, node.role, node.name, node.text]
                .iter()
                .any(|&s| s != NONE && s >= string_count)
        {
            return Err(PackError::StringIndexOutOfRange);
        }
        if node.flags & !FLAG_MASK != 0 {
            return Err(PackError::NodeFlagsInvalid);
        }
        nodes.push(node);
    }
    let snapshot = Snapshot { strings, nodes };
    if !is_canonical(&snapshot) {
        return Err(PackError::NotCanonical);
    }
    Ok(snapshot)
}

/// Canonical form: strings appear exactly once, in first-use order over (tag, role, name, text) per node.
fn is_canonical(s: &Snapshot) -> bool {
    let mut next = 0u32;
    for n in &s.nodes {
        for idx in [n.tag, n.role, n.name, n.text] {
            if idx == NONE {
                continue;
            }
            match idx.cmp(&next) {
                std::cmp::Ordering::Less => {}
                std::cmp::Ordering::Equal => next += 1,
                std::cmp::Ordering::Greater => return false,
            }
        }
    }
    next as usize == s.strings.len() && unique(&s.strings)
}

fn unique(strings: &[String]) -> bool {
    let mut seen = std::collections::BTreeSet::new();
    strings.iter().all(|s| seen.insert(s.as_str()))
}

pub const CLASSES: [&str; 11] = [
    "button", "link", "textbox", "checkbox", "heading", "row", "cell", "table", "form", "list",
    "other",
];

/// Deliberately coarse stand-in for recognition: maps role or tag to a fixed class vocabulary.
pub fn classify(tag: &str, role: Option<&str>) -> &'static str {
    let key = role.unwrap_or(tag);
    match key {
        "button" => "button",
        "link" | "a" => "link",
        "textbox" | "input" | "textarea" => "textbox",
        "checkbox" => "checkbox",
        "heading" | "h1" | "h2" | "h3" | "h4" | "h5" | "h6" => "heading",
        "row" | "tr" => "row",
        "cell" | "columnheader" | "td" | "th" => "cell",
        "table" => "table",
        "form" => "form",
        "list" | "ul" | "ol" => "list",
        _ => "other",
    }
}

pub fn summary(snapshot: &Snapshot, digest: &str) -> Value {
    let mut depth = vec![0i64; snapshot.nodes.len()];
    let mut max_depth = 0i64;
    let mut classes: BTreeMap<&str, i64> = BTreeMap::new();
    for (i, n) in snapshot.nodes.iter().enumerate() {
        if n.parent != NONE {
            depth[i] = depth[n.parent as usize] + 1;
        }
        max_depth = max_depth.max(depth[i]);
        let tag = snapshot.strings[n.tag as usize].as_str();
        let role = (n.role != NONE).then(|| snapshot.strings[n.role as usize].as_str());
        *classes.entry(classify(tag, role)).or_insert(0) += 1;
    }
    Value::Object(vec![
        ("digest".to_string(), Value::Str(digest.to_string())),
        (
            "node_count".to_string(),
            Value::Int(snapshot.nodes.len() as i64),
        ),
        (
            "string_count".to_string(),
            Value::Int(snapshot.strings.len() as i64),
        ),
        ("max_depth".to_string(), Value::Int(max_depth)),
        (
            "classes".to_string(),
            Value::Object(
                classes
                    .into_iter()
                    .map(|(k, v)| (k.to_string(), Value::Int(v)))
                    .collect(),
            ),
        ),
    ])
}

/// Validates a pack and returns its canonical JSON summary; the digest covers the exact pack bytes.
pub fn analyze(bytes: &[u8]) -> Result<Vec<u8>, PackError> {
    let snapshot = decode(bytes)?;
    let digest = vsf_canon::sha256_hex(bytes);
    Ok(vsf_canon::to_canonical(&summary(&snapshot, &digest)))
}

#[cfg(test)]
mod tests {
    use super::*;

    fn sample() -> Snapshot {
        let mut b = Builder::new();
        let root = b.node(NONE, "html", None, None, None, 1);
        let form = b.node(root, "form", None, Some("Order"), None, 1);
        b.node(form, "input", Some("textbox"), Some("Name"), None, 0b111);
        b.node(form, "button", None, None, Some("Submit é"), 0b11);
        b.finish()
    }

    #[test]
    fn roundtrip_is_exact_and_canonical() {
        let bytes = encode(&sample());
        assert_eq!(decode(&bytes).unwrap(), sample());
        assert_eq!(encode(&decode(&bytes).unwrap()), bytes);
    }

    #[test]
    fn empty_snapshot_is_valid_but_empty_input_is_not() {
        let bytes = encode(&Snapshot::default());
        assert_eq!(bytes.len(), HEADER_LEN + 4);
        assert_eq!(decode(&bytes).unwrap(), Snapshot::default());
        assert_eq!(decode(&[]).unwrap_err(), PackError::Empty);
    }

    #[test]
    fn summary_counts_classes_and_depth() {
        let bytes = encode(&sample());
        let json = String::from_utf8(analyze(&bytes).unwrap()).unwrap();
        assert!(
            json.contains(r#""classes":{"button":1,"form":1,"other":1,"textbox":1}"#),
            "{json}"
        );
        assert!(json.contains(r#""max_depth":2"#));
        assert!(json.contains(&vsf_canon::sha256_hex(&bytes)));
    }

    fn corrupt(f: impl FnOnce(&mut Vec<u8>)) -> PackError {
        let mut bytes = encode(&sample());
        f(&mut bytes);
        decode(&bytes).unwrap_err()
    }

    #[test]
    fn rejects_every_malformed_shape() {
        assert_eq!(corrupt(|b| b[0] = b'X'), PackError::BadMagic);
        assert_eq!(corrupt(|b| b[4] = 9), PackError::BadVersion);
        assert_eq!(corrupt(|b| b[6] = 1), PackError::BadFlags);
        assert_eq!(corrupt(|b| b[20] = 1), PackError::ReservedNonzero);
        assert_eq!(
            corrupt(|b| b.truncate(HEADER_LEN - 1)),
            PackError::Truncated
        );
        assert_eq!(corrupt(|b| b.truncate(b.len() - 1)), PackError::Truncated);
        assert_eq!(corrupt(|b| b.push(0)), PackError::LengthMismatch);
        assert_eq!(
            corrupt(|b| b[16..20].copy_from_slice(&u32::MAX.to_le_bytes())),
            PackError::TooLarge
        );
    }

    #[test]
    fn rejects_structural_corruption() {
        let bytes = encode(&sample());
        let nodes_at = bytes.len() - 4 * 24;
        let set = |at: usize, v: u32| {
            let mut b = bytes.clone();
            b[at..at + 4].copy_from_slice(&v.to_le_bytes());
            decode(&b).unwrap_err()
        };
        assert_eq!(set(nodes_at + 24, 1), PackError::ParentInvalid);
        assert_eq!(
            set(nodes_at + 24 + 4, 999),
            PackError::StringIndexOutOfRange
        );
        assert_eq!(set(nodes_at + 4, NONE), PackError::StringIndexOutOfRange);
        assert_eq!(set(nodes_at + 20, 16), PackError::NodeFlagsInvalid);
        assert_eq!(set(HEADER_LEN + 4, 99), PackError::OffsetsInvalid);
    }

    #[test]
    fn rejects_invalid_utf8_padding_and_non_canonical_strings() {
        let bytes = encode(&sample());
        let string_count = u32_at(&bytes, 8) as usize;
        let blob_at = HEADER_LEN + 4 * (string_count + 1);
        let mut bad = bytes.clone();
        bad[blob_at] = 0xff;
        assert_eq!(decode(&bad).unwrap_err(), PackError::InvalidUtf8);
        let blob_len = u32_at(&bytes, 12) as usize;
        if pad_len(blob_len) > 0 {
            let mut padded = bytes.clone();
            padded[blob_at + blob_len] = 1;
            assert_eq!(decode(&padded).unwrap_err(), PackError::PaddingNonzero);
        }
        let mut reordered = sample();
        reordered.strings.swap(0, 1);
        for n in &mut reordered.nodes {
            for f in [&mut n.tag, &mut n.role, &mut n.name, &mut n.text] {
                if *f == 0 {
                    *f = 1;
                } else if *f == 1 {
                    *f = 0;
                }
            }
        }
        assert_eq!(
            decode(&encode(&reordered)).unwrap_err(),
            PackError::NotCanonical
        );
        let mut unused = sample();
        unused.strings.push("unused".to_string());
        assert_eq!(
            decode(&encode(&unused)).unwrap_err(),
            PackError::NotCanonical
        );
    }

    #[test]
    fn never_panics_on_arbitrary_truncations_and_bit_flips() {
        let bytes = encode(&sample());
        for cut in 0..bytes.len() {
            let _ = decode(&bytes[..cut]);
        }
        for i in 0..bytes.len() {
            for bit in 0..8 {
                let mut b = bytes.clone();
                b[i] ^= 1 << bit;
                let _ = decode(&b);
            }
        }
    }
}
