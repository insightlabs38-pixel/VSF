#![allow(clippy::unwrap_used, clippy::expect_used, clippy::panic)]

use std::path::PathBuf;
use vsf_assemble::{MAX_ROWS, assemble, make_frame};
use vsf_canon::Value;

const VARIANT: &str = "var_00112233445566778899aabbccddeeff";

fn header(origin: &str, variant: &str) -> Value {
    Value::Object(vec![
        ("epoch".into(), Value::Int(7)),
        ("observed_at".into(), Value::Int(1_792_368_200_000)),
        ("origin".into(), Value::Str(origin.into())),
        ("variant_id".into(), Value::Str(variant.into())),
    ])
}

fn fixture(name: &str) -> Vec<u8> {
    let dir =
        PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../../../fixtures/recognition/snapshots");
    std::fs::read(dir.join(format!("{name}.json"))).unwrap()
}

fn run_bytes(snapshot: &[u8]) -> Result<Value, &'static str> {
    let out = assemble(&make_frame(
        &header("http://127.0.0.1:8080", VARIANT),
        snapshot,
    ))?;
    Ok(vsf_canon::parse(&out).unwrap())
}

fn run(name: &str) -> Value {
    run_bytes(&fixture(name)).unwrap()
}

fn entities(out: &Value) -> Vec<Value> {
    match out
        .get("semantic")
        .and_then(|s| s.get("body"))
        .and_then(|b| b.get("entities"))
    {
        Some(Value::Array(a)) => a.clone(),
        _ => Vec::new(),
    }
}

fn field_names(entity: &Value) -> Vec<String> {
    match entity.get("fields") {
        Some(Value::Array(a)) => a
            .iter()
            .filter_map(|f| f.get("name").and_then(Value::as_str).map(str::to_string))
            .collect(),
        _ => Vec::new(),
    }
}

fn actions(out: &Value) -> Vec<(String, String)> {
    match out.get("actions") {
        Some(Value::Object(m)) => m
            .iter()
            .map(|(k, v)| {
                (
                    k.clone(),
                    v.get("state")
                        .and_then(Value::as_str)
                        .unwrap_or("")
                        .to_string(),
                )
            })
            .collect(),
        _ => Vec::new(),
    }
}

fn snapshot(nodes: &[String]) -> Vec<u8> {
    format!(
        r#"{{"schema":"vsf-observation-snapshot-v1","nodes":[{}]}}"#,
        nodes.join(",")
    )
    .into_bytes()
}

fn node(
    id: u32,
    parent: i64,
    tag: &str,
    role: Option<&str>,
    text: Option<&str>,
    flags: u32,
) -> String {
    let q = |v: Option<&str>| {
        v.map_or("null".to_string(), |x| {
            String::from_utf8(vsf_canon::to_canonical(&Value::Str(x.into()))).unwrap()
        })
    };
    let scope = if id == 1 {
        r#"{"kind":"document","origin":"http://fixture.invalid","mode":null,"opaque":null}"#
    } else {
        "null"
    };
    format!(
        r#"{{"id":{id},"parent":{parent},"tag":"{tag}","role":{},"name":{},"text":{},"flags":{flags},"rels":[],"scope":{scope}}}"#,
        q(role),
        q(text),
        q(text)
    )
}

#[test]
fn orders_table_becomes_a_typed_grid_with_row_actions() {
    let out = run("table-orders");
    let ents = entities(&out);
    assert_eq!(ents.len(), 2);
    assert_eq!(
        ents[0].get("label").and_then(Value::as_str),
        Some("Open orders")
    );
    assert_eq!(field_names(&ents[0]), ["Order", "Customer", "Total"]);
    let types: Vec<_> = match ents[0].get("fields") {
        Some(Value::Array(a)) => a
            .iter()
            .filter_map(|f| f.get("value_type").and_then(Value::as_str))
            .collect(),
        _ => Vec::new(),
    };
    assert_eq!(types, ["integer", "text", "decimal"]);
    let rows = match out.get("rows").and_then(|r| r.get("t_open_orders")) {
        Some(Value::Array(a)) => a.clone(),
        _ => Vec::new(),
    };
    let keys: Vec<_> = rows
        .iter()
        .filter_map(|r| r.get("key").and_then(Value::as_str))
        .collect();
    assert_eq!(keys, ["k_1001", "k_1002", "k_1003"]);
    assert_eq!(
        rows[2]
            .get("values")
            .and_then(|v| v.get("f_open_orders_total"))
            .and_then(Value::as_str),
        Some("99.99")
    );
    let acts = actions(&out);
    let names: Vec<_> = acts.iter().map(|a| a.0.as_str()).collect();
    assert_eq!(
        names,
        [
            "act_page_actions_export_csv",
            "act_t_open_orders_delete",
            "act_t_open_orders_open"
        ]
    );
    assert!(
        acts.iter().all(|a| a.1 == "unknown"),
        "no write or read is available without a witness"
    );
    let open = out
        .get("actions")
        .and_then(|a| a.get("act_t_open_orders_open"))
        .unwrap();
    let Some(Value::Array(src)) = open.get("sources") else {
        panic!("sources")
    };
    assert_eq!(src.len(), 3);
    assert_eq!(src[1].get("row").and_then(Value::as_str), Some("k_1002"));
}

