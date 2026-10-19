#![allow(clippy::unwrap_used, clippy::expect_used, clippy::panic)]

mod common;

use common::{base_manifest, bytes, made, manifest};
use std::collections::{BTreeMap, BTreeSet};
use vsf_versioning::lineage::{Graph, Relation, check_acyclic, manifest_id};

fn add(g: &mut Graph, parents: &[&str], tag: i64) -> String {
    let (id, b) = made(parents, tag);
    assert_eq!(g.add(&b).unwrap(), id);
    id
}

fn root(g: &mut Graph, tag: i64) -> String {
    add(g, &[], tag)
}

#[test]
fn identity_is_the_content_digest_and_ignores_whitespace() {
    let b = bytes(&manifest(&[], 1));
    let (id, _) = manifest_id(&b).unwrap();
    let spaced = String::from_utf8(b.clone()).unwrap().replace(",\"", ", \"");
    assert_eq!(manifest_id(spaced.as_bytes()).unwrap().0, id);
    assert_ne!(manifest_id(&bytes(&manifest(&[], 2))).unwrap().0, id);
    assert!(id.starts_with("sha256:"));
}

#[test]
fn a_chain_has_one_head_and_increasing_generations() {
    let mut g = Graph::new();
    let a = root(&mut g, 1);
    let b = add(&mut g, &[&a], 2);
    let c = add(&mut g, &[&b], 3);
    assert_eq!(g.heads(), vec![c.clone()]);
    assert_eq!(
        [&a, &b, &c].map(|i| g.get(i).unwrap().generation),
        [0, 1, 2]
    );
    assert_eq!(g.relation(&a, &c), Relation::Ancestor);
    assert_eq!(g.relation(&c, &a), Relation::Descendant);
    assert_eq!(g.relation(&b, &b), Relation::Same);
    assert_eq!(g.topological(), vec![a, b, c]);
}

#[test]
fn divergent_branches_are_two_heads_with_a_deterministic_merge_base() {
    let mut g = Graph::new();
    let base = root(&mut g, 1);
    let left = add(&mut g, &[&base], 2);
    let right = add(&mut g, &[&base], 3);
    assert_eq!(g.heads().len(), 2);
    assert_eq!(
        g.relation(&left, &right),
        Relation::Diverged {
            bases: vec![base.clone()]
        }
    );
    let merged = add(&mut g, &[&left, &right], 4);
    assert_eq!(g.heads(), vec![merged.clone()]);
    assert!(g.is_merge(&merged));
    assert_eq!(
        g.get(&merged).unwrap().parents,
        vec![left.clone(), right.clone()]
    );
    assert_eq!(g.get(&merged).unwrap().generation, 2);
    assert_eq!(g.relation(&left, &merged), Relation::Ancestor);
}

#[test]
fn a_criss_cross_merge_reports_every_lowest_common_ancestor_in_a_stable_order() {
    let mut g = Graph::new();
    let base = root(&mut g, 1);
    let a = add(&mut g, &[&base], 2);
    let b = add(&mut g, &[&base], 3);
    let m1 = add(&mut g, &[&a, &b], 4);
    let m2 = add(&mut g, &[&b, &a], 5);
    let bases = g.merge_bases(&m1, &m2);
    let mut want = vec![a, b];
    want.sort();
    let mut got = bases.clone();
    got.sort();
    assert_eq!(got, want);
    assert_eq!(bases, g.merge_bases(&m1, &m2));
    assert_eq!(g.merge_bases(&m1, &m2), g.merge_bases(&m1, &m2));
    assert!(
        !bases.contains(&base),
        "the root is shadowed by the nearer ancestors"
    );
}

#[test]
fn unrelated_roots_have_no_base() {
    let mut g = Graph::new();
    let a = root(&mut g, 1);
    let b = root(&mut g, 2);
    assert_eq!(g.relation(&a, &b), Relation::Unrelated);
    assert_eq!(g.heads().len(), 2);
}

