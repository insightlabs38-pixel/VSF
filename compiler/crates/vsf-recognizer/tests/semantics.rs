#![allow(clippy::unwrap_used, clippy::expect_used, clippy::panic)]

mod common;
use common::*;
use vsf_recognizer::engine::recognize;
use vsf_recognizer::obs::{F_DISABLED, F_VISIBLE, RelKind};
use vsf_recognizer::semantics::{Binding, LabelSource, Rules, Verb};

fn label_of(rec: &vsf_recognizer::engine::Recognition, id: u32) -> Option<(String, LabelSource)> {
    match &rec.field(id)?.binding {
        Binding::Bound(c) => Some((c.text.clone(), c.source.clone())),
        _ => None,
    }
}

#[test]
fn explicit_relations_beat_nearby_text_even_when_the_wrong_label_sits_closest() {
    let mut b = B::new();
    let form = b.add(1, "form", Some("form"), Some("Contact"), None, V);
    let real = b.text(form, "label", "Email address");
    let decoy = b.text(form, "span", "Name");
    let input = b.input(form);
    b.rel(input, RelKind::Label, real);
    let rec = recognize(&b.build(), Rules::V1);
    assert_eq!(
        label_of(&rec, input),
        Some((
            "Email address".into(),
            LabelSource::Relation(RelKind::Label)
        ))
    );
    let _ = decoy;
}

#[test]
fn labelledby_with_several_targets_joins_them_and_a_dangling_target_is_ignored() {
    let mut b = B::new();
    let a = b.text(1, "span", "Shipping");
    let c = b.text(1, "span", "street");
    let input = b.input(1);
    b.rel(input, RelKind::LabelledBy, a);
    b.rel(input, RelKind::LabelledBy, c);
    let rec = recognize(&b.build(), Rules::V1);
    assert_eq!(
        label_of(&rec, input),
        Some((
            "Shipping street".into(),
            LabelSource::Relation(RelKind::LabelledBy)
        ))
    );
}

#[test]
fn competing_nearby_text_is_ambiguous_and_never_silently_bound() {
    let mut b = B::new();
    let row = b.div(1);
    b.text(row, "span", "Email");
    b.text(row, "span", "We never share it");
    let input = b.input(row);
    let rec = recognize(&b.build(), Rules::V2);
    match &rec.field(input).unwrap().binding {
        Binding::Ambiguous(c) => assert_eq!(c.len(), 2),
        other => panic!("{other:?}"),
    }
}

#[test]
fn a_heading_above_a_label_does_not_compete_with_it() {
    let mut b = B::new();
    let row = b.div(1);
    b.text(row, "h3", "Account");
    b.text(row, "span", "Username");
    let input = b.input(row);
    let rec = recognize(&b.build(), Rules::V1);
    assert_eq!(
        label_of(&rec, input),
        Some(("Username".into(), LabelSource::Proximity))
    );
}

#[test]
fn one_label_over_several_controls_names_none_of_them() {
    let mut b = B::new();
    let row = b.div(1);
    b.text(row, "span", "Date");
    let d = b.input(row);
    let m = b.input(row);
    let y = b.input(row);
    let rec = recognize(&b.build(), Rules::V2);
    for id in [d, m, y] {
        assert!(
            matches!(rec.field(id).unwrap().binding, Binding::Ambiguous(_)),
            "{id}"
        );
    }
}

#[test]
fn nesting_hides_the_label_from_the_baseline_and_delegation_recovers_it() {
    let mut b = B::new();
    let item = b.div(1);
    let lab = b.div(item);
    b.text(lab, "label", "Phone number");
    let ctl = b.div(item);
    let inner = b.div(ctl);
    let deep = b.div(inner);
    let input = b.input(deep);
    let obs = b.build();
    assert!(matches!(
        recognize(&obs, Rules::V1).field(input).unwrap().binding,
        Binding::Unbound
    ));
    assert_eq!(
        label_of(&recognize(&obs, Rules::V2), input),
        Some(("Phone number".into(), LabelSource::WrapperSibling))
    );
}

#[test]
fn delegation_never_borrows_the_label_of_a_neighbouring_control() {
    let mut b = B::new();
    let a = b.div(1);
    let la = b.div(a);
    b.text(la, "label", "First");
    let ca = b.div(a);
    let first = b.input(ca);
    let second_item = b.div(1);
    let cb = b.div(second_item);
    let second = b.input(cb);
    let rec = recognize(&b.build(), Rules::V2);
    assert_eq!(label_of(&rec, first).unwrap().0, "First");
    assert!(matches!(
        rec.field(second).unwrap().binding,
        Binding::Unbound
    ));
}

