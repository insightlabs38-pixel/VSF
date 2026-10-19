#![allow(clippy::unwrap_used, clippy::expect_used, clippy::panic)]

mod common;
use common::*;
use std::collections::BTreeSet;
use vsf_recognizer::engine::{UnitKey, UnitKind, recognize};
use vsf_recognizer::incremental::{Session, diff};
use vsf_recognizer::ledger::{Ledger, build_ledger};
use vsf_recognizer::obs::{F_VISIBLE, Observation};
use vsf_recognizer::semantics::Rules;

const ALL: [&str; 6] = [
    "plain-form",
    "table-orders",
    "aria-grid",
    "adversarial",
    "heldout-shadow-ds",
    "heldout-datagrid-edit",
];

fn claim_ids(l: &Ledger) -> BTreeSet<String> {
    l.claims.keys().cloned().collect()
}

/// The incremental state must equal a run from nothing: same units, same reads, same claims.
fn assert_equivalent(s: &Session, rules: Rules) {
    let fresh = recognize(&s.obs, rules);
    assert_eq!(fresh.units, s.rec.units);
    let fl = build_ledger(&s.obs, &fresh, 0);
    assert_eq!(claim_ids(&fl), claim_ids(&s.ledger));
    assert_eq!(s.ledger.validate(), vec![]);
}

fn find(obs: &Observation, f: impl Fn(&vsf_recognizer::obs::Node) -> bool) -> u32 {
    obs.nodes.iter().find(|n| f(n)).unwrap().id
}

#[test]
fn changing_a_label_recomputes_only_the_control_it_names() {
    let obs = snapshot("plain-form");
    let mut s = Session::new(obs.clone(), Rules::V2, 1);
    let total = s.rec.units.len();
    let label = find(&obs, |n| {
        n.tag == "label" && n.text.as_deref() == Some("Last name")
    });
    let control = find(&obs, |n| {
        n.tag == "input" && n.name.as_deref() == Some("Last name")
    });
    let mut e = Edit::of(&obs);
    e.node(label).text = Some("Surname".into());
    e.node(control).name = Some("Surname".into());
    let up = s.update(e.build());
    assert_eq!(up.recomputed, vec![(UnitKind::Field, control)]);
    assert_eq!(up.reused, total - 1);
    println!(
        "incremental-report units={total} recomputed={} reused={}",
        up.recomputed.len(),
        up.reused
    );
    assert!(up.created.is_empty() && up.dropped.is_empty());
    assert_equivalent(&s, Rules::V2);
    let changed = s.ledger.find(control, "label");
    assert_eq!(changed[0].object, "Surname");
}

#[test]
fn an_unrelated_change_recomputes_nothing_and_keeps_every_claim() {
    let obs = snapshot("plain-form");
    let mut s = Session::new(obs.clone(), Rules::V2, 1);
    let before = claim_ids(&s.ledger);
    let h1 = find(&obs, |n| n.tag == "h1");
    let mut e = Edit::of(&obs);
    e.node(h1).text = Some("Customer record".into());
    e.node(h1).name = Some("Customer record".into());
    let up = s.update(e.build());
    assert!(
        up.recomputed.is_empty() && up.created.is_empty() && up.dropped.is_empty(),
        "{up:?}"
    );
    assert_eq!(claim_ids(&s.ledger), before);
    assert_equivalent(&s, Rules::V2);
}

#[test]
fn adding_and_removing_controls_creates_and_drops_units() {
    let obs = snapshot("plain-form");
    let mut s = Session::new(obs.clone(), Rules::V2, 1);
    let form = find(&obs, |n| n.tag == "form");
    let mut e = Edit::of(&obs);
    let lab = e.insert(form, 0, "label", None, None, Some("Nickname"), F_VISIBLE);
    let input = e.insert(form, 1, "input", Some("textbox"), None, None, CTL);
    let _ = lab;
    let up = s.update(e.build());
    assert!(up.created.contains(&(UnitKind::Field, input)));
    assert_equivalent(&s, Rules::V2);
    let mut e2 = Edit::of(&s.obs);
    e2.remove(input);
    let up2 = s.update(e2.build());
    assert!(up2.dropped.contains(&(UnitKind::Field, input)));
    assert!(s.ledger.by_subject(input).next().is_none());
    assert_equivalent(&s, Rules::V2);
}

