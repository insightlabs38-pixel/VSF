#![allow(clippy::unwrap_used, clippy::expect_used, clippy::panic)]

mod common;
use common::{fixtures, pointer_set, valid_bytes, valid_names};
use vsf_canon::{Value, parse, to_canonical};
use vsf_ir::doc::decode_document;
use vsf_ir::semantics::validate;

fn codes(bytes: &[u8]) -> Vec<&'static str> {
    let body = decode_document(bytes).unwrap();
    let mut c: Vec<_> = validate(&body).into_iter().map(|v| v.code).collect();
    c.sort_unstable();
    c.dedup();
    c
}

#[test]
fn every_valid_fixture_passes_semantic_validation() {
    for name in valid_names() {
        let body = decode_document(&valid_bytes(&name)).unwrap();
        let found = validate(&body);
        assert!(found.is_empty(), "{name}: {found:?}");
    }
}

#[test]
fn every_semantic_case_reports_exactly_the_expected_codes() {
    let doc = parse(&std::fs::read(fixtures().join("semantic/cases.json")).unwrap()).unwrap();
    let Some(Value::Array(cases)) = doc.get("cases") else {
        panic!("cases")
    };
    assert!(cases.len() >= 40, "{}", cases.len());
    for case in cases {
        let name = case.get("name").unwrap().as_str().unwrap();
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
        let Some(Value::Array(want)) = case.get("expect") else {
            panic!("expect")
        };
        let mut want: Vec<&str> = want.iter().map(|x| x.as_str().unwrap()).collect();
        want.sort_unstable();
        assert_eq!(codes(&to_canonical(&v)), want, "{name}");
    }
}
