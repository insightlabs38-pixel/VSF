#![allow(clippy::unwrap_used, clippy::expect_used, clippy::panic)]

mod common;
use common::*;
use std::collections::BTreeMap;
use vsf_canon::Value;
use vsf_recognizer::contract::{Access, Availability, Contract, derive};
use vsf_recognizer::engine::recognize;
use vsf_recognizer::ledger::build_ledger;
use vsf_recognizer::obs::Observation;
use vsf_recognizer::semantics::Rules;
use vsf_recognizer::trust::ContextChange;
use vsf_recognizer::witness::{AcceptedWitnesses, EffectClass, Emission, Witness, WitnessPolicy};

fn contract_of(name: &str) -> Contract {
    let obs = snapshot(name);
    let rec = recognize(&obs, Rules::V2);
    derive(
        &rec,
        &build_ledger(&obs, &rec, 0),
        &AcceptedWitnesses::none(),
        &BTreeMap::new(),
    )
}

struct World {
    obs: Observation,
    rec: vsf_recognizer::engine::Recognition,
}

impl World {
    fn of(obs: Observation) -> World {
        let rec = recognize(&obs, Rules::V2);
        World { obs, rec }
    }

    fn derive(
        &self,
        docs: &[Wit],
        pol: &WitnessPolicy,
        declared: &BTreeMap<u32, EffectClass>,
    ) -> (Contract, AcceptedWitnesses) {
        let values: Vec<Value> = docs.iter().map(Wit::value).collect();
        let acc = AcceptedWitnesses::collect(&values, pol, &self.obs);
        let c = derive(
            &self.rec,
            &build_ledger(&self.obs, &self.rec, 0),
            &acc,
            declared,
        );
        (c, acc)
    }
}

fn offer(c: &Contract, node: u32, access: Access) -> &vsf_recognizer::contract::Offer {
    c.for_node(node)
        .find(|o| o.access == access)
        .unwrap_or_else(|| panic!("no {access:?} offer for {node}"))
}

fn page(labels: &[&str]) -> (Observation, Vec<u32>) {
    let mut b = B::new();
    let root = b.div(1);
    let ids = labels.iter().map(|l| b.button(root, l)).collect();
    (b.build(), ids)
}

#[test]
fn nothing_source_backed_is_emitted_without_a_witness_on_any_fixture() {
    let mut withheld = 0;
    for name in REGRESSION.iter().chain(HELD_OUT.iter()) {
        let c = contract_of(name);
        assert!(
            c.emitted().all(|o| o.access == Access::Local),
            "{name} emitted a source operation"
        );
        assert_eq!(c.synthetic_only().count(), 0);
        withheld += c
            .offers
            .iter()
            .filter(|o| o.access == Access::Invoke)
            .count();
    }
    assert!(withheld > 20);
}

#[test]
fn harmless_looking_labels_are_not_read_only_until_a_witness_says_so() {
    let labels = [
        "View", "Cancel", "Search", "Open", "Export", "Back", "Close",
    ];
    let (obs, ids) = page(&labels);
    let w = World::of(obs);
    let (c, _) = w.derive(&[], &wpolicy(Emission::Production), &BTreeMap::new());
    for id in &ids {
        let o = offer(&c, *id, Access::Invoke);
        assert_eq!(o.availability, Availability::Unknown("effect_unverified"));
        assert!(!o.authorized());
    }
    assert_eq!(c.emitted().count(), 0);
}

#[test]
fn a_view_button_that_mutates_is_a_write_and_a_delete_that_only_reads_is_a_read() {
    let (obs, ids) = page(&["View", "Delete all"]);
    let (view, delete) = (ids[0], ids[1]);
    let w = World::of(obs);
    let docs = [
        Wit::of(&w.obs, view, "action", "non_idempotent_write"),
        Wit::of(&w.obs, delete, "action", "read_only"),
    ];
    let (c, acc) = w.derive(&docs, &wpolicy(Emission::Production), &BTreeMap::new());
    assert_eq!(acc.len(), 2);
    let v = offer(&c, view, Access::Write);
    assert!(v.authorized() && v.effect.intent_mismatch);
    assert_eq!(v.effect.demonstrated, Some(EffectClass::NonIdempotentWrite));
    let d = offer(&c, delete, Access::Read);
    assert!(d.authorized() && d.effect.intent_mismatch);
    assert!(c.for_node(view).all(|o| o.access != Access::Read));
}

