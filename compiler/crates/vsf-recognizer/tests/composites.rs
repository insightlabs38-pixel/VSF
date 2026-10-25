#![allow(clippy::unwrap_used, clippy::expect_used, clippy::panic)]

mod common;
use common::{B, BTN, V};
use vsf_recognizer::composites::{Composite, CompositeKind, recognize_composites};
use vsf_recognizer::obs::{F_VISIBLE, RelKind};

fn of(cs: &[Composite], kind: CompositeKind) -> Vec<&Composite> {
    cs.iter().filter(|c| c.kind == kind).collect()
}

fn names(c: &Composite) -> Vec<String> {
    c.members.iter().filter_map(|m| m.name.clone()).collect()
}

#[test]
fn a_tablist_lists_its_tabs_whether_or_not_their_panels_are_shown() {
    let mut b = B::new();
    let list = b.add(1, "ul", Some("tablist"), None, None, V);
    for t in ["One", "Two", "Three"] {
        b.add(list, "li", Some("tab"), Some(t), None, BTN);
    }
    let cs = recognize_composites(&b.build());
    let tabs = of(&cs, CompositeKind::Tabs);
    assert_eq!(tabs.len(), 1);
    assert_eq!(names(tabs[0]), ["One", "Two", "Three"]);
}

#[test]
fn a_tablist_without_tabs_and_plain_links_are_not_tabs() {
    let mut b = B::new();
    b.add(1, "ul", Some("tablist"), None, None, V);
    let nav = b.add(1, "ul", None, None, None, V);
    b.add(nav, "a", Some("link"), Some("Home"), Some("Home"), BTN);
    assert!(of(&recognize_composites(&b.build()), CompositeKind::Tabs).is_empty());
}

#[test]
fn expandable_buttons_group_into_a_disclosure_but_a_single_one_does_not() {
    let mut b = B::new();
    let root = b.div(1);
    let mut panels = Vec::new();
    for q in ["Q1", "Q2"] {
        let item = b.div(root);
        let btn = b.button(item, q);
        b.state(btn, "expanded=false");
        let panel = b.add(item, "div", None, None, Some("answer"), 0);
        b.rel(btn, RelKind::Controls, panel);
        panels.push(btn);
    }
    let lone = b.div(1);
    let one = b.button(lone, "Only one");
    b.state(one, "expanded=false");
    let target = b.add(lone, "div", None, None, Some("x"), 0);
    b.rel(one, RelKind::Controls, target);
    let cs = recognize_composites(&b.build());
    let d = of(&cs, CompositeKind::Disclosure);
    assert_eq!(d.len(), 1);
    assert_eq!(names(d[0]), ["Q1", "Q2"]);
}

#[test]
fn a_menu_button_owns_its_menu_and_a_combobox_does_not_claim_it() {
    let mut b = B::new();
    let btn = b.button(1, "Links");
    b.state(btn, "haspopup=true");
    b.state(btn, "expanded=false");
    let menu = b.add(1, "ul", Some("menu"), None, None, 0);
    for t in ["Home", "Docs"] {
        b.add(menu, "a", Some("menuitem"), Some(t), Some(t), 0);
    }
    b.rel(btn, RelKind::Controls, menu);
    let cs = recognize_composites(&b.build());
    let menus = of(&cs, CompositeKind::Menu);
    assert_eq!(menus.len(), 1);
    assert_eq!(names(menus[0]), ["Home", "Docs"]);
    assert!(of(&cs, CompositeKind::ComboboxPopup).is_empty());
}

#[test]
fn a_combobox_lists_the_options_of_the_popup_it_controls_even_while_hidden() {
    let mut b = B::new();
    let combo = b.add(1, "span", Some("combobox"), None, None, BTN);
    b.state(combo, "haspopup=true");
    b.state(combo, "expanded=false");
    let list = b.add(1, "ul", Some("listbox"), None, None, 0);
    for t in ["Slow", "Fast"] {
        b.add(list, "li", Some("option"), Some(t), None, 0);
    }
    b.rel(combo, RelKind::Controls, list);
    let cs = recognize_composites(&b.build());
    let c = of(&cs, CompositeKind::ComboboxPopup);
    assert_eq!(c.len(), 1);
    assert_eq!(c[0].owner, Some(combo));
    assert_eq!(names(c[0]), ["Slow", "Fast"]);
}

#[test]
fn a_native_select_is_a_choice_field_and_not_a_popup_composite() {
    let mut b = B::new();
    let sel = b.add(1, "select", Some("combobox"), Some("Size"), None, F_VISIBLE);
    b.add(sel, "option", Some("option"), Some("S"), None, V);
    assert!(
        of(
            &recognize_composites(&b.build()),
            CompositeKind::ComboboxPopup
        )
        .is_empty()
    );
}

#[test]
fn dialogs_list_their_controls_and_record_modality() {
    let mut b = B::new();
    let d = b.add(1, "div", Some("dialog"), Some("Create user"), None, V);
    b.state(d, "modal=true");
    b.input(d);
    b.button(d, "Save");
    let cs = recognize_composites(&b.build());
    let ds = of(&cs, CompositeKind::Dialog);
    assert_eq!(ds.len(), 1);
    assert_eq!(ds[0].label.as_deref(), Some("Create user"));
    assert!(ds[0].evidence.contains(&"state:modal"));
    assert_eq!(ds[0].members.len(), 2);
}

