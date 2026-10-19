//! Availability of UI and tools: intent is a hint, a declared effect is a claim, and only a witness demonstrates an effect.

use crate::engine::Recognition;
use crate::ledger::{Certainty, Ledger};
use crate::semantics::{Binding, Verb};
use crate::val::{list, n, obj, s};
use crate::witness::{AcceptedWitnesses, EffectClass, OperationKind};
use std::collections::BTreeMap;
use vsf_canon::Value;

pub const SCHEMA: &str = "vsf-availability-v2";

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Access {
    /// Reads the observed page state; touches no source operation.
    Local,
    /// A source-backed operation demonstrated to change nothing.
    Read,
    /// A source-backed operation demonstrated to change state.
    Write,
    /// A source-backed action whose effect is not demonstrated: an interface affordance, never a tool.
    Invoke,
}

impl Access {
    pub fn as_str(self) -> &'static str {
        match self {
            Access::Local => "local",
            Access::Read => "read",
            Access::Write => "write",
            Access::Invoke => "invoke",
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Availability {
    Available,
    Unknown(&'static str),
    Conflicted(&'static str),
    Unusable(&'static str),
}

impl Availability {
    pub fn as_str(self) -> &'static str {
        match self {
            Availability::Available => "available",
            Availability::Unknown(_) => "unknown",
            Availability::Conflicted(_) => "conflicted",
            Availability::Unusable(_) => "unusable",
        }
    }

    pub fn reason(self) -> Option<&'static str> {
        match self {
            Availability::Available => None,
            Availability::Unknown(r) | Availability::Conflicted(r) | Availability::Unusable(r) => {
                Some(r)
            }
        }
    }

    /// What the surface shows: never a silent enable for anything short of Available.
    pub fn ui(self) -> &'static str {
        match self {
            Availability::Available => "enabled",
            Availability::Unknown(_) => "needs_review",
            Availability::Conflicted(_) => "conflict_badge",
            Availability::Unusable(_) => "disabled",
        }
    }
}

/// The four separate questions about an action: what it looks like, what is claimed, what was shown, what is allowed.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct EffectView {
    pub intent: Vec<Verb>,
    pub declared: Option<EffectClass>,
    pub demonstrated: Option<EffectClass>,
    pub witnesses: Vec<String>,
    pub synthetic: bool,
    /// The label suggested one thing and the witness showed another.
    pub intent_mismatch: bool,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Offer {
    pub node: u32,
    pub tool: String,
    pub access: Access,
    pub availability: Availability,
    pub effect: EffectView,
}

impl Offer {
    /// Executable by a caller: available, source-backed or local, and not resting on a synthetic witness.
    pub fn authorized(&self) -> bool {
        self.availability == Availability::Available
            && self.access != Access::Invoke
            && !self.effect.synthetic
    }
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct Contract {
    pub offers: Vec<Offer>,
}

impl Contract {
    /// The tools a caller may actually invoke in production.
    pub fn emitted(&self) -> impl Iterator<Item = &Offer> {
        self.offers.iter().filter(|o| o.authorized())
    }

    /// Tools that exist only because a test witness said so; never offered to a caller.
    pub fn synthetic_only(&self) -> impl Iterator<Item = &Offer> {
        self.offers.iter().filter(|o| {
            o.availability == Availability::Available
                && o.access != Access::Invoke
                && o.effect.synthetic
        })
    }

    pub fn for_node(&self, node: u32) -> impl Iterator<Item = &Offer> {
        self.offers.iter().filter(move |o| o.node == node)
    }

    pub fn to_value(&self) -> Value {
        let offers = self
            .offers
            .iter()
            .map(|o| {
                let e = &o.effect;
                obj(vec![
                    ("access", s(o.access.as_str())),
                    ("availability", s(o.availability.as_str())),
                    (
                        "effect",
                        obj(vec![
                            (
                                "declared",
                                e.declared.map_or(Value::Null, |c| s(c.as_str())),
                            ),
                            (
                                "demonstrated",
                                e.demonstrated.map_or(Value::Null, |c| s(c.as_str())),
                            ),
                            (
                                "intent",
                                list(e.intent.iter().map(|v| s(v.effect_hint())).collect()),
                            ),
                            ("intent_mismatch", Value::Bool(e.intent_mismatch)),
                            ("synthetic", Value::Bool(e.synthetic)),
                            (
                                "witnesses",
                                list(e.witnesses.iter().map(|w| s(w)).collect()),
                            ),
                        ]),
                    ),
                    ("emitted", Value::Bool(o.authorized())),
                    ("node", n(i64::from(o.node))),
                    ("reason", o.availability.reason().map_or(Value::Null, s)),
                    ("tool", s(&o.tool)),
                    ("ui", s(o.availability.ui())),
                ])
            })
            .collect();
        obj(vec![("offers", list(offers)), ("schema", s(SCHEMA))])
    }
}

fn slug(name: &str) -> String {
    let mut out = String::new();
    for c in name.chars() {
        if c.is_ascii_alphanumeric() {
            out.push(c.to_ascii_lowercase());
        } else if !out.ends_with('_') && !out.is_empty() {
            out.push('_');
        }
    }
    out.trim_end_matches('_').chars().take(40).collect()
}

fn tool_name(prefix: &str, name: Option<&str>, node: u32) -> String {
    let base = name
        .map(slug)
        .filter(|x| !x.is_empty())
        .unwrap_or_else(|| "unnamed".into());
    format!("{prefix}_{base}_{node}")
}

/// Conflicts and unknowns recorded against the node win over anything locally derived, except unusability.
fn from_ledger(ledger: &Ledger, node: u32) -> Option<Availability> {
    let mut unknown = false;
    for c in ledger.by_subject(node) {
        match c.certainty {
            Certainty::Conflicted if ledger.resolutions.contains_key(&c.id) => {}
            Certainty::Conflicted => return Some(Availability::Conflicted("conflicting_evidence")),
            Certainty::Unknown if c.predicate.starts_with("candidates:") => unknown = true,
            _ => {}
        }
    }
    unknown.then_some(Availability::Unknown("ambiguous_candidates"))
}

fn hint(v: Verb) -> EffectClass {
    match v {
        Verb::Query | Verb::Export | Verb::Navigate | Verb::Dismiss => EffectClass::ReadOnly,
        Verb::Create | Verb::Commit => EffectClass::NonIdempotentWrite,
        Verb::Destroy => EffectClass::Destructive,
    }
}

type Shown = Result<Option<(EffectClass, Vec<String>, bool)>, Availability>;

/// What a set of witnesses establishes for one operation: the class, or a reason it is not established.
fn demonstrate(w: &AcceptedWitnesses, node: u32, kind: OperationKind) -> Shown {
    let found = w.for_op(node, kind);
    let Some(first) = found.first() else {
        return Ok(None);
    };
    if found.iter().any(|a| a.effect != first.effect) {
        return Err(Availability::Conflicted("witness_conflict"));
    }
    Ok(Some((
        first.effect,
        found.iter().map(|a| a.id.clone()).collect(),
        found.iter().any(|a| a.synthetic),
    )))
}

/// Applies a declared effect over a demonstration: any disagreement is a conflict, never a pick.
fn judged(base: Availability, shown: &Shown, declared: Option<EffectClass>) -> Availability {
    if !matches!(base, Availability::Available) {
        return base;
    }
    match shown {
        Err(a) => *a,
        Ok(None) => Availability::Unknown("effect_unverified"),
        Ok(Some((EffectClass::Indeterminate, _, _))) => {
            Availability::Unknown("effect_indeterminate")
        }
        Ok(Some((c, _, _))) if declared.is_some_and(|d| d != *c) => {
            Availability::Conflicted("declared_effect_conflict")
        }
        Ok(Some(_)) => base,
    }
}

pub fn derive(
    rec: &Recognition,
    ledger: &Ledger,
    witnesses: &AcceptedWitnesses,
    declared: &BTreeMap<u32, EffectClass>,
) -> Contract {
    let mut offers = Vec::new();
    for f in rec.fields() {
        let name = match &f.binding {
            Binding::Bound(c) => Some(c.text.as_str()),
            _ => None,
        };
        let base = if let Some(why) = f.why_unusable {
            Availability::Unusable(why)
        } else if !f.action_roles.is_empty() {
            Availability::Conflicted("kind_conflict")
        } else if let Some(a) =
            from_ledger(ledger, f.node).filter(|a| matches!(a, Availability::Conflicted(_)))
        {
            a
        } else if name.is_none() {
            Availability::Unknown("label_unknown")
        } else {
            Availability::Available
        };
        offers.push(Offer {
            node: f.node,
            tool: tool_name("local_read", name, f.node),
            access: Access::Local,
            availability: base,
            effect: EffectView {
                intent: Vec::new(),
                declared: None,
                demonstrated: None,
                witnesses: Vec::new(),
                synthetic: false,
                intent_mismatch: false,
            },
        });
        let shown = demonstrate(witnesses, f.node, OperationKind::FieldWrite);
        let mut availability = judged(base, &shown, declared.get(&f.node).copied());
        if matches!(shown, Ok(Some((EffectClass::ReadOnly, _, _)))) && availability == base {
            availability = Availability::Conflicted("witness_effect_mismatch");
        }
        let (demonstrated, ids, synthetic) = match shown {
            Ok(Some((c, ids, syn))) => (Some(c), ids, syn),
            _ => (None, Vec::new(), false),
        };
        offers.push(Offer {
            node: f.node,
            tool: tool_name("write", name, f.node),
            access: if demonstrated.is_some() {
                Access::Write
            } else {
                Access::Invoke
            },
            availability,
            effect: EffectView {
                intent: Vec::new(),
                declared: declared.get(&f.node).copied(),
                demonstrated,
                witnesses: ids,
                synthetic,
                intent_mismatch: false,
            },
        });
    }
    for a in rec.actions() {
        let base = if let Some(why) = a.why_unusable {
            Availability::Unusable(why)
        } else if let Some(c @ Availability::Conflicted(_)) = from_ledger(ledger, a.node) {
            c
        } else if rec
            .units
            .contains_key(&(crate::engine::UnitKind::Field, a.node))
        {
            Availability::Conflicted("kind_conflict")
        } else if a.name.is_none() {
            Availability::Unknown("name_unknown")
        } else if a.verbs.len() > 1 {
            Availability::Unknown("verb_ambiguous")
        } else {
            Availability::Available
        };
        let shown = demonstrate(witnesses, a.node, OperationKind::Action);
        let decl = declared.get(&a.node).copied();
        let availability = judged(base, &shown, decl);
        let (demonstrated, ids, synthetic) = match &shown {
            Ok(Some((c, ids, syn))) => (Some(*c), ids.clone(), *syn),
            _ => (None, Vec::new(), false),
        };
        let access = match demonstrated {
            Some(EffectClass::ReadOnly) => Access::Read,
            Some(EffectClass::Indeterminate) | None => Access::Invoke,
            Some(_) => Access::Write,
        };
        let intent_mismatch = demonstrated.is_some_and(|d| {
            a.verbs
                .iter()
                .any(|v| (hint(*v) == EffectClass::ReadOnly) != (d == EffectClass::ReadOnly))
        });
        offers.push(Offer {
            node: a.node,
            tool: tool_name(access.as_str(), a.name.as_deref(), a.node),
            access,
            availability,
            effect: EffectView {
                intent: a.verbs.clone(),
                declared: decl,
                demonstrated,
                witnesses: ids,
                synthetic,
                intent_mismatch,
            },
        });
    }
    offers.sort_by_key(|x| (x.node, x.access as u8));
    Contract { offers }
}
