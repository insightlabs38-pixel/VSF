//! UI and tool availability: UNKNOWN and CONFLICTED withhold, and a write needs a behavioural witness.

use crate::engine::Recognition;
use crate::ledger::{Certainty, Ledger};
use crate::semantics::{Binding, Verb};
use crate::val::{list, n, obj, s};
use std::collections::BTreeSet;
use vsf_canon::Value;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Access {
    Read,
    Write,
}

impl Access {
    pub fn as_str(self) -> &'static str {
        match self {
            Access::Read => "read",
            Access::Write => "write",
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

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Offer {
    pub node: u32,
    pub tool: String,
    pub access: Access,
    pub availability: Availability,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct Contract {
    pub offers: Vec<Offer>,
}

impl Contract {
    /// The tools a caller may actually invoke.
    pub fn emitted(&self) -> impl Iterator<Item = &Offer> {
        self.offers
            .iter()
            .filter(|o| o.availability == Availability::Available)
    }

    pub fn for_node(&self, node: u32) -> impl Iterator<Item = &Offer> {
        self.offers.iter().filter(move |o| o.node == node)
    }

    pub fn to_value(&self) -> Value {
        let offers = self
            .offers
            .iter()
            .map(|o| {
                obj(vec![
                    ("access", s(o.access.as_str())),
                    ("availability", s(o.availability.as_str())),
                    (
                        "emitted",
                        Value::Bool(o.availability == Availability::Available),
                    ),
                    ("node", n(i64::from(o.node))),
                    ("reason", o.availability.reason().map_or(Value::Null, s)),
                    ("tool", s(&o.tool)),
                    ("ui", s(o.availability.ui())),
                ])
            })
            .collect();
        obj(vec![
            ("offers", list(offers)),
            ("schema", s("vsf-availability-v1")),
        ])
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

fn tool_name(access: Access, name: Option<&str>, node: u32) -> String {
    let base = name
        .map(slug)
        .filter(|x| !x.is_empty())
        .unwrap_or_else(|| "unnamed".into());
    format!("{}_{base}_{node}", access.as_str())
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

fn write_gate(a: Availability, witnessed: bool) -> Availability {
    match a {
        Availability::Available if !witnessed => Availability::Unknown("effect_unverified"),
        other => other,
    }
}

pub fn derive(rec: &Recognition, ledger: &Ledger, witnessed: &BTreeSet<u32>) -> Contract {
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
        let w = witnessed.contains(&f.node);
        offers.push(Offer {
            node: f.node,
            tool: tool_name(Access::Read, name, f.node),
            access: Access::Read,
            availability: base,
        });
        offers.push(Offer {
            node: f.node,
            tool: tool_name(Access::Write, name, f.node),
            access: Access::Write,
            availability: write_gate(base, w),
        });
    }
    for a in rec.actions() {
        let writes = a
            .verbs
            .iter()
            .any(|v| matches!(v, Verb::Commit | Verb::Destroy | Verb::Create));
        let access = if writes || a.verbs.is_empty() {
            Access::Write
        } else {
            Access::Read
        };
        let base = if let Some(why) = a.why_unusable {
            Availability::Unusable(why)
        } else if let Some(c @ Availability::Conflicted(_)) = from_ledger(ledger, a.node) {
            c
        } else if rec
            .units
            .contains_key(&(crate::engine::UnitKind::Field, a.node))
        {
            Availability::Conflicted("kind_conflict")
        } else if a.verbs.is_empty() {
            Availability::Unknown("verb_unknown")
        } else if a.verbs.len() > 1 {
            Availability::Unknown("verb_ambiguous")
        } else {
            Availability::Available
        };
        let eff = if access == Access::Write {
            write_gate(base, witnessed.contains(&a.node))
        } else {
            base
        };
        offers.push(Offer {
            node: a.node,
            tool: tool_name(access, a.name.as_deref(), a.node),
            access,
            availability: eff,
        });
    }
    offers.sort_by_key(|x| (x.node, x.access as u8));
    Contract { offers }
}
