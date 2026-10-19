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

mod identity {
    use super::*;
    use vsf_canon::Value;
    use vsf_recognizer::ledger::{
        Export, NewClaim, SourceKind, SourceMeta, WITHHELD, build_ledger_in, node_digest,
    };
    use vsf_recognizer::obs::{F_ACCOUNT, Observation};
    use vsf_recognizer::trust::TrustContext;

    fn ctx(principal: &str) -> TrustContext {
        TrustContext {
            app_id: "acme:crm-eu1".into(),
            variant_id: "var_0123456789abcdef0123456789abcdef".into(),
            origin: "http://fixture.invalid".into(),
            principal: principal.into(),
            runtime_id: "rt_ext1".into(),
            mode: "extension".into(),
            capability_digest: format!("sha256:{}", "c".repeat(64)),
            source_revision: format!("sha256:{}", "d".repeat(64)),
        }
    }

    const P1: &str = "prin_11111111111111111111111111111111";
    const P2: &str = "prin_22222222222222222222222222222222";

    fn claim(source: &str, kind: EvidenceType) -> NewClaim {
        NewClaim {
            subject: 7,
            predicate: "label".into(),
            object: "Total".into(),
            certainty: Certainty::Inferred,
            confidence: Some(Strength::Explicit),
            evidence: vec![vsf_recognizer::ledger::EvidenceRef {
                kind,
                source: source.into(),
                nodes: vec![7],
                node_digests: vec!["d".repeat(64)],
                sensitive: false,
            }],
            unit: None,
            producer: "recognizer.rules-v2".into(),
        }
    }

    fn meta(kind: SourceKind, at: i64) -> SourceMeta {
        SourceMeta {
            kind,
            label: String::new(),
            captured_at: at,
            nodes: 1,
            context: None,
        }
    }

    #[test]
    fn equal_node_ids_digests_text_and_kinds_from_different_sources_keep_separate_claims() {
        let mut l = Ledger::default();
        l.record("sha256:aaaa", meta(SourceKind::Observation, 1))
            .unwrap();
        l.record("sha256:bbbb", meta(SourceKind::Observation, 1))
            .unwrap();
        let a = l.push(claim("sha256:aaaa", EvidenceType::ObservedStructure));
        let b = l.push(claim("sha256:bbbb", EvidenceType::ObservedStructure));
        assert_ne!(a, b);
        assert_eq!(l.claims.len(), 2);
        assert_eq!(l.validate(), vec![]);
        assert_eq!(
            l.push(claim("sha256:aaaa", EvidenceType::ObservedStructure)),
            a
        );
    }

    #[test]
    fn confidence_unit_producer_and_sensitivity_are_part_of_identity() {
        let mut l = Ledger::default();
        l.record("sha256:aaaa", meta(SourceKind::Observation, 1))
            .unwrap();
        let base = claim("sha256:aaaa", EvidenceType::ObservedStructure);
        let mut ids = std::collections::BTreeSet::new();
        ids.insert(l.push(base.clone()));
        ids.insert(l.push(NewClaim {
            confidence: Some(Strength::Lexical),
            ..base.clone()
        }));
        ids.insert(l.push(NewClaim {
            unit: Some((vsf_recognizer::engine::UnitKind::Field, 7)),
            ..base.clone()
        }));
        ids.insert(l.push(NewClaim {
            producer: "recognizer.rules-v1".into(),
            ..base.clone()
        }));
        let mut sens = base.clone();
        sens.evidence[0].sensitive = true;
        ids.insert(l.push(sens));
        assert_eq!(ids.len(), 5);
    }

    #[test]
    fn identical_pages_seen_by_different_principals_or_rules_are_different_evidence() {
        let obs = snapshot("plain-form");
        let rec = recognize(&obs, Rules::V2);
        let a = build_ledger_in(&obs, &rec, 1, Some(&ctx(P1)));
        let b = build_ledger_in(&obs, &rec, 1, Some(&ctx(P2)));
        assert_eq!(a.claims.len(), b.claims.len());
        assert!(a.claims.keys().all(|k| !b.claims.contains_key(k)));
        assert_ne!(a.sources.keys().next(), b.sources.keys().next());
        let v1 = build_ledger(&obs, &recognize(&obs, Rules::V1), 1);
        let v2 = build_ledger(&obs, &rec, 1);
        let shared = v1
            .claims
            .keys()
            .filter(|k| v2.claims.contains_key(*k))
            .count();
        assert_eq!(
            shared, 0,
            "same content from another rules version is another claim"
        );
    }

