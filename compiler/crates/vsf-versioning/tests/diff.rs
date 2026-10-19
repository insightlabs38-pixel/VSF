#![allow(clippy::unwrap_used, clippy::expect_used, clippy::panic)]

mod common;

use common::{Edit, edited, s};
use vsf_canon::Value;
use vsf_versioning::diff::{Class, Op, diff};

const V: (u32, u32, u32) = (1, 0, 0);

fn obj(pairs: &[(&str, Value)]) -> Value {
    Value::Object(
        pairs
            .iter()
            .map(|(k, v)| (k.to_string(), v.clone()))
            .collect(),
    )
}

fn arr(items: &[&str]) -> Value {
    Value::Array(items.iter().map(|i| s(i)).collect())
}

struct Case {
    name: &'static str,
    fixture: &'static str,
    edits: Vec<Edit<'static>>,
    class: Class,
    path: &'static str,
    op: Op,
}

fn run(cases: Vec<Case>) {
    for c in cases {
        let from = edited(c.fixture, V, &[]);
        let to = edited(c.fixture, V, &c.edits);
        let d = diff(&from, V, &to, V).unwrap();
        assert_eq!(d.class, Some(c.class), "{}: {:?}", c.name, d.changes);
        let hit = d.changes.iter().find(|x| x.path == c.path);
        assert!(
            hit.is_some_and(|h| h.op == c.op && h.class == c.class),
            "{}: wanted {} {:?} in {:?}",
            c.name,
            c.path,
            c.op,
            d.changes
        );
        let back = diff(&to, V, &from, V).unwrap();
        assert_eq!(
            back.changes.len(),
            d.changes.len(),
            "{}: not symmetric",
            c.name
        );
    }
}

fn new_field() -> Value {
    obj(&[
        ("id", s("note")),
        ("name", s("Note")),
        ("value_type", s("text")),
        ("nullable", s("yes")),
        ("certainty", s("verified")),
        ("evidence", arr(&["ev_dom"])),
    ])
}

#[test]
fn semantic_changes_fall_into_distinct_classes() {
    let f = "semantic-orders";
    run(vec![
        Case {
            name: "entity label",
            fixture: f,
            edits: vec![Edit::Set("/body/entities/0/label", s("Purchase"))],
            class: Class::Cosmetic,
            path: "/entities[order]/label",
            op: Op::Modified,
        },
        Case {
            name: "field name",
            fixture: f,
            edits: vec![Edit::Set("/body/entities/0/fields/0/name", s("Number"))],
            class: Class::Cosmetic,
            path: "/entities[order]/fields[order_id]/name",
            op: Op::Modified,
        },
        Case {
            name: "evidence reference",
            fixture: f,
            edits: vec![Edit::Push(
                "/body/entities/0/fields/0/evidence",
                s("ev_doc"),
            )],
            class: Class::Cosmetic,
            path: "/entities[order]/fields[order_id]/evidence",
            op: Op::Modified,
        },
        Case {
            name: "new field",
            fixture: f,
            edits: vec![Edit::Push("/body/entities/0/fields", new_field())],
            class: Class::Additive,
            path: "/entities[order]/fields[note]",
            op: Op::Added,
        },
        Case {
            name: "removed field",
            fixture: f,
            edits: vec![Edit::Remove("/body/entities/0/fields/1")],
            class: Class::Breaking,
            path: "/entities[order]/fields[total]",
            op: Op::Removed,
        },
        Case {
            name: "field type",
            fixture: f,
            edits: vec![Edit::Set(
                "/body/entities/0/fields/0/value_type",
                s("integer"),
            )],
            class: Class::Breaking,
            path: "/entities[order]/fields[order_id]/value_type",
            op: Op::Modified,
        },
        Case {
            name: "certainty weakened",
            fixture: f,
            edits: vec![Edit::Set("/body/entities/0/certainty", s("inferred"))],
            class: Class::Breaking,
            path: "/entities[order]/certainty",
            op: Op::Modified,
        },
        Case {
            name: "declared effect",
            fixture: f,
            edits: vec![Edit::Set("/body/actions/0/effect/declared", s("read_only"))],
            class: Class::Permission,
            path: "/actions[submit_order]/effect/declared",
            op: Op::Modified,
        },
        Case {
            name: "source confirmation",
            fixture: f,
            edits: vec![Edit::Set("/body/actions/0/source_confirmation", s("yes"))],
            class: Class::Permission,
            path: "/actions[submit_order]/source_confirmation",
            op: Op::Modified,
        },
        Case {
            name: "removed action",
            fixture: f,
            edits: vec![Edit::Remove("/body/actions/1")],
            class: Class::Breaking,
            path: "/actions[delete_order]",
            op: Op::Removed,
        },
        Case {
            name: "primitive set",
            fixture: f,
            edits: vec![Edit::Set(
                "/body/primitive_set",
                s(&format!("sha256:{}", "9".repeat(64))),
            )],
            class: Class::Unknown,
            path: "/primitive_set",
            op: Op::Modified,
        },
        Case {
            name: "compiler version",
            fixture: f,
            edits: vec![Edit::Set("/body/compiler_version", s("0.2.0+abcdef123456"))],
            class: Class::Cosmetic,
            path: "/compiler_version",
            op: Op::Modified,
        },
    ]);
}

