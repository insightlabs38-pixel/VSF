#![allow(clippy::unwrap_used, clippy::expect_used, clippy::panic)]

mod common;

use common::read_fixture;
use std::collections::BTreeSet;
use vsf_canon::{Value, parse};
use vsf_ir::matrix::{ObservedContext, Pins, Support};
use vsf_ir::rt::Ctx;
use vsf_ir::{Body, CompatStatus, SemVer, VersionAxes, VersionManifest};
use vsf_versioning::pin::{RevocationSet, State, contract_pin, resolve};

fn matrix() -> Value {
    let path = format!(
        "{}/../../../schemas/ir/fixtures/matrix/matrix.json",
        env!("CARGO_MANIFEST_DIR")
    );
    parse(&std::fs::read(path).unwrap()).unwrap()
}

fn manifest() -> VersionManifest {
    let Body::Version(mut m) = read_fixture("version-manifest-rollback-1-1", (1, 1, 0)) else {
        panic!()
    };
    m.axes = VersionAxes::decode(
        matrix().get("base").unwrap(),
        "",
        &Ctx { version: (1, 1, 0) },
    )
    .unwrap();
    m.revocations = None;
    m
}

fn host(edit: impl Fn(&mut Vec<(String, Value)>)) -> Support {
    let Value::Object(mut e) = matrix().get("host").unwrap().clone() else {
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
        principal: Some("prin_b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2".into()),
    }
}

const ID: &str = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";

fn go(
    m: &VersionManifest,
    s: &Support,
    o: &ObservedContext,
    p: &Pins,
    r: &RevocationSet,
) -> vsf_versioning::pin::Resolution {
    resolve(ID, m, s, o, p, r)
}

#[test]
fn a_matching_source_is_compatible_and_may_write() {
    let m = manifest();
    let r = go(
        &m,
        &host(|_| {}),
        &here(),
        &contract_pin(&m),
        &RevocationSet::default(),
    );
    assert_eq!(
        (r.state, r.read, r.write, r.revoked),
        (State::Compatible, true, true, false),
        "{:?}",
        r.findings
    );
    assert_eq!(
        r.exposes
            .get("app.records.update_stage")
            .map(String::as_str),
        Some("1.3.0")
    );
}

#[test]
fn source_drift_changes_the_state_never_the_public_contract() {
    let m = manifest();
    let pins = contract_pin(&m);
    let exposed = go(&m, &host(|_| {}), &here(), &pins, &RevocationSet::default()).exposes;
    type Edit = fn(&mut Vec<(String, Value)>);
    let drifts: Vec<(&str, Edit, State, bool, bool)> = vec![
        (
            "fingerprint changed",
            |e| {
                let o = Value::Object(vec![
                    (
                        "fingerprint".into(),
                        Value::Str(format!("sha256:{}", "7".repeat(64))),
                    ),
                    (
                        "variant".into(),
                        Value::Str("var_a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1".into()),
                    ),
                ]);
                put(e, "observed", Some(o));
            },
            State::Blocked,
            false,
            false,
        ),
        (
            "not observed",
            |e| put(e, "observed", None),
            State::Degraded,
            true,
            false,
        ),
        (
            "presentation newer than the host",
            |e| put(e, "presentation_max", Some(Value::Int(1))),
            State::Degraded,
            true,
            false,
        ),
        (
            "older compiler, translatable",
            |e| {
                put(
                    e,
                    "compiler",
                    Some(Value::Object(vec![
                        ("min".into(), Value::Str("0.2.0".into())),
                        ("max".into(), Value::Str("0.3.9".into())),
                    ])),
                );
            },
            State::Compatible,
            true,
            true,
        ),
        (
            "operation unknown to the host",
            |e| {
                put(
                    e,
                    "operations",
                    Some(Value::Object(vec![(
                        "app.records.list".into(),
                        Value::Str("1.0.5".into()),
                    )])),
                );
            },
            State::Blocked,
            false,
            false,
        ),
        (
            "host policy floor raised",
            |e| put(e, "min_policy", Some(Value::Int(9))),
            State::Blocked,
            false,
            false,
        ),
    ];
    for (name, edit, state, read, write) in drifts {
        let r = go(&m, &host(edit), &here(), &pins, &RevocationSet::default());
        assert_eq!(
            (r.state, r.read, r.write),
            (state, read, write),
            "{name}: {:?}",
            r.findings
        );
        assert_eq!(r.exposes, exposed, "{name}: the pinned public schema moved");
    }
}

#[test]
fn unobserved_origin_or_principal_is_never_compatible() {
    let m = manifest();
    for observed in [
        ObservedContext::default(),
        ObservedContext {
            origin: Some("https://crm.example.com".into()),
            principal: None,
        },
    ] {
        let r = go(
            &m,
            &host(|_| {}),
            &observed,
            &contract_pin(&m),
            &RevocationSet::default(),
        );
        assert_eq!(
            (r.state, r.write),
            (State::Degraded, false),
            "{:?}",
            r.findings
        );
        assert_eq!(r.overall, CompatStatus::Unknown);
    }
    let moved = ObservedContext {
        origin: Some("https://other.example.com".into()),
        principal: here().principal,
    };
    assert_eq!(
        go(
            &m,
            &host(|_| {}),
            &moved,
            &contract_pin(&m),
            &RevocationSet::default()
        )
        .state,
        State::Blocked
    );
    let other = ObservedContext {
        origin: here().origin,
        principal: Some("prin_c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3".into()),
    };
    let r = go(
        &m,
        &host(|_| {}),
        &other,
        &contract_pin(&m),
        &RevocationSet::default(),
    );
    assert_eq!(
        (r.state, r.overall),
        (State::Blocked, CompatStatus::NeedsReauth)
    );
}