#[test]
fn profile_form_keeps_labels_choices_and_scoped_actions() {
    let out = run("plain-form");
    let ents = entities(&out);
    assert_eq!(ents.len(), 1);
    assert_eq!(
        field_names(&ents[0]),
        [
            "First name",
            "Last name",
            "Email",
            "Country",
            "Weekly digest",
            "Notes"
        ]
    );
    let acts = actions(&out);
    assert_eq!(
        acts.iter().map(|a| a.0.as_str()).collect::<Vec<_>>(),
        [
            "act_form_customer_profile_cancel",
            "act_form_customer_profile_save_changes"
        ]
    );
}

#[test]
fn credentials_and_hidden_fields_are_withheld_not_rendered() {
    let out = run("adversarial");
    let Some(Value::Array(w)) = out.get("withheld") else {
        panic!("withheld")
    };
    let reasons: Vec<_> = w
        .iter()
        .filter_map(|x| x.get("reason").and_then(Value::as_str))
        .collect();
    assert!(reasons.contains(&"credential_field") && reasons.contains(&"not_visible"));
    for e in entities(&out) {
        assert!(
            !field_names(&e)
                .iter()
                .any(|n| n.to_lowercase().contains("password"))
        );
    }
    let disabled = actions(&out)
        .into_iter()
        .find(|a| a.0.ends_with("pay_later"))
        .unwrap();
    assert_eq!(disabled.1, "unusable");
}

#[test]
fn every_fixture_assembles_to_a_valid_semantic_document() {
    for name in [
        "table-orders",
        "plain-form",
        "adversarial",
        "aria-grid",
        "heldout-datagrid-edit",
        "heldout-shadow-ds",
    ] {
        let out = run(name);
        let doc = vsf_canon::to_canonical(out.get("semantic").unwrap());
        let (report, valid) = vsf_ir::api::validate_report(&doc).unwrap();
        assert!(valid, "{name}: {}", String::from_utf8_lossy(&report));
        let digest = out.get("semantic_digest").and_then(Value::as_str).unwrap();
        let parsed = vsf_canon::parse(&report).unwrap();
        assert_eq!(
            parsed
                .get("decode")
                .and_then(|d| d.get("digest"))
                .and_then(Value::as_str),
            Some(digest),
            "{name}"
        );
    }
}

#[test]
fn output_is_deterministic_and_bound_to_the_origin() {
    let a = assemble(&make_frame(
        &header("http://127.0.0.1:8080", VARIANT),
        &fixture("table-orders"),
    ))
    .unwrap();
    let b = assemble(&make_frame(
        &header("http://127.0.0.1:8080", VARIANT),
        &fixture("table-orders"),
    ))
    .unwrap();
    assert_eq!(a, b);
    let other = assemble(&make_frame(
        &header("http://127.0.0.1:8081", VARIANT),
        &fixture("table-orders"),
    ))
    .unwrap();
    assert_ne!(
        a, other,
        "the document is bound to the origin it was observed on"
    );
}

#[test]
fn editable_grid_without_readable_columns_is_not_shown_as_an_empty_table() {
    let out = run("heldout-datagrid-edit");
    let views = match out.get("views") {
        Some(Value::Array(v)) => v
            .iter()
            .filter_map(|x| x.get("layout").and_then(Value::as_str))
            .collect::<Vec<_>>(),
        _ => Vec::new(),
    };
    assert!(!views.contains(&"grid"));
}

