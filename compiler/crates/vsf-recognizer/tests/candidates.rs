#![allow(clippy::unwrap_used, clippy::expect_used, clippy::panic)]

mod common;
use common::*;
use std::path::PathBuf;
use vsf_recognizer::candidates::{Hypothesis, Resolution, groups, lookup};
use vsf_recognizer::engine::recognize;
use vsf_recognizer::ledger::{Certainty, build_ledger};
use vsf_recognizer::semantics::{FieldKind, Rules, Verb};

#[test]
fn a_control_that_is_also_a_button_yields_two_candidates_and_a_conflict() {
    let mut b = B::new();
    let root = b.div(1);
    let both = b.add(root, "input", Some("button"), Some("Go"), None, BTN);
    let plain = b.button(root, "Save");
    let obs = b.build();
    let rec = recognize(&obs, Rules::V2);
    let gs = groups(&rec);
    let g = lookup(&gs, both, "kind").unwrap();
    assert_eq!(g.resolution, Resolution::Conflicted);
    let hs: Vec<_> = g.candidates.iter().map(|c| c.hypothesis.clone()).collect();
    assert_eq!(
        hs,
        [
            Hypothesis::Field(FieldKind::Text),
            Hypothesis::Action("button".into())
        ]
    );
    assert!(lookup(&gs, plain, "kind").is_none());
    let ledger = build_ledger(&obs, &rec, 0);
    let c = ledger.find(both, "candidates:kind");
    assert_eq!(c.len(), 1);
    assert_eq!(c[0].certainty, Certainty::Conflicted);
    assert!(ledger.find(plain, "candidates:kind").is_empty());
}

#[test]
fn wording_that_names_two_verbs_keeps_each_as_its_own_candidate() {
    let mut b = B::new();
    let root = b.div(1);
    let two = b.button(root, "Edit or delete");
    let one = b.button(root, "Delete");
    let none = b.button(root, "Zzz");
    let obs = b.build();
    let rec = recognize(&obs, Rules::V2);
    let gs = groups(&rec);
    let g = lookup(&gs, two, "verb").unwrap();
    assert_eq!(g.resolution, Resolution::Ambiguous);
    let hs: Vec<_> = g.candidates.iter().map(|c| c.hypothesis.clone()).collect();
    assert_eq!(
        hs,
        [
            Hypothesis::Verb(Verb::Destroy),
            Hypothesis::Verb(Verb::Navigate)
        ]
    );
    assert!(lookup(&gs, one, "verb").is_none());
    assert!(lookup(&gs, none, "verb").is_none());
    let ledger = build_ledger(&obs, &rec, 0);
    let c = ledger.find(two, "candidates:verb");
    assert_eq!(c[0].certainty, Certainty::Unknown);
    assert_eq!(c[0].object, "verb:destroy|verb:navigate");
}

#[test]
fn adversarial_page_has_separate_candidates_and_no_silent_pick() {
    let rec = recognize(&snapshot("adversarial"), Rules::V2);
    let gs = groups(&rec);
    let labels: Vec<_> = gs.iter().filter(|g| g.topic == "label").collect();
    assert!(!labels.is_empty());
    for g in &labels {
        assert!(g.candidates.len() >= 2);
        assert_eq!(g.resolution, Resolution::Ambiguous);
        let f = rec.field(g.node).unwrap();
        assert!(matches!(
            f.binding,
            vsf_recognizer::semantics::Binding::Ambiguous(_)
        ));
    }
    let kinds = gs.iter().filter(|g| g.topic == "kind").count();
    assert_eq!(kinds, 0);
}

#[test]
fn recognition_is_deterministic_across_runs_and_threads() {
    let one = |name: &'static str| {
        let obs = snapshot(name);
        let rec = recognize(&obs, Rules::V2);
        vsf_recognizer::val::pretty(&build_ledger(&obs, &rec, 7).to_value())
    };
    for name in REGRESSION.iter().chain(HELD_OUT.iter()) {
        let base = one(name);
        let again = one(name);
        let name: &'static str = name;
        let threaded = std::thread::spawn(move || one(name)).join().unwrap();
        assert_eq!(base, again);
        assert_eq!(base, threaded);
    }
}

#[test]
fn the_crate_has_no_model_or_network_dependency() {
    let root = PathBuf::from(env!("CARGO_MANIFEST_DIR"));
    let manifest = std::fs::read_to_string(root.join("Cargo.toml")).unwrap();
    let deps: Vec<&str> = manifest
        .split("[dependencies]")
        .nth(1)
        .unwrap()
        .split('[')
        .next()
        .unwrap()
        .lines()
        .filter(|l| l.contains('='))
        .collect();
    let names: Vec<&str> = deps
        .iter()
        .map(|d| {
            d.split(|c: char| c == '.' || c.is_whitespace())
                .next()
                .unwrap_or("")
        })
        .collect();
    assert_eq!(names, ["vsf-canon", "vsf-redact"], "{deps:?}");
    for entry in std::fs::read_dir(root.join("src")).unwrap() {
        let p = entry.unwrap().path();
        let text = std::fs::read_to_string(&p).unwrap();
        for banned in [
            "std::net",
            "process::Command",
            "TcpStream",
            "reqwest",
            "hyper",
            "anthropic",
            "openai",
        ] {
            assert!(!text.contains(banned), "{} uses {banned}", p.display());
        }
    }
}
