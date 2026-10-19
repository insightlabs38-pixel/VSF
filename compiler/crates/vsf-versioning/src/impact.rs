//! Which subjects a change reaches, and which conformance tests must run because of it.

use crate::diff::{Change, Class, Diff};
use std::collections::{BTreeMap, BTreeSet};
use vsf_canon::Value;
use vsf_ir::Body;

/// "x depends on y": when y changes, x is impacted.
#[derive(Debug, Clone, Default)]
pub struct DepGraph {
    depends_on: BTreeMap<String, BTreeSet<String>>,
    nodes: BTreeSet<String>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct TestSpec {
    pub id: String,
    pub covers: Vec<String>,
    /// Runs on every change (smoke and contract-wide checks).
    pub always: bool,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Mode {
    Targeted,
    /// Impact could not be bounded; every test runs.
    Full(&'static str),
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Selection {
    pub mode: Mode,
    pub changed: Vec<String>,
    pub impacted: Vec<String>,
    pub tests: Vec<String>,
}

fn items<'a>(v: &'a Value, key: &str) -> &'a [Value] {
    match v.get(key) {
        Some(Value::Array(a)) => a,
        _ => &[],
    }
}

fn text<'a>(v: &'a Value, key: &str) -> Option<&'a str> {
    v.get(key).and_then(Value::as_str)
}

impl DepGraph {
    fn node(&mut self, n: String) -> String {
        self.nodes.insert(n.clone());
        n
    }

    fn edge(&mut self, from: &str, to: &str) {
        let (f, t) = (self.node(from.to_string()), self.node(to.to_string()));
        self.depends_on.entry(f).or_default().insert(t);
    }

    /// Edges from every body given; passing both sides of a diff keeps removed subjects connected.
    pub fn from_bodies(bodies: &[&Body]) -> DepGraph {
        let mut g = DepGraph::default();
        for b in bodies {
            let v = b.encode();
            match b.family() {
                "semantic" => g.add_semantic(&v),
                "presentation" => g.add_presentation(&v),
                "operation" => {
                    if let Some(id) = text(&v, "operation_id") {
                        g.node(format!("operation:{id}"));
                    }
                }
                _ => {}
            }
        }
        g
    }

    fn add_semantic(&mut self, v: &Value) {
        for e in items(v, "entities") {
            let Some(eid) = text(e, "id") else { continue };
            self.node(format!("entity:{eid}"));
            for f in items(e, "fields") {
                if let Some(fid) = text(f, "id") {
                    self.edge(&format!("field:{eid}.{fid}"), &format!("entity:{eid}"));
                }
            }
        }
        for r in items(v, "relationships") {
            let Some(id) = text(r, "id") else { continue };
            self.node(format!("relationship:{id}"));
            for end in ["from", "to"] {
                if let Some(e) = text(r, end) {
                    self.edge(&format!("relationship:{id}"), &format!("entity:{e}"));
                }
            }
        }
        for a in items(v, "actions") {
            let Some(id) = text(a, "id") else { continue };
            self.node(format!("action:{id}"));
            if let Some(e) = text(a, "entity") {
                self.edge(&format!("action:{id}"), &format!("entity:{e}"));
            }
        }
        for m in items(v, "state_machines") {
            let Some(id) = text(m, "id") else { continue };
            self.node(format!("state_machine:{id}"));
            if let Some(e) = text(m, "entity") {
                self.edge(&format!("state_machine:{id}"), &format!("entity:{e}"));
            }
            for t in items(m, "transitions") {
                if let Some(a) = text(t, "action") {
                    self.edge(&format!("state_machine:{id}"), &format!("action:{a}"));
                }
            }
        }
    }

    fn add_presentation(&mut self, v: &Value) {
        self.node("presentation:global".to_string());
        for c in items(v, "components") {
            let Some(id) = text(c, "id") else { continue };
            let me = format!("component:{id}");
            self.edge(&me, "presentation:global");
            for kid in items(c, "children").iter().filter_map(Value::as_str) {
                self.edge(&me, &format!("component:{kid}"));
            }
            if let Some(b) = c.get("binding") {
                let entity = text(b, "entity");
                if let Some(e) = entity {
                    self.edge(&me, &format!("entity:{e}"));
                }
                if let (Some(e), Some(f)) = (entity, text(b, "field")) {
                    self.edge(&me, &format!("field:{e}.{f}"));
                }
                if let Some(a) = text(b, "action") {
                    self.edge(&me, &format!("action:{a}"));
                }
            }
        }
        for r in items(v, "regions") {
            let Some(id) = text(r, "id") else { continue };
            for kid in items(r, "components").iter().filter_map(Value::as_str) {
                self.edge(&format!("region:{id}"), &format!("component:{kid}"));
            }
        }
        for k in items(v, "constraints") {
            let Some(id) = text(k, "id") else { continue };
            for kid in items(k, "subjects").iter().filter_map(Value::as_str) {
                self.edge(&format!("constraint:{id}"), &format!("component:{kid}"));
            }
        }
    }

