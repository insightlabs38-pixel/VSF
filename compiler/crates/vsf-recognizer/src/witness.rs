//! Behavioural witnesses: typed, context-bound proof of what an operation did, and the only route to an executable operation.

use crate::obs::Observation;
use crate::protocol::Revocations;
use crate::trust::{ContextChange, TrustContext, digest_ok};
use crate::val::{obj, s};
use std::collections::{BTreeMap, BTreeSet};
use vsf_canon::{Value, sha256_hex, to_canonical};

pub const SCHEMA: &str = "vsf-behavioral-witness-v1";
const SKEW_MS: i64 = 5 * 60 * 1000;
const MAX_SAFE: i64 = 9_007_199_254_740_991;

/// Ordered by danger, as in the IR: a higher class is always the more cautious reading.
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord)]
pub enum EffectClass {
    ReadOnly,
    IdempotentWrite,
    ConditionalWrite,
    NonIdempotentWrite,
    Destructive,
    Indeterminate,
}

impl EffectClass {
    pub fn as_str(self) -> &'static str {
        match self {
            EffectClass::ReadOnly => "read_only",
            EffectClass::IdempotentWrite => "idempotent_write",
            EffectClass::ConditionalWrite => "conditional_write",
            EffectClass::NonIdempotentWrite => "non_idempotent_write",
            EffectClass::Destructive => "destructive",
            EffectClass::Indeterminate => "indeterminate",
        }
    }

    pub fn parse(x: &str) -> Option<EffectClass> {
        [
            EffectClass::ReadOnly,
            EffectClass::IdempotentWrite,
            EffectClass::ConditionalWrite,
            EffectClass::NonIdempotentWrite,
            EffectClass::Destructive,
            EffectClass::Indeterminate,
        ]
        .into_iter()
        .find(|c| c.as_str() == x)
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord)]
pub enum OperationKind {
    Action,
    FieldWrite,
}

