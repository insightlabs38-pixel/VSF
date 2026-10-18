#![allow(clippy::unwrap_used, clippy::expect_used, clippy::panic)]

use std::collections::{BTreeMap, BTreeSet};
use std::path::{Path, PathBuf};
use vsf_canon::{parse, to_canonical};
use vsf_registry::compose::{extract_all, run_fixture};
use vsf_registry::fixture::{
    self, Fixture, render_aria_grid, render_html_table, render_visual_only, seeded_files,
};
use vsf_registry::harness::{FIXTURES, load_package};
use vsf_registry::register::{Recognition, Registration};

fn repo() -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("../../..")
        .canonicalize()
        .unwrap()
}

fn package() -> vsf_registry::harness::Package {
    load_package(&repo()).unwrap()
}

/// A scratch copy of just the files the package reads, so tests can tamper with it.
fn scratch(name: &str) -> PathBuf {
    let dir = std::env::temp_dir().join(format!("vsf-registry-{name}-{}", std::process::id()));
    let _ = std::fs::remove_dir_all(&dir);
    let copy = |rel: &str| {
        let to = dir.join(rel);
        std::fs::create_dir_all(to.parent().unwrap()).unwrap();
        std::fs::copy(repo().join(rel), &to).unwrap();
    };
    for rel in [
        "registry/primitives/registry.v1.json",
        "registry/primitives/policy.v1.json",
        "registry/primitives/status.v1.json",
        "docs/deep/primitives/collection-extract.md",
        "compiler/crates/vsf-registry/src/compose.rs",
    ] {
        copy(rel);
    }
    fn walk(from: &Path, to: &Path) {
        std::fs::create_dir_all(to).unwrap();
        for e in std::fs::read_dir(from).unwrap() {
            let p = e.unwrap().path();
            let t = to.join(p.file_name().unwrap());
            if p.is_dir() {
                walk(&p, &t)
            } else {
                std::fs::copy(&p, &t).unwrap();
            }
        }
    }
    walk(&repo().join(FIXTURES), &dir.join(FIXTURES));
    dir
}

fn edit(dir: &Path, rel: &str, from: &str, to: &str) {
    let p = dir.join(rel);
    let text = std::fs::read_to_string(&p).unwrap();
    assert!(text.contains(from), "{rel} lacks {from}");
    std::fs::write(p, text.replacen(from, to, 1)).unwrap();
}

#[test]
fn every_fixture_descriptor_round_trips_and_ids_are_unique() {
    let fixtures = fixture::load_dir(&repo().join(FIXTURES)).unwrap();
    assert_eq!(fixtures.len(), 22);
    let ids: BTreeSet<&str> = fixtures.iter().map(|f| f.id.as_str()).collect();
    assert_eq!(ids.len(), fixtures.len());
    for f in &fixtures {
        assert_eq!(Fixture::decode(&f.encode()).unwrap(), *f, "{}", f.id);
    }
    for pol in ["positive", "negative", "adversarial"] {
        for app in ["synthetic-html-tables", "synthetic-aria-grids"] {
            assert!(
                fixtures.iter().any(|f| f.polarity == pol && f.app == app)
                    || pol == "negative" && app == "synthetic-html-tables",
                "{pol}/{app}"
            );
        }
    }
}

#[test]
fn seeded_fixtures_are_reproducible_and_match_the_committed_files() {
    for (rel, text) in seeded_files() {
        let committed =
            std::fs::read_to_string(repo().join(FIXTURES).join("collection").join(&rel)).unwrap();
        assert_eq!(committed, text, "{rel}");
    }
    assert_eq!(render_html_table(7), render_html_table(7));
    assert_ne!(render_html_table(7).nodes, render_html_table(8).nodes);
    assert_eq!(render_aria_grid(5).seed, Some(5));
    assert_eq!(seeded_files().len(), 9);
}

