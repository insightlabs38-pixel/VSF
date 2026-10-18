#![allow(clippy::unwrap_used, clippy::expect_used, clippy::panic)]

use std::collections::BTreeSet;
use std::path::PathBuf;
use vsf_canon::{Value, parse, to_canonical};
use vsf_registry::graph::{GraphError, verify};
use vsf_registry::import::{classify, import};
use vsf_registry::json::pretty;
use vsf_registry::model::{Entry, Kind, Maturity, Registry, load};
use vsf_registry::policy::Policy;
use vsf_registry::register::{
    Evidence, Recognition, RegisterError, Registrar, Registration, recognize,
};

fn root() -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../../..")
}

fn read(rel: &str) -> Vec<u8> {
    std::fs::read(root().join(rel)).unwrap()
}

fn policy() -> Policy {
    Policy::decode(&load(&read("registry/primitives/policy.v1.json")).unwrap()).unwrap()
}

fn registry() -> Registry {
    Registry::decode(&load(&read("registry/primitives/registry.v1.json")).unwrap()).unwrap()
}

#[test]
fn all_924_catalog_entries_import_unchanged_and_exactly_once() {
    let catalog = parse(&read("registry/candidates/catalog.v0.json")).unwrap();
    let Some(Value::Array(raw)) = catalog.get("entries") else {
        panic!("entries")
    };
    let reg = registry();
    assert_eq!((raw.len(), reg.entries.len()), (924, 924));
    for (src, got) in raw.iter().zip(&reg.entries) {
        let f = |k: &str| src.get(k).unwrap().as_str().unwrap().to_string();
        assert_eq!(
            (
                f("id"),
                f("name"),
                f("family"),
                f("family_code"),
                f("layer"),
                f("launch_priority")
            ),
            (
                got.id.clone(),
                got.name.clone(),
                got.family.clone(),
                got.family_code.clone(),
                got.layer.clone(),
                got.launch_priority.clone()
            )
        );
    }
    let ids: BTreeSet<&str> = reg.entries.iter().map(|e| e.id.as_str()).collect();
    assert_eq!(ids.len(), 924);
}

#[test]
fn committed_registry_equals_a_fresh_import() {
    let fresh = import(&read("registry/candidates/catalog.v0.json"), &policy()).unwrap();
    assert_eq!(
        pretty(&fresh.encode()).as_bytes(),
        read("registry/primitives/registry.v1.json").as_slice()
    );
    assert_eq!(
        to_canonical(&parse(&read("registry/primitives/registry.v1.json")).unwrap()),
        to_canonical(&fresh.encode())
    );
}

#[test]
fn import_makes_no_maturity_claim_and_every_classification_is_justified() {
    let reg = registry();
    let policy = policy();
    assert!(
        reg.entries
            .iter()
            .all(|e| e.maturity == Maturity::Candidate)
    );
    let rules: BTreeSet<&str> = policy.keyword_rules.iter().map(|r| r.id.as_str()).collect();
    for e in &reg.entries {
        let known = rules.contains(e.rule.as_str())
            || e.rule == format!("F-{}", e.family_code)
            || e.rule == format!("F-{}-UNRESOLVED", e.family_code)
            || e.rule.split('+').all(|r| rules.contains(r));
        assert!(known, "{} {}", e.id, e.rule);
        let (kind, rule) = classify(&policy, &e.family_code, &e.name).unwrap();
        assert_eq!((kind, rule.as_str()), (e.kind, e.rule.as_str()), "{}", e.id);
        assert_eq!(
            e.kind == Kind::Unresolved,
            e.rule.contains("UNRESOLVED") || e.rule.contains('+'),
            "{}",
            e.id
        );
    }
    let by = |k: Kind| reg.entries.iter().filter(|e| e.kind == k).count();
    assert_eq!(
        by(Kind::Atomic)
            + by(Kind::Composite)
            + by(Kind::Adapter)
            + by(Kind::Service)
            + by(Kind::Unresolved),
        924
    );
    assert!(
        by(Kind::Unresolved) * 10 < 924,
        "unresolved share must stay small"
    );
}

