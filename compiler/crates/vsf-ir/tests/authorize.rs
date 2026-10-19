#![allow(clippy::unwrap_used, clippy::expect_used, clippy::panic)]

mod common;
use common::*;
use vsf_canon::{Value, parse};
use vsf_ir::api::authorize_report;

fn env(now: i64, runtime: &str) -> Vec<u8> {
    format!(
        r#"{{"now":{now},"runtime_id":"{runtime}","source_state":"sha256:{}","capability_digest":"sha256:{}","principal_ref":"prin_b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2","variant_id":"var_a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1"}}"#,
        "3a".repeat(32),
        "c4".repeat(32)
    )
    .into_bytes()
}

fn report(doc: &[u8], env: &[u8]) -> (bool, String, String) {
    let v = parse(&authorize_report(doc, env).unwrap()).unwrap();
    let Value::Object(f) = v else { panic!() };
    let text = |k: &str| match f.iter().find(|(n, _)| n == k).map(|(_, v)| v) {
        Some(Value::Str(s)) => s.clone(),
        _ => String::new(),
    };
    let ok = matches!(
        f.iter().find(|(n, _)| n == "authorized").map(|(_, v)| v),
        Some(Value::Bool(true))
    );
    (ok, text("reason"), text("digest"))
}

const NOW: i64 = 1_792_368_400_000;

#[test]
fn a_fresh_witness_for_this_context_authorizes_and_names_the_document() {
    let doc = valid_bytes("operation-witnessed-1-1");
    let (ok, reason, digest) = report(&doc, &env(NOW, "rt_ext01"));
    assert!(ok, "{reason}");
    assert!(digest.starts_with("sha256:"));
}

#[test]
fn every_other_context_is_refused_with_its_reason() {
    let doc = valid_bytes("operation-witnessed-1-1");
    assert_eq!(
        report(&doc, &env(NOW, "rt_other")).1,
        "witness_other_environment"
    );
    assert_eq!(
        report(&doc, &env(1_792_454_700_000, "rt_ext01")).1,
        "witness_expired"
    );
    assert_eq!(report(&doc, b"{}").1, "env_malformed");
    assert_eq!(report(&doc, b"not json").1, "env_malformed");
    assert_eq!(report(&doc, br#"{"now":"x"}"#).1, "env_malformed");
}

#[test]
fn only_operation_contracts_can_be_authorized() {
    let doc = valid_bytes("semantic-orders");
    let (ok, reason, _) = report(&doc, &env(NOW, "rt_ext01"));
    assert!(!ok);
    assert_eq!(reason, "not_operation_contract");
}

#[test]
fn undecodable_and_non_canonical_input_never_authorize() {
    let (ok, reason, _) = report(
        br#"{"schema":"vsf.operation/1.1.0"}"#,
        &env(NOW, "rt_ext01"),
    );
    assert!(!ok);
    assert_eq!(reason, "undecodable");
    assert!(authorize_report(b"\xff", &env(NOW, "rt_ext01")).is_err());
}