    /// The seeds and everything that transitively depends on them.
    pub fn impacted(&self, seeds: &BTreeSet<String>) -> BTreeSet<String> {
        let mut dependents: BTreeMap<&str, Vec<&str>> = BTreeMap::new();
        for (from, tos) in &self.depends_on {
            for to in tos {
                dependents
                    .entry(to.as_str())
                    .or_default()
                    .push(from.as_str());
            }
        }
        let mut out: BTreeSet<String> = seeds.clone();
        let mut stack: Vec<&str> = seeds.iter().map(String::as_str).collect();
        while let Some(n) = stack.pop() {
            for d in dependents.get(n).into_iter().flatten() {
                if out.insert((*d).to_string()) {
                    stack.push(d);
                }
            }
        }
        out
    }

    /// What the node directly relies on; tests of those exercise the node too. Presentation-wide settings are left out.
    fn direct_dependencies(&self, n: &str) -> Vec<String> {
        self.depends_on
            .get(n)
            .into_iter()
            .flatten()
            .filter(|d| !d.starts_with("presentation:"))
            .cloned()
            .collect()
    }

    fn knows(&self, n: &str) -> bool {
        self.nodes.contains(n)
    }
}

/// Reads the subject a path points at; `None` when the path reaches outside anything the graph models.
fn subject(family: &str, c: &Change, operation_id: Option<&str>) -> Option<String> {
    let parts: Vec<&str> = c.path.trim_start_matches('/').split('/').collect();
    let keyed = |seg: &str, name: &str| -> Option<String> {
        seg.strip_prefix(name)
            .and_then(|r| r.strip_prefix('['))
            .and_then(|r| r.strip_suffix(']'))
            .map(String::from)
    };
    match family {
        "semantic" => {
            let first = parts.first()?;
            if let Some(e) = keyed(first, "entities") {
                if let Some(f) = parts.get(1).and_then(|p| keyed(p, "fields")) {
                    return Some(format!("field:{e}.{f}"));
                }
                return Some(format!("entity:{e}"));
            }
            for (name, kind) in [
                ("relationships", "relationship"),
                ("actions", "action"),
                ("state_machines", "state_machine"),
            ] {
                if let Some(id) = keyed(first, name) {
                    return Some(format!("{kind}:{id}"));
                }
            }
            if first.starts_with("evidence[")
                || first.starts_with("claims[")
                || *first == "observations"
                || *first == "compiler_version"
            {
                return Some(format!("provenance:{}", c.path));
            }
            None
        }
        "presentation" => {
            let first = parts.first()?;
            for (name, kind) in [
                ("components", "component"),
                ("regions", "region"),
                ("constraints", "constraint"),
            ] {
                if let Some(id) = keyed(first, name) {
                    return Some(format!("{kind}:{id}"));
                }
            }
            matches!(
                *first,
                "theme" | "locale" | "direction" | "presentation_version"
            )
            .then(|| "presentation:global".to_string())
        }
        "operation" => operation_id.map(|id| format!("operation:{id}")),
        _ => None,
    }
}

/// Targeted selection with a conservative full fallback whenever the reach of a change is not known.
pub fn select(
    diff: &Diff,
    graph: &DepGraph,
    catalog: &[TestSpec],
    operation_id: Option<&str>,
) -> Selection {
    let all = || {
        let mut t: Vec<String> = catalog.iter().map(|t| t.id.clone()).collect();
        t.sort();
        t
    };
    let mut seeds = BTreeSet::new();
    let mut fallback: Option<&'static str> = None;
    if diff.class == Some(Class::Unknown) {
        fallback = Some("unknown_change_class");
    }
    for c in &diff.changes {
        match subject(diff.family, c, operation_id) {
            Some(s) if s.starts_with("provenance:") => {}
            Some(s) if graph.knows(&s) || diff.family == "operation" => {
                seeds.insert(s);
            }
            Some(_) => fallback = fallback.or(Some("subject_outside_graph")),
            None => fallback = fallback.or(Some("unmapped_path")),
        }
    }
    let impacted = graph.impacted(&seeds);
    let mut covered_by_tests = impacted.clone();
    for seed in &seeds {
        covered_by_tests.extend(graph.direct_dependencies(seed));
    }
    let reach_of = |seed: &String| {
        let mut reach = graph.impacted(&BTreeSet::from([seed.clone()]));
        reach.extend(graph.direct_dependencies(seed));
        reach
    };
    let uncovered = seeds.iter().any(|seed| {
        let reach = reach_of(seed);
        !catalog
            .iter()
            .any(|t| t.covers.iter().any(|c| reach.contains(c)))
    });
    if uncovered {
        fallback = fallback.or(Some("uncovered_subject"));
    }
    let mut changed: Vec<String> = seeds.into_iter().collect();
    changed.sort();
    if let Some(reason) = fallback {
        return Selection {
            mode: Mode::Full(reason),
            changed,
            impacted: impacted.into_iter().collect(),
            tests: all(),
        };
    }
    let mut tests: BTreeSet<String> = catalog
        .iter()
        .filter(|t| t.always)
        .map(|t| t.id.clone())
        .collect();
    for t in catalog {
        if t.covers.iter().any(|s| covered_by_tests.contains(s)) {
            tests.insert(t.id.clone());
        }
    }
    Selection {
        mode: Mode::Targeted,
        changed,
        impacted: impacted.into_iter().collect(),
        tests: tests.into_iter().collect(),
    }
}
