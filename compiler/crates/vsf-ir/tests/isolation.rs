#![allow(clippy::unwrap_used, clippy::expect_used, clippy::panic)]

mod common;
use common::valid_bytes;
use vsf_ir::Body;
use vsf_ir::doc::decode_document;
use vsf_ir::generated::*;
use vsf_ir::matrix::{ObservedContext, evaluate_context, overall};
use vsf_ir::rt::Ctx;
use vsf_ir::semantics::{ExecEnv, authorizes_execution};

fn operation() -> OperationContract {
    match decode_document(&valid_bytes("operation-witnessed-1-1")).unwrap() {
        Body::Operation(o) => o,
        _ => panic!("family"),
    }
}

fn manifest() -> VersionManifest {
    match decode_document(&valid_bytes("version-manifest-rollback-1-1")).unwrap() {
        Body::Version(m) => m,
        _ => panic!("family"),
    }
}

type Mutation = Box<dyn Fn(&mut ExecEnv, &mut OperationContract)>;

fn env() -> ExecEnv {
    ExecEnv {
        now: 1_792_368_400_000,
        runtime_id: "rt_ext01".into(),
        source_state: format!("sha256:{}", "3a".repeat(32)),
        capability_digest: format!("sha256:{}", "c4".repeat(32)),
        principal_ref: "prin_b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2".into(),
        variant_id: "var_a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1".into(),
    }
}

#[test]
fn a_fresh_witness_for_this_exact_context_authorizes_execution() {
    assert_eq!(authorizes_execution(&operation(), &env()), Ok(()));
}

#[test]
fn every_change_of_context_time_or_witness_state_withholds_execution() {
    let cases: Vec<(&str, Mutation, &str)> = vec![
        (
            "other runtime",
            Box::new(|e, _| e.runtime_id = "rt_other".into()),
            "witness_other_environment",
        ),
        (
            "capability changed",
            Box::new(|e, _| e.capability_digest = format!("sha256:{}", "00".repeat(32))),
            "witness_capability_changed",
        ),
        (
            "source state changed",
            Box::new(|e, _| e.source_state = format!("sha256:{}", "00".repeat(32))),
            "witness_source_state_changed",
        ),
        (
            "other principal",
            Box::new(|e, _| e.principal_ref = "prin_00000000000000000000000000000000".into()),
            "witness_other_principal",
        ),
        (
            "other variant",
            Box::new(|e, _| e.variant_id = "var_00000000000000000000000000000000".into()),
            "witness_other_variant",
        ),
        (
            "expired",
            Box::new(|e, _| e.now = 1_792_454_700_000),
            "witness_expired",
        ),
        (
            "revoked",
            Box::new(|e, c| {
                c.witnesses[0].revoked_at = Some(Instant(e.now));
            }),
            "not_executable",
        ),
        (
            "synthetic",
            Box::new(|_, c| c.witnesses[0].synthetic = Some(true)),
            "not_executable",
        ),
        (
            "1.0.0-style witness",
            Box::new(|_, c| {
                let w = &mut c.witnesses[0];
                w.runtime_id = None;
                w.observed_effect = None;
                w.source_state = None;
                w.capability_digest = None;
                w.expires_at = None;
                w.issuer = None;
            }),
            "witness_context_missing",
        ),
        (
            "not active",
            Box::new(|_, c| c.lifecycle = Lifecycle::Proposed),
            "not_executable",
        ),
        (
            "failed run",
            Box::new(|_, c| c.witnesses[0].result = WitnessResult::Failed),
            "not_executable",
        ),
    ];
    for (name, mutate, want) in cases {
        let (mut e, mut c) = (env(), operation());
        mutate(&mut e, &mut c);
        assert_eq!(authorizes_execution(&c, &e), Err(want), "{name}");
    }
}

fn ctx(origin: &str, principal: &str) -> ObservedContext {
    ObservedContext {
        origin: Some(origin.into()),
        principal: Some(principal.into()),
    }
}

const OWNER: &str = "prin_b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2";

fn status(m: &VersionManifest, c: &ObservedContext) -> String {
    let r = evaluate_context(m, c);
    overall(&r).as_str().to_string()
}