#[test]
fn duplicated_labels_are_told_apart_by_the_region_they_sit_in() {
    let rec = recognize(&snapshot("adversarial"), Rules::V2);
    let named: Vec<_> = rec
        .fields()
        .filter_map(|f| match &f.binding {
            Binding::Bound(c) if c.text == "Name" => Some(f.scope.clone()),
            _ => None,
        })
        .collect();
    assert_eq!(named.len(), 2);
    assert!(named.contains(&Some("Billing".into())) && named.contains(&Some("Shipping".into())));
}

#[test]
fn hidden_disabled_and_credential_controls_are_found_but_never_usable() {
    let rec = recognize(&snapshot("adversarial"), Rules::V2);
    let unusable: Vec<_> = rec
        .fields()
        .filter(|f| !f.usable)
        .map(|f| f.why_unusable.unwrap())
        .collect();
    assert_eq!(unusable.len(), 2);
    assert!(unusable.contains(&"not_visible") && unusable.contains(&"credential_field"));
    let mut b = B::new();
    let off = b.add(
        1,
        "input",
        Some("textbox"),
        Some("Off"),
        None,
        CTL | F_DISABLED,
    );
    let ghost = b.add(
        1,
        "input",
        Some("textbox"),
        Some("Ghost"),
        None,
        CTL & !F_VISIBLE,
    );
    let rec = recognize(&b.build(), Rules::V2);
    assert_eq!(rec.field(off).unwrap().why_unusable, Some("disabled"));
    assert_eq!(rec.field(ghost).unwrap().why_unusable, Some("not_visible"));
}

#[test]
fn overlapping_roles_keep_every_reading_instead_of_picking_one() {
    let rec = recognize(&snapshot("adversarial"), Rules::V2);
    let both: Vec<_> = rec
        .actions()
        .filter(|a| a.roles.len() > 1)
        .map(|a| (a.name.clone().unwrap(), a.roles.clone()))
        .collect();
    assert_eq!(both.len(), 2);
    assert!(
        both.iter()
            .any(|(n, r)| n == "Download invoice" && r == &["button", "link"])
    );
    assert!(
        both.iter()
            .any(|(n, r)| n == "Terms" && r == &["link", "button"])
    );
}

#[test]
fn action_names_map_to_verbs_by_lexicon_and_collisions_stay_separate() {
    use vsf_recognizer::semantics::verbs_of;
    assert_eq!(verbs_of("Save changes"), vec![Verb::Commit]);
    assert_eq!(verbs_of("Delete"), vec![Verb::Destroy]);
    assert_eq!(verbs_of("Add row"), vec![Verb::Create]);
    assert_eq!(
        verbs_of("Edit or delete"),
        vec![Verb::Destroy, Verb::Navigate]
    );
    assert_eq!(verbs_of("Ação"), Vec::<Verb>::new());
    assert_eq!(verbs_of(""), Vec::<Verb>::new());
    assert_eq!(Verb::Destroy.effect_hint(), "destructive_candidate");
}

#[test]
fn collections_expose_columns_rows_and_the_actions_in_each_row() {
    let rec = recognize(&snapshot("table-orders"), Rules::V1);
    let c = rec.collections().next().unwrap();
    assert_eq!(c.label.as_deref(), Some("Open orders"));
    assert_eq!(
        c.columns.iter().flatten().cloned().collect::<Vec<_>>(),
        ["Order", "Customer", "Total", "Actions"]
    );
    assert_eq!(c.rows, 3);
    assert_eq!(c.row_actions.len(), 6);
    let grid = recognize(&snapshot("aria-grid"), Rules::V1);
    assert_eq!(grid.collections().next().unwrap().rows, 2);
}

#[test]
fn a_css_grid_of_plain_divs_is_not_a_collection() {
    let rec = recognize(&snapshot("adversarial"), Rules::V2);
    assert_eq!(rec.collections().count(), 0);
}

