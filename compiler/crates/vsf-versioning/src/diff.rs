//! Deterministic semantic diff of two documents of one family, with a conservative change class per path.

use crate::{Res, refuse};
use std::collections::{BTreeMap, BTreeSet};
use vsf_canon::{Value, sha256_hex, to_canonical};
use vsf_ir::Body;
use vsf_ir::doc::document_digest_at;

/// Ordered by how much review a change needs; the diff takes the maximum.
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub enum Class {
    Cosmetic,
    Additive,
    Breaking,
    Permission,
    Unknown,
}

impl Class {
    pub fn as_str(self) -> &'static str {
        match self {
            Class::Cosmetic => "cosmetic",
            Class::Additive => "additive",
            Class::Breaking => "breaking",
            Class::Permission => "permission",
            Class::Unknown => "unknown",
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord)]
pub enum Op {
    Added,
    Removed,
    Modified,
}

impl Op {
    pub fn as_str(self) -> &'static str {
        match self {
            Op::Added => "added",
            Op::Removed => "removed",
            Op::Modified => "modified",
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Change {
    /// JSON pointer into the body, with keyed list elements addressed by key.
    pub path: String,
    /// The same path with keys replaced by `[]` / `{}`; what the rules matched.
    pub pattern: String,
    pub op: Op,
    pub class: Class,
    pub before: Option<String>,
    pub after: Option<String>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Diff {
    pub family: &'static str,
    pub from: String,
    pub to: String,
    pub class: Option<Class>,
    pub changes: Vec<Change>,
}

type Rule = (&'static str, Option<Op>, Class);

/// Lists whose elements are matched by key rather than position: pattern, key fields.
const KEYED: &[(&str, &[&str])] = &[
    ("entities", &["id"]),
    ("entities[]/fields", &["id"]),
    ("relationships", &["id"]),
    ("actions", &["id"]),
    ("state_machines", &["id"]),
    ("state_machines[]/states", &["id"]),
    ("state_machines[]/transitions", &["from", "to", "action"]),
    ("evidence", &["id"]),
    ("claims", &["id"]),
    ("witnesses", &["id"]),
    ("components", &["id"]),
    ("constraints", &["id"]),
    ("regions", &["id"]),
];

/// Objects whose keys are names, not fields.
const NAMED: &[&str] = &["inputs", "outputs", "operations"];

const SEMANTIC: &[Rule] = &[
    ("source_app_id", None, Class::Breaking),
    ("source_variant_id", None, Class::Breaking),
    ("observations", None, Class::Cosmetic),
    ("compiler_version", None, Class::Cosmetic),
    ("primitive_set", None, Class::Unknown),
    ("entities[]/label", None, Class::Cosmetic),
    ("entities[]/evidence", None, Class::Cosmetic),
    ("entities[]/fields[]/name", None, Class::Cosmetic),
    ("entities[]/fields[]/evidence", None, Class::Cosmetic),
    ("entities[]/fields[]", Some(Op::Added), Class::Additive),
    ("entities[]/fields[]", Some(Op::Removed), Class::Breaking),
    ("entities[]/fields[]/*", None, Class::Breaking),
    ("entities[]", Some(Op::Added), Class::Additive),
    ("entities[]", Some(Op::Removed), Class::Breaking),
    ("entities[]/*", None, Class::Breaking),
    ("relationships[]/evidence", None, Class::Cosmetic),
    ("relationships[]", Some(Op::Added), Class::Additive),
    ("relationships[]", Some(Op::Removed), Class::Breaking),
    ("relationships[]/*", None, Class::Breaking),
    ("actions[]/label", None, Class::Cosmetic),
    ("actions[]/evidence", None, Class::Cosmetic),
    ("actions[]/effect/*", None, Class::Permission),
    ("actions[]/source_confirmation", None, Class::Permission),
    ("actions[]", Some(Op::Added), Class::Additive),
    ("actions[]", Some(Op::Removed), Class::Breaking),
    ("actions[]/*", None, Class::Breaking),
    ("state_machines[]/states[]/label", None, Class::Cosmetic),
    (
        "state_machines[]/states[]",
        Some(Op::Added),
        Class::Additive,
    ),
    (
        "state_machines[]/transitions[]",
        Some(Op::Added),
        Class::Additive,
    ),
    ("state_machines[]", Some(Op::Added), Class::Additive),
    ("state_machines[]/*", None, Class::Breaking),
    ("state_machines[]", Some(Op::Removed), Class::Breaking),
    ("evidence[]", None, Class::Cosmetic),
    ("evidence[]/*", None, Class::Cosmetic),
    ("claims[]/supports", None, Class::Cosmetic),
    ("claims[]/contradicts", None, Class::Cosmetic),
    ("claims[]", Some(Op::Added), Class::Additive),
    ("claims[]", Some(Op::Removed), Class::Breaking),
    ("claims[]/*", None, Class::Breaking),
];

const OPERATION: &[Rule] = &[
    ("contract_version", None, Class::Cosmetic),
    ("provenance/*", None, Class::Cosmetic),
    ("operation_id", None, Class::Breaking),
    ("lifecycle", None, Class::Permission),
    ("source/capabilities", Some(Op::Added), Class::Additive),
    ("source/*", None, Class::Breaking),
    ("inputs/{}/description", None, Class::Cosmetic),
    ("inputs/{}", Some(Op::Added), Class::Additive),
    ("inputs/{}", None, Class::Breaking),
    ("inputs/{}/*", None, Class::Breaking),
    ("outputs/{}/description", None, Class::Cosmetic),
    ("outputs/{}", Some(Op::Added), Class::Additive),
    ("outputs/{}", None, Class::Breaking),
    ("outputs/{}/*", None, Class::Breaking),
    ("effect/*", None, Class::Permission),
    ("evidence_effects", None, Class::Permission),
    ("freshness/*", None, Class::Permission),
    ("verification/*", None, Class::Permission),
    ("strategies", None, Class::Permission),
    ("on_ambiguity", None, Class::Permission),
    ("authorization/*", None, Class::Permission),
    ("witnesses", None, Class::Permission),
    ("witnesses[]", None, Class::Permission),
    ("witnesses[]/*", None, Class::Permission),
    ("uncertainty", None, Class::Breaking),
];

const PRESENTATION: &[Rule] = &[
    ("presentation_version", None, Class::Cosmetic),
    ("semantic", None, Class::Breaking),
    ("locale", None, Class::Cosmetic),
    ("direction", None, Class::Cosmetic),
    ("theme/*", None, Class::Cosmetic),
    ("components[]/children", None, Class::Cosmetic),
    ("components[]/accessibility/name", None, Class::Cosmetic),
    ("components[]/accessibility/shortcut", None, Class::Cosmetic),
    ("components[]/binding/action", None, Class::Permission),
    ("components[]/binding", Some(Op::Added), Class::Permission),
    ("components[]/binding", Some(Op::Removed), Class::Permission),
    ("components[]/binding/*", None, Class::Breaking),
    ("components[]", Some(Op::Added), Class::Additive),
    ("components[]", Some(Op::Removed), Class::Breaking),
    ("components[]/*", None, Class::Breaking),
    ("constraints[]", Some(Op::Added), Class::Additive),
    ("constraints[]", Some(Op::Removed), Class::Breaking),
    ("constraints[]/subjects", None, Class::Breaking),
    ("constraints[]/*", None, Class::Breaking),
    ("regions[]/components", None, Class::Cosmetic),
    ("regions[]", Some(Op::Added), Class::Additive),
    ("regions[]", Some(Op::Removed), Class::Breaking),
    ("regions[]/*", None, Class::Breaking),
];

fn rules_for(family: &str) -> Option<&'static [Rule]> {
    match family {
        "semantic" => Some(SEMANTIC),
        "operation" => Some(OPERATION),
        "presentation" => Some(PRESENTATION),
        _ => None,
    }
}

fn pattern_matches(rule: &str, pattern: &str) -> bool {
    match rule.strip_suffix("/*") {
        Some(prefix) => {
            pattern.len() > prefix.len()
                && pattern.starts_with(prefix)
                && pattern.as_bytes()[prefix.len()] == b'/'
        }
        None => rule == pattern,
    }
}

fn classify(rules: &[Rule], pattern: &str, op: Op) -> Class {
    rules
        .iter()
        .find(|(r, o, _)| pattern_matches(r, pattern) && o.is_none_or(|x| x == op))
        .map_or(Class::Unknown, |(_, _, c)| *c)
}

fn key_text(v: &Value, fields: &[&str]) -> Option<String> {
    let parts: Option<Vec<String>> = fields
        .iter()
        .map(|f| match v.get(f) {
            Some(Value::Str(s)) => Some(s.clone()),
            Some(Value::Null) | None if *f == "action" => Some(String::new()),
            _ => None,
        })
        .collect();
    parts.map(|p| p.join("\u{1f}"))
}

fn show(v: &Value) -> String {
    match v {
        Value::Str(s) if s.chars().count() <= 120 => s.clone(),
        Value::Int(i) => i.to_string(),
        Value::Bool(b) => b.to_string(),
        Value::Null => "null".into(),
        other => format!("sha256:{}", sha256_hex(&to_canonical(other))),
    }
}

struct Walk<'a> {
    rules: &'a [Rule],
    out: Vec<Change>,
}

impl Walk<'_> {
    fn push(
        &mut self,
        path: &str,
        pattern: &str,
        op: Op,
        before: Option<&Value>,
        after: Option<&Value>,
    ) {
        let mut class = classify(self.rules, pattern, op);
        if pattern == "inputs/{}"
            && op == Op::Added
            && after.and_then(|v| v.get("required")) == Some(&Value::Bool(true))
        {
            class = Class::Breaking;
        }
        self.out.push(Change {
            path: path.to_string(),
            pattern: pattern.to_string(),
            op,
            class,
            before: before.map(show),
            after: after.map(show),
        });
    }

    fn walk(&mut self, a: &Value, b: &Value, path: &str, pattern: &str) {
        if a == b {
            return;
        }
        match (a, b) {
            (Value::Object(x), Value::Object(y)) => {
                let named = NAMED.contains(&pattern.rsplit('/').next().unwrap_or(""));
                let keys: BTreeSet<&str> =
                    x.iter().chain(y.iter()).map(|(k, _)| k.as_str()).collect();
                for k in keys {
                    let (va, vb) = (a.get(k), b.get(k));
                    let child_path = format!("{path}/{k}");
                    let child_pat = if named {
                        format!("{pattern}/{{}}")
                    } else if pattern.is_empty() {
                        k.to_string()
                    } else {
                        format!("{pattern}/{k}")
                    };
                    match (va, vb) {
                        (Some(l), Some(r)) => self.walk(l, r, &child_path, &child_pat),
                        (Some(l), None) => {
                            self.push(&child_path, &child_pat, Op::Removed, Some(l), None)
                        }
                        (None, Some(r)) => {
                            self.push(&child_path, &child_pat, Op::Added, None, Some(r))
                        }
                        (None, None) => {}
                    }
                }
            }
            (Value::Array(x), Value::Array(y)) => self.walk_list(x, y, path, pattern),
            _ => self.push(path, pattern, Op::Modified, Some(a), Some(b)),
        }
    }

    fn walk_list(&mut self, x: &[Value], y: &[Value], path: &str, pattern: &str) {
        if let Some((_, key)) = KEYED.iter().find(|(p, _)| *p == pattern) {
            let index = |items: &[Value]| -> Option<BTreeMap<String, usize>> {
                let mut m = BTreeMap::new();
                for (i, v) in items.iter().enumerate() {
                    if m.insert(key_text(v, key)?, i).is_some() {
                        return None;
                    }
                }
                Some(m)
            };
            if let (Some(ix), Some(iy)) = (index(x), index(y)) {
                let keys: BTreeSet<&String> = ix.keys().chain(iy.keys()).collect();
                let child_pat = format!("{pattern}[]");
                for k in keys {
                    let label = k.replace('\u{1f}', ",");
                    let child_path = format!("{path}[{label}]");
                    match (ix.get(k), iy.get(k)) {
                        (Some(i), Some(j)) => self.walk(&x[*i], &y[*j], &child_path, &child_pat),
                        (Some(i), None) => {
                            self.push(&child_path, &child_pat, Op::Removed, Some(&x[*i]), None)
                        }
                        (None, Some(j)) => {
                            self.push(&child_path, &child_pat, Op::Added, None, Some(&y[*j]))
                        }
                        (None, None) => {}
                    }
                }
                return;
            }
            // Ambiguous or missing keys: nothing safe to say about individual elements.
            self.push(
                path,
                pattern,
                Op::Modified,
                Some(&Value::Array(x.to_vec())),
                Some(&Value::Array(y.to_vec())),
            );
            return;
        }
        self.push(
            path,
            pattern,
            Op::Modified,
            Some(&Value::Array(x.to_vec())),
            Some(&Value::Array(y.to_vec())),
        );
    }
}

/// Same-family documents only; each version is the one the document was written under, so digests stay stable.
pub fn diff(
    from: &Body,
    from_version: (u32, u32, u32),
    to: &Body,
    to_version: (u32, u32, u32),
) -> Res<Diff> {
    let family = from.family();
    if family != to.family() {
        return Err(refuse(
            "diff.family_mismatch",
            format!("{family} vs {}", to.family()),
        ));
    }
    let rules = rules_for(family).ok_or_else(|| refuse("diff.unsupported_family", family))?;
    let mut w = Walk {
        rules,
        out: Vec::new(),
    };
    w.walk(&from.encode(), &to.encode(), "", "");
    let mut changes = w.out;
    changes.sort_by(|a, b| a.path.cmp(&b.path).then(a.op.cmp(&b.op)));
    Ok(Diff {
        family,
        from: document_digest_at(from, from_version)
            .ok_or_else(|| refuse("diff.unknown_version", ""))?,
        to: document_digest_at(to, to_version).ok_or_else(|| refuse("diff.unknown_version", ""))?,
        class: changes.iter().map(|c| c.class).max(),
        changes,
    })
}