#[test]
fn operation_changes_fall_into_distinct_classes() {
    let f = "operation-update-stage";
    let optional = obj(&[("type", s("text")), ("required", Value::Bool(false))]);
    let required = obj(&[("type", s("text")), ("required", Value::Bool(true))]);
    run(vec![
        Case {
            name: "contract version",
            fixture: f,
            edits: vec![Edit::Set("/body/contract_version", s("1.4.0"))],
            class: Class::Cosmetic,
            path: "/contract_version",
            op: Op::Modified,
        },
        Case {
            name: "verifier suite",
            fixture: f,
            edits: vec![Edit::Set(
                "/body/provenance/verifier_suite",
                s("other_suite"),
            )],
            class: Class::Cosmetic,
            path: "/provenance/verifier_suite",
            op: Op::Modified,
        },
        Case {
            name: "optional input",
            fixture: f,
            edits: vec![Edit::Set("/body/inputs/note", optional)],
            class: Class::Additive,
            path: "/inputs/note",
            op: Op::Added,
        },
        Case {
            name: "required input",
            fixture: f,
            edits: vec![Edit::Set("/body/inputs/note", required)],
            class: Class::Breaking,
            path: "/inputs/note",
            op: Op::Added,
        },
        Case {
            name: "removed input",
            fixture: f,
            edits: vec![Edit::Remove("/body/inputs/stage")],
            class: Class::Breaking,
            path: "/inputs/stage",
            op: Op::Removed,
        },
        Case {
            name: "input type",
            fixture: f,
            edits: vec![Edit::Set("/body/inputs/record_id/type", s("integer"))],
            class: Class::Breaking,
            path: "/inputs/record_id/type",
            op: Op::Modified,
        },
        Case {
            name: "effect class",
            fixture: f,
            edits: vec![Edit::Set("/body/effect/class", s("destructive"))],
            class: Class::Permission,
            path: "/effect/class",
            op: Op::Modified,
        },
        Case {
            name: "confirmation",
            fixture: f,
            edits: vec![Edit::Set("/body/effect/confirmation", s("none"))],
            class: Class::Permission,
            path: "/effect/confirmation",
            op: Op::Modified,
        },
        Case {
            name: "required scopes",
            fixture: f,
            edits: vec![Edit::Push(
                "/body/authorization/required_scopes",
                s("records.admin"),
            )],
            class: Class::Permission,
            path: "/authorization/required_scopes",
            op: Op::Modified,
        },
        Case {
            name: "freshness",
            fixture: f,
            edits: vec![Edit::Set("/body/freshness/before_write", s("none"))],
            class: Class::Permission,
            path: "/freshness/before_write",
            op: Op::Modified,
        },
        Case {
            name: "lifecycle",
            fixture: f,
            edits: vec![Edit::Set("/body/lifecycle", s("security_revoked"))],
            class: Class::Permission,
            path: "/lifecycle",
            op: Op::Modified,
        },
        Case {
            name: "strategy",
            fixture: f,
            edits: vec![Edit::Push("/body/strategies", s("native_companion"))],
            class: Class::Permission,
            path: "/strategies",
            op: Op::Modified,
        },
        Case {
            name: "source variant",
            fixture: f,
            edits: vec![Edit::Set("/body/source/variant_class", s("admin"))],
            class: Class::Breaking,
            path: "/source/variant_class",
            op: Op::Modified,
        },
        Case {
            name: "source capability",
            fixture: f,
            edits: vec![Edit::Push("/body/source/capabilities", s("bulk_edit"))],
            class: Class::Breaking,
            path: "/source/capabilities",
            op: Op::Modified,
        },
    ]);
}

