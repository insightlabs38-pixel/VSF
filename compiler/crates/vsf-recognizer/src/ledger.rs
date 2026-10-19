//! Evidence ledger: every claim is addressed by what it says, who produced it and exactly which sources it cites.

use crate::candidates::{Group, Resolution};
use crate::engine::{Output, Recognition, Unit, UnitKey, UnitKind};
use crate::obs::{F_ACCOUNT, F_CREDENTIAL, Node, Observation};
use crate::semantics::{Binding, Field, Rules, Strength};
use crate::trust::TrustContext;
use crate::val::{count, list, n, obj, s};
use std::collections::BTreeMap;
use vsf_canon::{Value, sha256_hex, to_canonical};

pub const LEDGER_SCHEMA: &str = "vsf-evidence-ledger-v2";
const CLAIM_DOMAIN: &str = "vsf.claim.v2";
pub const WITHHELD: &str = "[withheld]";

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

    pub fn parse(x: &str) -> Option<EvidenceType> {
        [
            EvidenceType::ObservedStructure,
            EvidenceType::RuntimeBehavior,
            EvidenceType::DocumentedSchema,
            EvidenceType::ProtocolTrace,
            EvidenceType::SourceAccessibility,
            EvidenceType::UserAssertion,
            EvidenceType::HeuristicInference,
        ]
        .into_iter()
        .find(|e| e.as_str() == x)
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

    pub fn parse(x: &str) -> Option<Certainty> {
        [
            Certainty::Inferred,
            Certainty::Unknown,
            Certainty::Conflicted,
        ]
        .into_iter()
        .find(|c| c.as_str() == x)
    }
}

/// What kind of thing a recorded source is; evidence kinds are only valid against matching sources.
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord)]
pub enum SourceKind {
    Observation,
    Protocol,
    Decision,
    Witness,
}

