#![allow(clippy::unwrap_used, clippy::expect_used, clippy::panic)]

mod common;
use common::*;
use std::collections::BTreeSet;
use std::path::PathBuf;
use vsf_canon::Value;
use vsf_recognizer::engine::recognize;
use vsf_recognizer::ledger::{Certainty, SourceKind, build_ledger, build_ledger_in};
use vsf_recognizer::protocol::{Grant, Protocol, ProtocolPolicy, Standing, assess, validate};
use vsf_recognizer::reconcile::{open_conflicts, reconcile, reconcile_contradictions};
use vsf_recognizer::semantics::Rules;

const MONTH: i64 = 30 * 24 * 3600 * 1000;

fn root() -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../../..")
}

fn expand(text: &str) -> String {
    text.replace("{{AWS_KEY}}", &["AKIA", "IOSFODNN7EXAMPLE"].concat())
        .replace("{{USERINFO}}", &["user", ":pw", "@"].concat())
}

/// Documents canonical JSON cannot represent (a fractional number) come back as `None`: they are refused at parse.
fn fixtures(sub: &str) -> Vec<(String, Option<Value>)> {
    let dir = root().join("schemas/protocol-evidence/fixtures").join(sub);
    let mut out = Vec::new();
    for e in std::fs::read_dir(dir).unwrap() {
        let p = e.unwrap().path();
        let text = expand(&std::fs::read_to_string(&p).unwrap());
        out.push((
            p.file_stem().unwrap().to_string_lossy().into_owned(),
            vsf_canon::parse(text.as_bytes()).ok(),
        ));
    }
    out.sort_by(|a, b| a.0.cmp(&b.0));
    out
}

#[test]
fn every_shared_valid_fixture_passes_and_every_invalid_one_fails_for_the_expected_reason() {
    let valid = fixtures("valid");
    assert!(valid.len() >= 5);
    for (name, doc) in &valid {
        let doc = doc.as_ref().unwrap();
        assert_eq!(validate(doc), vec![], "{name}");
        assert!(Protocol::from_value(doc).is_ok(), "{name}");
    }
    let invalid = fixtures("invalid");
    assert!(invalid.len() >= 19);
    let mut refused_at_parse = 0;
    for (name, f) in &invalid {
        let Some(f) = f else {
            refused_at_parse += 1;
            assert_eq!(name, "sample-values-are-not-allowed");
            continue;
        };
        let expect: BTreeSet<String> = match f.get("expect") {
            Some(Value::Array(a)) => a.iter().map(|x| x.as_str().unwrap().to_string()).collect(),
            _ => panic!("{name}"),
        };
        let got: BTreeSet<String> = validate(f.get("doc").unwrap())
            .into_iter()
            .map(|p| p.code.to_string())
            .collect();
        assert_eq!(got, expect, "{name}");
    }
    assert!(refused_at_parse <= 1);
}

struct Spec {
    id: &'static str,
    kind: &'static str,
    support: &'static str,
    origin: &'static str,
    app: &'static str,
    recorded: i64,
    principal: &'static str,
    scope: &'static str,
    granted: i64,
    reviewer: Option<&'static str>,
    witness: &'static str,
    request: &'static str,
    path: &'static str,
}

impl Spec {
    fn observed() -> Spec {
        Spec {
            id: "pev_cccccccccccccccccccc",
            kind: "observed_traffic",
            support: "supported",
            origin: "https://crm.example.com",
            app: "acme:crm-eu1",
            recorded: 1_790_000_000_000,
            principal: OWNER,
            scope: "observe.protocol",
            granted: 1_789_999_999_000,
            reviewer: Some(REVIEWER),
            witness: "profile-w1",
            request: r#"{"name":"first_name","type":"string","required":true}"#,
            path: "/v1/profile",
        }
    }

    fn json(&self) -> String {
        let (stability, method, auth) = match self.kind {
            "observed_traffic" => (
                "undocumented",
                "runtime_observation",
                format!(
                    r#"{{"basis":"user_grant","principal_ref":"{}","scope":"{}","granted_at":{}}}"#,
                    self.principal, self.scope, self.granted
                ),
            ),
            "user_supplied_spec" => (
                "undocumented",
                "user_assertion",
                format!(
                    r#"{{"basis":"user_grant","principal_ref":"{}","scope":"{}","granted_at":{}}}"#,
                    self.principal, self.scope, self.granted
                ),
            ),
            _ => (
                "documented",
                "vendor_documentation",
                r#"{"basis":"public_documentation"}"#.to_string(),
            ),
        };
        let promotion = self.reviewer.map_or(String::new(), |r| {
            format!(
                r#","promotion":{{"decision_id":"review.x","reviewed_by":"{r}","documentation_reference":"https://docs.example.com/r","witnesses":["{}"]}}"#,
                self.witness
            )
        });
        format!(
            r#"{{"schema":"vsf-protocol-evidence-v1","id":"{}","source_app_id":"{}","origin":"{}","kind":"{}","stability":"{stability}","support":"{}","provenance":{{"method":"{method}","reference":"https://docs.example.com/a","recorded_at":{},"authorization":{auth}}},"endpoint":{{"protocol":"http","method":"POST","path_template":"{}"}},"shape":{{"request":[{}],"response":[]}}{promotion}}}"#,
            self.id,
            self.app,
            self.origin,
            self.kind,
            self.support,
            self.recorded,
            self.path,
            self.request
        )
    }

