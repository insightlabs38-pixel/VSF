//! Dependency closure, exact cycle chains and a stable topological order.

use crate::model::Registry;
use std::collections::{BTreeMap, BTreeSet};

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum GraphError {
    MissingDependency { from: String, to: String },
    SelfDependency { id: String },
    Cycle { chain: Vec<String> },
}

impl GraphError {
    pub fn code(&self) -> &'static str {
        match self {
            GraphError::MissingDependency { .. } => "graph.missing_dependency",
            GraphError::SelfDependency { .. } => "graph.self_dependency",
            GraphError::Cycle { .. } => "graph.cycle",
        }
    }

    pub fn describe(&self) -> String {
        match self {
            GraphError::MissingDependency { from, to } => format!("{from} requires missing {to}"),
            GraphError::SelfDependency { id } => format!("{id} requires itself"),
            GraphError::Cycle { chain } => chain.join(" -> "),
        }
    }
}

fn visit<'a>(
    id: &'a str,
    deps: &BTreeMap<&'a str, Vec<&'a str>>,
    state: &mut BTreeMap<&'a str, u8>,
    stack: &mut Vec<&'a str>,
    out: &mut Vec<GraphError>,
) {
    state.insert(id, 1);
    stack.push(id);
    for &d in deps.get(id).map(Vec::as_slice).unwrap_or(&[]) {
        if d == id {
            continue;
        }
        match state.get(d) {
            Some(2) => {}
            Some(1) => {
                let start = stack.iter().position(|x| *x == d).unwrap_or(0);
                let mut cycle: Vec<&str> = stack[start..].to_vec();
                let min = cycle
                    .iter()
                    .enumerate()
                    .min_by_key(|(_, x)| **x)
                    .map(|(i, _)| i)
                    .unwrap_or(0);
                cycle.rotate_left(min);
                let mut chain: Vec<String> = cycle.iter().map(|x| (*x).to_string()).collect();
                chain.push(chain[0].clone());
                let e = GraphError::Cycle { chain };
                if !out.contains(&e) {
                    out.push(e);
                }
            }
            _ => visit(d, deps, state, stack, out),
        }
    }
    stack.pop();
    state.insert(id, 2);
}

/// Dependencies first; ties break by id so the order is reproducible byte for byte.
pub fn verify(reg: &Registry) -> Result<Vec<String>, Vec<GraphError>> {
    let ids: BTreeSet<&str> = reg.entries.iter().map(|e| e.id.as_str()).collect();
    let mut errors = Vec::new();
    let mut deps: BTreeMap<&str, Vec<&str>> = BTreeMap::new();
    for e in &reg.entries {
        let mut list = Vec::new();
        for r in &e.requires {
            if *r == e.id {
                errors.push(GraphError::SelfDependency { id: e.id.clone() });
            } else if !ids.contains(r.as_str()) {
                errors.push(GraphError::MissingDependency {
                    from: e.id.clone(),
                    to: r.clone(),
                });
            } else {
                list.push(r.as_str());
            }
        }
        deps.insert(e.id.as_str(), list);
    }
    let mut state = BTreeMap::new();
    for &id in &ids {
        if !state.contains_key(id) {
            visit(id, &deps, &mut state, &mut Vec::new(), &mut errors);
        }
    }
    if !errors.is_empty() {
        return Err(errors);
    }
    let mut remaining: BTreeMap<&str, usize> = deps
        .iter()
        .map(|(k, v)| (*k, v.iter().collect::<BTreeSet<_>>().len()))
        .collect();
    let mut dependents: BTreeMap<&str, Vec<&str>> = BTreeMap::new();
    for (k, v) in &deps {
        for d in v.iter().collect::<BTreeSet<_>>() {
            dependents.entry(*d).or_default().push(*k);
        }
    }
    let mut ready: BTreeSet<&str> = remaining
        .iter()
        .filter(|(_, n)| **n == 0)
        .map(|(k, _)| *k)
        .collect();
    let mut order = Vec::with_capacity(ids.len());
    while let Some(&next) = ready.iter().next() {
        ready.remove(next);
        order.push(next.to_string());
        for &d in dependents.get(next).map(Vec::as_slice).unwrap_or(&[]) {
            if let Some(n) = remaining.get_mut(d) {
                *n -= 1;
                if *n == 0 {
                    ready.insert(d);
                }
            }
        }
    }
    Ok(order)
}
