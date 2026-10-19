//! Evidence ledger: every semantic claim carries the immutable observation, the nodes and the kind of evidence behind it.

use crate::engine::{Output, Recognition, Unit, UnitKey};
use crate::obs::{Node, Observation};
use crate::semantics::{Binding, Field, Strength};
use crate::val::{count, list, n, obj, s};
use std::collections::BTreeMap;
use vsf_canon::{Value, sha256_hex, to_canonical};

#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord)]
pub enum EvidenceType {
    ObservedStructure,
    RuntimeBehavior,
    DocumentedSchema,
    ProtocolTrace,
    SourceAccessibility,
    UserAssertion,
    HeuristicInference,
}

impl EvidenceType {
    pub fn as_str(self) -> &'static str {
        match self {
            EvidenceType::ObservedStructure => "observed_structure",
            EvidenceType::RuntimeBehavior => "runtime_behavior",
            EvidenceType::DocumentedSchema => "documented_schema",
            EvidenceType::ProtocolTrace => "protocol_trace",
            EvidenceType::SourceAccessibility => "source_accessibility",
            EvidenceType::UserAssertion => "user_assertion",
            EvidenceType::HeuristicInference => "heuristic_inference",
        }
    }
}

/// `Verified` needs behavioural witnesses and is deliberately not producible by recognition.
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord)]
pub enum Certainty {
    Inferred,
    Unknown,
    Conflicted,
}

