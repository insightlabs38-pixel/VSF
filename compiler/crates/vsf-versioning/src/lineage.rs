//! Content-addressed manifest identity and the lineage graph over it.

use crate::{Res, refuse};
use std::collections::{BTreeMap, BTreeSet};
use vsf_ir::doc::{decode_value, document_digest_at};
use vsf_ir::semantics::validate_version;
use vsf_ir::{Body, Channel, VersionManifest};

pub const MAX_PARENTS: usize = 64;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Node {
    pub id: String,
    /// Ordered: the first parent is the mainline, later ones are merged in.
    pub parents: Vec<String>,
    pub generation: u32,
    pub created_at: i64,
    pub channel: Channel,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Relation {
    Same,
    Ancestor,
    Descendant,
    Diverged { bases: Vec<String> },
    Unrelated,
}

/// The id of a manifest document: its typed digest under the version it was written in, independent of whitespace.
pub fn manifest_id(bytes: &[u8]) -> Res<(String, VersionManifest)> {
    let value = vsf_canon::parse(bytes).map_err(|e| refuse("lineage.undecodable", e.code()))?;
    let (body, version) =
        decode_value(&value).map_err(|e| refuse("lineage.undecodable", e.code))?;
    let Body::Version(m) = &body else {
        return Err(refuse("lineage.not_a_manifest", body.family()));
    };
    let id =
        document_digest_at(&body, version).ok_or_else(|| refuse("lineage.unknown_version", ""))?;
    Ok((id, m.clone()))
}

#[derive(Debug, Clone, Default)]
pub struct Graph {
    nodes: BTreeMap<String, Node>,
    /// Ids whose history was pruned by retention; they act as opaque roots.
    pruned: BTreeSet<String>,
}

/// Rejects a parent map that is cyclic or names a parent that is neither present nor pruned.
pub fn check_acyclic(
    parents: &BTreeMap<String, Vec<String>>,
    pruned: &BTreeSet<String>,
) -> Res<Vec<String>> {
    let mut indegree: BTreeMap<&str, usize> = parents.keys().map(|k| (k.as_str(), 0)).collect();
    let mut children: BTreeMap<&str, Vec<&str>> = BTreeMap::new();
    for (id, ps) in parents {
        for p in ps {
            if pruned.contains(p) && !parents.contains_key(p) {
                continue;
            }
            if !parents.contains_key(p) {
                return Err(refuse("lineage.missing_parent", format!("{id} -> {p}")));
            }
            *indegree.entry(id.as_str()).or_default() += 1;
            children.entry(p.as_str()).or_default().push(id.as_str());
        }
    }
    let mut ready: BTreeSet<&str> = indegree
        .iter()
        .filter(|(_, d)| **d == 0)
        .map(|(k, _)| *k)
        .collect();
    let mut order = Vec::new();
    while let Some(next) = ready.pop_first() {
        order.push(next.to_string());
        for c in children.get(next).into_iter().flatten() {
            if let Some(d) = indegree.get_mut(c) {
                *d -= 1;
                if *d == 0 {
                    ready.insert(c);
                }
            }
        }
    }
    if order.len() != parents.len() {
        let stuck = indegree
            .iter()
            .find(|(_, d)| **d > 0)
            .map_or("", |(k, _)| *k);
        return Err(refuse("lineage.cycle", stuck));
    }
    Ok(order)
}

impl Graph {
    pub fn new() -> Self {
        Self::default()
    }

    pub fn with_pruned(ids: impl IntoIterator<Item = String>) -> Self {
        Self {
            nodes: BTreeMap::new(),
            pruned: ids.into_iter().collect(),
        }
    }

    pub fn len(&self) -> usize {
        self.nodes.len()
    }

    pub fn is_empty(&self) -> bool {
        self.nodes.is_empty()
    }

    pub fn get(&self, id: &str) -> Option<&Node> {
        self.nodes.get(id)
    }

    fn known(&self, id: &str) -> bool {
        self.nodes.contains_key(id) || self.pruned.contains(id)
    }

    /// Adds one manifest document; its parents must already be present. Adding it again is a no-op.
    pub fn add(&mut self, bytes: &[u8]) -> Res<String> {
        let (id, m) = manifest_id(bytes)?;
        if self.nodes.contains_key(&id) {
            return Ok(id);
        }
        let node = self.node_of(&id, &m)?;
        self.nodes.insert(id.clone(), node);
        Ok(id)
    }

    fn node_of(&self, id: &str, m: &VersionManifest) -> Res<Node> {
        if let Some(v) = validate_version(m).first() {
            return Err(refuse("lineage.invalid_manifest", v.code));
        }
        if m.lineage.len() > MAX_PARENTS {
            return Err(refuse(
                "lineage.too_many_parents",
                m.lineage.len().to_string(),
            ));
        }
        let mut seen = BTreeSet::new();
        let mut generation = 0u32;
        let mut parents = Vec::new();
        for p in &m.lineage {
            let p = p.as_str();
            if p == id {
                return Err(refuse("lineage.self_parent", id));
            }
            if !seen.insert(p) {
                return Err(refuse("lineage.duplicate_parent", p));
            }
            if !self.known(p) {
                return Err(refuse("lineage.missing_parent", p));
            }
            if let Some(n) = self.nodes.get(p) {
                generation = generation.max(n.generation + 1);
            } else {
                generation = generation.max(1);
            }
            parents.push(p.to_string());
        }
        Ok(Node {
            id: id.to_string(),
            parents,
            generation,
            created_at: m.created_at.0,
            channel: m.channel,
        })
    }

    /// Rebuilds a graph from stored documents in any order. Every claimed id must equal the content digest.
    pub fn rebuild(docs: &[(String, Vec<u8>)], pruned: &BTreeSet<String>) -> Res<Graph> {
        let mut parsed: BTreeMap<String, VersionManifest> = BTreeMap::new();
        for (claimed, bytes) in docs {
            let (id, m) = manifest_id(bytes)?;
            if &id != claimed {
                return Err(refuse("lineage.id_mismatch", claimed.clone()));
            }
            if parsed.insert(id.clone(), m).is_some() {
                return Err(refuse("lineage.duplicate", id));
            }
        }
        let map: BTreeMap<String, Vec<String>> = parsed
            .iter()
            .map(|(id, m)| {
                (
                    id.clone(),
                    m.lineage.iter().map(|p| p.as_str().to_string()).collect(),
                )
            })
            .collect();
        let order = check_acyclic(&map, pruned)?;
        let mut g = Graph::with_pruned(pruned.iter().cloned());
        for id in order {
            let m = &parsed[&id];
            let node = g.node_of(&id, m)?;
            g.nodes.insert(id, node);
        }
        Ok(g)
    }

    pub fn children(&self, id: &str) -> Vec<&str> {
        self.nodes
            .values()
            .filter(|n| n.parents.iter().any(|p| p == id))
            .map(|n| n.id.as_str())
            .collect()
    }

    /// Manifests nothing builds on; more than one means the history has diverged.
    pub fn heads(&self) -> Vec<String> {
        let parents: BTreeSet<&str> = self
            .nodes
            .values()
            .flat_map(|n| n.parents.iter().map(String::as_str))
            .collect();
        self.nodes
            .keys()
            .filter(|k| !parents.contains(k.as_str()))
            .cloned()
            .collect()
    }

    /// The id and every ancestor it can reach, including pruned boundaries.
    pub fn ancestry(&self, id: &str) -> BTreeSet<String> {
        let mut out = BTreeSet::new();
        let mut stack = vec![id.to_string()];
        while let Some(next) = stack.pop() {
            if !out.insert(next.clone()) {
                continue;
            }
            if let Some(n) = self.nodes.get(&next) {
                stack.extend(n.parents.iter().cloned());
            }
        }
        out
    }

    /// Lowest common ancestors: common ancestors that are not an ancestor of another common ancestor.
    pub fn merge_bases(&self, a: &str, b: &str) -> Vec<String> {
        let common: BTreeSet<String> = self
            .ancestry(a)
            .intersection(&self.ancestry(b))
            .cloned()
            .collect();
        let mut shadowed = BTreeSet::new();
        for c in &common {
            for anc in self.ancestry(c) {
                if &anc != c {
                    shadowed.insert(anc);
                }
            }
        }
        let mut bases: Vec<String> = common
            .into_iter()
            .filter(|c| !shadowed.contains(c))
            .collect();
        bases.sort_by(|x, y| {
            let gx = self.nodes.get(x).map_or(0, |n| n.generation);
            let gy = self.nodes.get(y).map_or(0, |n| n.generation);
            gy.cmp(&gx).then_with(|| x.cmp(y))
        });
        bases
    }

    pub fn relation(&self, a: &str, b: &str) -> Relation {
        if a == b {
            return Relation::Same;
        }
        if self.ancestry(b).contains(a) {
            return Relation::Ancestor;
        }
        if self.ancestry(a).contains(b) {
            return Relation::Descendant;
        }
        let bases = self.merge_bases(a, b);
        if bases.is_empty() {
            Relation::Unrelated
        } else {
            Relation::Diverged { bases }
        }
    }

    /// Parents before children; ties break by id so every replica agrees.
    pub fn topological(&self) -> Vec<String> {
        let map: BTreeMap<String, Vec<String>> = self
            .nodes
            .iter()
            .map(|(k, n)| (k.clone(), n.parents.clone()))
            .collect();
        check_acyclic(&map, &self.pruned).unwrap_or_default()
    }

    pub fn is_merge(&self, id: &str) -> bool {
        self.nodes.get(id).is_some_and(|n| n.parents.len() > 1)
    }
}