#[test]
fn presentation_changes_fall_into_distinct_classes() {
    let f = "presentation-orders";
    let comp = obj(&[
        ("id", s("txt_note")),
        ("kind", s("text")),
        ("children", Value::Array(vec![])),
        (
            "accessibility",
            obj(&[("role", s("text")), ("name_source", s("content"))]),
        ),
    ]);
    run(vec![
        Case {
            name: "theme",
            fixture: f,
            edits: vec![Edit::Set("/body/theme/density", s("compact"))],
            class: Class::Cosmetic,
            path: "/theme/density",
            op: Op::Modified,
        },
        Case {
            name: "reordered children",
            fixture: f,
            edits: vec![Edit::Set(
                "/body/components/0/children",
                arr(&["btn_submit", "txt_total", "btn_delete", "orig_widget"]),
            )],
            class: Class::Cosmetic,
            path: "/components[form_order]/children",
            op: Op::Modified,
        },
        Case {
            name: "accessible name",
            fixture: f,
            edits: vec![Edit::Set(
                "/body/components/0/accessibility/name",
                s("Purchase"),
            )],
            class: Class::Cosmetic,
            path: "/components[form_order]/accessibility/name",
            op: Op::Modified,
        },
        Case {
            name: "new component",
            fixture: f,
            edits: vec![Edit::Push("/body/components", comp)],
            class: Class::Additive,
            path: "/components[txt_note]",
            op: Op::Added,
        },
        Case {
            name: "removed constraint",
            fixture: f,
            edits: vec![Edit::Remove("/body/constraints/0")],
            class: Class::Breaking,
            path: "/constraints[contrast]",
            op: Op::Removed,
        },
        Case {
            name: "weaker constraint",
            fixture: f,
            edits: vec![Edit::Set("/body/constraints/0/value", Value::Int(3))],
            class: Class::Breaking,
            path: "/constraints[contrast]/value",
            op: Op::Modified,
        },
        Case {
            name: "rebound entity",
            fixture: f,
            edits: vec![Edit::Set(
                "/body/components/0/binding/entity",
                s("customer"),
            )],
            class: Class::Breaking,
            path: "/components[form_order]/binding/entity",
            op: Op::Modified,
        },
        Case {
            name: "semantic digest",
            fixture: f,
            edits: vec![Edit::Set(
                "/body/semantic",
                s(&format!("sha256:{}", "8".repeat(64))),
            )],
            class: Class::Breaking,
            path: "/semantic",
            op: Op::Modified,
        },
        Case {
            name: "new region",
            fixture: f,
            edits: vec![Edit::Push(
                "/body/regions",
                obj(&[("id", s("aside")), ("components", arr(&["form_order"]))]),
            )],
            class: Class::Additive,
            path: "/regions[aside]",
            op: Op::Added,
        },
    ]);
}