    fn build(&self) -> Protocol {
        Protocol::from_json(self.json().as_bytes()).unwrap()
    }
}

fn standing(s: &Spec) -> Standing {
    assess(&s.build(), &policy())
}

#[test]
fn a_supported_promoted_fresh_document_for_this_app_origin_and_principal_is_authoritative() {
    assert_eq!(standing(&Spec::observed()), Standing::Authoritative);
    let documented = Spec {
        kind: "documented_api",
        ..Spec::observed()
    };
    assert_eq!(standing(&documented), Standing::Authoritative);
}

#[test]
fn candidate_unsupported_user_supplied_unpromoted_unverified_and_stale_evidence_is_advisory_only() {
    let o = Spec::observed;
    let advisory = |s: Spec, why: &str| {
        assert_eq!(
            standing(&s),
            Standing::Advisory(match why {
                "candidate" => "candidate",
                "unsupported" => "unsupported",
                "user_supplied" => "user_supplied",
                "unpromoted" => "unpromoted",
                "promotion_unverified" => "promotion_unverified",
                _ => "stale",
            }),
            "{why}"
        );
    };
    advisory(
        Spec {
            support: "candidate",
            reviewer: None,
            ..o()
        },
        "candidate",
    );
    advisory(
        Spec {
            support: "unsupported",
            reviewer: None,
            ..o()
        },
        "unsupported",
    );
    advisory(
        Spec {
            kind: "user_supplied_spec",
            ..o()
        },
        "user_supplied",
    );
    advisory(
        Spec {
            kind: "documented_api",
            reviewer: None,
            ..o()
        },
        "unpromoted",
    );
    advisory(
        Spec {
            witness: "never-demonstrated",
            ..o()
        },
        "promotion_unverified",
    );
    advisory(
        Spec {
            kind: "documented_api",
            recorded: 1_000_000,
            ..o()
        },
        "stale",
    );
}