#[test]
fn malformed_frames_and_headers_are_refused_with_stable_codes() {
    let snap = fixture("plain-form");
    let good = make_frame(&header("http://127.0.0.1:8080", VARIANT), &snap);
    assert_eq!(assemble(&[]), Err("assemble.bad_frame"));
    assert_eq!(assemble(&good[..3]), Err("assemble.bad_frame"));
    assert_eq!(assemble(&0u32.to_be_bytes()), Err("assemble.bad_frame"));
    assert_eq!(
        assemble(&99_999u32.to_be_bytes()),
        Err("assemble.bad_frame")
    );
    let mut cut = good.clone();
    cut.truncate(10);
    assert_eq!(assemble(&cut), Err("assemble.bad_frame"));
    let bad = |h: Value| assemble(&make_frame(&h, &snap)).unwrap_err();
    assert_eq!(bad(header("ftp://x.test", VARIANT)), "assemble.bad_origin");
    assert_eq!(
        bad(header("http://UPPER.test", VARIANT)),
        "assemble.bad_origin"
    );
    assert_eq!(
        bad(header("http://a.test/path", VARIANT)),
        "assemble.bad_origin"
    );
    assert_eq!(
        bad(header("http://a.test", "var_short")),
        "assemble.bad_variant"
    );
    assert_eq!(bad(Value::Null), "assemble.bad_header");
    assert_eq!(bad(Value::Object(vec![])), "assemble.bad_header");
    let negative = Value::Object(vec![
        ("epoch".into(), Value::Int(-1)),
        ("observed_at".into(), Value::Int(0)),
        ("origin".into(), Value::Str("http://a.test".into())),
        ("variant_id".into(), Value::Str(VARIANT.into())),
    ]);
    assert_eq!(bad(negative), "assemble.bad_header");
    assert_eq!(
        assemble(&make_frame(&header("http://a.test", VARIANT), b"{}")).unwrap_err(),
        "obs.bad_schema"
    );
    assert_eq!(
        assemble(&make_frame(&header("http://a.test", VARIANT), b"not json")).unwrap_err(),
        "obs.parse"
    );
}

#[test]
fn hostile_text_cannot_break_the_document_or_smuggle_controls() {
    let nodes = vec![
        node(1, -1, "#document", None, None, 1),
        node(2, 1, "main", Some("main"), None, 1),
        node(3, 2, "table", Some("table"), None, 1),
        node(4, 3, "tr", Some("row"), None, 1),
        node(
            5,
            4,
            "th",
            Some("columnheader"),
            Some("Na\u{202e}me\u{0}\n"),
            1,
        ),
        node(6, 3, "tr", Some("row"), None, 1),
        node(7, 6, "td", Some("cell"), Some("\u{7}x\u{2028}y"), 1),
        node(8, 3, "tr", Some("row"), None, 1),
        node(9, 8, "td", Some("cell"), Some("xy"), 1),
    ];
    let out = run_bytes(&snapshot(&nodes)).unwrap();
    let doc = vsf_canon::to_canonical(out.get("semantic").unwrap());
    let (_, valid) = vsf_ir::api::validate_report(&doc).unwrap();
    assert!(valid);
    let text = String::from_utf8(vsf_canon::to_canonical(&out)).unwrap();
    assert!(!text.contains("\\u202e") && !text.contains("\\u2028") && !text.contains("\\u0007"));
    let rows = out.get("rows").and_then(|r| r.get("t_table_1")).cloned();
    let Some(Value::Array(rows)) = rows else {
        panic!("rows: {text}")
    };
    assert_eq!(rows.len(), 2);
    assert_eq!(
        rows[0].get("key").and_then(Value::as_str),
        Some("r1"),
        "duplicate first cells fall back to ordinals"
    );
}

#[test]
fn oversized_tables_are_truncated_and_say_so() {
    let mut nodes = vec![
        node(1, -1, "#document", None, None, 1),
        node(2, 1, "table", Some("table"), None, 1),
        node(3, 2, "tr", Some("row"), None, 1),
        node(4, 3, "th", Some("columnheader"), Some("N"), 1),
    ];
    let mut id = 5;
    for i in 0..(MAX_ROWS + 50) {
        nodes.push(node(id, 2, "tr", Some("row"), None, 1));
        nodes.push(node(
            id + 1,
            i64::from(id),
            "td",
            Some("cell"),
            Some(&i.to_string()),
            1,
        ));
        id += 2;
    }
    let out = run_bytes(&snapshot(&nodes)).unwrap();
    let limits = out.get("limits").unwrap();
    assert_eq!(limits.get("truncated"), Some(&Value::Bool(true)));
    let total: usize = match out.get("rows") {
        Some(Value::Object(m)) => m
            .iter()
            .map(|(_, v)| if let Value::Array(a) = v { a.len() } else { 0 })
            .sum(),
        _ => 0,
    };
    assert_eq!(total, MAX_ROWS);
}

#[test]
fn identifiers_stay_unique_when_names_collide() {
    let mut nodes = vec![
        node(1, -1, "#document", None, None, 1),
        node(2, 1, "form", Some("form"), None, 1),
    ];
    for id in 3..6 {
        nodes.push(node(id, 2, "button", Some("button"), Some("Save"), 3));
    }
    let out = run_bytes(&snapshot(&nodes)).unwrap();
    let doc = vsf_canon::to_canonical(out.get("semantic").unwrap());
    assert!(vsf_ir::api::validate_report(&doc).unwrap().1);
    assert_eq!(
        actions(&out).len(),
        1,
        "same-named actions in one scope are one action with three sources"
    );
}