#[test]
fn malformed_descriptors_are_rejected() {
    let good = render_html_table(1).encode();
    let text = String::from_utf8(to_canonical(&good)).unwrap();
    let bad = |t: String| {
        Fixture::decode(&parse(t.as_bytes()).unwrap())
            .unwrap_err()
            .code
    };
    assert_eq!(
        bad(text.replacen("\"polarity\":\"positive\"", "\"polarity\":\"maybe\"", 1)),
        "fixture.bad_polarity"
    );
    assert_eq!(
        bad(text.replacen(
            "\"schema\":\"vsf-primitive-fixture-v1\"",
            "\"schema\":\"v0\"",
            1
        )),
        "registry.bad_schema"
    );
    assert_eq!(
        bad(text.replacen("\"seed\":", "\"sede\":", 1)),
        "registry.unknown_field"
    );
    assert_eq!(
        bad(text.replacen("\"id\":2,\"parent\":1", "\"id\":2,\"parent\":9", 1)),
        "fixture.parent_after_child"
    );
    assert_eq!(
        bad(text.replacen("\"id\":2,\"parent\":1", "\"id\":1,\"parent\":1", 1)),
        "fixture.bad_node_id"
    );
    let mut f = render_html_table(1);
    f.expect.clear();
    assert_eq!(
        Fixture::decode(&f.encode()).unwrap_err().code,
        "fixture.positive_without_expectation"
    );
    let mut n = render_visual_only(1);
    n.expect = render_html_table(1).expect;
    assert_eq!(
        Fixture::decode(&n.encode()).unwrap_err().code,
        "fixture.negative_with_expectation"
    );
}

#[test]
fn every_fixture_passes_the_shared_composite() {
    let pkg = package();
    for f in &pkg.fixtures {
        run_fixture(f, &pkg.registered).unwrap();
    }
}

#[test]
fn two_independent_sources_yield_the_same_semantics_on_held_out_seeds() {
    let pkg = package();
    for seed in 100..400 {
        let (columns, rows) = fixture::logical(seed);
        let html = extract_all(&render_html_table(seed).nodes, &pkg.registered);
        let aria = extract_all(&render_aria_grid(seed).nodes, &pkg.registered);
        assert_eq!((html.len(), aria.len()), (1, 1), "seed {seed}");
        assert_eq!(
            (&html[0].columns, &html[0].rows),
            (&columns, &rows),
            "html seed {seed}"
        );
        assert_eq!(
            (&aria[0].columns, &aria[0].rows),
            (&columns, &rows),
            "aria seed {seed}"
        );
        assert_eq!(
            (html[0].primitive.as_str(), aria[0].primitive.as_str()),
            ("TABLE-001", "TABLE-002")
        );
        assert!(
            extract_all(&render_visual_only(seed).nodes, &pkg.registered).is_empty(),
            "visual-only seed {seed}"
        );
    }
}

#[test]
fn a_style_based_recognizer_is_caught_by_the_lookalike_fixture() {
    let pkg = package();
    let mut cheat: BTreeMap<String, Registration> = pkg.registered.clone();
    cheat.insert(
        "CHEAT".into(),
        Registration {
            id: "CHEAT".into(),
            maturity: vsf_registry::model::Maturity::Candidate,
            execution: vec![],
            needs_capabilities: vec![],
            recognition: Some(Recognition {
                produces: "collection".into(),
                signature: vec!["attr:style".into()],
                excludes: vec![],
                precedence: 99,
            }),
            evidence: vec![],
            implementation: None,
        },
    );
    let lookalike = pkg
        .fixtures
        .iter()
        .find(|f| f.id == "grid.visual-only.css-grid")
        .unwrap();
    assert!(run_fixture(lookalike, &pkg.registered).is_ok());
    assert!(run_fixture(lookalike, &cheat).is_err());
}