#[test]
fn forged_promotion_wrong_origin_app_principal_revoked_or_future_evidence_is_rejected() {
    let o = Spec::observed;
    let rej =
        |s: Spec, why: &'static str| assert_eq!(standing(&s), Standing::Rejected(why), "{why}");
    rej(
        Spec {
            reviewer: Some("prin_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
            ..o()
        },
        "promotion_unauthorized",
    );
    rej(
        Spec {
            reviewer: Some(OWNER),
            ..o()
        },
        "promotion_unauthorized",
    );
    rej(
        Spec {
            origin: "https://evil.example.com",
            ..o()
        },
        "wrong_origin",
    );
    rej(
        Spec {
            app: "acme:crm-us1",
            ..o()
        },
        "wrong_application",
    );
    rej(
        Spec {
            principal: "prin_cccccccccccccccccccccccccccccccc",
            ..o()
        },
        "wrong_principal",
    );
    rej(
        Spec {
            granted: 1_789_999_000_000,
            ..o()
        },
        "grant_not_held",
    );
    rej(
        Spec {
            scope: "observe.other",
            ..o()
        },
        "grant_not_held",
    );
    rej(
        Spec {
            recorded: NOW + 3_600_000,
            ..o()
        },
        "from_the_future",
    );
}

#[test]
fn revoked_grants_reviewers_documents_and_witnesses_end_authority() {
    let s = Spec::observed();
    let p = s.build();
    let mut pol = policy();
    pol.grants[0].revoked_at = Some(NOW - 1);
    assert_eq!(assess(&p, &pol), Standing::Rejected("grant_revoked"));
    let mut pol = policy();
    pol.revoked.principals.insert(REVIEWER.into());
    assert_eq!(assess(&p, &pol), Standing::Rejected("revoked_reviewer"));
    let mut pol = policy();
    pol.revoked.documents.insert(p.digest.clone());
    assert_eq!(assess(&p, &pol), Standing::Rejected("revoked_document"));
    let mut pol = policy();
    pol.revoked.witnesses.insert("profile-w1".into());
    assert_eq!(assess(&p, &pol), Standing::Advisory("promotion_unverified"));
    let mut pol = policy();
    pol.revoked.principals.insert(OWNER.into());
    assert_eq!(assess(&p, &pol), Standing::Rejected("revoked_principal"));
    let mut pol: ProtocolPolicy = policy();
    pol.grants.push(Grant {
        principal: OWNER.into(),
        scope: "x.y".into(),
        granted_at: 1,
        revoked_at: None,
    });
    assert_eq!(assess(&p, &pol), Standing::Authoritative);
}

#[test]
fn a_self_promoted_observation_is_rejected() {
    let mut pol = policy();
    pol.reviewers.insert(OWNER.into());
    let s = Spec {
        reviewer: Some(OWNER),
        ..Spec::observed()
    };
    assert_eq!(
        assess(&s.build(), &pol),
        Standing::Rejected("self_promotion")
    );
}

#[test]
fn malformed_documents_are_refused_whole() {
    let good = Spec::observed().json();
    for (name, bad) in [
        ("unknown top key", good.replacen("{\"schema\"", "{\"extra\":1,\"schema\"", 1)),
        ("unknown nested key", good.replace("\"required\":true", "\"required\":true,\"example\":\"x\"")),
        ("bad type", good.replace("\"recorded_at\":1790000000000", "\"recorded_at\":\"soon\"")),
        ("negative time", good.replace("\"recorded_at\":1790000000000", "\"recorded_at\":-1")),
        ("duplicate field", good.replace("\"request\":[{\"name\":\"first_name\",\"type\":\"string\",\"required\":true}]", "\"request\":[{\"name\":\"a\",\"type\":\"string\",\"required\":true},{\"name\":\"a\",\"type\":\"string\",\"required\":true}]")),
        ("array root", "[]".to_string()),
        ("non-canonical origin", good.replace("https://crm.example.com", "https://crm.example.com:443")),
        ("grant fields on public basis", good.replace("\"observed_traffic\"", "\"documented_api\"")),
    ] {
        assert!(Protocol::from_json(bad.as_bytes()).is_err(), "{name}");
    }
}

fn plain() -> (
    vsf_recognizer::obs::Observation,
    vsf_recognizer::engine::Recognition,
) {
    let obs = snapshot("plain-form");
    let rec = recognize(&obs, Rules::V2);
    (obs, rec)
}

#[test]
fn advisory_and_rejected_evidence_never_conflicts_and_never_changes_an_operation() {
    let (obs, rec) = plain();
    let clean = build_ledger(&obs, &rec, NOW);
    let baseline = bare_contract(&rec, &clean);
    let mut l = build_ledger(&obs, &rec, NOW);
    let conflicting = r#"{"name":"weekly_digest","type":"string","required":true},{"name":"phone","type":"string","required":true},{"name":"first_name","type":"integer","required":true}"#;
    for s in [
        Spec {
            support: "candidate",
            reviewer: None,
            request: conflicting,
            ..Spec::observed()
        },
        Spec {
            support: "unsupported",
            reviewer: None,
            request: conflicting,
            ..Spec::observed()
        },
        Spec {
            kind: "user_supplied_spec",
            request: conflicting,
            ..Spec::observed()
        },
        Spec {
            origin: "https://evil.example.com",
            request: conflicting,
            ..Spec::observed()
        },
    ] {
        let p = s.build();
        reconcile(&mut l, &obs, &rec, &p, assess(&p, &policy()), None, MONTH);
    }
    assert!(open_conflicts(&l).is_empty());
    assert!(
        l.claims
            .values()
            .filter(|c| c.predicate.starts_with("protocol:"))
            .all(|c| c.certainty != Certainty::Conflicted)
    );
    assert_eq!(l.validate(), vec![]);
    assert_eq!(bare_contract(&rec, &l), baseline);
}

#[test]
fn contradictory_authoritative_sources_both_survive_and_clash() {
    let (obs, rec) = plain();
    let mut l = build_ledger(&obs, &rec, NOW);
    let a = Spec {
        request: r#"{"name":"first_name","type":"string","required":true}"#,
        ..Spec::observed()
    }
    .build();
    let b = Spec {
        request: r#"{"name":"first_name","type":"integer","required":true}"#,
        ..Spec::observed()
    }
    .build();
    assert_eq!(a.id, b.id);
    assert_ne!(a.digest, b.digest);
    let docs = [(&a, Standing::Authoritative), (&b, Standing::Authoritative)];
    for (p, s) in &docs {
        reconcile(&mut l, &obs, &rec, p, *s, None, MONTH);
    }
    let added = reconcile_contradictions(&mut l, &obs, &docs);
    assert_eq!(added.len(), 1);
    assert_eq!(
        l.sources
            .values()
            .filter(|m| m.kind == SourceKind::Protocol)
            .count(),
        2
    );
    let readings = l
        .claims
        .values()
        .filter(|c| c.predicate == "protocol:field")
        .count();
    assert_eq!(readings, 2, "both documents' readings are kept");
    assert!(
        open_conflicts(&l)
            .iter()
            .any(|c| c.predicate == "protocol:contradiction")
    );
    assert_eq!(l.validate(), vec![]);
}

#[test]
fn a_ledger_bound_to_a_context_refuses_evidence_when_the_observation_origin_differs() {
    let (obs, rec) = plain();
    let ctx = trust();
    let mut l = build_ledger_in(&obs, &rec, NOW, Some(&ctx));
    let p = Spec::observed().build();
    let ids = reconcile(
        &mut l,
        &obs,
        &rec,
        &p,
        assess(&p, &policy()),
        Some(&ctx),
        MONTH,
    );
    assert_eq!(ids.len(), 1);
    assert_eq!(l.claims[&ids[0]].predicate, "protocol:rejected");
    assert!(
        l.claims[&ids[0]]
            .object
            .starts_with("observation_origin_mismatch")
    );
}
