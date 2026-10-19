#![allow(clippy::unwrap_used, clippy::expect_used, clippy::panic)]

mod common;
use common::{fixtures, pointer_set, valid_bytes, valid_names};
use vsf_canon::{Value, parse, sha256_hex, to_canonical};
use vsf_ir::doc::{
    decode_document, decode_value, document_digest_at, encode_document, encode_document_at,
    typed_digest,
};

#[test]
fn every_valid_fixture_decodes_and_reencodes_to_identical_bytes() {
    let names = valid_names();
    assert!(names.len() >= 10);
    for name in names {
        let bytes = valid_bytes(&name);
        let (body, ver) =
            decode_value(&parse(&bytes).unwrap()).unwrap_or_else(|e| panic!("{name}: {e}"));
        assert_eq!(encode_document_at(&body, ver).unwrap(), bytes, "{name}");
        assert_eq!(decode_document(&bytes).unwrap(), body, "{name}");
        let upgraded = decode_document(&encode_document(&body)).unwrap();
        assert_eq!(
            upgraded, body,
            "{name}: re-encoding at the current version loses nothing"
        );
    }
}

fn pins() -> Value {
    parse(&std::fs::read(fixtures().join("pins.json")).unwrap()).unwrap()
}

fn compute_pins() -> Value {
    let entries = valid_names()
        .into_iter()
        .map(|name| {
            let bytes = valid_bytes(&name);
            let (body, ver) = decode_value(&parse(&bytes).unwrap()).unwrap();
            vsf_ir::rt::object(vec![
                ("name", vsf_ir::rt::text(&name)),
                ("canonical_bytes", vsf_ir::rt::int(bytes.len() as i64)),
                ("canonical_sha256", vsf_ir::rt::text(&sha256_hex(&bytes))),
                (
                    "document_digest",
                    vsf_ir::rt::text(&document_digest_at(&body, ver).unwrap()),
                ),
            ])
        })
        .collect();
    vsf_ir::rt::object(vec![
        ("schema", vsf_ir::rt::text("vsf-ir-pins-v1")),
        ("entries", Value::Array(entries)),
    ])
}

#[test]
fn pinned_digests_match_this_implementation() {
    if std::env::var_os("VSF_UPDATE_PINS").is_some() {
        let mut text = String::from_utf8(to_canonical(&compute_pins())).unwrap();
        text.push('\n');
        std::fs::write(fixtures().join("pins.json"), text).unwrap();
    }
    assert_eq!(
        to_canonical(&pins()),
        to_canonical(&compute_pins()),
        "run with VSF_UPDATE_PINS=1 after an intentional schema change"
    );
}

#[test]
fn nfc_and_nfd_text_are_preserved_and_stay_distinct() {
    let bytes = valid_bytes("semantic-orders");
    let body = decode_document(&bytes).unwrap();
    let vsf_ir::Body::Semantic(sem) = &body else {
        panic!("family")
    };
    let labels: Vec<&str> = sem
        .entities
        .iter()
        .filter_map(|e| e.label.as_ref().map(|l| l.as_str()))
        .collect();
    let nfc = labels
        .iter()
        .find(|l| **l == "Caf\u{e9}")
        .expect("NFC label kept");
    let nfd = labels
        .iter()
        .find(|l| **l == "Cafe\u{301}")
        .expect("NFD label kept");
    assert_ne!(nfc, nfd);
    assert_ne!(sha256_hex(nfc.as_bytes()), sha256_hex(nfd.as_bytes()));
    assert!(labels.contains(&"\u{5e9}\u{5dc}\u{5d5}\u{5dd} \u{1f600}"));
}

#[test]
fn typed_digests_are_domain_separated() {
    let canonical = b"{}";
    assert_ne!(
        typed_digest("document:operation", canonical),
        typed_digest("document:semantic", canonical)
    );
    assert_ne!(
        typed_digest("a", canonical),
        format!("sha256:{}", sha256_hex(canonical))
    );
    assert_eq!(typed_digest("a", canonical), typed_digest("a", canonical));
}

#[test]
fn every_invalid_case_fails_closed_with_the_expected_code_and_path() {
    let doc = parse(&std::fs::read(fixtures().join("invalid/cases.json")).unwrap()).unwrap();
    let Some(Value::Array(cases)) = doc.get("cases") else {
        panic!("cases")
    };
    assert!(cases.len() >= 70);
    for case in cases {
        let name = case.get("name").unwrap().as_str().unwrap();
        let expect = case.get("expect").unwrap();
        let (want_code, want_path) = (
            expect.get("code").unwrap().as_str().unwrap(),
            expect.get("path").unwrap().as_str().unwrap(),
        );
        let bytes: Vec<u8> = if let Some(Value::Str(raw)) = case.get("raw") {
            raw.clone().into_bytes()
        } else {
            let mut v = parse(&valid_bytes(case.get("base").unwrap().as_str().unwrap())).unwrap();
            let Some(Value::Array(edits)) = case.get("edits") else {
                panic!("edits")
            };
            for e in edits {
                let path = e.get("path").unwrap().as_str().unwrap();
                match e.get("op").unwrap().as_str().unwrap() {
                    "set" => pointer_set(&mut v, path, Some(e.get("value").unwrap().clone())),
                    "remove" => pointer_set(&mut v, path, None),
                    other => panic!("op {other}"),
                }
            }
            to_canonical(&v)
        };
        let err = decode_document(&bytes).expect_err(name);
        assert_eq!(
            (err.code, err.path.as_str()),
            (want_code, want_path),
            "{name}: {err}"
        );
    }
}
