#![allow(clippy::unwrap_used, clippy::expect_used, clippy::panic)]

mod common;

use common::{Edit, edited, s};
use vsf_versioning::diff::diff;
use vsf_versioning::impact::{DepGraph, Mode, Selection, TestSpec, select};

const V: (u32, u32, u32) = (1, 0, 0);

fn spec(id: &str, covers: &[&str], always: bool) -> TestSpec {
    TestSpec {
        id: id.into(),
        covers: covers.iter().map(|c| c.to_string()).collect(),
        always,
    }
}

fn catalog() -> Vec<TestSpec> {
    vec![
        spec("smoke", &[], true),
        spec("order_total", &["field:order.total"], false),
        spec("order_id", &["field:order.order_id"], false),
        spec("submit", &["action:submit_order"], false),
        spec("delete", &["action:delete_order"], false),
        spec("customer", &["entity:customer"], false),
        spec("cafe", &["entity:cafe_nfc"], false),
        spec("emoji", &["entity:emoji_rtl", "entity:cafe_nfd"], false),
        spec("order_form", &["component:form_order"], false),
        spec("theme", &["presentation:global"], false),
        spec("surface", &["component:orig_widget"], false),
        spec("relation", &["relationship:rel_order_customer"], false),
        spec("machine", &["state_machine:sm_order"], false),
    ]
}

fn graph() -> DepGraph {
    DepGraph::from_bodies(&[
        &edited("semantic-orders", V, &[]),
        &edited("presentation-orders", V, &[]),
    ])
}

fn semantic(edits: &[Edit]) -> Selection {
    let from = edited("semantic-orders", V, &[]);
    let to = edited("semantic-orders", V, edits);
    select(&diff(&from, &to).unwrap(), &graph(), &catalog(), None)
}

#[test]
fn a_field_change_selects_its_tests_and_those_of_what_is_bound_to_it() {
    let sel = semantic(&[Edit::Set(
        "/body/entities/0/fields/1/value_type",
        s("integer"),
    )]);
    assert_eq!(sel.mode, Mode::Targeted);
    assert_eq!(sel.changed, vec!["field:order.total"]);
    assert!(
        sel.impacted.contains(&"component:txt_total".to_string()),
        "{:?}",
        sel.impacted
    );
    assert!(
        sel.impacted.contains(&"component:form_order".to_string()),
        "the parent of a bound control is impacted"
    );
    assert_eq!(sel.tests, vec!["order_form", "order_total", "smoke"]);
}

#[test]
fn an_unrelated_change_leaves_unrelated_tests_out() {
    let sel = semantic(&[Edit::Set(
        "/body/entities/1/fields/0/value_type",
        s("integer"),
    )]);
    assert_eq!(sel.mode, Mode::Targeted);
    assert_eq!(sel.tests, vec!["customer", "smoke"]);
    assert!(!sel.tests.contains(&"order_total".to_string()));
    assert!(!sel.tests.contains(&"cafe".to_string()));
    assert!(!sel.tests.contains(&"theme".to_string()));
}

#[test]
fn an_action_change_reaches_its_buttons_and_state_machines() {
    let sel = semantic(&[Edit::Set("/body/actions/0/effect/declared", s("read_only"))]);
    assert_eq!(sel.changed, vec!["action:submit_order"]);
    for want in ["submit", "order_form", "machine", "smoke"] {
        assert!(
            sel.tests.contains(&want.to_string()),
            "{want} missing from {:?}",
            sel.tests
        );
    }
    assert!(!sel.tests.contains(&"delete".to_string()));
}

#[test]
fn a_cosmetic_provenance_change_runs_only_the_always_tests() {
    let sel = semantic(&[Edit::Set("/body/compiler_version", s("0.2.0+abcdef123456"))]);
    assert_eq!(
        (sel.mode, sel.tests),
        (Mode::Targeted, vec!["smoke".to_string()])
    );
}

