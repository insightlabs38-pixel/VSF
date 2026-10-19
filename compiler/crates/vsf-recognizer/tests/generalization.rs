#![allow(clippy::unwrap_used, clippy::expect_used, clippy::panic)]

mod common;
use common::*;
use std::collections::BTreeMap;
use vsf_recognizer::engine::{Recognition, recognize};
use vsf_recognizer::semantics::{Binding, LabelSource, Rules, Strength};

fn labels(rec: &Recognition) -> BTreeMap<u32, (String, LabelSource)> {
    rec.fields()
        .filter_map(|f| match &f.binding {
            Binding::Bound(c) => Some((f.node, (c.text.clone(), c.source.clone()))),
            _ => None,
        })
        .collect()
}

fn usable_unbound(rec: &Recognition) -> usize {
    rec.fields()
        .filter(|f| f.usable && matches!(f.binding, Binding::Unbound))
        .count()
}

#[test]
fn one_delegation_rule_fixes_two_unrelated_held_out_apps() {
    let truth: [(&str, &[&str], &[LabelSource]); 2] = [
        (
            "heldout-shadow-ds",
            &["Display name", "Time zone", "Email me"],
            &[LabelSource::WrapperSibling, LabelSource::EnclosingText],
        ),
        (
            "heldout-datagrid-edit",
            &[
                "Product", "Price", "Currency", "Product", "Price", "Currency",
            ],
            &[LabelSource::ColumnHeader],
        ),
    ];
    for (app, expected, sources) in truth {
        let obs = snapshot(app);
        let before = recognize(&obs, Rules::V1);
        let after = recognize(&obs, Rules::V2);
        assert_eq!(usable_unbound(&before), expected.len(), "{app} before");
        assert_eq!(usable_unbound(&after), 0, "{app} after");
        let got: Vec<String> = labels(&after).values().map(|(t, _)| t.clone()).collect();
        assert_eq!(got, expected, "{app} labels");
        assert!(
            labels(&after).values().all(|(_, s)| sources.contains(s)),
            "{app} sources"
        );
        assert!(
            labels(&after)
                .values()
                .all(|(_, s)| s.strength() == Strength::Delegated)
        );
    }
}

#[test]
fn the_regression_corpus_is_unchanged_by_the_improvement() {
    for app in REGRESSION {
        let obs = snapshot(app);
        let a = recognize(&obs, Rules::V1);
        let b = recognize(&obs, Rules::V2);
        let fa: Vec<_> = a.fields().collect();
        let fb: Vec<_> = b.fields().collect();
        assert_eq!(fa, fb, "{app} fields");
        assert_eq!(
            a.actions().collect::<Vec<_>>(),
            b.actions().collect::<Vec<_>>()
        );
        assert_eq!(
            a.collections().collect::<Vec<_>>(),
            b.collections().collect::<Vec<_>>()
        );
    }
}

#[test]
fn adversarial_page_has_no_false_confident_binding() {
    let rec = recognize(&snapshot("adversarial"), Rules::V2);
    let by_label: BTreeMap<u32, String> =
        labels(&rec).into_iter().map(|(k, (t, _))| (k, t)).collect();
    let truth: Vec<(&str, Option<&str>)> = vec![
        ("Name", Some("Billing")),
        ("Email", Some("Billing")),
        ("Name", Some("Shipping")),
        ("Coupon", None),
    ];
    let got: Vec<(&str, Option<&str>)> = rec
        .fields()
        .filter(|f| by_label.contains_key(&f.node))
        .map(|f| (by_label[&f.node].as_str(), f.scope.as_deref()))
        .collect();
    assert_eq!(got.len(), truth.len());
    for (g, t) in got.iter().zip(&truth) {
        assert_eq!(g, t);
    }
    let two_phones = rec
        .fields()
        .filter(|f| matches!(f.binding, Binding::Ambiguous(_)))
        .count();
    assert_eq!(two_phones, 2);
    assert!(
        rec.fields()
            .filter(|f| !by_label.contains_key(&f.node))
            .all(|f| !matches!(f.binding, Binding::Bound(_)))
    );
}

#[test]
fn recognition_code_names_no_framework_or_vendor() {
    let dir = std::path::PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("src");
    let vendors = [
        "material",
        "mui",
        "antd",
        "ant-design",
        "bootstrap",
        "chakra",
        "salesforce",
        "lightning",
        "fluent",
        "primeng",
        "vaadin",
        "ionic",
        "bulma",
        "tailwind",
        "angular",
        "react",
        "vue",
        "svelte",
        "lit-",
        "shoelace",
    ];
    for e in std::fs::read_dir(dir).unwrap() {
        let p = e.unwrap().path();
        let text = std::fs::read_to_string(&p).unwrap().to_lowercase();
        for v in vendors {
            assert!(!text.contains(v), "{} mentions {v}", p.display());
        }
    }
}