#[test]
fn import_rejects_duplicates_and_maturity_claims() {
    let policy = policy();
    let canonical = String::from_utf8(to_canonical(
        &parse(&read("registry/candidates/catalog.v0.json")).unwrap(),
    ))
    .unwrap();
    let dup = canonical.replacen("\"id\":\"OBS-002\"", "\"id\":\"OBS-001\"", 1);
    assert_ne!(dup, canonical);
    assert_eq!(
        import(dup.as_bytes(), &policy).unwrap_err().code,
        "registry.duplicate_id"
    );
    let claimed = canonical.replacen(
        "\"maturity\":\"candidate\"",
        "\"maturity\":\"conformant\"",
        1,
    );
    assert_ne!(claimed, canonical);
    assert_eq!(
        import(claimed.as_bytes(), &policy).unwrap_err().code,
        "registry.import_claims_maturity"
    );
    let bogus = canonical.replacen("\"maturity\":\"candidate\"", "\"maturity\":\"stable\"", 1);
    assert_eq!(
        import(bogus.as_bytes(), &policy).unwrap_err().code,
        "registry.bad_maturity"
    );
    let extra = canonical.replacen(
        "\"maturity\":\"candidate\"",
        "\"maturity\":\"candidate\",\"verified\":true",
        1,
    );
    assert_eq!(
        import(extra.as_bytes(), &policy).unwrap_err().code,
        "registry.unknown_field"
    );
}

#[test]
fn ownership_dependencies_and_advancement_are_complete() {
    let reg = registry();
    let policy = policy();
    let index = reg.index();
    for e in &reg.entries {
        assert!(!e.owner.is_empty(), "{}", e.id);
        if e.id == policy.root {
            assert!(e.requires.is_empty());
        } else {
            assert!(!e.requires.is_empty(), "{}", e.id);
        }
        for r in &e.requires {
            assert!(index.contains_key(r.as_str()), "{} -> {r}", e.id);
        }
    }
    for kind in ["atomic", "composite", "adapter", "service"] {
        let levels = &policy.advancement[kind];
        for level in [
            "specified",
            "implemented",
            "conformant",
            "production_proven",
        ] {
            assert!(!levels[level].is_empty(), "{kind}/{level}");
        }
    }
    assert!(
        policy.advancement["UNRESOLVED"]["specified"]
            .contains(&"classification-decision".to_string())
    );
}

fn tiny() -> Registry {
    let mk = |id: &str, kind: Kind, requires: &[&str]| Entry {
        id: id.into(),
        name: id.into(),
        family: "F".into(),
        family_code: "F".into(),
        layer: "Core".into(),
        launch_priority: "Core".into(),
        kind,
        rule: "F-F".into(),
        owner: "o".into(),
        requires: requires.iter().map(|s| (*s).into()).collect(),
        maturity: Maturity::Candidate,
    };
    Registry {
        catalog_sha256: String::new(),
        entries: vec![
            mk("A-1", Kind::Atomic, &[]),
            mk("A-2", Kind::Atomic, &["A-1"]),
            mk("A-3", Kind::Composite, &["A-1", "A-2"]),
            mk("A-4", Kind::Unresolved, &["A-1"]),
            mk("A-5", Kind::Atomic, &["A-1"]),
        ],
    }
}

fn rec(produces: &str, sig: &[&str], excl: &[&str], precedence: u32) -> Recognition {
    Recognition {
        produces: produces.into(),
        signature: sig.iter().map(|s| (*s).into()).collect(),
        excludes: excl.iter().map(|s| (*s).into()).collect(),
        precedence,
    }
}

fn reg(id: &str, maturity: Maturity) -> Registration {
    Registration {
        id: id.into(),
        maturity,
        execution: vec!["browser".into()],
        needs_capabilities: vec!["dom.snapshot".into()],
        recognition: None,
        evidence: Vec::new(),
        implementation: None,
    }
}

fn fx(id: &str, pol: &str) -> Evidence {
    Evidence {
        kind: "fixture".into(),
        id: id.into(),
        polarity: Some(pol.into()),
        independent: false,
    }
}

fn spec() -> Evidence {
    Evidence {
        kind: "spec-document".into(),
        id: "docs/x.md".into(),
        polarity: None,
        independent: false,
    }
}

