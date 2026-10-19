//! Protocol evidence against UI evidence: disagreement is a conflicted claim until a decision closes it.

use crate::engine::Recognition;
use crate::index::tokens;
use crate::ledger::{
    Certainty, Claim, EvidenceRef, EvidenceType, Ledger, NewClaim, SourceKind, SourceMeta,
    evidence, observation_source,
};
use crate::obs::Observation;
use crate::protocol::contradictions;
pub use crate::protocol::{Protocol, ProtocolField, Standing};
use crate::semantics::{Binding, Field, FieldKind, Strength};
use crate::trust::{TrustContext, principal_ok};
use vsf_canon::sha256_hex;

const PRODUCER: &str = "reconcile.v2";

fn protocol_evidence(p: &Protocol) -> EvidenceRef {
    let kind = match p.kind.as_str() {
        "observed_traffic" => EvidenceType::ProtocolTrace,
        "user_supplied_spec" => EvidenceType::UserAssertion,
        _ => EvidenceType::DocumentedSchema,
    };
    EvidenceRef::new(kind, &p.digest)
}

fn record_protocol(ledger: &mut Ledger, p: &Protocol) {
    let _ = ledger.record(
        &p.digest,
        SourceMeta {
            kind: SourceKind::Protocol,
            label: p.id.clone(),
            captured_at: p.recorded_at,
            nodes: 0,
            context: None,
        },
    );
}

fn name_tokens(name: &str) -> Vec<String> {
    let mut spaced = String::new();
    let mut prev_lower = false;
    for c in name.chars() {
        if c.is_uppercase() && prev_lower {
            spaced.push(' ');
        }
        prev_lower = c.is_lowercase() || c.is_ascii_digit();
        spaced.push(c);
    }
    tokens(&spaced).collect()
}

fn compatible(kind: FieldKind, ty: &str) -> Option<bool> {
    let ok: &[&str] = match kind {
        FieldKind::Toggle => &["boolean"],
        FieldKind::Range => &["integer", "number"],
        FieldKind::Text => &["string", "integer", "number"],
        FieldKind::Choice => &["string", "integer", "number", "boolean"],
    };
    match ty {
        "string" | "integer" | "number" | "boolean" => Some(ok.contains(&ty)),
        _ => None,
    }
}

fn label_of(f: &Field) -> Option<&str> {
    match &f.binding {
        Binding::Bound(c) => Some(&c.text),
        _ => None,
    }
}

/// Compares one protocol document with the UI; only an authoritative standing can conflict.
pub fn reconcile(
    ledger: &mut Ledger,
    obs: &Observation,
    rec: &Recognition,
    p: &Protocol,
    standing: Standing,
    ctx: Option<&TrustContext>,
    max_age_ms: i64,
) -> Vec<String> {
    record_protocol(ledger, p);
    let root = obs.nodes.first().map_or(0, |n| n.id);
    let source = observation_source(obs, ctx);
    let captured = ledger.sources.get(&source).map_or(0, |m| m.captured_at);
    let mut added = Vec::new();
    let mut push = |ledger: &mut Ledger,
                    subject: u32,
                    predicate: &str,
                    object: String,
                    certainty: Certainty,
                    confidence: Option<Strength>,
                    ev: Vec<EvidenceRef>| {
        added.push(ledger.push(NewClaim {
            subject,
            predicate: predicate.to_string(),
            object,
            certainty,
            confidence,
            evidence: ev,
            unit: None,
            producer: PRODUCER.to_string(),
        }));
    };
    let standing = match (ctx, obs.origin()) {
        (Some(c), Some(o)) if c.origin != o => Standing::Rejected("observation_origin_mismatch"),
        _ => standing,
    };
    if let Standing::Rejected(why) = standing {
        push(
            ledger,
            root,
            "protocol:rejected",
            format!("{why}@{}", p.id),
            Certainty::Unknown,
            None,
            vec![protocol_evidence(p)],
        );
        return added;
    }
    if p.recorded_at.saturating_add(max_age_ms) < captured {
        push(
            ledger,
            root,
            "protocol:stale",
            format!("{}@{}", p.operation, p.id),
            Certainty::Unknown,
            None,
            vec![protocol_evidence(p)],
        );
        return added;
    }
    let authoritative = standing == Standing::Authoritative;
    let weak = if authoritative {
        Certainty::Conflicted
    } else {
        Certainty::Unknown
    };
    let note = standing
        .reason()
        .map_or(String::new(), |r| format!("|advisory:{r}"));
    let ui = |id: u32| evidence(obs, &source, EvidenceType::ObservedStructure, &[id]);
    let fields: Vec<&Field> = rec
        .fields()
        .filter(|f| f.why_unusable != Some("credential_field"))
        .collect();
    for pf in &p.request {
        let want = name_tokens(&pf.name);
        let by_label: Vec<&&Field> = fields
            .iter()
            .filter(|f| label_of(f).is_some_and(|l| tokens(l).collect::<Vec<_>>() == want))
            .collect();
        let by_scope: Vec<&&Field> = fields
            .iter()
            .filter(|f| {
                let (Some(l), Some(sc)) = (label_of(f), f.scope.as_deref()) else {
                    return false;
                };
                tokens(sc).chain(tokens(l)).collect::<Vec<_>>() == want
            })
            .collect();
        let object = |tail: &str| format!("{}:{}{}{}@{}", pf.name, pf.ty, tail, note, p.id);
        let hit = if by_scope.len() == 1 {
            Some(by_scope[0])
        } else if by_label.len() == 1 && by_scope.is_empty() {
            Some(by_label[0])
        } else {
            None
        };
        if let Some(f) = hit {
            let ev = vec![ui(f.node), protocol_evidence(p)];
            match compatible(f.kind, &pf.ty) {
                Some(true) if authoritative => push(
                    ledger,
                    f.node,
                    "protocol:field",
                    object(""),
                    Certainty::Inferred,
                    Some(Strength::Structural),
                    ev,
                ),
                Some(true) => push(
                    ledger,
                    f.node,
                    "protocol:field",
                    object(""),
                    Certainty::Unknown,
                    None,
                    ev,
                ),
                Some(false) => push(
                    ledger,
                    f.node,
                    "protocol:type",
                    object(&format!("|ui={}", f.kind.as_str())),
                    weak,
                    None,
                    ev,
                ),
                None => push(
                    ledger,
                    f.node,
                    "protocol:type",
                    object("|uncomparable"),
                    Certainty::Unknown,
                    None,
                    ev,
                ),
            }
        } else if by_label.len() + by_scope.len() > 1 || by_label.len() > 1 {
            push(
                ledger,
                root,
                "protocol:field",
                object("|ambiguous"),
                Certainty::Unknown,
                None,
                vec![protocol_evidence(p)],
            );
        } else if pf.required {
            push(
                ledger,
                root,
                "protocol:required_missing",
                object(""),
                weak,
                None,
                vec![ui(root), protocol_evidence(p)],
            );
        }
    }
    added
}