impl Certainty {
    pub fn as_str(self) -> &'static str {
        match self {
            Certainty::Inferred => "inferred",
            Certainty::Unknown => "unknown",
            Certainty::Conflicted => "conflicted",
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct EvidenceRef {
    pub kind: EvidenceType,
    /// Digest of the observation the evidence was read from, or of the contract document.
    pub source: String,
    pub nodes: Vec<u32>,
    pub node_digests: Vec<String>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Claim {
    pub id: String,
    pub subject: u32,
    pub predicate: String,
    pub object: String,
    pub certainty: Certainty,
    pub confidence: Option<Strength>,
    pub evidence: Vec<EvidenceRef>,
    pub unit: Option<UnitKey>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SourceMeta {
    pub captured_at: i64,
    pub nodes: usize,
}

#[derive(Debug, Clone, Default)]
pub struct Ledger {
    /// Every observation (or contract) any claim cites; entries are never rewritten.
    pub sources: BTreeMap<String, SourceMeta>,
    pub claims: BTreeMap<String, Claim>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct LedgerError {
    pub claim: String,
    pub problem: &'static str,
}

/// Content digest of a node and its immediate structure; changes whenever anything a claim could rely on changes.
pub fn node_digest(obs: &Observation, i: usize) -> String {
    let nd: &Node = &obs.nodes[i];
    let v = obj(vec![
        (
            "children",
            list(
                obs.children[i]
                    .iter()
                    .map(|&c| n(i64::from(obs.nodes[c].id)))
                    .collect(),
            ),
        ),
        ("flags", n(i64::from(nd.flags))),
        ("name", nd.name.as_deref().map_or(Value::Null, s)),
        (
            "parent",
            nd.parent.map_or(n(-1), |p| n(i64::from(obs.nodes[p].id))),
        ),
        (
            "rels",
            list(
                nd.rels
                    .iter()
                    .map(|(k, t)| list(vec![s(&format!("{k:?}")), n(i64::from(obs.nodes[*t].id))]))
                    .collect(),
            ),
        ),
        ("role", nd.role.as_deref().map_or(Value::Null, s)),
        ("tag", s(&nd.tag)),
        ("text", nd.text.as_deref().map_or(Value::Null, s)),
    ]);
    sha256_hex(&to_canonical(&v))
}

fn evidence(obs: &Observation, kind: EvidenceType, ids: &[u32]) -> EvidenceRef {
    let mut nodes: Vec<u32> = ids.to_vec();
    nodes.sort_unstable();
    nodes.dedup();
    let node_digests = nodes
        .iter()
        .filter_map(|&id| obs.index_of(id))
        .map(|i| node_digest(obs, i))
        .collect();
    EvidenceRef {
        kind,
        source: obs.digest.clone(),
        nodes,
        node_digests,
    }
}

fn claim_id(
    subject: u32,
    predicate: &str,
    object: &str,
    certainty: Certainty,
    ev: &[EvidenceRef],
) -> String {
    let v = obj(vec![
        ("certainty", s(certainty.as_str())),
        (
            "evidence",
            list(
                ev.iter()
                    .map(|e| {
                        obj(vec![
                            (
                                "digests",
                                list(e.node_digests.iter().map(|d| s(d)).collect()),
                            ),
                            ("kind", s(e.kind.as_str())),
                            (
                                "nodes",
                                list(e.nodes.iter().map(|&x| n(i64::from(x))).collect()),
                            ),
                        ])
                    })
                    .collect(),
            ),
        ),
        ("object", s(object)),
        ("predicate", s(predicate)),
        ("subject", n(i64::from(subject))),
    ]);
    format!("clm_{}", &sha256_hex(&to_canonical(&v))[..32])
}

impl Ledger {
    pub fn record(&mut self, source: &str, captured_at: i64, nodes: usize) {
        self.sources
            .entry(source.to_string())
            .or_insert(SourceMeta { captured_at, nodes });
    }

    #[allow(clippy::too_many_arguments)]
    pub fn push(
        &mut self,
        subject: u32,
        predicate: &str,
        object: &str,
        certainty: Certainty,
        confidence: Option<Strength>,
        evidence: Vec<EvidenceRef>,
        unit: Option<UnitKey>,
    ) -> String {
        let id = claim_id(subject, predicate, object, certainty, &evidence);
        self.claims.entry(id.clone()).or_insert(Claim {
            id: id.clone(),
            subject,
            predicate: predicate.to_string(),
            object: object.to_string(),
            certainty,
            confidence,
            evidence,
            unit,
        });
        id
    }

    pub fn by_subject(&self, subject: u32) -> impl Iterator<Item = &Claim> {
        self.claims.values().filter(move |c| c.subject == subject)
    }

    pub fn find(&self, subject: u32, predicate: &str) -> Vec<&Claim> {
        self.by_subject(subject)
            .filter(|c| c.predicate == predicate)
            .collect()
    }

    /// Structural invariants: nothing unbacked, nothing citing an unrecorded source, no certainty beyond its evidence.
    pub fn validate(&self) -> Vec<LedgerError> {
        let mut out = Vec::new();
        for c in self.claims.values() {
            let mut bad = |problem| {
                out.push(LedgerError {
                    claim: c.id.clone(),
                    problem,
                })
            };
            if c.evidence.is_empty() {
                bad("no_evidence");
            }
            if c.evidence
                .iter()
                .any(|e| !self.sources.contains_key(&e.source))
            {
                bad("unrecorded_source");
            }
            if c.evidence
                .iter()
                .any(|e| e.nodes.len() != e.node_digests.len())
                && c.evidence
                    .iter()
                    .all(|e| e.kind == EvidenceType::ObservedStructure)
            {
                bad("evidence_not_resolvable");
            }
            if c.certainty == Certainty::Inferred
                && c.evidence
                    .iter()
                    .all(|e| e.kind == EvidenceType::HeuristicInference)
                && c.confidence != Some(Strength::Lexical)
            {
                bad("heuristic_only_not_lexical");
            }
            if c.id != claim_id(c.subject, &c.predicate, &c.object, c.certainty, &c.evidence) {
                bad("id_mismatch");
            }
        }
        out
    }

    /// Claims whose evidence no longer matches `current`: the node changed or vanished.
    pub fn stale_against(&self, current: &Observation) -> Vec<&Claim> {
        self.claims
            .values()
            .filter(|c| {
                c.evidence.iter().any(|e| {
                    e.kind == EvidenceType::ObservedStructure
                        && e.nodes.iter().zip(&e.node_digests).any(|(&id, d)| {
                            current
                                .index_of(id)
                                .map(|i| node_digest(current, i))
                                .as_ref()
                                != Some(d)
                        })
                })
            })
            .collect()
    }

    pub fn to_value(&self) -> Value {
        let sources = self
            .sources
            .iter()
            .map(|(d, m)| {
                obj(vec![
                    ("captured_at", n(m.captured_at)),
                    ("digest", s(d)),
                    ("nodes", count(m.nodes)),
                ])
            })
            .collect();
        let claims = self
            .claims
            .values()
            .map(|c| {
                obj(vec![
                    ("certainty", s(c.certainty.as_str())),
                    (
                        "confidence",
                        c.confidence.map_or(Value::Null, |x| s(x.as_str())),
                    ),
                    (
                        "evidence",
                        list(
                            c.evidence
                                .iter()
                                .map(|e| {
                                    obj(vec![
                                        ("kind", s(e.kind.as_str())),
                                        (
                                            "nodes",
                                            list(
                                                e.nodes.iter().map(|&x| n(i64::from(x))).collect(),
                                            ),
                                        ),
                                        ("source", s(&e.source)),
                                    ])
                                })
                                .collect(),
                        ),
                    ),
                    ("id", s(&c.id)),
                    ("object", s(&c.object)),
                    ("predicate", s(&c.predicate)),
                    ("subject", n(i64::from(c.subject))),
                ])
            })
            .collect();
        obj(vec![
            ("claims", list(claims)),
            ("schema", s("vsf-evidence-ledger-v1")),
            ("sources", list(sources)),
        ])
    }
}

fn binding_claims(l: &mut Ledger, obs: &Observation, f: &Field, key: UnitKey) {
    let subject = f.node;
    match &f.binding {
        Binding::Bound(c) => {
            let mut ids = vec![subject];
            ids.extend(&c.from);
            let kind = EvidenceType::ObservedStructure;
            l.push(
                subject,
                "label",
                &c.text,
                Certainty::Inferred,
                Some(c.source.strength()),
                vec![evidence(obs, kind, &ids)],
                Some(key),
            );
            l.push(
                subject,
                "label_source",
                c.source.as_str(),
                Certainty::Inferred,
                Some(c.source.strength()),
                vec![evidence(obs, kind, &ids)],
                Some(key),
            );
        }
        Binding::Ambiguous(cands) => {
            let mut ids = vec![subject];
            for c in cands {
                ids.extend(&c.from);
            }
            let mut texts: Vec<&str> = cands.iter().map(|c| c.text.as_str()).collect();
            texts.sort_unstable();
            l.push(
                subject,
                "label",
                &format!("ambiguous:{}", texts.join("|")),
                Certainty::Unknown,
                None,
                vec![evidence(obs, EvidenceType::ObservedStructure, &ids)],
                Some(key),
            );
        }
        Binding::Unbound => {
            l.push(
                subject,
                "label",
                "none",
                Certainty::Unknown,
                None,
                vec![evidence(obs, EvidenceType::ObservedStructure, &[subject])],
                Some(key),
            );
        }
    }
}

pub fn claims_for_unit(l: &mut Ledger, obs: &Observation, u: &Unit) {
    let key = u.key;
    match &u.output {
        Output::Field(f) => {
            let own = evidence(obs, EvidenceType::ObservedStructure, &[f.node]);
            l.push(
                f.node,
                "is_field",
                f.kind.as_str(),
                Certainty::Inferred,
                Some(Strength::Structural),
                vec![own.clone()],
                Some(key),
            );
            binding_claims(l, obs, f, key);
            if let Some(sc) = &f.scope {
                l.push(
                    f.node,
                    "scope",
                    sc,
                    Certainty::Inferred,
                    Some(Strength::Structural),
                    vec![own.clone()],
                    Some(key),
                );
            }
            let usable = if f.usable {
                "true".to_string()
            } else {
                format!("false:{}", f.why_unusable.unwrap_or("unknown"))
            };
            l.push(
                f.node,
                "usable",
                &usable,
                Certainty::Inferred,
                Some(Strength::Explicit),
                vec![own],
                Some(key),
            );
        }
        Output::Action(a) => {
            let own = evidence(obs, EvidenceType::ObservedStructure, &[a.node]);
            l.push(
                a.node,
                "is_action",
                &a.roles.join("|"),
                Certainty::Inferred,
                Some(Strength::Structural),
                vec![own.clone()],
                Some(key),
            );
            if a.verbs.is_empty() {
                l.push(
                    a.node,
                    "verb",
                    "none",
                    Certainty::Unknown,
                    None,
                    vec![own.clone()],
                    Some(key),
                );
            }
            for v in &a.verbs {
                let lex = EvidenceRef {
                    kind: EvidenceType::HeuristicInference,
                    ..own.clone()
                };
                l.push(
                    a.node,
                    "verb",
                    v.as_str(),
                    Certainty::Inferred,
                    Some(Strength::Lexical),
                    vec![own.clone(), lex],
                    Some(key),
                );
            }
            let usable = if a.usable {
                "true".to_string()
            } else {
                format!("false:{}", a.why_unusable.unwrap_or("unknown"))
            };
            l.push(
                a.node,
                "usable",
                &usable,
                Certainty::Inferred,
                Some(Strength::Explicit),
                vec![own],
                Some(key),
            );
        }
        Output::Collection(c) => {
            let own = evidence(obs, EvidenceType::ObservedStructure, &[c.node]);
            l.push(
                c.node,
                "is_collection",
                &c.rows.to_string(),
                Certainty::Inferred,
                Some(Strength::Structural),
                vec![own.clone()],
                Some(key),
            );
            let cols: Vec<String> = c
                .columns
                .iter()
                .map(|x| x.clone().unwrap_or_default())
                .collect();
            l.push(
                c.node,
                "columns",
                &cols.join("|"),
                Certainty::Inferred,
                Some(Strength::Structural),
                vec![own],
                Some(key),
            );
        }
    }
}

/// Builds the ledger for a whole recognition run.
pub fn build_ledger(obs: &Observation, rec: &Recognition, captured_at: i64) -> Ledger {
    let mut l = Ledger::default();
    l.record(&obs.digest, captured_at, obs.len());
    for u in rec.units.values() {
        claims_for_unit(&mut l, obs, u);
    }
    l
}