impl OperationKind {
    pub fn as_str(self) -> &'static str {
        match self {
            OperationKind::Action => "action",
            OperationKind::FieldWrite => "field_write",
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum WitnessStatus {
    Verified,
    Failed,
    Inconclusive,
    /// Made by a test; accepted only where the policy says emission is for tests.
    Synthetic,
}

impl WitnessStatus {
    fn parse(x: &str) -> Option<WitnessStatus> {
        match x {
            "verified" => Some(WitnessStatus::Verified),
            "failed" => Some(WitnessStatus::Failed),
            "inconclusive" => Some(WitnessStatus::Inconclusive),
            "synthetic" => Some(WitnessStatus::Synthetic),
            _ => None,
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Emission {
    Production,
    TestOnly,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Witness {
    pub id: String,
    pub digest: String,
    pub status: WitnessStatus,
    pub issuer: String,
    pub context: TrustContext,
    pub kind: OperationKind,
    pub node: u32,
    pub node_digest: String,
    pub effect: EffectClass,
    pub evidence_digest: String,
    pub verified_at: i64,
    pub expires_at: i64,
    pub revoked_at: Option<i64>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Reject {
    pub id: String,
    pub reason: &'static str,
}

fn closed(v: &Value, keys: &[&str]) -> bool {
    matches!(v, Value::Object(e) if e.len() == keys.len() && keys.iter().all(|k| e.iter().any(|(x, _)| x == k)))
}

fn ident_ok(x: &str) -> bool {
    let b = x.as_bytes();
    !b.is_empty()
        && b.len() <= 64
        && b[0].is_ascii_lowercase()
        && b[1..]
            .iter()
            .all(|&c| c.is_ascii_lowercase() || c.is_ascii_digit() || b"_-".contains(&c))
}

fn id_ok(x: &str) -> bool {
    x.strip_prefix("wit_").is_some_and(|h| {
        (16..=64).contains(&h.len())
            && h.bytes()
                .all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(&b))
    })
}

impl Witness {
    /// Strict parse: every key present and nothing extra, every identifier well-formed.
    pub fn from_value(v: &Value) -> Result<Witness, &'static str> {
        if !matches!(v, Value::Object(_)) {
            return Err("malformed");
        }
        let top = [
            "schema",
            "id",
            "status",
            "issuer",
            "context",
            "operation",
            "effect",
            "verified_at",
            "expires_at",
        ];
        let has_revoked = v.get("revoked_at").is_some();
        let mut keys = top.to_vec();
        if has_revoked {
            keys.push("revoked_at");
        }
        if !closed(v, &keys) {
            return Err("malformed");
        }
        let text = |o: &Value, k: &str| o.get(k).and_then(Value::as_str).map(String::from);
        let time = |o: &Value, k: &str| {
            o.get(k)
                .and_then(Value::as_int)
                .filter(|t| (0..=MAX_SAFE).contains(t))
        };
        if text(v, "schema").as_deref() != Some(SCHEMA) {
            return Err("malformed");
        }
        let id = text(v, "id").filter(|x| id_ok(x)).ok_or("malformed")?;
        let status = text(v, "status")
            .and_then(|x| WitnessStatus::parse(&x))
            .ok_or("malformed")?;
        let issuer = text(v, "issuer")
            .filter(|x| ident_ok(x))
            .ok_or("malformed")?;
        let context = TrustContext::from_value(v.get("context").ok_or("malformed")?)
            .map_err(|_| "malformed")?;
        let op = v.get("operation").ok_or("malformed")?;
        if !closed(op, &["kind", "node", "node_digest"]) {
            return Err("malformed");
        }
        let kind = match text(op, "kind").as_deref() {
            Some("action") => OperationKind::Action,
            Some("field_write") => OperationKind::FieldWrite,
            _ => return Err("malformed"),
        };
        let node = op
            .get("node")
            .and_then(Value::as_int)
            .and_then(|n| u32::try_from(n).ok())
            .ok_or("malformed")?;
        let node_digest = text(op, "node_digest")
            .filter(|d| {
                d.len() == 64
                    && d.bytes()
                        .all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(&b))
            })
            .ok_or("malformed")?;
        let eff = v.get("effect").ok_or("malformed")?;
        if !closed(eff, &["class", "evidence_digest"]) {
            return Err("malformed");
        }
        let effect = text(eff, "class")
            .and_then(|c| EffectClass::parse(&c))
            .ok_or("malformed")?;
        let evidence_digest = text(eff, "evidence_digest")
            .filter(|d| digest_ok(d))
            .ok_or("malformed")?;
        let verified_at = time(v, "verified_at").ok_or("malformed")?;
        let expires_at = time(v, "expires_at").ok_or("malformed")?;
        if expires_at <= verified_at {
            return Err("malformed");
        }
        let revoked_at = match v.get("revoked_at") {
            None => None,
            Some(_) => Some(time(v, "revoked_at").ok_or("malformed")?),
        };
        Ok(Witness {
            id,
            digest: format!("sha256:{}", sha256_hex(&to_canonical(v))),
            status,
            issuer,
            context,
            kind,
            node,
            node_digest,
            effect,
            evidence_digest,
            verified_at,
            expires_at,
            revoked_at,
        })
    }

    pub fn to_value(&self) -> Value {
        let status = match self.status {
            WitnessStatus::Verified => "verified",
            WitnessStatus::Failed => "failed",
            WitnessStatus::Inconclusive => "inconclusive",
            WitnessStatus::Synthetic => "synthetic",
        };
        let mut e = vec![
            ("context", self.context.to_value()),
            (
                "effect",
                obj(vec![
                    ("class", s(self.effect.as_str())),
                    ("evidence_digest", s(&self.evidence_digest)),
                ]),
            ),
            ("expires_at", Value::Int(self.expires_at)),
            ("id", s(&self.id)),
            ("issuer", s(&self.issuer)),
            (
                "operation",
                obj(vec![
                    ("kind", s(self.kind.as_str())),
                    ("node", Value::Int(i64::from(self.node))),
                    ("node_digest", s(&self.node_digest)),
                ]),
            ),
            ("schema", s(SCHEMA)),
            ("status", s(status)),
            ("verified_at", Value::Int(self.verified_at)),
        ];
        if let Some(r) = self.revoked_at {
            e.push(("revoked_at", Value::Int(r)));
        }
        obj(e)
    }
}

/// Everything a witness is judged against; supplied by the host, never by the witness.
#[derive(Debug, Clone)]
pub struct WitnessPolicy {
    pub trust: TrustContext,
    pub now: i64,
    pub max_age_ms: i64,
    pub trusted_issuers: BTreeSet<String>,
    pub revoked: Revocations,
    pub emission: Emission,
}

impl WitnessPolicy {
    pub fn from_value(v: &Value) -> Result<WitnessPolicy, String> {
        let keys = [
            "schema",
            "trust",
            "now",
            "max_age_ms",
            "trusted_issuers",
            "emission",
        ];
        let optional = ["revoked"];
        let Value::Object(entries) = v else {
            return Err("policy.not_object".into());
        };
        if entries
            .iter()
            .any(|(k, _)| !keys.contains(&k.as_str()) && !optional.contains(&k.as_str()))
            || keys.iter().any(|k| v.get(k).is_none())
        {
            return Err("policy.keys".into());
        }
        if v.get("schema").and_then(Value::as_str) != Some("vsf-witness-policy-v1") {
            return Err("policy.bad_schema".into());
        }
        let int = |k: &str| {
            v.get(k)
                .and_then(Value::as_int)
                .filter(|t| (0..=MAX_SAFE).contains(t))
                .ok_or_else(|| format!("policy.type:{k}"))
        };
        let trusted_issuers: BTreeSet<String> = match v.get("trusted_issuers") {
            Some(Value::Array(a)) => a
                .iter()
                .map(|x| {
                    x.as_str()
                        .filter(|i| ident_ok(i))
                        .map(String::from)
                        .ok_or_else(|| "policy.trusted_issuers".to_string())
                })
                .collect::<Result<_, _>>()?,
            _ => return Err("policy.trusted_issuers".into()),
        };
        let emission = match v.get("emission").and_then(Value::as_str) {
            Some("production") => Emission::Production,
            Some("test_only") => Emission::TestOnly,
            _ => return Err("policy.emission".into()),
        };
        Ok(WitnessPolicy {
            trust: TrustContext::from_value(v.get("trust").ok_or("policy.missing:trust")?)?,
            now: int("now")?,
            max_age_ms: int("max_age_ms")?,
            trusted_issuers,
            revoked: match v.get("revoked") {
                None => Revocations::default(),
                Some(r) => Revocations::from_value(r)?,
            },
            emission,
        })
    }
}

/// A witness that passed every check; only `AcceptedWitnesses::collect` can make one.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Accepted {
    pub id: String,
    pub issuer: String,
    pub effect: EffectClass,
    pub synthetic: bool,
}

/// Witnesses that authorize operations. There is no constructor from bare node ids.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct AcceptedWitnesses {
    by_op: BTreeMap<(u32, OperationKind), Vec<Accepted>>,
    pub rejected: Vec<Reject>,
}

impl AcceptedWitnesses {
    pub fn none() -> AcceptedWitnesses {
        AcceptedWitnesses::default()
    }

    pub fn for_op(&self, node: u32, kind: OperationKind) -> &[Accepted] {
        self.by_op.get(&(node, kind)).map_or(&[], Vec::as_slice)
    }

    pub fn len(&self) -> usize {
        self.by_op.values().map(Vec::len).sum()
    }

    pub fn is_empty(&self) -> bool {
        self.by_op.is_empty()
    }

    pub fn ids(&self) -> BTreeSet<String> {
        self.by_op
            .values()
            .flatten()
            .map(|a| a.id.clone())
            .collect()
    }

    /// Judges every document against the policy and the current observation; failures are recorded, never skipped.
    pub fn collect(docs: &[Value], pol: &WitnessPolicy, obs: &Observation) -> AcceptedWitnesses {
        let mut out = AcceptedWitnesses::default();
        for d in docs {
            match judge(d, pol, obs) {
                Ok((key, a)) => out.by_op.entry(key).or_default().push(a),
                Err(r) => out.rejected.push(r),
            }
        }
        for list in out.by_op.values_mut() {
            list.sort_by(|a, b| a.id.cmp(&b.id));
            list.dedup_by(|a, b| a.id == b.id);
        }
        out
    }
}

type Key = (u32, OperationKind);

fn judge(d: &Value, pol: &WitnessPolicy, obs: &Observation) -> Result<(Key, Accepted), Reject> {
    let w = Witness::from_value(d).map_err(|reason| Reject {
        id: d
            .get("id")
            .and_then(Value::as_str)
            .unwrap_or("")
            .to_string(),
        reason,
    })?;
    let no = |reason| Reject {
        id: w.id.clone(),
        reason,
    };
    let r = &pol.revoked;
    if r.witnesses.contains(&w.id)
        || r.principals.contains(&w.context.principal)
        || r.operations.contains(&(w.node, w.node_digest.clone()))
        || w.revoked_at.is_some_and(|t| t <= pol.now)
    {
        return Err(no("revoked"));
    }
    if r.authorities.contains(&w.issuer) {
        return Err(no("revoked_authority"));
    }
    if !pol.trusted_issuers.contains(&w.issuer) {
        return Err(no("untrusted_issuer"));
    }
    match w.status {
        WitnessStatus::Failed => return Err(no("failed")),
        WitnessStatus::Inconclusive => return Err(no("inconclusive")),
        WitnessStatus::Synthetic if pol.emission == Emission::Production => {
            return Err(no("synthetic_not_allowed"));
        }
        _ => {}
    }
    if let Some(change) = pol.trust.diff(&w.context).into_iter().next() {
        return Err(no(match change {
            ContextChange::App => "wrong_application",
            ContextChange::Origin => "wrong_origin",
            ContextChange::Principal => "wrong_principal",
            ContextChange::Variant => "wrong_variant",
            ContextChange::Runtime => "wrong_environment",
            ContextChange::Capability => "capability_changed",
            ContextChange::SourceRevision => "source_version_changed",
        }));
    }
    if w.verified_at > pol.now.saturating_add(SKEW_MS) {
        return Err(no("from_the_future"));
    }
    if w.expires_at <= pol.now {
        return Err(no("expired"));
    }
    if pol.now.saturating_sub(w.verified_at) > pol.max_age_ms {
        return Err(no("stale"));
    }
    let Some(i) = obs.index_of(w.node) else {
        return Err(no("operation_missing"));
    };
    if crate::ledger::node_digest(obs, i) != w.node_digest {
        return Err(no("operation_changed"));
    }
    Ok((
        (w.node, w.kind),
        Accepted {
            id: w.id,
            issuer: w.issuer,
            effect: w.effect,
            synthetic: w.status == WitnessStatus::Synthetic,
        },
    ))
}
