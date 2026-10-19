//! A captured observation as the recognizer sees it: an id-stable tree with explicit relations and boundaries.

use std::collections::HashMap;
use std::fmt;
use vsf_canon::{Value, sha256_hex, to_canonical};

pub const F_VISIBLE: u32 = 1;
pub const F_INTERACTIVE: u32 = 2;
pub const F_EDITABLE: u32 = 4;
pub const F_REDACTED: u32 = 8;
pub const F_DISABLED: u32 = 16;
pub const F_OPAQUE: u32 = 32;
pub const F_RTL: u32 = 64;
pub const F_ACCOUNT: u32 = 128;
pub const F_CREDENTIAL: u32 = 256;

pub const MAX_NODES: usize = 2_000_000;
const MAX_TEXT: usize = 4096;

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

pub fn err(code: &'static str, detail: impl Into<String>) -> Error {
    Error {
        code,
        detail: detail.into(),
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, PartialOrd, Ord)]
pub enum RelKind {
    Label,
    LabelledBy,
    DescribedBy,
}

impl RelKind {
    fn parse(s: &str) -> Option<RelKind> {
        match s {
            "label" => Some(RelKind::Label),
            "labelledby" => Some(RelKind::LabelledBy),
            "describedby" => Some(RelKind::DescribedBy),
            _ => None,
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Scope {
    pub kind: String,
    pub origin: Option<String>,
    pub mode: Option<String>,
    pub opaque: Option<String>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Node {
    pub id: u32,
    pub parent: Option<usize>,
    pub tag: String,
    pub role: Option<String>,
    pub name: Option<String>,
    pub text: Option<String>,
    pub flags: u32,
    pub rels: Vec<(RelKind, usize)>,
    pub scope: Option<Scope>,
}

impl Node {
    pub fn has(&self, flag: u32) -> bool {
        self.flags & flag != 0
    }
    pub fn visible(&self) -> bool {
        self.has(F_VISIBLE)
    }
}

#[derive(Debug, Clone)]
pub struct Observation {
    pub nodes: Vec<Node>,
    pub children: Vec<Vec<usize>>,
    pub digest: String,
    pub dangling_rels: usize,
    by_id: HashMap<u32, usize>,
}

fn opt_str(v: &Value, key: &str, path: &str) -> Result<Option<String>, Error> {
    match v.get(key) {
        None | Some(Value::Null) => Ok(None),
        Some(Value::Str(s)) if s.chars().count() <= MAX_TEXT => Ok(Some(s.clone())),
        Some(_) => Err(err("obs.bad_field", format!("{path}/{key}"))),
    }
}

fn int(v: &Value, key: &str, path: &str) -> Result<i64, Error> {
    v.get(key)
        .and_then(Value::as_int)
        .ok_or_else(|| err("obs.bad_field", format!("{path}/{key}")))
}

impl Observation {
    pub fn from_json(bytes: &[u8]) -> Result<Observation, Error> {
        let root = vsf_canon::parse(bytes).map_err(|e| err("obs.parse", e.code()))?;
        if root.get("schema").and_then(Value::as_str) != Some("vsf-observation-snapshot-v1") {
            return Err(err(
                "obs.bad_schema",
                "expected vsf-observation-snapshot-v1",
            ));
        }
        let Some(Value::Array(items)) = root.get("nodes") else {
            return Err(err("obs.bad_field", "/nodes"));
        };
        if items.len() > MAX_NODES {
            return Err(err("obs.too_large", items.len().to_string()));
        }
        let mut by_id: HashMap<u32, usize> = HashMap::with_capacity(items.len());
        let mut raw = Vec::with_capacity(items.len());
        for (i, it) in items.iter().enumerate() {
            let p = format!("/nodes/{i}");
            let id = u32::try_from(int(it, "id", &p)?)
                .map_err(|_| err("obs.bad_field", format!("{p}/id")))?;
            if by_id.insert(id, i).is_some() {
                return Err(err("obs.duplicate_id", id.to_string()));
            }
            raw.push((it, id));
        }
        let mut nodes = Vec::with_capacity(items.len());
        let mut dangling = 0;
        for (i, (it, id)) in raw.iter().enumerate() {
            let p = format!("/nodes/{i}");
            let parent_id = int(it, "parent", &p)?;
            let parent = if parent_id == -1 {
                None
            } else {
                let pid = u32::try_from(parent_id)
                    .map_err(|_| err("obs.bad_field", format!("{p}/parent")))?;
                let idx = *by_id
                    .get(&pid)
                    .ok_or_else(|| err("obs.unknown_parent", format!("{p}: {pid}")))?;
                if idx >= i {
                    return Err(err("obs.not_preorder", p));
                }
                Some(idx)
            };
            let tag = it
                .get("tag")
                .and_then(Value::as_str)
                .filter(|t| !t.is_empty() && t.len() <= 64)
                .ok_or_else(|| err("obs.bad_field", format!("{p}/tag")))?
                .to_string();
            let flags = u32::try_from(int(it, "flags", &p)?)
                .map_err(|_| err("obs.bad_field", format!("{p}/flags")))?;
            let mut rels = Vec::new();
            if let Some(Value::Array(list)) = it.get("rels") {
                for r in list {
                    let Value::Array(pair) = r else {
                        return Err(err("obs.bad_field", format!("{p}/rels")));
                    };
                    let (Some(kind), Some(target)) = (
                        pair.first()
                            .and_then(Value::as_str)
                            .and_then(RelKind::parse),
                        pair.get(1)
                            .and_then(Value::as_int)
                            .and_then(|t| u32::try_from(t).ok()),
                    ) else {
                        return Err(err("obs.bad_field", format!("{p}/rels")));
                    };
                    match by_id.get(&target) {
                        Some(&t) => rels.push((kind, t)),
                        None => dangling += 1,
                    }
                }
            }
            let scope = match it.get("scope") {
                None | Some(Value::Null) => None,
                Some(s) => Some(Scope {
                    kind: s
                        .get("kind")
                        .and_then(Value::as_str)
                        .unwrap_or("")
                        .to_string(),
                    origin: opt_str(s, "origin", &p)?,
                    mode: opt_str(s, "mode", &p)?,
                    opaque: opt_str(s, "opaque", &p)?,
                }),
            };
            nodes.push(Node {
                id: *id,
                parent,
                tag,
                role: opt_str(it, "role", &p)?,
                name: opt_str(it, "name", &p)?,
                text: opt_str(it, "text", &p)?,
                flags,
                rels,
                scope,
            });
        }
        let mut children = vec![Vec::new(); nodes.len()];
        for (i, n) in nodes.iter().enumerate() {
            if let Some(p) = n.parent {
                children[p].push(i);
            }
        }
        Ok(Observation {
            nodes,
            children,
            digest: format!("sha256:{}", sha256_hex(&to_canonical(&root))),
            dangling_rels: dangling,
            by_id,
        })
    }

    /// Origin of the root document, when the capture recorded one.
    pub fn origin(&self) -> Option<&str> {
        self.nodes.first()?.scope.as_ref()?.origin.as_deref()
    }

    pub fn index_of(&self, id: u32) -> Option<usize> {
        self.by_id.get(&id).copied()
    }

    pub fn len(&self) -> usize {
        self.nodes.len()
    }

    pub fn is_empty(&self) -> bool {
        self.nodes.is_empty()
    }

    /// Ancestors from the parent up to the root.
    pub fn ancestors(&self, mut i: usize) -> impl Iterator<Item = usize> + '_ {
        std::iter::from_fn(move || {
            let p = self.nodes[i].parent?;
            i = p;
            Some(p)
        })
    }

    /// Position of `i` among its parent's children, if it has a parent.
    pub fn sibling_pos(&self, i: usize) -> Option<(usize, usize)> {
        let p = self.nodes[i].parent?;
        self.children[p]
            .iter()
            .position(|&c| c == i)
            .map(|k| (p, k))
    }

    /// Everything beneath `i` in preorder, `i` excluded.
    pub fn descendants(&self, i: usize) -> Vec<usize> {
        let mut out = Vec::new();
        let mut stack: Vec<usize> = self.children[i].iter().rev().copied().collect();
        while let Some(n) = stack.pop() {
            out.push(n);
            stack.extend(self.children[n].iter().rev().copied());
        }
        out
    }

    pub fn from_nodes(nodes: Vec<Node>) -> Observation {
        let by_id = nodes.iter().enumerate().map(|(i, n)| (n.id, i)).collect();
        let mut children = vec![Vec::new(); nodes.len()];
        for (i, n) in nodes.iter().enumerate() {
            if let Some(p) = n.parent {
                children[p].push(i);
            }
        }
        let digest = format!("sha256:{}", sha256_hex(format!("{nodes:?}").as_bytes()));
        Observation {
            nodes,
            children,
            digest,
            dangling_rels: 0,
            by_id,
        }
    }
}
