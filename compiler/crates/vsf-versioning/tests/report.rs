#![allow(clippy::unwrap_used, clippy::expect_used, clippy::panic)]

mod common;

use common::{Edit, edited, s};
use std::path::PathBuf;
use vsf_canon::{Value, parse};
use vsf_registry::json::pretty;
use vsf_versioning::diff::diff;
use vsf_versioning::impact::{DepGraph, TestSpec, select};
use vsf_versioning::report::to_value;

const V: (u32, u32, u32) = (1, 0, 0);

fn dir() -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../../../fixtures/versions/diff")
}

fn reports() -> Vec<(&'static str, String)> {
    let v = (1, 0, 0);
    let sem = edited("semantic-orders", v, &[]);
    let sem2 = edited(
        "semantic-orders",
        v,
        &[
            Edit::Set("/body/entities/0/label", s("Purchase")),
            Edit::Remove("/body/entities/0/fields/1"),
            Edit::Set("/body/actions/0/effect/declared", s("read_only")),
        ],
    );
    let pres = edited("presentation-orders", v, &[]);
    let pres2 = edited(
        "presentation-orders",
        v,
        &[Edit::Set("/body/theme/density", s("compact"))],
    );
    let catalog = vec![
        TestSpec {
            id: "smoke".into(),
            covers: vec![],
            always: true,
        },
        TestSpec {
            id: "form".into(),
            covers: vec!["component:form_order".into()],
            always: false,
        },
        TestSpec {
            id: "theme".into(),
            covers: vec!["presentation:global".into()],
            always: false,
        },
        TestSpec {
            id: "submit".into(),
            covers: vec!["action:submit_order".into()],
            always: false,
        },
    ];
    let d1 = diff(&sem, V, &sem2, V).unwrap();
    let g = DepGraph::from_bodies(&[&sem, &sem2, &pres]);
    let d2 = diff(&pres, V, &pres2, V).unwrap();
    let g2 = DepGraph::from_bodies(&[&pres, &pres2]);
    let none = diff(&sem, V, &sem, V).unwrap();
    vec![
        (
            "semantic-breaking-with-selection",
            pretty(&to_value(&d1, Some(&select(&d1, &g, &catalog, None)))),
        ),
        (
            "presentation-cosmetic-with-selection",
            pretty(&to_value(&d2, Some(&select(&d2, &g2, &catalog, None)))),
        ),
        ("no-change", pretty(&to_value(&none, None))),
    ]
}

#[test]
fn the_committed_diff_reports_are_what_the_emitter_writes() {
    let want = reports();
    if std::env::var_os("VSF_UPDATE_VERSION_FIXTURES").is_some() {
        std::fs::create_dir_all(dir()).unwrap();
        for (name, text) in &want {
            std::fs::write(dir().join(format!("{name}.json")), format!("{text}\n")).unwrap();
        }
    }
    for (name, text) in &want {
        let got = std::fs::read_to_string(dir().join(format!("{name}.json")))
            .unwrap_or_else(|_| panic!("missing {name}; run with VSF_UPDATE_VERSION_FIXTURES=1"));
        assert_eq!(&got, &format!("{text}\n"), "{name} drifted");
    }
}

#[test]
fn a_report_states_everything_a_consumer_needs_without_prose() {
    let (_, text) = &reports()[0];
    let v = parse(text.as_bytes()).unwrap();
    assert_eq!(
        v.get("schema").and_then(Value::as_str),
        Some("vsf-diff-report-v1")
    );
    assert_eq!(v.get("class").and_then(Value::as_str), Some("permission"));
    assert_eq!(v.get("review_required"), Some(&Value::Bool(true)));
    let Some(Value::Array(changes)) = v.get("changes") else {
        panic!()
    };
    let summary = v.get("summary").unwrap();
    let total: i64 = ["cosmetic", "additive", "breaking", "permission", "unknown"]
        .iter()
        .map(|k| summary.get(k).and_then(Value::as_int).unwrap())
        .sum();
    assert_eq!(total as usize, changes.len());
    for c in changes {
        let class = c.get("class").and_then(Value::as_str).unwrap();
        let review = c.get("review") == Some(&Value::Bool(true));
        assert_eq!(
            review,
            matches!(class, "breaking" | "permission" | "unknown"),
            "{class}"
        );
    }
}

#[test]
fn the_command_line_tool_writes_the_same_report() {
    let v = (1, 0, 0);
    let from = edited("presentation-orders", v, &[]);
    let to = edited(
        "presentation-orders",
        v,
        &[Edit::Set("/body/theme/density", s("compact"))],
    );
    let tmp = std::env::temp_dir().join(format!("vsf-diff-cli-{}", std::process::id()));
    std::fs::create_dir_all(&tmp).unwrap();
    let (a, b) = (tmp.join("a.json"), tmp.join("b.json"));
    std::fs::write(&a, vsf_ir::doc::encode_document_at(&from, v).unwrap()).unwrap();
    std::fs::write(&b, vsf_ir::doc::encode_document_at(&to, v).unwrap()).unwrap();
    let out = std::process::Command::new(env!("CARGO_BIN_EXE_vsf-versioning"))
        .arg("diff")
        .arg(&a)
        .arg(&b)
        .output()
        .unwrap();
    assert!(
        out.status.success(),
        "{}",
        String::from_utf8_lossy(&out.stderr)
    );
    let want = pretty(&to_value(&diff(&from, V, &to, V).unwrap(), None));
    assert_eq!(
        String::from_utf8(out.stdout).unwrap().trim_end(),
        want.trim_end()
    );
    let unresolved = std::process::Command::new(env!("CARGO_BIN_EXE_vsf-versioning"))
        .arg("diff")
        .arg(
            PathBuf::from(env!("CARGO_MANIFEST_DIR"))
                .join("../../../schemas/ir/fixtures/valid/presentation-orders.json"),
        )
        .arg(&b)
        .output()
        .unwrap();
    assert_eq!(
        unresolved.status.code(),
        Some(2),
        "a document with an unresolved digest placeholder is refused"
    );
    let usage = std::process::Command::new(env!("CARGO_BIN_EXE_vsf-versioning"))
        .args(["frobnicate"])
        .output()
        .unwrap();
    assert_eq!(usage.status.code(), Some(2));
}