#[test]
fn the_fixture_apps_bind_their_fields_as_authored() {
    let rec = recognize(&snapshot("plain-form"), Rules::V1);
    let labels: Vec<String> = rec
        .fields()
        .map(|f| match &f.binding {
            Binding::Bound(c) => c.text.clone(),
            _ => "?".into(),
        })
        .collect();
    assert_eq!(
        labels,
        [
            "First name",
            "Last name",
            "Email",
            "Country",
            "Weekly digest",
            "Notes"
        ]
    );
}

#[test]
fn rows_are_found_through_plain_wrappers_and_footers_and_hidden_rows_are_not_data() {
    let mut b = B::new();
    let grid = b.add(1, "div", Some("grid"), Some("People"), None, V);
    let wrap = b.div(grid);
    let head = b.add(wrap, "div", Some("rowgroup"), None, None, V);
    let hwrap = b.div(head);
    let hrow = b.add(hwrap, "div", Some("row"), None, None, V);
    for h in ["Name", "Age"] {
        b.add(hrow, "div", Some("columnheader"), None, Some(h), V);
    }
    let body = b.add(wrap, "div", Some("rowgroup"), None, None, V);
    let inner = b.div(body);
    for (n, a) in [("Ann", "30"), ("Bo", "41")] {
        let row = b.add(inner, "div", Some("row"), None, None, V);
        b.add(row, "div", Some("gridcell"), None, Some(n), V);
        b.add(row, "div", Some("gridcell"), None, Some(a), V);
    }
    let hidden = b.add(inner, "div", Some("row"), None, None, 0);
    b.add(hidden, "div", Some("gridcell"), None, Some("x"), 0);
    let rec = recognize(&b.build(), Rules::V2);
    let c = rec.collections().next().unwrap();
    assert_eq!(c.rows, 2);
    assert_eq!(c.columns.iter().flatten().count(), 2);

    let mut t = B::new();
    let table = t.add(1, "table", Some("table"), None, None, V);
    let tbody = t.add(table, "tbody", None, None, None, V);
    let row = t.add(tbody, "tr", Some("row"), None, None, V);
    t.add(row, "td", Some("cell"), None, Some("a"), V);
    let tfoot = t.add(table, "tfoot", None, None, None, V);
    let total = t.add(tfoot, "tr", Some("row"), None, None, V);
    t.add(total, "td", Some("cell"), None, Some("Total"), V);
    let rec = recognize(&t.build(), Rules::V2);
    assert_eq!(rec.collections().next().unwrap().rows, 1);
}

#[test]
fn radios_without_a_label_take_their_name_from_content_and_file_inputs_have_a_kind() {
    let mut b = B::new();
    let group = b.add(1, "div", Some("radiogroup"), Some("Plan"), None, V);
    let radio = b.add(group, "div", Some("radio"), None, Some("Monthly"), CTL);
    let file = b.add(1, "input", None, Some("Attachment"), None, CTL);
    b.state(file, "type=file");
    let date = b.add(1, "input", None, Some("Due"), None, CTL);
    b.state(date, "type=date");
    let rec = recognize(&b.build(), Rules::V2);
    assert_eq!(
        label_of(&rec, radio),
        Some(("Monthly".into(), LabelSource::Content))
    );
    assert_eq!(rec.field(file).unwrap().kind.as_str(), "file");
    assert_eq!(rec.field(date).unwrap().kind.as_str(), "date");
}

#[test]
fn states_hint_at_effects_only_when_the_name_says_nothing_and_separators_are_not_columns() {
    let mut b = B::new();
    let bold = b.add(1, "button", Some("button"), Some("Bold"), None, BTN);
    b.state(bold, "pressed=false");
    let more = b.add(1, "button", Some("button"), Some("Options"), None, BTN);
    b.state(more, "haspopup=menu");
    let save = b.add(1, "button", Some("button"), Some("Save"), None, BTN);
    b.state(save, "pressed=true");
    let grid = b.add(1, "div", Some("grid"), None, None, V);
    let row = b.add(grid, "div", Some("row"), None, None, V);
    for h in ["A", "B"] {
        b.add(row, "div", Some("columnheader"), None, Some(h), V);
        b.add(row, "span", None, None, None, V);
    }
    let rec = recognize(&b.build(), Rules::V2);
    let verbs = |id| rec.action(id).unwrap().verbs.clone();
    assert_eq!(verbs(bold), vec![Verb::Toggle]);
    assert_eq!(verbs(more), vec![Verb::Reveal]);
    assert_eq!(verbs(save), vec![Verb::Commit]);
    assert_eq!(rec.collections().next().unwrap().columns.len(), 2);
}