#[test]
fn a_declared_effect_never_authorizes_and_a_contradiction_with_a_witness_is_a_conflict() {
    let (obs, ids) = page(&["Refresh"]);
    let id = ids[0];
    let w = World::of(obs);
    let declared: BTreeMap<u32, EffectClass> = [(id, EffectClass::ReadOnly)].into_iter().collect();
    let (c, _) = w.derive(&[], &wpolicy(Emission::Production), &declared);
    let o = offer(&c, id, Access::Invoke);
    assert_eq!(o.availability, Availability::Unknown("effect_unverified"));
    assert_eq!(o.effect.declared, Some(EffectClass::ReadOnly));
    let docs = [Wit::of(&w.obs, id, "action", "destructive")];
    let (c, _) = w.derive(&docs, &wpolicy(Emission::Production), &declared);
    let o = offer(&c, id, Access::Write);
    assert_eq!(
        o.availability,
        Availability::Conflicted("declared_effect_conflict")
    );
    assert!(!o.authorized());
    let (c, _) = w.derive(
        &[Wit::of(&w.obs, id, "action", "read_only")],
        &wpolicy(Emission::Production),
        &declared,
    );
    assert!(offer(&c, id, Access::Read).authorized());
}

#[test]
fn indeterminate_and_contradicting_witnesses_do_not_authorize() {
    let (obs, ids) = page(&["Sync", "Run"]);
    let w = World::of(obs);
    let mut second = Wit::of(&w.obs, ids[1], "action", "destructive");
    second.id = "wit_00000000000000ff".into();
    let docs = [
        Wit::of(&w.obs, ids[0], "action", "indeterminate"),
        Wit::of(&w.obs, ids[1], "action", "read_only"),
        second,
    ];
    let (c, _) = w.derive(&docs, &wpolicy(Emission::Production), &BTreeMap::new());
    assert_eq!(
        offer(&c, ids[0], Access::Invoke).availability,
        Availability::Unknown("effect_indeterminate")
    );
    let run = c.for_node(ids[1]).next().unwrap();
    assert_eq!(
        run.availability,
        Availability::Conflicted("witness_conflict")
    );
    assert_eq!(c.emitted().count(), 0);
}

fn rejected_with(mutate: impl Fn(&mut Wit), policy: impl Fn(&mut WitnessPolicy)) -> &'static str {
    let (obs, ids) = page(&["Save changes"]);
    let w = World::of(obs);
    let mut d = Wit::of(&w.obs, ids[0], "action", "idempotent_write");
    mutate(&mut d);
    let mut pol = wpolicy(Emission::Production);
    policy(&mut pol);
    let (c, acc) = w.derive(&[d], &pol, &BTreeMap::new());
    assert_eq!(acc.len(), 0, "accepted");
    assert_eq!(c.emitted().count(), 0);
    acc.rejected[0].reason
}

#[test]
fn a_witness_from_another_source_user_environment_or_version_is_refused() {
    let nothing = |_: &mut WitnessPolicy| {};
    assert_eq!(
        rejected_with(|w| w.ctx.app_id = "acme:crm-us1".into(), nothing),
        "wrong_application"
    );
    assert_eq!(
        rejected_with(
            |w| w.ctx.origin = "https://other.example.com".into(),
            nothing
        ),
        "wrong_origin"
    );
    assert_eq!(
        rejected_with(
            |w| w.ctx.principal = "prin_22222222222222222222222222222222".into(),
            nothing
        ),
        "wrong_principal"
    );
    assert_eq!(
        rejected_with(
            |w| w.ctx.variant_id = "var_ffffffffffffffffffffffffffffffff".into(),
            nothing
        ),
        "wrong_variant"
    );
    assert_eq!(
        rejected_with(|w| w.ctx.runtime_id = "rt_other".into(), nothing),
        "wrong_environment"
    );
    assert_eq!(
        rejected_with(|w| w.ctx.mode = "headless_worker".into(), nothing),
        "wrong_environment"
    );
    assert_eq!(
        rejected_with(
            |w| w.ctx.capability_digest = format!("sha256:{}", "1".repeat(64)),
            nothing
        ),
        "capability_changed"
    );
    assert_eq!(
        rejected_with(
            |w| w.ctx.source_revision = format!("sha256:{}", "2".repeat(64)),
            nothing
        ),
        "source_version_changed"
    );
    assert!(ContextChange::SourceRevision.invalidates_behaviour());
}