#[test]
fn a_change_under_a_collection_recomputes_the_collection_and_not_the_page() {
    let obs = snapshot("table-orders");
    let mut s = Session::new(obs.clone(), Rules::V2, 1);
    let table = find(&obs, |n| n.tag == "table");
    let cell = find(&obs, |n| n.tag == "td" && n.text.as_deref() == Some("Ana"));
    let mut e = Edit::of(&obs);
    e.node(cell).text = Some("Anna".into());
    let up = s.update(e.build());
    assert!(up.recomputed.contains(&(UnitKind::Collection, table)));
    assert!(up.recomputed.len() < s.rec.units.len() / 2, "{up:?}");
    assert_equivalent(&s, Rules::V2);
}

#[test]
fn the_diff_reports_exactly_what_changed() {
    let obs = snapshot("adversarial");
    let mut e = Edit::of(&obs);
    let h1 = find(&obs, |n| n.tag == "h1");
    e.node(h1).text = Some("x".into());
    let gone = find(&obs, |n| {
        n.tag == "button" && n.name.as_deref() == Some("Terms")
    });
    e.remove(gone);
    let d = diff(&obs, &e.build());
    assert!(d.changed.contains(&h1) && d.removed.contains(&gone));
    assert!(d.added.is_empty());
    assert!(diff(&obs, &obs).touched().is_empty());
}

struct Rng(u64);
impl Rng {
    fn next(&mut self) -> u64 {
        self.0 ^= self.0 << 13;
        self.0 ^= self.0 >> 7;
        self.0 ^= self.0 << 17;
        self.0
    }
    fn below(&mut self, n: usize) -> usize {
        (self.next() % n as u64) as usize
    }
}

#[test]
fn random_edit_sequences_always_agree_with_a_fresh_run() {
    for (k, name) in ALL.iter().enumerate() {
        for rules in [Rules::V1, Rules::V2] {
            let mut r = Rng(0x9e37_79b9 + k as u64 * 7919 + 1);
            let mut s = Session::new(snapshot(name), rules, 1);
            for step in 0..40 {
                let mut e = Edit::of(&s.obs);
                let ids = e.all_ids();
                let id = ids[r.below(ids.len())];
                match r.below(6) {
                    0 => e.node(id).text = Some(format!("t{step}")),
                    1 => e.node(id).name = Some(format!("Name {step}")),
                    2 => e.node(id).flags ^= F_VISIBLE,
                    3 if id != e.all_ids()[0] => e.remove(id),
                    4 => {
                        let at = r.below(4);
                        let tag = ["div", "label", "input", "button", "span"][r.below(5)];
                        let role = (tag == "input").then_some("textbox");
                        let flags = if tag == "input" || tag == "button" {
                            CTL
                        } else {
                            V
                        };
                        e.insert(id, at, tag, role, None, Some(&format!("x{step}")), flags);
                    }
                    _ => {
                        e.node(id).role = ["button", "textbox", "link", "grid", "cell"]
                            .get(r.below(6))
                            .map(|s| s.to_string())
                    }
                }
                s.update(e.build());
                assert_equivalent(&s, rules);
            }
        }
    }
}

#[test]
fn on_a_large_page_a_small_change_recomputes_a_small_fraction() {
    let mut b = B::new();
    let form = b.add(1, "form", Some("form"), Some("Big"), None, V);
    let mut inputs = Vec::new();
    for i in 0..3000 {
        let row = b.div(form);
        b.text(row, "label", &format!("Field {i}"));
        inputs.push(b.input(row));
    }
    let obs = b.build();
    let mut s = Session::new(obs.clone(), Rules::V2, 1);
    let total = s.rec.units.len();
    assert_eq!(total, 3000);
    let mut e = Edit::of(&obs);
    let lab = e.kids_of(e.kids_of(form)[1500])[0];
    e.node(lab).text = Some("Changed".into());
    let up = s.update(e.build());
    assert!(up.recomputed.len() + up.created.len() <= 2, "{up:?}");
    assert_eq!(up.reused, total - up.recomputed.len());
    assert_equivalent(&s, Rules::V2);
    let _: UnitKey = (UnitKind::Field, 0);
}