/// Authoritative documents that describe one operation differently each keep their claims; the clash is a conflict.
pub fn reconcile_contradictions(
    ledger: &mut Ledger,
    obs: &Observation,
    docs: &[(&Protocol, Standing)],
) -> Vec<String> {
    let root = obs.nodes.first().map_or(0, |n| n.id);
    let mut out = Vec::new();
    for (a, b, op) in contradictions(docs) {
        record_protocol(ledger, a);
        record_protocol(ledger, b);
        out.push(ledger.push(NewClaim {
            subject: root,
            predicate: "protocol:contradiction".into(),
            object: format!("{op}|{}|{}", a.id, b.id),
            certainty: Certainty::Conflicted,
            confidence: None,
            evidence: vec![protocol_evidence(a), protocol_evidence(b)],
            unit: None,
            producer: PRODUCER.to_string(),
        }));
    }
    out
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Side {
    Ui,
    Protocol,
}

impl Side {
    pub fn as_str(self) -> &'static str {
        match self {
            Side::Ui => "ui",
            Side::Protocol => "protocol",
        }
    }

    pub fn parse(x: &str) -> Option<Side> {
        [Side::Ui, Side::Protocol]
            .into_iter()
            .find(|s| s.as_str() == x)
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Decision {
    pub claim: String,
    pub side: Side,
    pub decided_by: String,
    pub reason: String,
}

/// Records a person's reasoned decision on an open conflict; it is never a behavioural witness.
pub fn resolve(ledger: &mut Ledger, d: Decision) -> Result<String, &'static str> {
    let Some(c) = ledger.claims.get(&d.claim) else {
        return Err("unknown_claim");
    };
    if c.certainty != Certainty::Conflicted {
        return Err("not_conflicted");
    }
    if ledger.resolutions.contains_key(&d.claim) {
        return Err("already_resolved");
    }
    if !principal_ok(&d.decided_by) {
        return Err("bad_principal");
    }
    if d.reason.trim().is_empty() || d.reason.chars().count() > 200 {
        return Err("bad_reason");
    }
    let subject = c.subject;
    let decision_id = format!(
        "sha256:{}",
        sha256_hex(
            format!(
                "{}|{}|{}|{}",
                d.claim,
                d.side.as_str(),
                d.decided_by,
                d.reason
            )
            .as_bytes()
        )
    );
    ledger
        .record(
            &decision_id,
            SourceMeta {
                kind: SourceKind::Decision,
                label: format!("dec_{}", &decision_id[7..39]),
                captured_at: 0,
                nodes: 0,
                context: None,
            },
        )
        .map_err(|_| "source_conflict")?;
    let id = ledger.push(NewClaim {
        subject,
        predicate: "protocol:resolution".into(),
        object: format!("{}:{}", d.side.as_str(), d.claim),
        certainty: Certainty::Inferred,
        confidence: None,
        evidence: vec![EvidenceRef::new(EvidenceType::UserAssertion, &decision_id)],
        unit: None,
        producer: PRODUCER.to_string(),
    });
    ledger.resolutions.insert(d.claim.clone(), d);
    Ok(id)
}

pub fn open_conflicts(ledger: &Ledger) -> Vec<&Claim> {
    ledger
        .claims
        .values()
        .filter(|c| c.certainty == Certainty::Conflicted && !ledger.resolutions.contains_key(&c.id))
        .collect()
}
