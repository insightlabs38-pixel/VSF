//! Protocol evidence against UI evidence: disagreement is a conflicted claim until a decision closes it.

use crate::engine::Recognition;
use crate::index::tokens;
use crate::ledger::{Certainty, Claim, EvidenceRef, EvidenceType, Ledger, node_digest};
use crate::obs::Observation;
use crate::semantics::{Binding, Field, FieldKind, Strength};
use vsf_canon::{Value, sha256_hex};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ProtocolField {
    pub name: String,
    pub ty: String,
    pub required: bool,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Protocol {
    pub id: String,
    pub kind: String,
    pub support: String,
    pub recorded_at: i64,
    pub operation: String,
    pub request: Vec<ProtocolField>,
}

fn text<'a>(v: &'a Value, key: &str) -> Result<&'a str, String> {
    v.get(key)
        .and_then(Value::as_str)
        .ok_or_else(|| format!("protocol.missing:{key}"))
}

impl Protocol {
    pub fn from_json(bytes: &[u8]) -> Result<Protocol, String> {
        let v = vsf_canon::parse(bytes).map_err(|e| format!("protocol.parse:{}", e.code()))?;
        if text(&v, "schema")? != "vsf-protocol-evidence-v1" {
            return Err("protocol.bad_schema".into());
        }
        let id = text(&v, "id")?.to_string();
        let kind = text(&v, "kind")?.to_string();
        let support = text(&v, "support")?.to_string();
        let recorded_at = v
            .get("provenance")
            .and_then(|p| p.get("recorded_at"))
            .and_then(Value::as_int)
            .ok_or("protocol.missing:recorded_at")?;
        let ep = v.get("endpoint").ok_or("protocol.missing:endpoint")?;
        let operation = format!(
            "{} {}",
            ep.get("method").and_then(Value::as_str).unwrap_or(""),
            text(ep, "path_template")?
        )
        .trim()
        .to_string();
        let Some(Value::Array(items)) = v.get("shape").and_then(|s| s.get("request")) else {
            return Err("protocol.missing:shape.request".into());
        };
        let mut request = Vec::new();
        for it in items {
            request.push(ProtocolField {
                name: text(it, "name")?.to_string(),
                ty: text(it, "type")?.to_string(),
                required: matches!(it.get("required"), Some(Value::Bool(true))),
            });
        }
        Ok(Protocol {
            id,
            kind,
            support,
            recorded_at,
            operation,
            request,
        })
    }

    fn evidence_kind(&self) -> EvidenceType {
        match self.kind.as_str() {
            "observed_traffic" => EvidenceType::ProtocolTrace,
            "user_supplied_spec" => EvidenceType::UserAssertion,
            _ => EvidenceType::DocumentedSchema,
        }
    }

    fn evidence(&self) -> EvidenceRef {
        EvidenceRef {
            kind: self.evidence_kind(),
            source: self.id.clone(),
            nodes: Vec::new(),
            node_digests: Vec::new(),
        }
    }

    /// An unsupported source can raise a question but never a conflict.
    fn authoritative(&self) -> bool {
        self.support != "unsupported"
    }
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

fn ui_evidence(obs: &Observation, id: u32) -> EvidenceRef {
    let digests = obs.index_of(id).map(|i| node_digest(obs, i));
    EvidenceRef {
        kind: EvidenceType::ObservedStructure,
        source: obs.digest.clone(),
        nodes: vec![id],
        node_digests: digests.into_iter().collect(),
    }
}

fn label_of(f: &Field) -> Option<&str> {
    match &f.binding {
        Binding::Bound(c) => Some(&c.text),
        _ => None,
    }
}

/// Adds claims comparing one protocol document with the recognized UI; returns the ids it added.
pub fn reconcile(
    ledger: &mut Ledger,
    obs: &Observation,
    rec: &Recognition,
    p: &Protocol,
    max_age_ms: i64,
) -> Vec<String> {
    ledger.record(&p.id, p.recorded_at, 0);
    let root = obs.nodes.first().map_or(0, |n| n.id);
    let captured = ledger.sources.get(&obs.digest).map_or(0, |m| m.captured_at);
    let mut added = Vec::new();
    let weak = if p.authoritative() {
        Certainty::Conflicted
    } else {
        Certainty::Unknown
    };
    if p.recorded_at.saturating_add(max_age_ms) < captured {
        added.push(ledger.push(
            root,
            "protocol:stale",
            &format!("{}@{}", p.operation, p.id),
            Certainty::Unknown,
            None,
            vec![p.evidence()],
            None,
        ));
        return added;
    }
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
        let object = |tail: &str| format!("{}:{}{}@{}", pf.name, pf.ty, tail, p.id);
        let hit = if by_scope.len() == 1 {
            Some(by_scope[0])
        } else if by_label.len() == 1 && by_scope.is_empty() {
            Some(by_label[0])
        } else {
            None
        };
        if let Some(f) = hit {
            let evidence = vec![ui_evidence(obs, f.node), p.evidence()];
            match compatible(f.kind, &pf.ty) {
                Some(true) => added.push(ledger.push(
                    f.node,
                    "protocol:field",
                    &object(""),
                    Certainty::Inferred,
                    Some(Strength::Structural),
                    evidence,
                    None,
                )),
                Some(false) => added.push(ledger.push(
                    f.node,
                    "protocol:type",
                    &object(&format!("|ui={}", f.kind.as_str())),
                    weak,
                    None,
                    evidence,
                    None,
                )),
                None => added.push(ledger.push(
                    f.node,
                    "protocol:type",
                    &object("|uncomparable"),
                    Certainty::Unknown,
                    None,
                    evidence,
                    None,
                )),
            }
        } else if by_label.len() + by_scope.len() > 1 || by_label.len() > 1 {
            added.push(ledger.push(
                root,
                "protocol:field",
                &object("|ambiguous"),
                Certainty::Unknown,
                None,
                vec![p.evidence()],
                None,
            ));
        } else if pf.required {
            added.push(ledger.push(
                root,
                "protocol:required_missing",
                &object(""),
                weak,
                None,
                vec![ui_evidence(obs, root), p.evidence()],
                None,
            ));
        }
    }
    added
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
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Decision {
    pub claim: String,
    pub side: Side,
    pub decided_by: String,
    pub reason: String,
}

fn principal_ok(p: &str) -> bool {
    p.strip_prefix("prin_").is_some_and(|h| {
        h.len() == 32
            && h.bytes()
                .all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(&b))
    })
}

/// Records a decision; refuses anything that is not an identified person giving a reason about a real, open conflict.
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
        "dec_{}",
        &sha256_hex(format!("{}|{}|{}", d.claim, d.side.as_str(), d.decided_by).as_bytes())[..32]
    );
    ledger.record(&decision_id, 0, 0);
    let ev = EvidenceRef {
        kind: EvidenceType::UserAssertion,
        source: decision_id,
        nodes: Vec::new(),
        node_digests: Vec::new(),
    };
    let id = ledger.push(
        subject,
        "protocol:resolution",
        &format!("{}:{}", d.side.as_str(), d.claim),
        Certainty::Inferred,
        None,
        vec![ev],
        None,
    );
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
