use std::collections::BTreeSet;

/// Allocates identifiers that satisfy the IR identifier rule and never repeat within one document.
#[derive(Default)]
pub struct Ids {
    taken: BTreeSet<String>,
}

fn slug(raw: &str) -> String {
    let mut out = String::new();
    for c in raw.chars() {
        if c.is_ascii_alphanumeric() {
            out.push(c.to_ascii_lowercase());
        } else if !out.is_empty() && !out.ends_with('_') {
            out.push('_');
        }
    }
    out.trim_end_matches('_').to_string()
}

pub fn slug_of(raw: &str) -> String {
    slug(raw)
}

impl Ids {
    pub fn alloc(&mut self, prefix: &str, raw: &str) -> String {
        let mut base = slug(raw);
        if base.is_empty() {
            base = "item".to_string();
        }
        let head = format!("{prefix}_{base}");
        let head: String = head.chars().take(56).collect();
        let head = head.trim_end_matches('_').to_string();
        let mut candidate = head.clone();
        let mut n = 2u32;
        while !self.taken.insert(candidate.clone()) {
            candidate = format!("{head}_{n}");
            n += 1;
        }
        candidate
    }
}
