#![allow(clippy::unwrap_used, clippy::expect_used, clippy::panic)]

use std::path::PathBuf;
use vsf_registry::dashboard::Dashboard;
use vsf_registry::fixture::Fixture;
use vsf_registry::harness::{Package, load_package};
use vsf_registry::json::pretty;
use vsf_registry::manifest::DASHBOARD;
use vsf_registry::model::Maturity;

fn repo() -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("../../..")
        .canonicalize()
        .unwrap()
}

fn package() -> Package {
    load_package(&repo()).unwrap()
}

#[test]
fn the_denominator_is_the_whole_catalog_and_tiers_are_cumulative() {
    let d = Dashboard::build(&package());
    assert_eq!(d.total, 924);
    assert_eq!(d.by_maturity.iter().map(|(_, n)| n).sum::<usize>(), 924);
    assert_eq!(d.by_kind.iter().map(|(_, n)| n).sum::<usize>(), 924);
    assert_eq!(d.families.iter().map(|f| f.total).sum::<usize>(), 924);
    let tiers = [
        d.total,
        d.at_least(Maturity::Implemented),
        d.at_least(Maturity::Conformant),
        d.at_least(Maturity::ProductionProven),
    ];
    assert!(tiers.windows(2).all(|w| w[0] >= w[1]));
}

#[test]
fn only_witnessed_claims_are_counted_and_nothing_is_validated_or_proven_yet() {
    let d = Dashboard::build(&package());
    assert_eq!(d.at_least(Maturity::Implemented), 2);
    assert_eq!(d.at_least(Maturity::Conformant), 0);
    assert_eq!(d.at_least(Maturity::ProductionProven), 0);
    assert_eq!(
        d.by_maturity
            .iter()
            .find(|(m, _)| *m == Maturity::Candidate)
            .unwrap()
            .1,
        922
    );
}

#[test]
fn source_apps_count_witness_fixtures_apart_from_unbound_ones() {
    let pkg = package();
    let d = Dashboard::build(&pkg);
    let bound: usize = d.apps.iter().map(|a| a.witness_fixtures).sum();
    let unbound: usize = d.apps.iter().map(|a| a.unbound_fixtures).sum();
    assert_eq!(bound + unbound, pkg.fixtures.len());
    assert_eq!(bound, 6);
    assert!(
        d.apps
            .iter()
            .all(|a| a.operations >= 1 && a.primitives == 1)
    );
    assert_eq!(d.source_operations, 3);
}

#[test]
fn extra_unbound_fixtures_and_apps_do_not_move_any_tier() {
    let mut pkg = package();
    let before = Dashboard::build(&pkg);
    let mut extra: Fixture = pkg.fixtures[0].clone();
    extra.id = "table.extra.unbound".into();
    extra.app = "padding-app".into();
    extra.operations = vec!["collection.read".into(), "collection.sort".into()];
    pkg.fixtures.push(extra);
    let after = Dashboard::build(&pkg);
    assert_eq!(before.by_maturity, after.by_maturity);
    assert_eq!(before.source_operations, after.source_operations);
    let pad = after.apps.iter().find(|a| a.app == "padding-app").unwrap();
    assert_eq!(
        (pad.witness_fixtures, pad.operations, pad.unbound_fixtures),
        (0, 0, 1)
    );
}

#[test]
fn a_higher_maturity_moves_only_its_own_tier_and_never_the_total() {
    let mut pkg = package();
    pkg.registered.get_mut("TABLE-001").unwrap().maturity = Maturity::Conformant;
    let d = Dashboard::build(&pkg);
    assert_eq!(d.total, 924);
    assert_eq!(d.at_least(Maturity::Implemented), 2);
    assert_eq!(d.at_least(Maturity::Conformant), 1);
    assert_eq!(d.at_least(Maturity::ProductionProven), 0);
    let fam = d.families.iter().find(|f| f.code == "TABLE").unwrap();
    assert_eq!((fam.implemented, fam.validated, fam.proven), (2, 1, 0));
}

#[test]
fn the_committed_dashboard_is_the_current_one_and_the_text_names_the_denominator() {
    let pkg = package();
    let d = Dashboard::build(&pkg);
    let have = std::fs::read_to_string(repo().join(DASHBOARD)).unwrap();
    assert_eq!(have, pretty(&d.encode()));
    let text = d.render();
    assert!(text.contains("fixed denominator): 924"));
    assert!(text.contains("implemented: 2/924 (0.2%)"));
    assert!(text.contains("independently validated: 0/924 (0.0%)"));
}
