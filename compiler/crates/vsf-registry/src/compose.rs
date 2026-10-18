//! The shared collection composite: semantic extraction from structure, never from appearance.

use crate::fixture::{Expected, Fixture, Node};
use crate::register::{Registration, recognize};
use std::collections::{BTreeMap, BTreeSet};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Collection {
    pub root: u32,
    pub primitive: String,
    pub columns: Vec<String>,
    pub rows: Vec<Vec<String>>,
}

struct Tree<'a> {
    nodes: &'a [Node],
    children: Vec<Vec<usize>>,
    hidden: Vec<bool>,
}

fn role(n: &Node) -> &str {
    n.attrs.get("role").map_or("", String::as_str)
}

fn predicates(n: &Node) -> BTreeSet<String> {
    let mut p: BTreeSet<String> = n
        .attrs
        .keys()
        .filter(|k| k.as_str() != "role")
        .map(|k| format!("attr:{k}"))
        .collect();
    p.insert(format!("tag:{}", n.tag.to_ascii_lowercase()));
    if !role(n).is_empty() {
        p.insert(format!("role:{}", role(n).to_ascii_lowercase()));
    }
    p
}

fn collapse(s: &str) -> String {
    s.split([' ', '\t', '\n', '\r'])
        .filter(|w| !w.is_empty())
        .collect::<Vec<_>>()
        .join(" ")
}

impl<'a> Tree<'a> {
    fn new(nodes: &'a [Node]) -> Tree<'a> {
        let pos: BTreeMap<u32, usize> = nodes.iter().enumerate().map(|(i, n)| (n.id, i)).collect();
        let mut children = vec![Vec::new(); nodes.len()];
        let mut hidden = vec![false; nodes.len()];
        for (i, n) in nodes.iter().enumerate() {
            let parent_hidden = pos.get(&n.parent).is_some_and(|&p| hidden[p]);
            hidden[i] = parent_hidden
                || n.attrs.contains_key("hidden")
                || n.attrs.get("aria-hidden").is_some_and(|v| v == "true");
            if let Some(&p) = pos.get(&n.parent) {
                children[p].push(i);
            }
        }
        Tree {
            nodes,
            children,
            hidden,
        }
    }

    fn text(&self, at: usize, roots: &BTreeMap<usize, String>, out: &mut Vec<String>) {
        if self.hidden[at] {
            return;
        }
        out.push(self.nodes[at].text.clone());
        for &c in &self.children[at] {
            if !roots.contains_key(&c) {
                self.text(c, roots, out);
            }
        }
    }

    fn cells(&self, at: usize, roots: &BTreeMap<usize, String>, out: &mut Vec<(bool, String)>) {
        for &c in &self.children[at] {
            if self.hidden[c] || roots.contains_key(&c) {
                continue;
            }
            let n = &self.nodes[c];
            let r = role(n).to_ascii_lowercase();
            let tag = n.tag.to_ascii_lowercase();
            if tag == "td"
                || tag == "th"
                || matches!(
                    r.as_str(),
                    "gridcell" | "cell" | "columnheader" | "rowheader"
                )
            {
                let mut parts = Vec::new();
                self.text(c, roots, &mut parts);
                out.push((
                    tag == "th" || matches!(r.as_str(), "columnheader" | "rowheader"),
                    collapse(&parts.join(" ")),
                ));
            } else {
                self.cells(c, roots, out);
            }
        }
    }

    fn rows(&self, at: usize, roots: &BTreeMap<usize, String>, out: &mut Vec<Vec<(bool, String)>>) {
        for &c in &self.children[at] {
            if self.hidden[c] || roots.contains_key(&c) {
                continue;
            }
            let n = &self.nodes[c];
            if n.tag.eq_ignore_ascii_case("tr") || role(n).eq_ignore_ascii_case("row") {
                let mut cells = Vec::new();
                self.cells(c, roots, &mut cells);
                out.push(cells);
            } else {
                self.rows(c, roots, out);
            }
        }
    }
}

/// Every collection the registered recognizers accept, each extracted by the same composite.
pub fn extract_all(nodes: &[Node], registered: &BTreeMap<String, Registration>) -> Vec<Collection> {
    let tree = Tree::new(nodes);
    let mut roots: BTreeMap<usize, String> = BTreeMap::new();
    for (i, n) in nodes.iter().enumerate() {
        if tree.hidden[i] {
            continue;
        }
        let hits = recognize(registered, &predicates(n));
        if let Some(h) = hits.iter().find(|h| {
            h.recognition
                .as_ref()
                .is_some_and(|r| r.produces == "collection")
        }) {
            roots.insert(i, h.id.clone());
        }
    }
    roots
        .iter()
        .map(|(&at, primitive)| {
            let mut rows = Vec::new();
            tree.rows(at, &roots, &mut rows);
            let header = rows
                .iter()
                .position(|r| !r.is_empty() && r.iter().all(|c| c.0));
            let columns = header
                .map(|i| rows[i].iter().map(|c| c.1.clone()).collect())
                .unwrap_or_default();
            let data = rows
                .iter()
                .enumerate()
                .filter(|(i, _)| Some(*i) != header)
                .map(|(_, r)| r.iter().map(|c| c.1.clone()).collect())
                .collect();
            Collection {
                root: nodes[at].id,
                primitive: primitive.clone(),
                columns,
                rows: data,
            }
        })
        .collect()
}

pub fn run_fixture(f: &Fixture, registered: &BTreeMap<String, Registration>) -> Result<(), String> {
    let got: Vec<Expected> = extract_all(&f.nodes, registered)
        .into_iter()
        .map(|c| Expected {
            root: c.root,
            primitive: c.primitive,
            columns: c.columns,
            rows: c.rows,
        })
        .collect();
    if got == f.expect {
        Ok(())
    } else {
        Err(format!("{}: expected {:?}, got {:?}", f.id, f.expect, got))
    }
}