fn implemented(id: &str) -> Registration {
    let mut r = reg(id, Maturity::Implemented);
    r.evidence = vec![spec(), fx("fx.pos", "positive"), fx("fx.neg", "negative")];
    r.implementation = Some("src/x.rs".into());
    r
}

fn registrar<'a>(tiny: &'a Registry, policy: &'a Policy) -> Registrar<'a> {
    Registrar::new(
        tiny,
        policy,
        ["fx.pos", "fx.neg", "fx.adv", "fx.ind"]
            .iter()
            .map(|s| (*s).to_string())
            .collect(),
    )
}

#[test]
fn registration_accepts_a_complete_implemented_primitive() {
    let (t, p) = (tiny(), policy());
    let mut r = registrar(&t, &p);
    r.register(implemented("A-1")).unwrap();
    assert_eq!(r.registered().len(), 1);
}

#[test]
fn registration_rejects_duplicates_unknown_ids_and_unresolved_kinds() {
    let (t, p) = (tiny(), policy());
    let mut r = registrar(&t, &p);
    r.register(reg("A-1", Maturity::Candidate)).unwrap();
    assert_eq!(
        r.register(reg("A-1", Maturity::Candidate))
            .unwrap_err()
            .code(),
        "register.duplicate_id"
    );
    assert_eq!(
        r.register(reg("ZZ-9", Maturity::Candidate)).unwrap_err(),
        RegisterError::UnknownPrimitive("ZZ-9".into())
    );
    assert_eq!(
        r.register(reg("A-4", Maturity::Specified))
            .unwrap_err()
            .code(),
        "register.unresolved_kind"
    );
    r.register(reg("A-4", Maturity::Candidate)).unwrap();
}

#[test]
fn registration_rejects_implemented_claims_without_a_witness() {
    let (t, p) = (tiny(), policy());
    let mut r = registrar(&t, &p);
    type Mutation = Box<dyn Fn(&mut Registration)>;
    let cases: Vec<(&str, Mutation)> = vec![
        (
            "no spec",
            Box::new(|x| x.evidence.retain(|e| e.kind != "spec-document")),
        ),
        ("no implementation", Box::new(|x| x.implementation = None)),
        (
            "empty implementation",
            Box::new(|x| x.implementation = Some(String::new())),
        ),
        (
            "no negative fixture",
            Box::new(|x| {
                x.evidence
                    .retain(|e| e.polarity.as_deref() != Some("negative"))
            }),
        ),
        (
            "no positive fixture",
            Box::new(|x| {
                x.evidence
                    .retain(|e| e.polarity.as_deref() != Some("positive"))
            }),
        ),
        (
            "unknown fixture",
            Box::new(|x| x.evidence.push(fx("fx.ghost", "positive"))),
        ),
        (
            "fixture without polarity",
            Box::new(|x| {
                x.evidence.push(Evidence {
                    kind: "fixture".into(),
                    id: "fx.pos".into(),
                    polarity: None,
                    independent: false,
                })
            }),
        ),
        (
            "unknown evidence kind",
            Box::new(|x| {
                x.evidence.push(Evidence {
                    kind: "vibes".into(),
                    id: "x".into(),
                    polarity: None,
                    independent: false,
                })
            }),
        ),
        (
            "empty evidence id",
            Box::new(|x| {
                x.evidence.push(Evidence {
                    kind: "review".into(),
                    id: String::new(),
                    polarity: None,
                    independent: false,
                })
            }),
        ),
    ];
    for (name, mutate) in cases {
        let mut x = implemented("A-1");
        mutate(&mut x);
        assert_eq!(
            r.register(x).unwrap_err().code(),
            "register.invalid_evidence",
            "{name}"
        );
    }
}

