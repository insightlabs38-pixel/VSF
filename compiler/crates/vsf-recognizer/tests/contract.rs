#![allow(clippy::unwrap_used, clippy::expect_used, clippy::panic)]

mod common;
use common::*;
use std::collections::BTreeSet;
use vsf_recognizer::contract::{Access, Availability, derive};
use vsf_recognizer::engine::recognize;
use vsf_recognizer::ledger::build_ledger;
use vsf_recognizer::semantics::Rules;

fn contract_of(name: &str, w: &BTreeSet<u32>) -> vsf_recognizer::contract::Contract {
    let obs = snapshot(name);
    let rec = recognize(&obs, Rules::V2);
    derive(&rec, &build_ledger(&obs, &rec, 0), w)
}

#[test]
fn no_write_tool_is_emitted_without_a_behavioural_witness() {
    let mut withheld = 0;
    for name in REGRESSION.iter().chain(HELD_OUT.iter()) {
        let c = contract_of(name, &BTreeSet::new());
        assert!(
            c.emitted().all(|o| o.access == Access::Read),
            "{name} emitted a write"
        );
        withheld += c
            .offers
            .iter()
            .filter(|o| o.access == Access::Write)
            .count();
    }
    assert!(withheld > 20);
}

#[test]
fn a_witness_unlocks_only_a_clean_write() {
    let mut b = B::new();
    let root = b.div(1);
    let save = b.button(root, "Save changes");
    let both = b.add(root, "input", Some("button"), Some("Delete all"), None, BTN);
    let multi = b.button(root, "Edit or delete");
    let obs = b.build();
    let rec = recognize(&obs, Rules::V2);
    let ledger = build_ledger(&obs, &rec, 0);
    let w: BTreeSet<u32> = [save, both, multi].into_iter().collect();
    let c = derive(&rec, &ledger, &w);
    let state = |n| {
        c.for_node(n)
            .find(|o| o.access == Access::Write)
            .unwrap()
            .availability
    };
    assert_eq!(state(save), Availability::Available);
    assert_eq!(state(both), Availability::Conflicted("kind_conflict"));
    assert_eq!(state(multi), Availability::Unknown("verb_ambiguous"));
    let none = derive(&rec, &ledger, &BTreeSet::new());
    let without = none
        .for_node(save)
        .find(|o| o.access == Access::Write)
        .unwrap();
    assert_eq!(
        without.availability,
        Availability::Unknown("effect_unverified")
    );
    assert_eq!(without.availability.ui(), "needs_review");
}

#[test]
fn unknown_and_conflicted_reach_the_ui_state_and_never_the_emitted_set() {
    let mut b = B::new();
    let root = b.div(1);
    let lonely = b.input(root);
    let clash = b.add(root, "input", Some("button"), Some("Go"), None, BTN);
    let off = b.add(root, "button", Some("button"), Some("Pay later"), None, V);
    let obs = b.build();
    let rec = recognize(&obs, Rules::V2);
    let c = derive(&rec, &build_ledger(&obs, &rec, 0), &BTreeSet::new());
    let read = |n| {
        c.for_node(n)
            .find(|o| o.access == Access::Read)
            .map(|o| o.availability)
    };
    assert_eq!(read(lonely), Some(Availability::Unknown("label_unknown")));
    assert_eq!(read(clash), Some(Availability::Conflicted("kind_conflict")));
    assert!(matches!(
        c.for_node(off).next().unwrap().availability,
        Availability::Unusable(_)
    ));
    assert_eq!(Availability::Conflicted("x").ui(), "conflict_badge");
    assert_eq!(
        c.emitted()
            .filter(|o| o.node == lonely || o.node == clash)
            .count(),
        0
    );
}

#[test]
fn credential_and_hidden_controls_stay_unusable_even_with_a_witness() {
    let obs = snapshot("adversarial");
    let rec = recognize(&obs, Rules::V2);
    let witnessed: BTreeSet<u32> = obs.nodes.iter().map(|n| n.id).collect();
    let c = derive(&rec, &build_ledger(&obs, &rec, 0), &witnessed);
    for f in rec.fields().filter(|f| f.why_unusable.is_some()) {
        assert!(
            c.for_node(f.node)
                .all(|o| matches!(o.availability, Availability::Unusable(_)))
        );
    }
}

#[test]
fn contract_document_is_stable() {
    let a = contract_of("plain-form", &BTreeSet::new());
    let b = contract_of("plain-form", &BTreeSet::new());
    assert_eq!(
        vsf_recognizer::val::pretty(&a.to_value()),
        vsf_recognizer::val::pretty(&b.to_value())
    );
}