#[test]
fn a_pin_refuses_a_candidate_with_another_contract_but_keeps_the_pinned_one_usable() {
    let old = manifest();
    let pins = contract_pin(&old);
    let mut newer = old.clone();
    newer.axes.operations.insert(
        vsf_ir::OperationId::new("app.records.update_stage").unwrap(),
        SemVer::new("1.4.0").unwrap(),
    );
    let s = host(|e| {
        put(
            e,
            "operations",
            Some(Value::Object(vec![
                (
                    "app.records.update_stage".into(),
                    Value::Str("1.4.0".into()),
                ),
                ("app.records.list".into(), Value::Str("1.0.5".into())),
            ])),
        );
    });
    let candidate = go(&newer, &s, &here(), &pins, &RevocationSet::default());
    assert_eq!(candidate.state, State::Blocked);
    assert!(
        candidate
            .findings
            .iter()
            .any(|f| f.axis == "operations" && f.reason == "pin_mismatch"),
        "{:?}",
        candidate.findings
    );
    let kept = go(&old, &s, &here(), &pins, &RevocationSet::default());
    assert_eq!(kept.state, State::Compatible, "{:?}", kept.findings);
    assert_eq!(
        kept.exposes
            .get("app.records.update_stage")
            .map(String::as_str),
        Some("1.3.0")
    );
}

#[test]
fn findings_are_ordered_most_severe_first_and_deterministically() {
    let m = manifest();
    let s = host(|e| {
        put(e, "presentation_max", Some(Value::Int(1)));
        put(e, "observed", None);
    });
    let a = go(
        &m,
        &s,
        &here(),
        &contract_pin(&m),
        &RevocationSet::default(),
    );
    let b = go(
        &m,
        &s,
        &here(),
        &contract_pin(&m),
        &RevocationSet::default(),
    );
    assert_eq!(a, b);
    let severities: Vec<u8> = a
        .findings
        .iter()
        .map(|f| vsf_ir::matrix::severity(f.status))
        .collect();
    let mut sorted = severities.clone();
    sorted.sort_by(|x, y| y.cmp(x));
    assert_eq!(severities, sorted);
}

#[test]
fn a_published_revocation_overrides_a_pin_on_every_runtime_variant() {
    let m = manifest();
    let pins = contract_pin(&m);
    let variants: [(&str, Support); 4] = [
        ("extension", host(|_| {})),
        (
            "localhost",
            host(|e| put(e, "runtime", Some(Value::Str("0.2.1".into())))),
        ),
        (
            "hosted",
            host(|e| put(e, "cache_format", Some(Value::Int(1)))),
        ),
        (
            "headless",
            host(|e| put(e, "presentation_max", Some(Value::Int(99)))),
        ),
    ];
    for (name, s) in &variants {
        assert_eq!(
            go(&m, s, &here(), &pins, &RevocationSet::default()).state,
            State::Compatible,
            "{name} baseline"
        );
    }
    let sets = [
        (
            "manifest",
            RevocationSet {
                manifests: BTreeSet::from([ID.to_string()]),
                ..Default::default()
            },
        ),
        (
            "artifact",
            RevocationSet {
                artifacts: BTreeSet::from([m.artifacts[0].digest.as_str().to_string()]),
                ..Default::default()
            },
        ),
        (
            "operation version",
            RevocationSet {
                operations: BTreeSet::from(["app.records.update_stage@1.3.0".to_string()]),
                ..Default::default()
            },
        ),
        (
            "operation any version",
            RevocationSet {
                operations: BTreeSet::from(["app.records.list@*".to_string()]),
                ..Default::default()
            },
        ),
        (
            "policy",
            RevocationSet {
                policies: BTreeSet::from([4]),
                ..Default::default()
            },
        ),
    ];
    for (rname, set) in &sets {
        for (name, s) in &variants {
            let r = go(&m, s, &here(), &pins, set);
            assert_eq!(
                (r.state, r.read, r.write, r.revoked),
                (State::Blocked, false, false, true),
                "{rname} on {name}: {:?}",
                r.findings
            );
            assert_eq!(r.overall, CompatStatus::SecurityRevoked);
        }
    }
    let unrelated = RevocationSet {
        operations: BTreeSet::from(["app.records.update_stage@1.2.0".to_string()]),
        ..Default::default()
    };
    assert_eq!(
        go(&m, &variants[0].1, &here(), &pins, &unrelated).state,
        State::Compatible
    );
}

#[test]
fn a_revocation_inside_the_manifest_also_blocks_a_pin() {
    let mut m = manifest();
    let Body::Version(with) = read_fixture("version-manifest-rollback-1-1", (1, 1, 0)) else {
        panic!()
    };
    m.revocations = with.revocations;
    m.artifacts = with.artifacts;
    m.artifacts[0].digest = vsf_ir::Digest::new(&format!("sha256:{}", "9a".repeat(32))).unwrap();
    let r = go(
        &m,
        &host(|_| {}),
        &here(),
        &contract_pin(&m),
        &RevocationSet::default(),
    );
    assert_eq!(
        (r.state, r.overall),
        (State::Blocked, CompatStatus::SecurityRevoked),
        "{:?}",
        r.findings
    );
}
