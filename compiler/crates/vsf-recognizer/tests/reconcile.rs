#![allow(clippy::unwrap_used, clippy::expect_used, clippy::panic)]

mod common;
use common::*;
use std::collections::BTreeSet;
use std::path::PathBuf;
use vsf_recognizer::contract::{Access, Availability, derive};
use vsf_recognizer::engine::recognize;
use vsf_recognizer::ledger::{Certainty, Ledger, build_ledger};
use vsf_recognizer::obs::Observation;
use vsf_recognizer::reconcile::{Decision, Protocol, Side, open_conflicts, reconcile, resolve};
use vsf_recognizer::semantics::Rules;

const NOW: i64 = 1_790_000_100_000;
const MONTH: i64 = 30 * 24 * 3600 * 1000;
const PERSON: &str = "prin_0123456789abcdef0123456789abcdef";

fn proto(name: &str) -> Protocol {
    let dir =
        PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../../../fixtures/recognition/protocol");
    Protocol::from_json(&std::fs::read(dir.join(format!("{name}.json"))).unwrap()).unwrap()
}

fn run(app: &str, doc: &str) -> (Observation, vsf_recognizer::engine::Recognition, Ledger) {
    let obs = snapshot(app);
    let rec = recognize(&obs, Rules::V2);
    let mut l = build_ledger(&obs, &rec, NOW);
    reconcile(&mut l, &obs, &rec, &proto(doc), MONTH);
    (obs, rec, l)
}

fn field_by_label(rec: &vsf_recognizer::engine::Recognition, label: &str) -> u32 {
    rec.fields()
        .find(|f| matches!(&f.binding, vsf_recognizer::semantics::Binding::Bound(c) if c.text == label))
        .unwrap()
        .node
}

#[test]
fn agreeing_evidence_is_recorded_as_inferred_with_both_sources() {
    let (_, rec, l) = run("plain-form", "profile-agree");
    assert!(open_conflicts(&l).is_empty());
    let first = field_by_label(&rec, "First name");
    let c = l.find(first, "protocol:field");
    assert_eq!(c.len(), 1);
    assert_eq!(c[0].certainty, Certainty::Inferred);
    assert_eq!(c[0].evidence.len(), 2);
    assert_eq!(l.validate(), vec![]);
}

#[test]
fn disagreement_is_a_conflict_citing_both_sides_and_no_source_wins() {
    let (_, rec, l) = run("plain-form", "profile-conflict");
    let digest = field_by_label(&rec, "Weekly digest");
    let t = l.find(digest, "protocol:type");
    assert_eq!(t[0].certainty, Certainty::Conflicted);
    assert!(t[0].object.contains("ui=toggle") && t[0].object.starts_with("weekly_digest:string"));
    assert_eq!(t[0].evidence.len(), 2);
    assert!(l.find(digest, "protocol:field").is_empty());
    let missing: Vec<_> = open_conflicts(&l)
        .into_iter()
        .filter(|c| c.predicate == "protocol:required_missing")
        .collect();
    assert_eq!(missing.len(), 1);
    assert!(missing[0].object.starts_with("phone:string"));
    assert_eq!(open_conflicts(&l).len(), 2);
    let first = field_by_label(&rec, "First name");
    assert_eq!(l.find(first, "protocol:field").len(), 1);
}

#[test]
fn a_conflict_withholds_the_tool_until_a_recorded_decision_resolves_it() {
    let (obs, rec, mut l) = run("plain-form", "profile-conflict");
    let digest = field_by_label(&rec, "Weekly digest");
    let read_state = |l: &Ledger| {
        derive(&rec, l, &BTreeSet::new())
            .for_node(digest)
            .find(|o| o.access == Access::Read)
            .unwrap()
            .availability
    };
    assert_eq!(
        read_state(&l),
        Availability::Conflicted("conflicting_evidence")
    );
    let id = l.find(digest, "protocol:type")[0].id.clone();
    let d = |who: &str, why: &str| Decision {
        claim: id.clone(),
        side: Side::Protocol,
        decided_by: who.into(),
        reason: why.into(),
    };
    assert_eq!(
        resolve(&mut l, d("someone", "docs are newer")),
        Err("bad_principal")
    );
    assert_eq!(resolve(&mut l, d(PERSON, "  ")), Err("bad_reason"));
    assert_eq!(
        read_state(&l),
        Availability::Conflicted("conflicting_evidence")
    );
    resolve(
        &mut l,
        d(PERSON, "vendor docs updated the field to a string"),
    )
    .unwrap();
    assert_eq!(read_state(&l), Availability::Available);
    assert_eq!(resolve(&mut l, d(PERSON, "again")), Err("already_resolved"));
    assert_eq!(open_conflicts(&l).len(), 1);
    let kept = l.claims.get(&id).unwrap();
    assert_eq!(kept.certainty, Certainty::Conflicted);
    assert_eq!(l.validate(), vec![]);
    let _ = obs;
}