impl SourceKind {
    pub fn as_str(self) -> &'static str {
        match self {
            SourceKind::Observation => "observation",
            SourceKind::Protocol => "protocol",
            SourceKind::Decision => "decision",
            SourceKind::Witness => "witness",
        }
    }

    pub fn parse(x: &str) -> Option<SourceKind> {
        [
            SourceKind::Observation,
            SourceKind::Protocol,
            SourceKind::Decision,
            SourceKind::Witness,
        ]
        .into_iter()
        .find(|k| k.as_str() == x)
    }

    fn admits(self, e: EvidenceType) -> bool {
        match self {
            SourceKind::Observation => matches!(
                e,
                EvidenceType::ObservedStructure
                    | EvidenceType::HeuristicInference
                    | EvidenceType::SourceAccessibility
            ),
            SourceKind::Protocol => matches!(
                e,
                EvidenceType::ProtocolTrace
                    | EvidenceType::DocumentedSchema
                    | EvidenceType::UserAssertion
            ),
            SourceKind::Decision => e == EvidenceType::UserAssertion,
            SourceKind::Witness => e == EvidenceType::RuntimeBehavior,
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct EvidenceRef {
    pub kind: EvidenceType,
    /// Id of the recorded source: an observation under its context, a protocol document by content, a decision or a witness.
    pub source: String,
    pub nodes: Vec<u32>,
    pub node_digests: Vec<String>,
    /// True when a cited node is account-specific or a credential, so the claim must not leave the device.
    pub sensitive: bool,
}

impl EvidenceRef {
    pub fn new(kind: EvidenceType, source: &str) -> EvidenceRef {
        EvidenceRef {
            kind,
            source: source.to_string(),
            nodes: Vec::new(),
            node_digests: Vec::new(),
            sensitive: false,
        }
    }
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
    /// The component and rules version that made the claim.
    pub producer: String,
}

impl Claim {
    pub fn sensitive(&self) -> bool {
        self.evidence.iter().any(|e| e.sensitive)
    }
}

/// A claim before it has an identity.
#[derive(Debug, Clone)]
pub struct NewClaim {
    pub subject: u32,
    pub predicate: String,
    pub object: String,
    pub certainty: Certainty,
    pub confidence: Option<Strength>,
    pub evidence: Vec<EvidenceRef>,
    pub unit: Option<UnitKey>,
    pub producer: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SourceMeta {
    pub kind: SourceKind,
    /// Human-readable pointer: a protocol document id, a decision id; empty for observations.
    pub label: String,
    pub captured_at: i64,
    pub nodes: usize,
    /// Digest of the trust context the source was gathered under, when there is one.
    pub context: Option<String>,
}

#[derive(Debug, Clone, Default)]
pub struct Ledger {
    /// Every source any claim cites; an entry is never rewritten, and a clashing re-record is refused.
    pub sources: BTreeMap<String, SourceMeta>,
    pub claims: BTreeMap<String, Claim>,
    /// Explicit human decisions on conflicted claims, by claim id. A conflict is never closed any other way.
    pub resolutions: BTreeMap<String, crate::reconcile::Decision>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct LedgerError {
    pub claim: String,
    pub problem: &'static str,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Export {
    /// Everything, for the user's own device.
    Local,
    /// Structure only: sensitive claims and anything carrying page text are withheld, node digests omitted.
    Shared,
}

/// Predicates whose object is text copied from the page.
fn carries_page_text(predicate: &str) -> bool {
    matches!(predicate, "label" | "scope" | "columns") || predicate.starts_with("candidates:")
}

pub fn producer_for(rules: Rules) -> String {
    format!(
        "recognizer.rules-{}",
        if rules.delegation { "v2" } else { "v1" }
    )
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

/// Id of an observation as a ledger source: the content digest, bound to its trust context when there is one.
pub fn observation_source(obs: &Observation, ctx: Option<&TrustContext>) -> String {
    match ctx {
        None => obs.digest.clone(),
        Some(c) => {
            let v = obj(vec![
                ("context", s(&c.digest())),
                ("domain", s("vsf.source.observation.v1")),
                ("observation", s(&obs.digest)),
            ]);
            format!("sha256:{}", sha256_hex(&to_canonical(&v)))
        }
    }
}

pub fn evidence(obs: &Observation, source: &str, kind: EvidenceType, ids: &[u32]) -> EvidenceRef {
    let mut nodes: Vec<u32> = ids.to_vec();
    nodes.sort_unstable();
    nodes.dedup();
    nodes.retain(|&id| obs.index_of(id).is_some());
    let node_digests = nodes
        .iter()
        .filter_map(|&id| obs.index_of(id))
        .map(|i| node_digest(obs, i))
        .collect();
    let sensitive = nodes
        .iter()
        .filter_map(|&id| obs.index_of(id))
        .any(|i| obs.nodes[i].flags & (F_ACCOUNT | F_CREDENTIAL) != 0);
    EvidenceRef {
        kind,
        source: source.to_string(),
        nodes,
        node_digests,
        sensitive,
    }
}

fn normalize(mut ev: Vec<EvidenceRef>) -> Vec<EvidenceRef> {
    ev.sort_by(|a, b| {
        (a.kind, &a.source, &a.nodes, &a.node_digests).cmp(&(
            b.kind,
            &b.source,
            &b.nodes,
            &b.node_digests,
        ))
    });
    ev.dedup();
    ev
}

fn unit_value(u: Option<UnitKey>) -> Value {
    u.map_or(Value::Null, |(k, id)| {
        list(vec![s(k.as_str()), n(i64::from(id))])
    })
}

fn evidence_value(e: &EvidenceRef, with_digests: bool) -> Value {
    let mut entries = vec![
        ("kind", s(e.kind.as_str())),
        (
            "nodes",
            list(e.nodes.iter().map(|&x| n(i64::from(x))).collect()),
        ),
        ("sensitive", Value::Bool(e.sensitive)),
        ("source", s(&e.source)),
    ];
    if with_digests {
        entries.push((
            "digests",
            list(e.node_digests.iter().map(|d| s(d)).collect()),
        ));
    }
    obj(entries)
}

/// Identity covers everything a claim asserts and everything it rests on, so no field can change unnoticed.
pub fn claim_id(c: &NewClaim) -> String {
    let v = obj(vec![
        ("certainty", s(c.certainty.as_str())),
        (
            "confidence",
            c.confidence.map_or(Value::Null, |x| s(x.as_str())),
        ),
        ("domain", s(CLAIM_DOMAIN)),
        (
            "evidence",
            list(c.evidence.iter().map(|e| evidence_value(e, true)).collect()),
        ),
        ("object", s(&c.object)),
        ("predicate", s(&c.predicate)),
        ("producer", s(&c.producer)),
        ("subject", n(i64::from(c.subject))),
        ("unit", unit_value(c.unit)),
    ]);
    format!("clm_{}", &sha256_hex(&to_canonical(&v))[..32])
}

fn id_of(c: &Claim) -> String {
    claim_id(&NewClaim {
        subject: c.subject,
        predicate: c.predicate.clone(),
        object: c.object.clone(),
        certainty: c.certainty,
        confidence: c.confidence,
        evidence: c.evidence.clone(),
        unit: c.unit,
        producer: c.producer.clone(),
    })
}

impl Ledger {
    /// Records a source. Re-recording identical metadata is a no-op; anything different under the same id is refused.
    pub fn record(&mut self, source: &str, meta: SourceMeta) -> Result<(), &'static str> {
        match self.sources.get(source) {
            Some(old) if *old == meta => Ok(()),
            Some(_) => Err("source_metadata_conflict"),
            None => {
                self.sources.insert(source.to_string(), meta);
                Ok(())
            }
        }
    }

    pub fn record_observation(
        &mut self,
        obs: &Observation,
        captured_at: i64,
        ctx: Option<&TrustContext>,
    ) -> Result<String, &'static str> {
        let id = observation_source(obs, ctx);
        self.record(
            &id,
            SourceMeta {
                kind: SourceKind::Observation,
                label: String::new(),
                captured_at,
                nodes: obs.len(),
                context: ctx.map(TrustContext::digest),
            },
        )?;
        Ok(id)
    }

    pub fn push(&mut self, mut c: NewClaim) -> String {
        c.evidence = normalize(c.evidence);
        let id = claim_id(&c);
        self.claims.entry(id.clone()).or_insert(Claim {
            id: id.clone(),
            subject: c.subject,
            predicate: c.predicate,
            object: c.object,
            certainty: c.certainty,
            confidence: c.confidence,
            evidence: c.evidence,
            unit: c.unit,
            producer: c.producer,
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

    /// Structural invariants: nothing unbacked, nothing citing an unrecorded or mismatched source, no certainty beyond its evidence.
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
            if c.producer.is_empty() {
                bad("no_producer");
            }
            if c.evidence
                .iter()
                .any(|e| !self.sources.contains_key(&e.source))
            {
                bad("unrecorded_source");
            }
            if c.evidence.iter().any(|e| {
                self.sources
                    .get(&e.source)
                    .is_some_and(|m| !m.kind.admits(e.kind))
            }) {
                bad("source_kind_mismatch");
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
            if c.id != id_of(c) {
                bad("id_mismatch");
            }
        }
        for (claim, d) in &self.resolutions {
            let mut bad = |problem| {
                out.push(LedgerError {
                    claim: claim.clone(),
                    problem,
                })
            };
            if d.claim != *claim {
                bad("resolution_key_mismatch");
            }
            match self.claims.get(claim) {
                Some(c) if c.certainty == Certainty::Conflicted => {}
                _ => bad("resolution_without_conflict"),
            }
            let marker = format!("{}:{}", d.side.as_str(), claim);
            if !self
                .claims
                .values()
                .any(|c| c.predicate == "protocol:resolution" && c.object == marker)
            {
                bad("resolution_unrecorded");
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
        self.export(Export::Local)
    }

    pub fn export(&self, mode: Export) -> Value {
        let local = mode == Export::Local;
        let sources = self
            .sources
            .iter()
            .map(|(d, m)| {
                obj(vec![
                    ("captured_at", n(m.captured_at)),
                    ("context", m.context.as_deref().map_or(Value::Null, s)),
                    ("digest", s(d)),
                    ("kind", s(m.kind.as_str())),
                    ("label", s(&m.label)),
                    ("nodes", count(m.nodes)),
                ])
            })
            .collect();
        let claims = self
            .claims
            .values()
            .map(|c| {
                let held = !local && (c.sensitive() || carries_page_text(&c.predicate));
                if !local && c.sensitive() {
                    return obj(vec![
                        ("certainty", s(c.certainty.as_str())),
                        ("predicate", s(&c.predicate)),
                        ("withheld", Value::Bool(true)),
                    ]);
                }
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
                                .map(|e| evidence_value(e, local))
                                .collect(),
                        ),
                    ),
                    ("id", s(&c.id)),
                    ("object", s(if held { WITHHELD } else { &c.object })),
                    ("predicate", s(&c.predicate)),
                    ("producer", s(&c.producer)),
                    ("subject", n(i64::from(c.subject))),
                    ("unit", unit_value(c.unit)),
                ])
            })
            .collect();
        let resolutions = self
            .resolutions
            .values()
            .map(|d| {
                let mut e = vec![("claim", s(&d.claim)), ("side", s(d.side.as_str()))];
                if local {
                    e.push(("decided_by", s(&d.decided_by)));
                    e.push(("reason", s(&d.reason)));
                }
                obj(e)
            })
            .collect();
        obj(vec![
            ("claims", list(claims)),
            ("export", s(if local { "local" } else { "shared" })),
            ("resolutions", list(resolutions)),
            ("schema", s(LEDGER_SCHEMA)),
            ("sources", list(sources)),
        ])
    }

    /// Rebuilds a ledger from a local export and checks it; a tampered or foreign-version document is refused.
    pub fn from_value(v: &Value) -> Result<Ledger, String> {
        match v.get("schema").and_then(Value::as_str) {
            Some(LEDGER_SCHEMA) => {}
            Some("vsf-evidence-ledger-v1") => return Err("ledger.v1_unverifiable".into()),
            _ => return Err("ledger.bad_schema".into()),
        }
        if v.get("export").and_then(Value::as_str) != Some("local") {
            return Err("ledger.not_local_export".into());
        }
        let arr = |k: &str| match v.get(k) {
            Some(Value::Array(a)) => Ok(a.clone()),
            _ => Err(format!("ledger.missing:{k}")),
        };
        let text = |x: &Value, k: &str| {
            x.get(k)
                .and_then(Value::as_str)
                .map(String::from)
                .ok_or_else(|| format!("ledger.missing:{k}"))
        };
        let int = |x: &Value, k: &str| {
            x.get(k)
                .and_then(Value::as_int)
                .ok_or_else(|| format!("ledger.missing:{k}"))
        };
        let mut l = Ledger::default();
        for m in arr("sources")? {
            let kind = SourceKind::parse(&text(&m, "kind")?).ok_or("ledger.source_kind")?;
            let meta = SourceMeta {
                kind,
                label: text(&m, "label")?,
                captured_at: int(&m, "captured_at")?,
                nodes: usize::try_from(int(&m, "nodes")?).map_err(|_| "ledger.nodes")?,
                context: m.get("context").and_then(Value::as_str).map(String::from),
            };
            l.record(&text(&m, "digest")?, meta).map_err(String::from)?;
        }
        for c in arr("claims")? {
            let evidence = match c.get("evidence") {
                Some(Value::Array(items)) => items
                    .iter()
                    .map(|e| {
                        let ids: Vec<u32> = match e.get("nodes") {
                            Some(Value::Array(a)) => a
                                .iter()
                                .filter_map(|x| x.as_int().and_then(|i| u32::try_from(i).ok()))
                                .collect(),
                            _ => return Err("ledger.evidence_nodes".to_string()),
                        };
                        let digests: Vec<String> = match e.get("digests") {
                            Some(Value::Array(a)) => a
                                .iter()
                                .filter_map(|x| x.as_str().map(String::from))
                                .collect(),
                            _ => return Err("ledger.evidence_digests".to_string()),
                        };
                        Ok(EvidenceRef {
                            kind: EvidenceType::parse(&text(e, "kind")?)
                                .ok_or("ledger.evidence_kind")?,
                            source: text(e, "source")?,
                            nodes: ids,
                            node_digests: digests,
                            sensitive: matches!(e.get("sensitive"), Some(Value::Bool(true))),
                        })
                    })
                    .collect::<Result<Vec<_>, String>>()?,
                _ => return Err("ledger.missing:evidence".into()),
            };
            let unit = match c.get("unit") {
                Some(Value::Array(p)) => {
                    let kind = match p.first().and_then(Value::as_str) {
                        Some("field") => UnitKind::Field,
                        Some("action") => UnitKind::Action,
                        Some("collection") => UnitKind::Collection,
                        _ => return Err("ledger.unit".into()),
                    };
                    let id = p
                        .get(1)
                        .and_then(Value::as_int)
                        .and_then(|i| u32::try_from(i).ok())
                        .ok_or("ledger.unit")?;
                    Some((kind, id))
                }
                _ => None,
            };
            let claim = Claim {
                id: text(&c, "id")?,
                subject: u32::try_from(int(&c, "subject")?).map_err(|_| "ledger.subject")?,
                predicate: text(&c, "predicate")?,
                object: text(&c, "object")?,
                certainty: Certainty::parse(&text(&c, "certainty")?).ok_or("ledger.certainty")?,
                confidence: match c.get("confidence").and_then(Value::as_str) {
                    None => None,
                    Some(x) => Some(Strength::parse(x).ok_or("ledger.confidence")?),
                },
                evidence,
                unit,
                producer: text(&c, "producer")?,
            };
            if l.claims.insert(claim.id.clone(), claim).is_some() {
                return Err("ledger.duplicate_claim".into());
            }
        }
        for d in arr("resolutions")? {
            let dec = crate::reconcile::Decision {
                claim: text(&d, "claim")?,
                side: crate::reconcile::Side::parse(&text(&d, "side")?).ok_or("ledger.side")?,
                decided_by: text(&d, "decided_by")?,
                reason: text(&d, "reason")?,
            };
            l.resolutions.insert(dec.claim.clone(), dec);
        }
        if let Some(e) = l.validate().first() {
            return Err(format!("ledger.invalid:{}:{}", e.problem, e.claim));
        }
        Ok(l)
    }
}

/// What every claim of one recognition pass shares: the observation source, the producer and the unit.
pub struct Ctx<'a> {
    pub obs: &'a Observation,
    pub source: &'a str,
    pub producer: &'a str,
}

impl Ctx<'_> {
    fn ev(&self, kind: EvidenceType, ids: &[u32]) -> EvidenceRef {
        evidence(self.obs, self.source, kind, ids)
    }

    #[allow(clippy::too_many_arguments)]
    fn claim(
        &self,
        l: &mut Ledger,
        subject: u32,
        predicate: &str,
        object: &str,
        certainty: Certainty,
        confidence: Option<Strength>,
        evidence: Vec<EvidenceRef>,
        unit: UnitKey,
    ) {
        l.push(NewClaim {
            subject,
            predicate: predicate.to_string(),
            object: object.to_string(),
            certainty,
            confidence,
            evidence,
            unit: Some(unit),
            producer: self.producer.to_string(),
        });
    }
}

fn binding_claims(l: &mut Ledger, cx: &Ctx, f: &Field, key: UnitKey) {
    let subject = f.node;
    let kind = EvidenceType::ObservedStructure;
    match &f.binding {
        Binding::Bound(c) => {
            let mut ids = vec![subject];
            ids.extend(&c.from);
            cx.claim(
                l,
                subject,
                "label",
                &c.text,
                Certainty::Inferred,
                Some(c.source.strength()),
                vec![cx.ev(kind, &ids)],
                key,
            );
            cx.claim(
                l,
                subject,
                "label_source",
                c.source.as_str(),
                Certainty::Inferred,
                Some(c.source.strength()),
                vec![cx.ev(kind, &ids)],
                key,
            );
        }
        Binding::Ambiguous(cands) => {
            let mut ids = vec![subject];
            for c in cands {
                ids.extend(&c.from);
            }
            let mut texts: Vec<&str> = cands.iter().map(|c| c.text.as_str()).collect();
            texts.sort_unstable();
            cx.claim(
                l,
                subject,
                "label",
                &format!("ambiguous:{}", texts.join("|")),
                Certainty::Unknown,
                None,
                vec![cx.ev(kind, &ids)],
                key,
            );
        }
        Binding::Unbound => {
            cx.claim(
                l,
                subject,
                "label",
                "none",
                Certainty::Unknown,
                None,
                vec![cx.ev(kind, &[subject])],
                key,
            );
        }
    }
}

fn candidate_claim(l: &mut Ledger, cx: &Ctx, g: &Group, key: UnitKey) {
    let certainty = match g.resolution {
        Resolution::Conflicted => Certainty::Conflicted,
        _ => Certainty::Unknown,
    };
    let described: Vec<String> = g
        .candidates
        .iter()
        .map(|c| c.hypothesis.describe())
        .collect();
    cx.claim(
        l,
        g.node,
        &format!("candidates:{}", g.topic),
        &described.join("|"),
        certainty,
        None,
        vec![cx.ev(EvidenceType::ObservedStructure, &[g.node])],
        key,
    );
}

pub fn claims_for_unit(l: &mut Ledger, cx: &Ctx, u: &Unit) {
    let key = u.key;
    let obs_kind = EvidenceType::ObservedStructure;
    match &u.output {
        Output::Field(f) => {
            let own = cx.ev(obs_kind, &[f.node]);
            cx.claim(
                l,
                f.node,
                "is_field",
                f.kind.as_str(),
                Certainty::Inferred,
                Some(Strength::Structural),
                vec![own.clone()],
                key,
            );
            binding_claims(l, cx, f, key);
            if !f.action_roles.is_empty() {
                let g = crate::candidates::kind_group(f);
                candidate_claim(l, cx, &g, key);
            }
            if let Some(sc) = &f.scope {
                cx.claim(
                    l,
                    f.node,
                    "scope",
                    sc,
                    Certainty::Inferred,
                    Some(Strength::Structural),
                    vec![own.clone()],
                    key,
                );
            }
            let usable = if f.usable {
                "true".to_string()
            } else {
                format!("false:{}", f.why_unusable.unwrap_or("unknown"))
            };
            cx.claim(
                l,
                f.node,
                "usable",
                &usable,
                Certainty::Inferred,
                Some(Strength::Explicit),
                vec![own],
                key,
            );
        }
        Output::Action(a) => {
            let own = cx.ev(obs_kind, &[a.node]);
            cx.claim(
                l,
                a.node,
                "is_action",
                &a.roles.join("|"),
                Certainty::Inferred,
                Some(Strength::Structural),
                vec![own.clone()],
                key,
            );
            if let Some(g) = crate::candidates::verb_group(a) {
                candidate_claim(l, cx, &g, key);
            }
            if a.verbs.is_empty() {
                cx.claim(
                    l,
                    a.node,
                    "verb",
                    "none",
                    Certainty::Unknown,
                    None,
                    vec![own.clone()],
                    key,
                );
            }
            for v in &a.verbs {
                let lex = EvidenceRef {
                    kind: EvidenceType::HeuristicInference,
                    ..own.clone()
                };
                cx.claim(
                    l,
                    a.node,
                    "verb",
                    v.as_str(),
                    Certainty::Inferred,
                    Some(Strength::Lexical),
                    vec![own.clone(), lex],
                    key,
                );
            }
            let usable = if a.usable {
                "true".to_string()
            } else {
                format!("false:{}", a.why_unusable.unwrap_or("unknown"))
            };
            cx.claim(
                l,
                a.node,
                "usable",
                &usable,
                Certainty::Inferred,
                Some(Strength::Explicit),
                vec![own],
                key,
            );
        }
        Output::Collection(c) => {
            let own = cx.ev(obs_kind, &[c.node]);
            cx.claim(
                l,
                c.node,
                "is_collection",
                &c.rows.to_string(),
                Certainty::Inferred,
                Some(Strength::Structural),
                vec![own.clone()],
                key,
            );
            let cols: Vec<String> = c
                .columns
                .iter()
                .map(|x| x.clone().unwrap_or_default())
                .collect();
            cx.claim(
                l,
                c.node,
                "columns",
                &cols.join("|"),
                Certainty::Inferred,
                Some(Strength::Structural),
                vec![own],
                key,
            );
        }
    }
}

/// Builds the ledger for a whole recognition run; with a context the observation is a source of that context only.
pub fn build_ledger_in(
    obs: &Observation,
    rec: &Recognition,
    captured_at: i64,
    ctx: Option<&TrustContext>,
) -> Ledger {
    let mut l = Ledger::default();
    let source = l
        .record_observation(obs, captured_at, ctx)
        .unwrap_or_default();
    let producer = producer_for(rec.rules);
    let cx = Ctx {
        obs,
        source: &source,
        producer: &producer,
    };
    for u in rec.units.values() {
        claims_for_unit(&mut l, &cx, u);
    }
    l
}

pub fn build_ledger(obs: &Observation, rec: &Recognition, captured_at: i64) -> Ledger {
    build_ledger_in(obs, rec, captured_at, None)
}
