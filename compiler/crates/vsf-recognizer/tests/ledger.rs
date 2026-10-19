#![allow(clippy::unwrap_used, clippy::expect_used, clippy::panic)]

mod common;
use common::*;
use std::path::PathBuf;
use vsf_recognizer::engine::recognize;
use vsf_recognizer::ledger::{Certainty, EvidenceType, Ledger, build_ledger};
use vsf_recognizer::obs::Observation;
use vsf_recognizer::semantics::{Rules, Strength};
use vsf_recognizer::val::pretty;

const ALL: [&str; 6] = [
    "plain-form",
    "table-orders",
    "aria-grid",
    "adversarial",
    "heldout-shadow-ds",
    "heldout-datagrid-edit",
];

fn ledger_of(name: &str) -> (Observation, Ledger) {
    let obs = snapshot(name);
    let rec = recognize(&obs, Rules::V2);
    let l = build_ledger(&obs, &rec, 1_792_368_100_000);
    (obs, l)
}

fn edited(name: &str, from: &str, to: &str) -> Observation {
    let dir =
        PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../../../fixtures/recognition/snapshots");
    let text = std::fs::read_to_string(dir.join(format!("{name}.json"))).unwrap();
    assert!(text.contains(from));
    Observation::from_json(text.replacen(from, to, 1).as_bytes()).unwrap()
}

#[test]
fn every_claim_on_every_fixture_traces_to_a_recorded_observation_with_evidence() {
    for name in ALL {
        let (obs, l) = ledger_of(name);
        assert!(l.claims.len() > 5, "{name}");
        assert_eq!(l.validate(), vec![], "{name}");
        let meta = l.sources.get(&obs.digest).unwrap();
        assert_eq!(
            (meta.captured_at, meta.nodes),
            (1_792_368_100_000, obs.len())
        );
        for c in l.claims.values() {
            assert!(!c.evidence.is_empty());
            assert!(c.evidence.iter().all(|e| l.sources.contains_key(&e.source)));
            assert!(c.id.starts_with("clm_") && c.id.len() == 36);
        }
    }
}

#[test]
fn recognition_never_claims_more_than_inferred() {
    for name in ALL {
        let (_, l) = ledger_of(name);
        assert!(
            l.claims
                .values()
                .all(|c| matches!(c.certainty, Certainty::Inferred | Certainty::Unknown)),
            "{name}"
        );
    }
}

#[test]
fn a_label_claim_names_the_label_node_the_control_and_their_digests() {
    let (obs, l) = ledger_of("plain-form");
    let field = obs
        .nodes
        .iter()
        .find(|n| n.name.as_deref() == Some("First name") && n.tag == "input")
        .unwrap();
    let claims = l.find(field.id, "label");
    assert_eq!(claims.len(), 1);
    let c = claims[0];
    assert_eq!(
        (c.object.as_str(), c.certainty, c.confidence),
        ("First name", Certainty::Inferred, Some(Strength::Explicit))
    );
    let ev = &c.evidence[0];
    assert_eq!(ev.kind, EvidenceType::ObservedStructure);
    assert_eq!(ev.nodes.len(), 2);
    assert!(ev.nodes.contains(&field.id));
    assert_eq!(ev.node_digests.len(), 2);
    assert!(ev.node_digests.iter().all(|d| d.len() == 64));
}

#[test]
fn lexical_verb_claims_say_so_and_carry_heuristic_evidence() {
    let (obs, l) = ledger_of("plain-form");
    let save = obs
        .nodes
        .iter()
        .find(|n| n.name.as_deref() == Some("Save changes"))
        .unwrap();
    let v = l.find(save.id, "verb");
    assert_eq!(v.len(), 1);
    assert_eq!(
        (v[0].object.as_str(), v[0].confidence),
        ("commit", Some(Strength::Lexical))
    );
    assert!(
        v[0].evidence
            .iter()
            .any(|e| e.kind == EvidenceType::HeuristicInference)
    );
}

#[test]
fn what_cannot_be_demonstrated_is_an_unknown_claim_not_a_missing_one() {
    let (obs, l) = ledger_of("heldout-datagrid-edit");
    let unbound = recognize(&obs, Rules::V1);
    let l1 = build_ledger(&obs, &unbound, 1);
    let field = unbound.fields().next().unwrap();
    let c = l1.find(field.node, "label");
    assert_eq!(
        (c[0].object.as_str(), c[0].certainty),
        ("none", Certainty::Unknown)
    );
    assert!(!c[0].evidence.is_empty());
    assert!(l.find(field.node, "label")[0].certainty == Certainty::Inferred);
}

#[test]
fn stale_evidence_is_detected_for_exactly_the_claims_that_relied_on_the_changed_node() {
    let (obs, l) = ledger_of("plain-form");
    let changed = edited("plain-form", "\"Last name\"", "\"Surname\"");
    let stale = l.stale_against(&changed);
    let last = obs
        .nodes
        .iter()
        .find(|n| n.name.as_deref() == Some("Last name") && n.tag == "input")
        .unwrap();
    assert!(!stale.is_empty());
    assert!(
        stale
            .iter()
            .all(|c| c.subject == last.id || c.evidence.iter().any(|e| e.nodes.contains(&last.id)))
    );
    let first = obs
        .nodes
        .iter()
        .find(|n| n.name.as_deref() == Some("First name") && n.tag == "input")
        .unwrap();
    assert!(stale.iter().all(|c| c.subject != first.id));
    assert!(l.stale_against(&obs).is_empty());
}

#[test]
fn tampered_claims_fail_validation() {
    let (_, l) = ledger_of("plain-form");
    let id = l.claims.keys().next().unwrap().clone();
    let mut bad = l.clone();
    bad.claims.get_mut(&id).unwrap().object = "forged".into();
    assert!(bad.validate().iter().any(|e| e.problem == "id_mismatch"));
    let mut bare = l.clone();
    bare.claims.get_mut(&id).unwrap().evidence.clear();
    assert!(bare.validate().iter().any(|e| e.problem == "no_evidence"));
    let mut orphan = l.clone();
    orphan.sources.clear();
    assert!(
        orphan
            .validate()
            .iter()
            .any(|e| e.problem == "unrecorded_source")
    );
}

#[test]
fn the_ledger_is_deterministic_and_content_addressed() {
    let (_, a) = ledger_of("adversarial");
    let (_, b) = ledger_of("adversarial");
    assert_eq!(pretty(&a.to_value()), pretty(&b.to_value()));
    let ids: Vec<_> = a.claims.keys().collect();
    let mut sorted = ids.clone();
    sorted.sort();
    assert_eq!(ids, sorted);
}
