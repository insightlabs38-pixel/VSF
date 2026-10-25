#![allow(clippy::unwrap_used, clippy::expect_used, clippy::panic)]

mod common;
use common::{B, BTN, CTL, V};
use vsf_recognizer::engine::recognize;
use vsf_recognizer::semantics::{Binding, Rules, Verb};

fn bound_text(b: &Binding) -> Option<&str> {
    match b {
        Binding::Bound(c) => Some(c.text.as_str()),
        _ => None,
    }
}

fn todo_list(b: &mut B, parent: u32, n: usize) -> u32 {
    let ul = b.add(parent, "ul", None, None, None, V);
    for k in 0..n {
        let li = b.add(ul, "li", None, None, None, V);
        b.add(li, "input", Some("checkbox"), None, None, CTL);
        b.text(li, "label", &format!("Task {k}"));
        b.button(li, "Delete");
    }
    ul
}

#[test]
fn a_list_of_records_is_a_collection_under_rules_3_only() {
    let mut b = B::new();
    todo_list(&mut b, 1, 3);
    let obs = b.build();
    let v3 = recognize(&obs, Rules::V3);
    let c: Vec<_> = v3.collections().collect();
    assert_eq!(c.len(), 1);
    assert_eq!(c[0].rows, 3);
    assert!(c[0].columns.is_empty());
    assert_eq!(c[0].row_actions.len(), 3);
    assert_eq!(recognize(&obs, Rules::V2).collections().count(), 0);
}

#[test]
fn navigation_lists_menus_and_single_items_are_not_record_lists() {
    let mut b = B::new();
    let nav = b.add(1, "nav", Some("navigation"), None, None, V);
    let ul = b.add(nav, "ul", None, None, None, V);
    for t in ["Home", "Docs"] {
        let li = b.add(ul, "li", None, None, None, V);
        b.add(li, "a", Some("link"), Some(t), Some(t), BTN);
    }
    let links = b.add(1, "ul", None, None, None, V);
    for t in ["One", "Two", "Three"] {
        let li = b.add(links, "li", None, None, None, V);
        b.add(li, "a", Some("link"), Some(t), Some(t), BTN);
    }
    let one = b.add(1, "ul", None, None, None, V);
    let li = b.add(one, "li", None, None, None, V);
    b.button(li, "Only");
    let obs = b.build();
    assert_eq!(recognize(&obs, Rules::V3).collections().count(), 0);
}

#[test]
fn text_after_a_checkbox_names_it_unless_a_control_comes_first() {
    let mut b = B::new();
    let row = b.div(1);
    let cb = b.add(row, "input", Some("checkbox"), None, None, CTL);
    b.text(row, "label", "Buy milk");
    let row2 = b.div(1);
    let cb2 = b.add(row2, "input", Some("checkbox"), None, None, CTL);
    b.input(row2);
    b.text(row2, "label", "Belongs to the input");
    let obs = b.build();
    let v3 = recognize(&obs, Rules::V3);
    assert_eq!(bound_text(&v3.field(cb).unwrap().binding), Some("Buy milk"));
    assert_ne!(
        bound_text(&v3.field(cb2).unwrap().binding),
        Some("Belongs to the input")
    );
    assert_ne!(
        bound_text(&recognize(&obs, Rules::V2).field(cb).unwrap().binding),
        Some("Buy milk")
    );
}

#[test]
fn a_control_in_a_column_header_takes_the_headers_name() {
    let mut b = B::new();
    let t = b.add(1, "table", Some("table"), None, None, V);
    let hr = b.add(t, "tr", Some("row"), None, None, V);
    let th = b.add(
        hr,
        "th",
        Some("columnheader"),
        Some("Select/Deselect All"),
        None,
        V,
    );
    let cb = b.add(th, "input", Some("checkbox"), None, None, CTL);
    let obs = b.build();
    assert_eq!(
        bound_text(&recognize(&obs, Rules::V3).field(cb).unwrap().binding),
        Some("Select/Deselect All")
    );
}

