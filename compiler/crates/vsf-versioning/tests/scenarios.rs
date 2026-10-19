#![allow(clippy::unwrap_used, clippy::expect_used, clippy::panic)]

mod common;

use common::{Edit, edited};
use std::collections::BTreeSet;
use vsf_canon::{Value, parse};
use vsf_ir::matrix::{ObservedContext, Support};
use vsf_ir::{ArtifactRole, VersionManifest};
use vsf_versioning::diff::{Class, diff};
use vsf_versioning::impact::{DepGraph, Mode, TestSpec, select};
use vsf_versioning::lineage::{Graph, Relation, manifest_id};
use vsf_versioning::pin::{RevocationSet, State, contract_pin, resolve};

fn host(edit: impl Fn(&mut Vec<(String, Value)>)) -> Support {
    let sample = common::sample::build();
    let Value::Object(mut e) = parse(&sample.support).unwrap() else {
        panic!()
    };
    edit(&mut e);
    Support::from_value(&Value::Object(e)).unwrap()
}

fn put(e: &mut Vec<(String, Value)>, key: &str, v: Option<Value>) {
    e.retain(|(k, _)| k != key);
    if let Some(v) = v {
        e.push((key.to_string(), v));
    }
}

fn here() -> ObservedContext {
    ObservedContext {
        origin: Some("https://crm.example.com".into()),
        principal: Some(common::sample::PRINCIPAL.into()),
    }
}

fn parsed(name: &str) -> (String, VersionManifest) {
    let sample = common::sample::build();
    let (_, id, bytes) = sample
        .manifests
        .iter()
        .find(|(n, _, _)| *n == name)
        .unwrap()
        .clone();
    (id, manifest_id(&bytes).unwrap().1)
}

fn digest_of(m: &VersionManifest, role: ArtifactRole) -> String {
    m.artifacts
        .iter()
        .find(|a| a.role == role)
        .unwrap()
        .digest
        .as_str()
        .to_string()
}

#[test]
fn an_old_pinned_contract_ignores_a_new_source_feature_and_stays_compatible() {
    let (id, m1) = parsed("m1");
    let pins = contract_pin(&m1);
    let drifted = host(|e| {
        put(
            e,
            "operations",
            Some(Value::Object(vec![
                (
                    "app.records.update_stage".into(),
                    Value::Str("1.3.0".into()),
                ),
                ("app.records.list".into(), Value::Str("1.0.5".into())),
                ("app.records.archive".into(), Value::Str("2.0.0".into())),
            ])),
        );
        let fp = format!("sha256:{}", "3b".repeat(32));
        put(
            e,
            "observed",
            Some(Value::Object(vec![
                ("fingerprint".into(), Value::Str(fp)),
                (
                    "variant".into(),
                    Value::Str("var_a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1".into()),
                ),
            ])),
        );
    });
    let r = resolve(
        &id,
        &m1,
        &drifted,
        &here(),
        &pins,
        &RevocationSet::default(),
    );
    assert_eq!(r.state, State::Compatible, "{:?}", r.findings);
    assert!(
        !r.exposes.contains_key("app.records.archive"),
        "a new source feature leaked into an old contract"
    );
    assert_eq!(r.exposes.len(), 2);
}

#[test]
fn a_source_that_lost_a_required_field_is_a_breaking_diff_and_a_blocked_pin() {
    let before = edited("semantic-orders", (1, 0, 0), &[]);
    let after = edited(
        "semantic-orders",
        (1, 0, 0),
        &[Edit::Remove("/body/entities/0/fields/1")],
    );
    let d = diff(&before, &after).unwrap();
    assert_eq!(d.class, Some(Class::Breaking));
    let pres = edited("presentation-orders", (1, 0, 0), &[]);
    let graph = DepGraph::from_bodies(&[&before, &after, &pres]);
    let tests = vec![TestSpec {
        id: "form".into(),
        covers: vec!["component:form_order".into()],
        always: false,
    }];
    let sel = select(&d, &graph, &tests, None);
    assert_eq!(sel.mode, Mode::Targeted);
    assert_eq!(
        sel.tests,
        vec!["form"],
        "the screen bound to the missing field must be re-tested"
    );

    let (id, m1) = parsed("m1");
    let gone = host(|e| {
        put(
            e,
            "observed",
            Some(Value::Object(vec![
                (
                    "fingerprint".into(),
                    Value::Str(format!("sha256:{}", "7".repeat(64))),
                ),
                (
                    "variant".into(),
                    Value::Str("var_a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1".into()),
                ),
            ])),
        );
    });
    let r = resolve(
        &id,
        &m1,
        &gone,
        &here(),
        &contract_pin(&m1),
        &RevocationSet::default(),
    );
    assert_eq!((r.state, r.read, r.write), (State::Blocked, false, false));
    let no_op = host(|e| {
        put(
            e,
            "operations",
            Some(Value::Object(vec![(
                "app.records.list".into(),
                Value::Str("1.0.5".into()),
            )])),
        )
    });
    let r = resolve(
        &id,
        &m1,
        &no_op,
        &here(),
        &contract_pin(&m1),
        &RevocationSet::default(),
    );
    assert!(
        r.findings
            .iter()
            .any(|f| f.axis == "operations" && f.reason == "operation_unknown"),
        "{:?}",
        r.findings
    );
    assert_eq!(r.state, State::Blocked);
}