#[test]
fn the_owner_at_the_origin_is_compatible_and_anything_else_is_not() {
    let m = manifest();
    assert_eq!(
        status(&m, &ctx("https://crm.example.com", OWNER)),
        "supported"
    );
    let other = evaluate_context(
        &m,
        &ctx(
            "https://crm.example.com",
            "prin_00000000000000000000000000000000",
        ),
    );
    assert_eq!(overall(&other), CompatStatus::NeedsReauth);
    assert!(
        other
            .iter()
            .any(|r| r.axis == "principal" && r.reason == "principal_changed")
    );
    let moved = evaluate_context(&m, &ctx("https://crm.example.org", OWNER));
    assert_eq!(overall(&moved), CompatStatus::SourceIncompatible);
}

#[test]
fn what_is_not_observed_or_not_declared_is_unknown_never_supported() {
    let m = manifest();
    let none = ObservedContext::default();
    let r = evaluate_context(&m, &none);
    assert_eq!(overall(&r), CompatStatus::Unknown);
    assert!(
        r.iter()
            .filter(|x| x.axis != "revocation")
            .all(|x| x.reason != "ok")
    );
    let mut old = manifest();
    old.origin = None;
    assert_eq!(
        overall(&evaluate_context(
            &old,
            &ctx("https://crm.example.com", OWNER)
        )),
        CompatStatus::Unknown
    );
    let mut unbound = manifest();
    unbound.artifacts[0].principal_ref = None;
    let r = evaluate_context(&unbound, &ctx("https://crm.example.com", OWNER));
    assert!(r.iter().any(|x| x.reason == "principal_unbound"));
}

#[test]
fn shared_structure_ignores_the_principal_but_never_the_origin() {
    let mut m = manifest();
    for a in &mut m.artifacts {
        a.visibility = Some(Visibility::GlobalStructure);
        a.principal_ref = None;
    }
    let shared = evaluate_context(
        &m,
        &ctx(
            "https://crm.example.com",
            "prin_00000000000000000000000000000000",
        ),
    );
    assert!(
        shared
            .iter()
            .any(|r| r.axis == "principal" && r.reason == "shared_structure")
    );
    assert_eq!(overall(&shared), CompatStatus::Supported);
    assert_eq!(
        overall(&evaluate_context(
            &m,
            &ctx("https://elsewhere.example", OWNER)
        )),
        CompatStatus::SourceIncompatible
    );
}

#[test]
fn a_revocation_outranks_compatibility_and_a_pin_cannot_revive_it() {
    let mut m = manifest();
    m.artifacts[3].digest = Digest::decode(
        &vsf_canon::Value::Str(format!("sha256:{}", "9a".repeat(32))),
        "",
        &Ctx { version: (1, 1, 0) },
    )
    .unwrap();
    let r = evaluate_context(&m, &ctx("https://crm.example.com", OWNER));
    assert_eq!(overall(&r), CompatStatus::SecurityRevoked);
    assert!(r.iter().any(|x| x.reason == "artifact_revoked"));
    let mut op = manifest();
    op.axes.operations.insert(
        OperationId::decode(
            &vsf_canon::Value::Str("app.records.purge".into()),
            "",
            &Ctx { version: (1, 1, 0) },
        )
        .unwrap(),
        SemVer::decode(
            &vsf_canon::Value::Str("1.0.0".into()),
            "",
            &Ctx { version: (1, 1, 0) },
        )
        .unwrap(),
    );
    assert!(
        evaluate_context(&op, &ctx("https://crm.example.com", OWNER))
            .iter()
            .any(|x| x.reason == "operation_revoked")
    );
    let granted = ctx(
        "https://crm.example.com",
        "prin_c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3",
    );
    assert!(
        evaluate_context(&manifest(), &granted)
            .iter()
            .any(|x| x.reason == "grant_revoked")
    );
}

#[test]
fn an_older_document_is_still_read_and_its_new_fields_are_simply_absent() {
    let old = decode_document(&valid_bytes("operation-update-stage")).unwrap();
    let Body::Operation(c) = old else {
        panic!("family")
    };
    assert!(c.witnesses[0].runtime_id.is_none() && c.witnesses[0].synthetic.is_none());
    assert_eq!(
        authorizes_execution(&c, &env()),
        Err("witness_context_missing")
    );
}