#[test]
fn changing_an_expectation_or_an_opt_out_role_fails_the_fixture() {
    let pkg = package();
    let mut wrong = pkg
        .fixtures
        .iter()
        .find(|f| f.id == "table.html.orders")
        .unwrap()
        .clone();
    wrong.expect[0].columns[0] = "Ordre".into();
    assert!(run_fixture(&wrong, &pkg.registered).is_err());
    let mut unmarked = pkg
        .fixtures
        .iter()
        .find(|f| f.id == "table.html.layout-table")
        .unwrap()
        .clone();
    unmarked.nodes.iter_mut().for_each(|n| {
        n.attrs.remove("role");
    });
    unmarked.expect.clear();
    assert!(run_fixture(&unmarked, &pkg.registered).is_err());
}

#[test]
fn at_most_one_collection_primitive_matches_any_node_in_any_fixture() {
    let pkg = package();
    let mut all = pkg.fixtures.clone();
    all.extend((400..450).flat_map(|s| [render_html_table(s), render_aria_grid(s)]));
    for f in &all {
        for n in &f.nodes {
            let mut p: BTreeSet<String> = n
                .attrs
                .keys()
                .filter(|k| *k != "role")
                .map(|k| format!("attr:{k}"))
                .collect();
            p.insert(format!("tag:{}", n.tag));
            if let Some(r) = n.attrs.get("role") {
                p.insert(format!("role:{r}"));
            }
            let hits = vsf_registry::register::recognize(&pkg.registered, &p);
            assert!(
                hits.len() <= 1,
                "{} node {} matched {:?}",
                f.id,
                n.id,
                hits.iter().map(|h| &h.id).collect::<Vec<_>>()
            );
        }
    }
}

#[test]
fn an_implemented_claim_without_a_passing_witness_fails_the_package_load() {
    let dir = scratch("nowitness");
    edit(
        &dir,
        "registry/primitives/status.v1.json",
        "{ \"kind\": \"fixture\", \"id\": \"table.html.orders\", \"polarity\": \"positive\" },\n",
        "",
    );
    assert_eq!(
        load_package(&dir).err().unwrap().code,
        "register.invalid_evidence"
    );
    let dir = scratch("failingwitness");
    edit(
        &dir,
        "fixtures/primitives/collection/html-table/table.html.orders.json",
        "\"Order\"",
        "\"Wrong\"",
    );
    let err = load_package(&dir);
    assert!(err.is_err());
    let dir = scratch("polarity");
    edit(
        &dir,
        "registry/primitives/status.v1.json",
        "\"id\": \"table.html.orders\", \"polarity\": \"positive\"",
        "\"id\": \"table.html.orders\", \"polarity\": \"negative\"",
    );
    assert_eq!(
        load_package(&dir).err().unwrap().code,
        "register.invalid_evidence"
    );
    let dir = scratch("ghost");
    edit(
        &dir,
        "registry/primitives/status.v1.json",
        "\"id\": \"table.html.orders\"",
        "\"id\": \"table.html.ghost\"",
    );
    assert_eq!(
        load_package(&dir).err().unwrap().code,
        "register.invalid_evidence"
    );
    let dir = scratch("noimpl");
    edit(
        &dir,
        "registry/primitives/status.v1.json",
        "\"implementation\": \"compiler/crates/vsf-registry/src/compose.rs\"\n    },\n    {\n      \"id\": \"TABLE-002\"",
        "\"implementation\": \"compiler/crates/vsf-registry/src/missing.rs\"\n    },\n    {\n      \"id\": \"TABLE-002\"",
    );
    assert_eq!(
        load_package(&dir).err().unwrap().code,
        "witness.missing_file"
    );
}

#[test]
fn both_tables_share_one_composition() {
    let pkg = package();
    assert_eq!(pkg.status.compositions.len(), 1);
    assert_eq!(
        pkg.status.compositions[0].reused_by,
        ["TABLE-001", "TABLE-002"]
    );
    assert_eq!(
        pkg.effective_maturity("TABLE-001"),
        vsf_registry::model::Maturity::Implemented
    );
    assert_eq!(
        pkg.effective_maturity("TABLE-003"),
        vsf_registry::model::Maturity::Candidate
    );
}