#[test]
fn higher_maturity_levels_demand_independent_adversarial_and_field_evidence() {
    let (t, p) = (tiny(), policy());
    let mut r = registrar(&t, &p);
    r.register(implemented("A-1")).unwrap();
    let mut c = implemented("A-2");
    c.maturity = Maturity::Conformant;
    assert_eq!(
        r.register(c.clone()).unwrap_err().code(),
        "register.invalid_evidence"
    );
    c.evidence.push(Evidence {
        independent: true,
        ..fx("fx.ind", "positive")
    });
    c.evidence.push(fx("fx.adv", "adversarial"));
    assert_eq!(
        r.register(c.clone()).unwrap_err().code(),
        "register.invalid_evidence"
    );
    c.evidence.push(Evidence {
        kind: "review".into(),
        id: "rev-1".into(),
        polarity: None,
        independent: false,
    });
    r.register(c).unwrap();
    let mut pp = implemented("A-5");
    pp.maturity = Maturity::ProductionProven;
    pp.evidence.extend([
        Evidence {
            independent: true,
            ..fx("fx.ind", "positive")
        },
        fx("fx.adv", "adversarial"),
        Evidence {
            kind: "review".into(),
            id: "rev-1".into(),
            polarity: None,
            independent: false,
        },
    ]);
    assert_eq!(
        r.register(pp.clone()).unwrap_err().code(),
        "register.invalid_evidence"
    );
    pp.evidence.push(Evidence {
        kind: "field".into(),
        id: "tenant-report".into(),
        polarity: None,
        independent: false,
    });
    r.register(pp).unwrap();
}

#[test]
fn a_conformant_composite_needs_its_dependencies_implemented() {
    let (t, p) = (tiny(), policy());
    let mut r = registrar(&t, &p);
    let mut c = implemented("A-3");
    c.maturity = Maturity::Conformant;
    c.evidence.extend([
        Evidence {
            independent: true,
            ..fx("fx.ind", "positive")
        },
        fx("fx.adv", "adversarial"),
        Evidence {
            kind: "review".into(),
            id: "r".into(),
            polarity: None,
            independent: false,
        },
    ]);
    assert_eq!(
        r.register(c.clone()).unwrap_err().code(),
        "register.invalid_evidence"
    );
    r.register(implemented("A-1")).unwrap();
    r.register(implemented("A-2")).unwrap();
    r.register(c).unwrap();
}

#[test]
fn ambiguous_recognitions_are_rejected_unless_exclusive_or_ordered() {
    let (t, p) = (tiny(), policy());
    let with = |id: &str, r: Recognition| {
        let mut x = reg(id, Maturity::Candidate);
        x.recognition = Some(r);
        x
    };
    let mut r = registrar(&t, &p);
    r.register(with(
        "A-1",
        rec("collection", &["tag:table"], &["role:presentation"], 10),
    ))
    .unwrap();
    let err = r
        .register(with(
            "A-2",
            rec("collection", &["tag:table", "attr:summary"], &[], 10),
        ))
        .unwrap_err();
    assert_eq!(
        err,
        RegisterError::AmbiguousRecognition {
            a: "A-1".into(),
            b: "A-2".into(),
            produces: "collection".into()
        }
    );
    r.register(with(
        "A-2",
        rec("collection", &["tag:table", "attr:summary"], &[], 20),
    ))
    .unwrap();
    r.register(
        with(
            "A-3",
            rec(
                "collection",
                &["role:presentation", "tag:table"],
                &["tag:table"],
                10,
            ),
        )
        .clone(),
    )
    .unwrap_err();
    r.register(with(
        "A-5",
        rec("collection", &["role:grid"], &["tag:table"], 10),
    ))
    .unwrap();
    r.register(with("A-3", rec("other-output", &["tag:table"], &[], 10)))
        .unwrap();
}

#[test]
fn malformed_recognitions_are_rejected() {
    let (t, p) = (tiny(), policy());
    let mut r = registrar(&t, &p);
    for bad in [
        rec("", &["tag:table"], &[], 1),
        rec("c", &[], &[], 1),
        rec("c", &["Table"], &[], 1),
        rec("c", &["tag:table"], &["tag:table"], 1),
    ] {
        let mut x = reg("A-1", Maturity::Candidate);
        x.recognition = Some(bad);
        assert_eq!(
            r.register(x).unwrap_err().code(),
            "register.invalid_recognition"
        );
    }
}