#[test]
fn rebuilding_in_any_order_gives_the_same_graph_and_order() {
    let mut docs = Vec::new();
    let mut g = Graph::new();
    let base = root(&mut g, 1);
    let l = add(&mut g, &[&base], 2);
    let r = add(&mut g, &[&base], 3);
    let m = add(&mut g, &[&l, &r], 4);
    let tail = add(&mut g, &[&m], 5);
    for (tag, parents) in [
        (1, vec![]),
        (2, vec![&base]),
        (3, vec![&base]),
        (4, vec![&l, &r]),
        (5, vec![&m]),
    ] {
        let refs: Vec<&str> = parents.iter().map(|s| s.as_str()).collect();
        docs.push(made(&refs, tag));
    }
    let want = g.topological();
    assert_eq!(want.last(), Some(&tail));
    let none = BTreeSet::new();
    let n = docs.len();
    let mut idx: Vec<usize> = (0..n).collect();
    let mut seen = 0;
    loop {
        let shuffled: Vec<(String, Vec<u8>)> = idx.iter().map(|i| docs[*i].clone()).collect();
        let rebuilt = Graph::rebuild(&shuffled, &none).unwrap();
        assert_eq!(rebuilt.topological(), want);
        assert_eq!(rebuilt.heads(), vec![tail.clone()]);
        seen += 1;
        // next permutation
        let Some(i) = (0..n - 1).rev().find(|i| idx[*i] < idx[*i + 1]) else {
            break;
        };
        let j = (i + 1..n).rev().find(|j| idx[*j] > idx[i]).unwrap();
        idx.swap(i, j);
        idx[i + 1..].reverse();
    }
    assert_eq!(seen, 120);
}

#[test]
fn corrupt_and_forged_stores_are_refused() {
    let none = BTreeSet::new();
    let (a, ab) = made(&[], 1);
    let (b, bb) = made(&[&a], 2);
    assert_eq!(
        Graph::rebuild(
            &[
                (a.clone(), ab.clone()),
                ("sha256:".to_string() + &"0".repeat(64), bb.clone())
            ],
            &none
        )
        .unwrap_err()
        .code,
        "lineage.id_mismatch"
    );
    assert_eq!(
        Graph::rebuild(&[(b.clone(), bb.clone())], &none)
            .unwrap_err()
            .code,
        "lineage.missing_parent"
    );
    assert_eq!(
        Graph::rebuild(&[(a.clone(), ab.clone()), (a.clone(), ab.clone())], &none)
            .unwrap_err()
            .code,
        "lineage.duplicate"
    );
    assert_eq!(
        Graph::rebuild(&[(a.clone(), b"{}".to_vec())], &none)
            .unwrap_err()
            .code,
        "lineage.undecodable"
    );
    let mut tampered = bb.clone();
    let at = tampered.len() - 6;
    tampered[at] ^= 1;
    assert!(Graph::rebuild(&[(a, ab), (b, tampered)], &none).is_err());
}

#[test]
fn cycles_and_bad_parents_are_refused() {
    let none = BTreeSet::new();
    let map = |rows: &[(&str, &[&str])]| -> BTreeMap<String, Vec<String>> {
        rows.iter()
            .map(|(k, v)| (k.to_string(), v.iter().map(|s| s.to_string()).collect()))
            .collect()
    };
    assert_eq!(
        check_acyclic(&map(&[("a", &["b"]), ("b", &["a"])]), &none)
            .unwrap_err()
            .code,
        "lineage.cycle"
    );
    assert_eq!(
        check_acyclic(&map(&[("a", &["a"])]), &none)
            .unwrap_err()
            .code,
        "lineage.cycle"
    );
    assert_eq!(
        check_acyclic(&map(&[("a", &["x"])]), &none)
            .unwrap_err()
            .code,
        "lineage.missing_parent"
    );
    let pruned: BTreeSet<String> = ["x".to_string()].into();
    assert_eq!(
        check_acyclic(&map(&[("a", &["x"])]), &pruned).unwrap(),
        vec!["a".to_string()]
    );
    let mut g = Graph::new();
    let a = root(&mut g, 1);
    let dup = bytes(&manifest(&[&a, &a], 2));
    assert!(g.add(&dup).is_err());
    let (_, again) = made(&[a.as_str()], 3);
    assert!(g.add(&again).is_ok());
    let far = "sha256:".to_string() + &"7".repeat(64);
    assert_eq!(
        g.add(&bytes(&manifest(&[&far], 4))).unwrap_err().code,
        "lineage.missing_parent"
    );
    let mut many = base_manifest();
    many.lineage = (0..65)
        .map(|i| vsf_ir::Digest::new(&format!("sha256:{i:064x}")).unwrap())
        .collect();
    many.promotion = None;
    many.channel = vsf_ir::Channel::Preview;
    assert!(g.add(&bytes(&many)).is_err());
}

#[test]
fn pruned_history_acts_as_an_opaque_root_and_an_unpruned_gap_does_not() {
    let gone = "sha256:".to_string() + &"5".repeat(64);
    let mut g = Graph::with_pruned([gone.clone()]);
    let child = add(&mut g, &[&gone], 1);
    assert_eq!(g.get(&child).unwrap().generation, 1);
    assert_eq!(g.ancestry(&child), BTreeSet::from([child, gone]));
}