#[test]
fn uncertain_impact_falls_back_to_every_test() {
    let unknown = semantic(&[Edit::Set(
        "/body/primitive_set",
        s(&format!("sha256:{}", "9".repeat(64))),
    )]);
    assert!(matches!(unknown.mode, Mode::Full(_)), "{:?}", unknown.mode);
    assert_eq!(unknown.tests.len(), catalog().len());
    let global = semantic(&[Edit::Set(
        "/body/source_app_id",
        s("other-app:app.other.test"),
    )]);
    assert_eq!(global.mode, Mode::Full("unmapped_path"));
    let from = edited("semantic-orders", V, &[]);
    let to = edited(
        "semantic-orders",
        V,
        &[Edit::Set("/body/entities/2/kind", s("other"))],
    );
    let no_cover = vec![
        spec("smoke", &[], true),
        spec("order_total", &["field:order.total"], false),
    ];
    let sel = select(&diff(&from, &to).unwrap(), &graph(), &no_cover, None);
    assert_eq!(sel.mode, Mode::Full("uncovered_subject"));
    assert_eq!(sel.tests.len(), 2);
}

#[test]
fn a_removed_subject_still_reaches_its_old_dependents() {
    let from = edited("semantic-orders", V, &[]);
    let to = edited(
        "semantic-orders",
        V,
        &[Edit::Remove("/body/entities/0/fields/1")],
    );
    let g = DepGraph::from_bodies(&[&from, &to, &edited("presentation-orders", V, &[])]);
    let sel = select(&diff(&from, &to).unwrap(), &g, &catalog(), None);
    assert_eq!(sel.mode, Mode::Targeted);
    assert!(
        sel.tests.contains(&"order_form".to_string()),
        "{:?}",
        sel.tests
    );
}

#[test]
fn presentation_global_changes_reach_every_component() {
    let from = edited("presentation-orders", V, &[]);
    let to = edited(
        "presentation-orders",
        V,
        &[Edit::Set("/body/theme/density", s("compact"))],
    );
    let sel = select(&diff(&from, &to).unwrap(), &graph(), &catalog(), None);
    assert_eq!(sel.changed, vec!["presentation:global"]);
    for want in ["theme", "order_form", "surface", "smoke"] {
        assert!(
            sel.tests.contains(&want.to_string()),
            "{want} missing from {:?}",
            sel.tests
        );
    }
    assert!(!sel.tests.contains(&"order_total".to_string()));
}

#[test]
fn operation_changes_select_tests_for_that_operation() {
    let from = edited("operation-update-stage", V, &[]);
    let to = edited(
        "operation-update-stage",
        V,
        &[Edit::Set("/body/effect/class", s("destructive"))],
    );
    let cat = vec![
        spec("smoke", &[], true),
        spec("op", &["operation:app.records.update_stage"], false),
        spec("other", &["operation:app.records.list"], false),
    ];
    let g = DepGraph::from_bodies(&[&from]);
    let sel = select(
        &diff(&from, &to).unwrap(),
        &g,
        &cat,
        Some("app.records.update_stage"),
    );
    assert_eq!(sel.mode, Mode::Targeted);
    assert_eq!(sel.tests, vec!["op", "smoke"]);
    let blind = select(&diff(&from, &to).unwrap(), &g, &cat, None);
    assert!(matches!(blind.mode, Mode::Full(_)));
}

#[test]
fn selection_is_deterministic_and_sorted() {
    let a = semantic(&[
        Edit::Set("/body/entities/0/fields/1/value_type", s("integer")),
        Edit::Set("/body/actions/1/effect/declared", s("read_only")),
    ]);
    let b = semantic(&[
        Edit::Set("/body/actions/1/effect/declared", s("read_only")),
        Edit::Set("/body/entities/0/fields/1/value_type", s("integer")),
    ]);
    assert_eq!(a, b);
    let mut sorted = a.tests.clone();
    sorted.sort();
    assert_eq!(a.tests, sorted);
}