#[test]
fn submit_buttons_are_actions_and_not_text_fields_under_rules_3() {
    let mut b = B::new();
    let s = b.add(1, "input", Some("button"), Some("Go"), None, BTN);
    b.state(s, "type=submit");
    let obs = b.build();
    assert!(recognize(&obs, Rules::V3).field(s).is_none());
    assert!(recognize(&obs, Rules::V3).action(s).is_some());
    assert!(recognize(&obs, Rules::V2).field(s).is_some());
}

fn verbs_of_link(href: Option<&str>, name: &str) -> Vec<Verb> {
    let mut b = B::new();
    let a = b.add(1, "a", Some("link"), Some(name), Some(name), BTN);
    if let Some(h) = href {
        b.state(a, &format!("href={h}"));
    }
    let obs = b.build();
    recognize(&obs, Rules::V3).action(a).unwrap().verbs.clone()
}

#[test]
fn links_navigate_unless_their_name_writes_or_their_handler_is_a_script() {
    assert_eq!(
        verbs_of_link(Some("url"), "Go to January 1"),
        [Verb::Navigate]
    );
    assert_eq!(verbs_of_link(Some("fragment"), "All"), [Verb::Navigate]);
    assert_eq!(
        verbs_of_link(Some("url"), "Download report"),
        [Verb::Export]
    );
    // The hazard direction: a link that says it deletes is never downgraded to navigation.
    assert!(verbs_of_link(Some("url"), "Delete").contains(&Verb::Destroy));
    assert!(!verbs_of_link(Some("url"), "Delete").contains(&Verb::Navigate));
    // A link run by a script says nothing about its effect.
    assert!(verbs_of_link(Some("script"), "Zzz").is_empty());
    assert_eq!(verbs_of_link(Some("url"), "Zzz"), [Verb::Navigate]);
}

#[test]
fn something_that_opens_a_popup_reveals_it_whatever_its_name_says() {
    let mut b = B::new();
    let add = b.button(1, "Add Delivery Address");
    b.state(add, "haspopup=dialog");
    let plain = b.button(1, "Add item");
    let obs = b.build();
    let v3 = recognize(&obs, Rules::V3);
    assert_eq!(v3.action(add).unwrap().verbs, [Verb::Reveal]);
    assert_eq!(v3.action(plain).unwrap().verbs, [Verb::Create]);
    assert_eq!(
        recognize(&obs, Rules::V2).action(add).unwrap().verbs,
        [Verb::Create]
    );
}

#[test]
fn extra_write_verbs_are_hints_under_rules_3_and_nothing_before() {
    let mut b = B::new();
    let f = b.button(1, "Follow");
    let u = b.button(1, "Upload files");
    let z = b.button(1, "Zzz");
    let obs = b.build();
    let v3 = recognize(&obs, Rules::V3);
    assert_eq!(v3.action(f).unwrap().verbs, [Verb::Commit]);
    assert_eq!(v3.action(u).unwrap().verbs, [Verb::Create]);
    assert!(v3.action(z).unwrap().verbs.is_empty());
    assert!(
        recognize(&obs, Rules::V2)
            .action(f)
            .unwrap()
            .verbs
            .is_empty()
    );
}

#[test]
fn an_action_reports_the_words_it_shows_when_its_name_differs() {
    let mut b = B::new();
    let g = b.add(
        1,
        "button",
        Some("button"),
        Some("Run the selected action"),
        None,
        BTN,
    );
    b.add(g, "span", None, None, Some("Go"), V);
    let obs = b.build();
    assert_eq!(
        recognize(&obs, Rules::V3)
            .action(g)
            .unwrap()
            .shown
            .as_deref(),
        Some("Go")
    );
}

#[test]
fn a_label_that_belongs_to_the_previous_control_does_not_compete() {
    let mut b = B::new();
    let g = b.div(1);
    b.text(g, "label", "Speed");
    b.add(g, "span", Some("combobox"), None, None, CTL);
    b.text(g, "label", "File");
    let second = b.add(g, "span", Some("combobox"), None, None, CTL);
    let obs = b.build();
    assert_eq!(
        bound_text(&recognize(&obs, Rules::V3).field(second).unwrap().binding),
        Some("File")
    );
    assert!(bound_text(&recognize(&obs, Rules::V2).field(second).unwrap().binding).is_none());
}