    #[test]
    fn a_recorded_source_cannot_be_overwritten() {
        let mut l = Ledger::default();
        l.record("sha256:aaaa", meta(SourceKind::Observation, 1))
            .unwrap();
        assert!(
            l.record("sha256:aaaa", meta(SourceKind::Observation, 1))
                .is_ok()
        );
        assert_eq!(
            l.record("sha256:aaaa", meta(SourceKind::Observation, 2)),
            Err("source_metadata_conflict")
        );
        assert_eq!(
            l.record("sha256:aaaa", meta(SourceKind::Protocol, 1)),
            Err("source_metadata_conflict")
        );
    }

    #[test]
    fn rewriting_what_a_claim_cites_is_detected() {
        let (_, l) = ledger_of("plain-form");
        let id = l.claims.keys().next().unwrap().clone();
        let mut moved = l.clone();
        moved
            .sources
            .insert("sha256:other".into(), meta(SourceKind::Observation, 9));
        moved.claims.get_mut(&id).unwrap().evidence[0].source = "sha256:other".into();
        assert!(moved.validate().iter().any(|e| e.problem == "id_mismatch"));
        let mut kinds = l.clone();
        let src = kinds.claims[&id].evidence[0].source.clone();
        kinds.sources.get_mut(&src).unwrap().kind = SourceKind::Protocol;
        assert!(
            kinds
                .validate()
                .iter()
                .any(|e| e.problem == "source_kind_mismatch")
        );
        for edit in [
            |c: &mut vsf_recognizer::ledger::Claim| c.producer = "forged".into(),
            |c: &mut vsf_recognizer::ledger::Claim| c.confidence = None,
            |c: &mut vsf_recognizer::ledger::Claim| c.evidence[0].sensitive = true,
            |c: &mut vsf_recognizer::ledger::Claim| c.unit = None,
            |c: &mut vsf_recognizer::ledger::Claim| c.evidence[0].node_digests[0] = "0".repeat(64),
        ] {
            let mut bad = l.clone();
            let target = bad
                .claims
                .values()
                .find(|c| {
                    c.confidence.is_some() && c.unit.is_some() && !c.evidence[0].nodes.is_empty()
                })
                .unwrap()
                .id
                .clone();
            edit(bad.claims.get_mut(&target).unwrap());
            assert!(bad.validate().iter().any(|e| e.problem == "id_mismatch"));
        }
    }

    #[test]
    fn a_local_export_round_trips_and_any_tampering_is_refused() {
        let (_, l) = ledger_of("adversarial");
        let v = l.to_value();
        let back = Ledger::from_value(&v).unwrap();
        assert_eq!(pretty(&back.to_value()), pretty(&v));
        let text = String::from_utf8(vsf_canon::to_canonical(&v)).unwrap();
        let object = l
            .claims
            .values()
            .find(|c| c.predicate == "label")
            .unwrap()
            .object
            .clone();
        let forged = text.replacen(
            &format!("\"object\":\"{object}\""),
            "\"object\":\"forged\"",
            1,
        );
        assert_ne!(forged, text);
        assert!(Ledger::from_value(&vsf_canon::parse(forged.as_bytes()).unwrap()).is_err());
        let old = text.replace("vsf-evidence-ledger-v2", "vsf-evidence-ledger-v1");
        assert_eq!(
            Ledger::from_value(&vsf_canon::parse(old.as_bytes()).unwrap()).unwrap_err(),
            "ledger.v1_unverifiable"
        );
        let shared = l.export(Export::Shared);
        assert_eq!(
            Ledger::from_value(&shared).unwrap_err(),
            "ledger.not_local_export"
        );
    }

    #[test]
    fn the_shared_export_carries_no_page_text_digests_or_private_claims() {
        let mut b = B::new();
        let root = b.div(1);
        let acct = b.add(
            root,
            "input",
            Some("textbox"),
            Some("Balance 1204"),
            None,
            CTL | F_ACCOUNT,
        );
        let plain = b.add(root, "input", Some("textbox"), Some("Notes"), None, CTL);
        let obs: Observation = b.build();
        let rec = recognize(&obs, Rules::V2);
        let l = build_ledger(&obs, &rec, 1);
        assert!(l.find(acct, "label").iter().all(|c| c.sensitive()));
        assert!(l.find(plain, "label").iter().all(|c| !c.sensitive()));
        let shared = pretty(&l.export(Export::Shared));
        assert!(!shared.contains("Balance 1204") && !shared.contains("Notes"));
        assert!(shared.contains(WITHHELD));
        assert!(!shared.contains("\"digests\""));
        let withheld = l.claims.values().filter(|c| c.sensitive()).count();
        assert!(withheld > 0);
        let local = pretty(&l.to_value());
        assert!(local.contains("Balance 1204") && local.contains("\"digests\""));
        let _ = (node_digest(&obs, 0), Value::Null);
    }
}
