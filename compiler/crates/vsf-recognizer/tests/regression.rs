#![allow(clippy::unwrap_used, clippy::expect_used, clippy::panic)]

mod common;
use common::snapshot;
use vsf_canon::Value;
use vsf_recognizer::engine::recognize;
use vsf_recognizer::report::recognition_value;
use vsf_recognizer::semantics::Rules;
use vsf_recognizer::val::pretty;

const PAGES: [&str; 6] = [
    "adversarial",
    "aria-grid",
    "heldout-datagrid-edit",
    "heldout-shadow-ds",
    "plain-form",
    "table-orders",
];

fn without_digest(text: &str) -> String {
    text.lines()
        .filter(|l| !l.contains("\"observation\""))
        .collect::<Vec<_>>()
        .join("\n")
}

#[test]
fn rules_one_and_two_keep_their_output_on_the_frozen_pages() {
    for (rules, version) in [(Rules::V1, "1"), (Rules::V2, "2")] {
        for page in PAGES {
            let rec = recognize(&snapshot(page), rules);
            let value: Value = recognition_value(&rec);
            let got = without_digest(&pretty(&value));
            let path = format!(
                "{}/../../../fixtures/recognition/outputs/{page}.rules{version}.json",
                env!("CARGO_MANIFEST_DIR")
            );
            let want = std::fs::read_to_string(&path).expect("golden output");
            assert_eq!(
                got.trim_end(),
                without_digest(&want).trim_end(),
                "{page} under rules {version} changed"
            );
        }
    }
}