#[test]
fn an_interface_rollback_restores_only_the_presentation_and_never_implies_a_data_rollback() {
    let (m1_id, m1) = parsed("m1");
    let (m3_id, m3) = parsed("m3");
    let (rb_id, rb) = parsed("rollback");
    assert_eq!(
        digest_of(&rb, ArtifactRole::Presentation),
        digest_of(&m1, ArtifactRole::Presentation)
    );
    assert_ne!(
        digest_of(&rb, ArtifactRole::Presentation),
        digest_of(&m3, ArtifactRole::Presentation)
    );
    for role in [
        ArtifactRole::Semantic,
        ArtifactRole::OperationContract,
        ArtifactRole::Observation,
    ] {
        assert_eq!(
            digest_of(&rb, role),
            digest_of(&m3, role),
            "{role:?} must not move on an interface rollback"
        );
    }
    let spec = rb.rollback.as_ref().unwrap();
    assert_eq!(
        (spec.target.as_str(), spec.roles.as_slice()),
        (m1_id.as_str(), [ArtifactRole::Presentation].as_slice())
    );

    let sample = common::sample::build();
    let docs: Vec<(String, Vec<u8>)> = sample
        .manifests
        .iter()
        .map(|(_, id, b)| (id.clone(), b.clone()))
        .collect();
    let g = Graph::rebuild(&docs, &BTreeSet::new()).unwrap();
    assert_eq!(g.relation(&m3_id, &rb_id), Relation::Ancestor);
    assert_eq!(g.relation(&m1_id, &rb_id), Relation::Ancestor);
    assert_eq!(g.heads(), vec![rb_id.clone()]);

    let r = resolve(
        &rb_id,
        &rb,
        &host(|_| {}),
        &here(),
        &contract_pin(&rb),
        &RevocationSet::default(),
    );
    assert_eq!(r.state, State::Compatible, "{:?}", r.findings);
}

#[test]
fn a_simultaneous_newer_version_cannot_displace_the_pinned_one() {
    let sample = common::sample::build();
    let docs: Vec<(String, Vec<u8>)> = sample
        .manifests
        .iter()
        .map(|(_, id, b)| (id.clone(), b.clone()))
        .collect();
    let g = Graph::rebuild(&docs[..3], &BTreeSet::new()).unwrap();
    let (m2_id, m2) = parsed("m2");
    let (b_id, branch) = parsed("branch");
    assert_eq!(
        g.heads().len(),
        2,
        "m2 and the preview branch diverge from m1"
    );
    assert!(matches!(
        g.relation(&m2_id, &b_id),
        Relation::Diverged { .. }
    ));
    let pins = contract_pin(&m2);
    let pinned = resolve(
        &m2_id,
        &m2,
        &host(|_| {}),
        &here(),
        &pins,
        &RevocationSet::default(),
    );
    assert_eq!(pinned.state, State::Compatible, "{:?}", pinned.findings);
    let mut newer = branch.clone();
    newer.axes.operations.insert(
        vsf_ir::OperationId::new("app.records.update_stage").unwrap(),
        vsf_ir::SemVer::new("1.9.0").unwrap(),
    );
    let newer_host = host(|e| {
        put(
            e,
            "operations",
            Some(Value::Object(vec![
                (
                    "app.records.update_stage".into(),
                    Value::Str("1.9.0".into()),
                ),
                ("app.records.list".into(), Value::Str("1.0.5".into())),
            ])),
        );
    });
    let candidate = resolve(
        &b_id,
        &newer,
        &newer_host,
        &here(),
        &pins,
        &RevocationSet::default(),
    );
    assert_eq!(candidate.state, State::Blocked);
    let still = resolve(
        &m2_id,
        &m2,
        &newer_host,
        &here(),
        &pins,
        &RevocationSet::default(),
    );
    assert_eq!(
        still.state,
        State::Compatible,
        "the pin outlives a newer version appearing"
    );
    assert_eq!(still.exposes, pinned.exposes);
}

#[test]
fn a_source_data_rollback_changes_no_artifact_and_is_not_confused_with_an_interface_one() {
    let (_, m3) = parsed("m3");
    let mut data = m3.clone();
    data.rollback = Some(vsf_ir::RollbackSpec {
        kind: vsf_ir::RollbackKind::SourceData,
        target: m3.lineage[0].clone(),
        roles: vec![],
    });
    assert!(
        vsf_ir::semantics::validate_version(&data)
            .iter()
            .all(|v| v.code != "version.rollback_scope")
    );
    data.rollback.as_mut().unwrap().roles = vec![ArtifactRole::Presentation];
    assert!(
        vsf_ir::semantics::validate_version(&data)
            .iter()
            .any(|v| v.code == "version.rollback_scope")
    );
    let mut iface = m3.clone();
    iface.rollback = Some(vsf_ir::RollbackSpec {
        kind: vsf_ir::RollbackKind::Interface,
        target: m3.lineage[0].clone(),
        roles: vec![ArtifactRole::Semantic],
    });
    assert!(
        vsf_ir::semantics::validate_version(&iface)
            .iter()
            .any(|v| v.code == "version.rollback_scope")
    );
}
