#![allow(clippy::unwrap_used, clippy::expect_used, clippy::panic)]

mod common;
use common::fixtures;
use vsf_canon::{Value, parse, to_canonical};
use vsf_ir::Body;
use vsf_ir::doc::decode_document;
use vsf_ir::semantics::validate_presentation;

fn read(path: &std::path::Path) -> Value {
    parse(&std::fs::read(path).unwrap()).unwrap()
}

fn semantic() -> vsf_ir::SemanticIR {
    let digest = vsf_ir::doc::digest_at("semantic", (1, 0, 0)).unwrap();
    let text = std::fs::read_to_string(fixtures().join("valid/semantic-orders.json"))
        .unwrap()
        .replace("$DIGEST", digest);
    let Body::Semantic(s) =
        decode_document(&to_canonical(&parse(text.as_bytes()).unwrap())).unwrap()
    else {
        panic!()
    };
    s
}

fn presentation(v: &Value) -> vsf_ir::PresentationIR {
    let Body::Presentation(p) = decode_document(&to_canonical(v)).unwrap() else {
        panic!()
    };
    p
}

fn edit(v: &Value, f: impl FnOnce(&mut Vec<(String, Value)>)) -> Value {
    let Value::Object(e) = v else { panic!() };
    let mut e = e.clone();
    f(&mut e);
    Value::Object(e)
}

fn slot<'a>(e: &'a mut [(String, Value)], k: &str) -> &'a mut Value {
    &mut e.iter_mut().find(|(n, _)| n == k).unwrap().1
}

fn codes(doc: &Value) -> Vec<&'static str> {
    validate_presentation(&presentation(doc), Some(&semantic()))
        .iter()
        .map(|v| v.code)
        .collect()
}

fn with_region(doc: &Value, id: &str, f: impl Fn(&mut Vec<(String, Value)>)) -> Value {
    edit(doc, |top| {
        let Value::Object(body) = slot(top, "body") else {
            panic!()
        };
        let Value::Array(rs) = slot(body, "regions") else {
            panic!()
        };
        for r in rs.iter_mut() {
            if r.get("id").and_then(Value::as_str) == Some(id) {
                let Value::Object(e) = r else { panic!() };
                f(e);
            }
        }
    })
}

fn set(e: &mut Vec<(String, Value)>, k: &str, v: Value) {
    e.retain(|(n, _)| n != k);
    e.push((k.to_string(), v));
}

#[test]
fn the_compositions_the_typescript_compiler_writes_validate_in_rust_against_their_semantics() {
    let dir = fixtures().join("../../../fixtures/ui/compositions");
    let mut seen = 0;
    for entry in std::fs::read_dir(dir).unwrap() {
        let path = entry.unwrap().path();
        let doc = read(&path);
        assert!(
            codes(&doc).is_empty(),
            "{}: {:?}",
            path.display(),
            codes(&doc)
        );
        seen += 1;
    }
    assert_eq!(seen, 4);
}

#[test]
fn a_region_that_can_be_hidden_may_not_hold_a_risky_operation() {
    let doc = read(&fixtures().join("../../../fixtures/ui/compositions/enterprise-grid.json"));
    let hidden = with_region(&doc, "shell_main", |e| {
        set(e, "role", Value::Str("toolbar".into()));
        set(
            e,
            "dock",
            Value::Object(vec![
                ("edge".into(), Value::Str("top".into())),
                ("collapsible".into(), Value::Bool(true)),
                ("resizable".into(), Value::Bool(false)),
            ]),
        );
    });
    assert!(
        codes(&hidden).contains(&"presentation.hideable_holds_risky_action"),
        "{:?}",
        codes(&hidden)
    );
    let narrow = with_region(&doc, "shell_main", |e| {
        set(
            e,
            "responsive",
            Value::Array(vec![Value::Object(vec![
                ("below".into(), Value::Int(40)),
                ("arrange".into(), Value::Str("stack".into())),
                ("collapse".into(), Value::Bool(true)),
            ])]),
        );
    });
    assert!(codes(&narrow).contains(&"presentation.hideable_holds_risky_action"));
}

#[test]
fn layout_fields_are_refused_in_documents_written_before_they_existed() {
    let doc = read(&fixtures().join("../../../fixtures/ui/compositions/enterprise-grid.json"));
    let old = edit(&doc, |e| {
        set(e, "schema", Value::Str("vsf.presentation/1.1.0".into()));
        set(
            e,
            "schema_digest",
            Value::Str(
                vsf_ir::doc::digest_at("presentation", (1, 1, 0))
                    .unwrap()
                    .into(),
            ),
        );
    });
    let err = decode_document(&to_canonical(&old)).unwrap_err();
    assert!(
        err.path.contains("regions") || err.path.contains("customization"),
        "{err:?}"
    );
}