#[test]
fn only_real_open_conflicts_can_be_decided() {
    let (_, rec, mut l) = run("plain-form", "profile-agree");
    let first = field_by_label(&rec, "First name");
    let ok = l.find(first, "protocol:field")[0].id.clone();
    let d = |claim: String| Decision {
        claim,
        side: Side::Ui,
        decided_by: PERSON.into(),
        reason: "r".into(),
    };
    assert_eq!(resolve(&mut l, d(ok)), Err("not_conflicted"));
    assert_eq!(
        resolve(&mut l, d("clm_missing".into())),
        Err("unknown_claim")
    );
}

#[test]
fn unsupported_evidence_can_only_raise_questions() {
    let (_, _, l) = run("plain-form", "profile-unsupported");
    assert!(open_conflicts(&l).is_empty());
    let unknown = l
        .claims
        .values()
        .filter(|c| c.predicate.starts_with("protocol:") && c.certainty == Certainty::Unknown)
        .count();
    assert_eq!(unknown, 2);
}

#[test]
fn stale_protocol_evidence_is_not_compared() {
    let (_, _, l) = run("plain-form", "profile-stale");
    let stale: Vec<_> = l
        .claims
        .values()
        .filter(|c| c.predicate == "protocol:stale")
        .collect();
    assert_eq!(stale.len(), 1);
    assert_eq!(stale[0].certainty, Certainty::Unknown);
    assert!(l.claims.values().all(|c| c.predicate != "protocol:field"));
}

#[test]
fn duplicated_ui_labels_bind_to_protocol_names_only_through_their_region() {
    let (_, rec, l) = run("adversarial", "checkout-scoped");
    let named: Vec<u32> = rec
        .fields()
        .filter(|f| matches!(&f.binding, vsf_recognizer::semantics::Binding::Bound(c) if c.text == "Name"))
        .map(|f| f.node)
        .collect();
    assert_eq!(named.len(), 2);
    for n in &named {
        assert_eq!(
            l.find(*n, "protocol:field").len(),
            1,
            "scoped name matches once"
        );
    }
    let objects: BTreeSet<String> = named
        .iter()
        .map(|n| l.find(*n, "protocol:field")[0].object.clone())
        .collect();
    assert_eq!(objects.len(), 2);
    let ambiguous: Vec<_> = l
        .claims
        .values()
        .filter(|c| c.object.contains("|ambiguous"))
        .collect();
    assert_eq!(ambiguous.len(), 1);
    assert_eq!(ambiguous[0].certainty, Certainty::Unknown);
}

#[test]
fn credential_fields_are_never_matched_to_protocol_names() {
    let mut b = B::new();
    let root = b.div(1);
    let pw = b.add(
        root,
        "input",
        Some("textbox"),
        Some("Password"),
        None,
        CTL | vsf_recognizer::obs::F_CREDENTIAL,
    );
    let obs = b.build();
    let rec = recognize(&obs, Rules::V2);
    let mut l = build_ledger(&obs, &rec, NOW);
    let mut p = proto("profile-agree");
    p.request[0].name = "password".into();
    reconcile(&mut l, &obs, &rec, &p, MONTH);
    assert!(l.find(pw, "protocol:field").is_empty());
    assert!(
        l.claims
            .values()
            .all(|c| !c.object.starts_with("password:string")
                || c.predicate == "protocol:required_missing")
    );
}

#[test]
fn malformed_protocol_documents_are_rejected() {
    assert!(Protocol::from_json(b"{}").is_err());
    assert!(Protocol::from_json(br#"{"schema":"other"}"#).is_err());
    assert!(Protocol::from_json(b"not json").is_err());
}