fn table(b: &mut B, heads: &[&str], rows: usize, row_box: bool) -> (u32, Vec<u32>) {
    let t = b.add(1, "table", Some("table"), None, None, V);
    let hr = b.add(t, "tr", Some("row"), None, None, V);
    let mut head_nodes = Vec::new();
    for h in heads {
        head_nodes.push(b.add(hr, "th", Some("columnheader"), Some(h), Some(h), V));
    }
    for r in 0..rows {
        let row = b.add(t, "tr", Some("row"), None, None, V);
        for c in 0..heads.len() {
            let cell = b.add(row, "td", Some("cell"), None, Some(&format!("r{r}c{c}")), V);
            if c == 0 && row_box {
                b.add(cell, "input", Some("checkbox"), None, None, BTN);
            }
        }
    }
    (t, head_nodes)
}

#[test]
fn row_checkboxes_make_a_bulk_selection_and_unchecked_tables_do_not() {
    let mut b = B::new();
    table(&mut b, &["A", "B"], 3, true);
    let cs = recognize_composites(&b.build());
    assert_eq!(of(&cs, CompositeKind::BulkSelection).len(), 1);
    let mut b = B::new();
    table(&mut b, &["A", "B"], 3, false);
    assert!(
        of(
            &recognize_composites(&b.build()),
            CompositeKind::BulkSelection
        )
        .is_empty()
    );
}

#[test]
fn sort_state_or_several_header_controls_mean_sortable_but_a_single_titled_link_does_not() {
    let mut b = B::new();
    let (_, heads) = table(&mut b, &["A", "B"], 2, false);
    b.state(heads[0], "sort=ascending");
    assert_eq!(
        of(
            &recognize_composites(&b.build()),
            CompositeKind::SortableColumns
        )
        .len(),
        1
    );

    let mut b = B::new();
    let (_, heads) = table(&mut b, &["A", "B"], 2, false);
    b.add(heads[0], "a", Some("link"), Some("A"), Some("A"), BTN);
    assert!(
        of(
            &recognize_composites(&b.build()),
            CompositeKind::SortableColumns
        )
        .is_empty()
    );

    let mut b = B::new();
    let (_, heads) = table(&mut b, &["A", "B"], 2, false);
    for h in &heads {
        b.add(*h, "a", Some("link"), Some("x"), Some("x"), BTN);
    }
    assert_eq!(
        of(
            &recognize_composites(&b.build()),
            CompositeKind::SortableColumns
        )
        .len(),
        1
    );
}

#[test]
fn pagination_needs_step_and_number_controls_and_calendar_days_are_not_pages() {
    let mut b = B::new();
    let nav = b.add(1, "nav", Some("navigation"), None, None, V);
    for t in ["Previous", "1", "2", "Next"] {
        b.add(nav, "a", Some("link"), Some(t), Some(t), BTN);
    }
    assert_eq!(
        of(&recognize_composites(&b.build()), CompositeKind::Pagination).len(),
        1
    );

    let mut b = B::new();
    let t = b.add(1, "table", Some("grid"), None, None, V);
    let hr = b.add(t, "tr", Some("row"), None, None, V);
    for d in ["Su", "Mo", "Tu", "We", "Th", "Fr", "Sa"] {
        b.add(hr, "th", Some("columnheader"), Some(d), Some(d), V);
    }
    let row = b.add(t, "tr", Some("row"), None, None, V);
    for d in 1..=7 {
        let cell = b.add(row, "td", Some("gridcell"), None, None, V);
        b.add(
            cell,
            "a",
            Some("link"),
            Some(&d.to_string()),
            Some(&d.to_string()),
            BTN,
        );
    }
    assert!(of(&recognize_composites(&b.build()), CompositeKind::Pagination).is_empty());
}

#[test]
fn a_search_box_inside_a_combobox_is_not_a_filter_bar_but_a_standalone_one_is() {
    let mut b = B::new();
    let combo = b.add(1, "div", Some("combobox"), None, None, BTN);
    b.state(combo, "haspopup=true");
    b.state(combo, "expanded=false");
    let s = b.add(
        combo,
        "input",
        Some("searchbox"),
        Some("Search"),
        None,
        common::CTL,
    );
    b.state(s, "type=search");
    assert!(of(&recognize_composites(&b.build()), CompositeKind::FilterBar).is_empty());

    let mut b = B::new();
    let s = b.add(
        1,
        "input",
        Some("searchbox"),
        Some("Search"),
        None,
        common::CTL,
    );
    b.state(s, "type=search");
    assert_eq!(
        of(&recognize_composites(&b.build()), CompositeKind::FilterBar).len(),
        1
    );
}

#[test]
fn file_and_date_inputs_are_recognized_by_input_type_only() {
    let mut b = B::new();
    let f = b.input(1);
    b.state(f, "type=file");
    let d = b.input(1);
    b.state(d, "type=date");
    let t = b.input(1);
    b.state(t, "type=text");
    let cs = recognize_composites(&b.build());
    assert_eq!(of(&cs, CompositeKind::FileInput).len(), 1);
    assert_eq!(of(&cs, CompositeKind::DatePicker).len(), 1);
}

#[test]
fn recognition_of_composites_is_deterministic() {
    let mut b = B::new();
    table(&mut b, &["A", "B"], 3, true);
    let obs = b.build();
    assert_eq!(recognize_composites(&obs), recognize_composites(&obs));
}
