#![allow(clippy::unwrap_used, clippy::expect_used, clippy::panic)]

mod common;

use vsf_canon::{Value, parse};
use vsf_versioning::api::promotion_report;

fn frame(ctx: &str, doc: &[u8]) -> Vec<u8> {
    let mut out = (ctx.len() as u32).to_le_bytes().to_vec();
    out.extend_from_slice(ctx.as_bytes());
    out.extend_from_slice(doc);
    out
}

fn ctx(now: i64, last: Option<(i64, &str)>, sample: &common::sample::Sample) -> String {
    let trust = String::from_utf8(sample.trust.clone()).unwrap();
    let last = last.map_or("null".to_string(), |(s, d)| {
        format!("{{\"sequence\":{s},\"digest\":\"{d}\"}}")
    });
    format!("{{\"now_ms\":{now},\"trust\":{trust},\"last\":{last}}}")
}

fn report(frame: &[u8]) -> Value {
    parse(&promotion_report(frame).unwrap()).unwrap()
}

#[test]
fn the_report_carries_the_verdict_and_the_facts_the_store_needs() {
    let s = common::sample::build();
    let (_, id, doc) = &s.manifests[1];
    let r = report(&frame(&ctx(1_792_368_500_000, None, &s), doc));
    assert_eq!(r.get("verified"), Some(&Value::Bool(true)), "{r:?}");
    assert_eq!(r.get("id").and_then(Value::as_str), Some(id.as_str()));
    assert_eq!(r.get("sequence").and_then(Value::as_int), Some(2));
    assert_eq!(
        r.get("previous").and_then(Value::as_str),
        Some(s.manifests[0].1.as_str())
    );
    let Some(Value::Array(a)) = r.get("artifacts") else {
        panic!()
    };
    assert_eq!(a.len(), 5);
}

#[test]
fn replay_expiry_tampering_and_garbage_are_unverified_not_trusted() {
    let s = common::sample::build();
    let (_, _, doc) = &s.manifests[1];
    let why = |f: Vec<u8>| {
        report(&f)
            .get("reason")
            .and_then(Value::as_str)
            .unwrap()
            .to_string()
    };
    assert_eq!(
        why(frame(
            &ctx(1_792_368_500_000, Some((3, "sha256:x")), &s),
            doc
        )),
        "promotion.rollback"
    );
    assert_eq!(
        why(frame(
            &ctx(1_792_368_500_000, Some((2, "sha256:x")), &s),
            doc
        )),
        "promotion.sequence_conflict"
    );
    assert_eq!(
        why(frame(&ctx(9_999_999_999_999, None, &s), doc)),
        "promotion.expired"
    );
    let mut tampered = doc.clone();
    let at = tampered.windows(5).position(|w| w == b"\"pv2\"").unwrap();
    tampered[at + 3] = b'9';
    assert_ne!(why(frame(&ctx(1_792_368_500_000, None, &s), &tampered)), "");
    assert_eq!(
        why(frame(&ctx(1_792_368_500_000, None, &s), b"{}")),
        "undecodable"
    );
    let op = &s
        .blobs
        .iter()
        .find(|(_, b)| b.starts_with(b"{\"body\":{\"contract_version\""))
        .map(|(_, b)| b.clone());
    if let Some(op) = op {
        assert_eq!(
            why(frame(&ctx(1_792_368_500_000, None, &s), op)),
            "not_a_version_manifest"
        );
    }
    assert!(promotion_report(&[1, 2]).is_err());
    assert!(promotion_report(&frame("not json", b"{}")).is_err());
}