#[test]
fn stale_expired_future_failed_synthetic_and_untrusted_witnesses_are_refused() {
    let nothing = |_: &mut WitnessPolicy| {};
    assert_eq!(rejected_with(|w| w.expires_at = NOW, nothing), "expired");
    assert_eq!(
        rejected_with(
            |w| {
                w.verified_at = NOW - 3 * 24 * 3600 * 1000;
                w.expires_at = NOW + 1000;
            },
            nothing
        ),
        "stale"
    );
    assert_eq!(
        rejected_with(
            |w| {
                w.verified_at = NOW + 3_600_000;
                w.expires_at = NOW + 7_200_000;
            },
            nothing
        ),
        "from_the_future"
    );
    assert_eq!(rejected_with(|w| w.status = "failed", nothing), "failed");
    assert_eq!(
        rejected_with(|w| w.status = "inconclusive", nothing),
        "inconclusive"
    );
    assert_eq!(
        rejected_with(|w| w.status = "synthetic", nothing),
        "synthetic_not_allowed"
    );
    assert_eq!(
        rejected_with(|w| w.issuer = "unknown-verifier", nothing),
        "untrusted_issuer"
    );
}

#[test]
fn revocation_of_a_witness_operation_principal_or_authority_ends_it() {
    assert_eq!(
        rejected_with(|w| w.revoked_at = Some(NOW - 1), |_| {}),
        "revoked"
    );
    assert_eq!(
        rejected_with(
            |_| {},
            |p| {
                p.revoked.witnesses.insert("wit_0000000000000016".into());
            }
        ),
        "revoked"
    );
    assert_eq!(
        rejected_with(
            |_| {},
            |p| {
                p.revoked.principals.insert(OWNER.into());
            }
        ),
        "revoked"
    );
    assert_eq!(
        rejected_with(
            |_| {},
            |p| {
                p.revoked.authorities.insert("fixture-verifier".into());
            }
        ),
        "revoked_authority"
    );
    let (obs, ids) = page(&["Save changes"]);
    let w = World::of(obs);
    let d = Wit::of(&w.obs, ids[0], "action", "idempotent_write");
    let mut pol = wpolicy(Emission::Production);
    pol.revoked.operations.insert((ids[0], d.digest.clone()));
    let (_, acc) = w.derive(&[d], &pol, &BTreeMap::new());
    assert_eq!(acc.rejected[0].reason, "revoked");
}

#[test]
fn a_witness_dies_when_the_node_it_covered_changes_or_vanishes() {
    let (obs, ids) = page(&["Save changes"]);
    let old = World::of(obs);
    let d = Wit::of(&old.obs, ids[0], "action", "idempotent_write");
    let mut e = Edit::of(&old.obs);
    e.node(ids[0]).name = Some("Delete account".into());
    let changed = World::of(e.build());
    let (c, acc) = changed.derive(
        &[d.json_clone()],
        &wpolicy(Emission::Production),
        &BTreeMap::new(),
    );
    assert_eq!(acc.rejected[0].reason, "operation_changed");
    assert_eq!(c.emitted().count(), 0);
    let mut e = Edit::of(&old.obs);
    e.remove(ids[0]);
    let gone = World::of(e.build());
    let (_, acc) = gone.derive(&[d], &wpolicy(Emission::Production), &BTreeMap::new());
    assert_eq!(acc.rejected[0].reason, "operation_missing");
}

impl Wit {
    fn json_clone(&self) -> Wit {
        Wit {
            ctx: self.ctx.clone(),
            id: self.id.clone(),
            digest: self.digest.clone(),
            ..*self
        }
    }
}

#[test]
fn a_witness_authorizes_only_its_own_operation_and_kind() {
    let (obs, ids) = page(&["Save changes", "Export data"]);
    let w = World::of(obs);
    let (c, _) = w.derive(
        &[Wit::of(&w.obs, ids[0], "action", "idempotent_write")],
        &wpolicy(Emission::Production),
        &BTreeMap::new(),
    );
    assert!(offer(&c, ids[0], Access::Write).authorized());
    assert!(!offer(&c, ids[1], Access::Invoke).authorized());
    let (c, _) = w.derive(
        &[Wit::of(&w.obs, ids[0], "field_write", "idempotent_write")],
        &wpolicy(Emission::Production),
        &BTreeMap::new(),
    );
    assert_eq!(c.emitted().count(), 0);
}