#[test]
fn a_button_that_triggers_another_action_is_a_permission_change() {
    let from = edited("presentation-orders", V, &[]);
    let to = edited(
        "presentation-orders",
        V,
        &[Edit::Set(
            "/body/components/2/binding/action",
            s("delete_order"),
        )],
    );
    let d = diff(&from, V, &to, V).unwrap();
    let hit = d
        .changes
        .iter()
        .find(|c| c.pattern == "components[]/binding/action");
    assert!(
        hit.is_some_and(|h| h.class == Class::Permission),
        "{:?}",
        d.changes
    );
    assert_eq!(d.class, Some(Class::Permission));
}

#[test]
fn the_diff_takes_the_most_severe_class_and_lists_changes_in_path_order() {
    let to = edited(
        "semantic-orders",
        V,
        &[
            Edit::Set("/body/entities/0/label", s("Purchase")),
            Edit::Set("/body/actions/0/effect/declared", s("read_only")),
            Edit::Remove("/body/entities/0/fields/1"),
        ],
    );
    let d = diff(&edited("semantic-orders", V, &[]), V, &to, V).unwrap();
    assert_eq!(d.class, Some(Class::Permission));
    let paths: Vec<&str> = d.changes.iter().map(|c| c.path.as_str()).collect();
    let mut sorted = paths.clone();
    sorted.sort();
    assert_eq!(paths, sorted);
    assert_eq!(
        diff(&edited("semantic-orders", V, &[]), V, &to, V).unwrap(),
        d,
        "deterministic"
    );
    assert!(
        Class::Cosmetic < Class::Additive
            && Class::Additive < Class::Breaking
            && Class::Breaking < Class::Permission
            && Class::Permission < Class::Unknown
    );
}

#[test]
fn identical_and_merely_reordered_documents_have_no_changes() {
    let a = edited("semantic-orders", V, &[]);
    assert_eq!(diff(&a, V, &a, V).unwrap().class, None);
    let Value::Array(mut entities) = edited_value("semantic-orders", "/entities") else {
        panic!()
    };
    entities.reverse();
    let reordered = edited(
        "semantic-orders",
        V,
        &[Edit::Set("/body/entities", Value::Array(entities))],
    );
    let d = diff(&a, V, &reordered, V).unwrap();
    assert!(d.changes.is_empty(), "{:?}", d.changes);
}

fn edited_value(name: &str, pointer: &str) -> Value {
    let path = format!(
        "{}/../../../schemas/ir/fixtures/valid/{name}.json",
        env!("CARGO_MANIFEST_DIR")
    );
    let text = std::fs::read_to_string(path).unwrap();
    let v = vsf_canon::parse(text.as_bytes()).unwrap();
    let mut cur = v.get("body").unwrap().clone();
    for seg in pointer.trim_start_matches('/').split('/') {
        cur = cur.get(seg).unwrap().clone();
    }
    cur
}

#[test]
fn unclassifiable_shapes_are_unknown_never_cosmetic() {
    let from = edited("semantic-orders", V, &[]);
    let dup = edited_value("semantic-orders", "/entities");
    let Value::Array(mut items) = dup else {
        panic!()
    };
    items.push(items[0].clone());
    let to = edited(
        "semantic-orders",
        V,
        &[Edit::Set("/body/entities", Value::Array(items))],
    );
    let d = diff(&from, V, &to, V).unwrap();
    assert_eq!(d.class, Some(Class::Unknown), "{:?}", d.changes);
}

#[test]
fn families_must_match_and_be_supported() {
    let sem = edited("semantic-orders", V, &[]);
    let op = edited("operation-update-stage", V, &[]);
    assert_eq!(
        diff(&sem, V, &op, V).unwrap_err().code,
        "diff.family_mismatch"
    );
    let obs = edited("observation-full", V, &[]);
    assert_eq!(
        diff(&obs, V, &obs, V).unwrap_err().code,
        "diff.unsupported_family"
    );
}