#[test]
fn capability_mismatches_and_unknown_modes_are_rejected() {
    let (t, p) = (tiny(), policy());
    let mut r = registrar(&t, &p);
    let mut x = reg("A-1", Maturity::Candidate);
    x.execution = vec!["browser".into(), "authorized_protocol".into()];
    assert_eq!(
        r.register(x).unwrap_err(),
        RegisterError::CapabilityMismatch {
            id: "A-1".into(),
            mode: "authorized_protocol".into(),
            capability: "dom.snapshot".into()
        }
    );
    let mut y = reg("A-1", Maturity::Candidate);
    y.execution = vec!["telepathy".into()];
    assert_eq!(r.register(y).unwrap_err().code(), "register.unknown_mode");
    let mut z = reg("A-1", Maturity::Candidate);
    z.execution = vec!["authorized_protocol".into()];
    z.needs_capabilities = vec!["api.direct".into()];
    r.register(z).unwrap();
}

#[test]
fn recognition_prefers_the_highest_precedence_and_honors_exclusions() {
    let (t, p) = (tiny(), policy());
    let mut r = registrar(&t, &p);
    let mut a = reg("A-1", Maturity::Candidate);
    a.recognition = Some(rec(
        "collection",
        &["tag:table"],
        &["role:presentation"],
        10,
    ));
    let mut b = reg("A-2", Maturity::Candidate);
    b.recognition = Some(rec("collection", &["tag:table", "attr:summary"], &[], 20));
    r.register(a).unwrap();
    r.register(b).unwrap();
    let obs = |xs: &[&str]| xs.iter().map(|s| (*s).to_string()).collect::<BTreeSet<_>>();
    let ids = |hits: Vec<&Registration>| hits.iter().map(|h| h.id.clone()).collect::<Vec<_>>();
    assert_eq!(
        ids(recognize(
            r.registered(),
            &obs(&["tag:table", "attr:summary"])
        )),
        ["A-2", "A-1"]
    );
    assert_eq!(
        ids(recognize(
            r.registered(),
            &obs(&["tag:table", "role:presentation"])
        )),
        Vec::<String>::new()
    );
    assert_eq!(
        ids(recognize(r.registered(), &obs(&["tag:div"]))),
        Vec::<String>::new()
    );
}

#[test]
fn the_real_registry_has_complete_acyclic_dependencies_in_a_stable_order() {
    let reg = registry();
    let order = verify(&reg).unwrap();
    assert_eq!(order.len(), 924);
    let pos: std::collections::BTreeMap<&str, usize> = order
        .iter()
        .enumerate()
        .map(|(i, s)| (s.as_str(), i))
        .collect();
    for e in &reg.entries {
        for d in &e.requires {
            assert!(pos[d.as_str()] < pos[e.id.as_str()], "{} before {d}", e.id);
        }
    }
    let mut shuffled = reg.clone();
    shuffled.entries.reverse();
    assert_eq!(verify(&shuffled).unwrap(), order);
    assert_eq!(order[0], "OBS-001");
    assert_eq!(vsf_canon::sha256_hex(order.join("\n").as_bytes()).len(), 64);
}

#[test]
fn graph_reports_the_exact_missing_dependency_and_cycle_chain() {
    let mut r = tiny();
    r.entries[1].requires.push("GHOST-1".into());
    assert_eq!(
        verify(&r).unwrap_err(),
        vec![GraphError::MissingDependency {
            from: "A-2".into(),
            to: "GHOST-1".into()
        }]
    );
    let mut c = tiny();
    c.entries[0].requires = vec!["A-3".into()];
    c.entries[2].requires = vec!["A-2".into()];
    c.entries[1].requires = vec!["A-1".into()];
    let errs = verify(&c).unwrap_err();
    assert_eq!(
        errs,
        vec![GraphError::Cycle {
            chain: ["A-1", "A-3", "A-2", "A-1"].map(String::from).to_vec()
        }]
    );
    assert_eq!(errs[0].describe(), "A-1 -> A-3 -> A-2 -> A-1");
    let mut s = tiny();
    s.entries[4].requires = vec!["A-5".into()];
    assert_eq!(verify(&s).unwrap_err()[0].code(), "graph.self_dependency");
    let mut both = tiny();
    both.entries[0].requires = vec!["A-2".into(), "NOPE".into()];
    let codes: Vec<&str> = verify(&both)
        .unwrap_err()
        .iter()
        .map(GraphError::code)
        .collect();
    assert_eq!(codes, ["graph.missing_dependency", "graph.cycle"]);
}
