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