#[test]
fn malformed_and_bare_id_witnesses_are_refused() {
    for text in [
        r#"{"id":"wit_0000000000000001"}"#,
        r#"[1,2,3]"#,
        r#"{"schema":"vsf-behavioral-witness-v1"}"#,
    ] {
        let v = vsf_canon::parse(text.as_bytes()).unwrap();
        assert!(Witness::from_value(&v).is_err());
    }
    let (obs, ids) = page(&["Save changes"]);
    let d = Wit::of(&obs, ids[0], "action", "idempotent_write");
    let extra = d
        .json()
        .replacen("{\"schema\"", "{\"extra\":1,\"schema\"", 1);
    assert!(Witness::from_value(&vsf_canon::parse(extra.as_bytes()).unwrap()).is_err());
    let inverted = Wit {
        expires_at: d.verified_at,
        ..d.json_clone()
    };
    assert!(Witness::from_value(&inverted.value()).is_err());
}

#[test]
fn synthetic_witnesses_exist_only_for_tests_and_never_reach_a_caller() {
    let (obs, ids) = page(&["Save changes"]);
    let w = World::of(obs);
    let mut d = Wit::of(&w.obs, ids[0], "action", "idempotent_write");
    d.status = "synthetic";
    d.issuer = "synthetic-test";
    let (c, acc) = w.derive(
        &[d.json_clone()],
        &wpolicy(Emission::TestOnly),
        &BTreeMap::new(),
    );
    assert_eq!(acc.len(), 1);
    assert_eq!(c.emitted().count(), 0);
    assert_eq!(c.synthetic_only().count(), 1);
    let (c, acc) = w.derive(&[d], &wpolicy(Emission::Production), &BTreeMap::new());
    assert_eq!(
        (acc.len(), c.emitted().count(), c.synthetic_only().count()),
        (0, 0, 0)
    );
}

#[test]
fn credential_and_hidden_controls_stay_unusable_even_with_a_witness_for_every_node() {
    let obs = snapshot("adversarial");
    let w = World::of(obs);
    let mut docs = Vec::new();
    for n in &w.obs.nodes {
        for kind in ["action", "field_write"] {
            let mut d = Wit::of(&w.obs, n.id, kind, "idempotent_write");
            d.id = format!(
                "wit_{:0>16x}",
                u64::from(n.id) * 2 + u64::from(kind == "action")
            );
            docs.push(d);
        }
    }
    let (c, acc) = w.derive(&docs, &wpolicy(Emission::Production), &BTreeMap::new());
    assert!(acc.len() > 10);
    for f in w.rec.fields().filter(|f| f.why_unusable.is_some()) {
        assert!(
            c.for_node(f.node)
                .all(|o| matches!(o.availability, Availability::Unusable(_)))
        );
    }
}

#[test]
fn a_human_decision_is_not_a_witness() {
    let (obs, ids) = page(&["Save changes"]);
    let w = World::of(obs);
    let (c, _) = w.derive(&[], &wpolicy(Emission::Production), &BTreeMap::new());
    assert_eq!(c.emitted().count(), 0);
    assert_eq!(
        offer(&c, ids[0], Access::Invoke).availability,
        Availability::Unknown("effect_unverified")
    );
}

#[test]
fn unknown_and_conflicted_reach_the_ui_state_and_never_the_emitted_set() {
    let mut b = B::new();
    let root = b.div(1);
    let lonely = b.input(root);
    let clash = b.add(root, "input", Some("button"), Some("Go"), None, BTN);
    let off = b.add(root, "button", Some("button"), Some("Pay later"), None, V);
    let obs = b.build();
    let rec = recognize(&obs, Rules::V2);
    let c = derive(
        &rec,
        &build_ledger(&obs, &rec, 0),
        &AcceptedWitnesses::none(),
        &BTreeMap::new(),
    );
    assert_eq!(
        offer(&c, lonely, Access::Local).availability,
        Availability::Unknown("label_unknown")
    );
    assert_eq!(
        offer(&c, clash, Access::Local).availability,
        Availability::Conflicted("kind_conflict")
    );
    assert!(matches!(
        c.for_node(off).next().unwrap().availability,
        Availability::Unusable(_)
    ));
    assert_eq!(Availability::Conflicted("x").ui(), "conflict_badge");
    assert_eq!(
        c.emitted()
            .filter(|o| o.node == lonely || o.node == clash)
            .count(),
        0
    );
}

#[test]
fn contract_document_is_stable_and_versioned() {
    let a = vsf_recognizer::val::pretty(&contract_of("plain-form").to_value());
    let b = vsf_recognizer::val::pretty(&contract_of("plain-form").to_value());
    assert_eq!(a, b);
    assert!(a.contains("vsf-availability-v2"));
}
