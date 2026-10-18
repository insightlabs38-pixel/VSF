//! Registration: duplicate ids, unsupported evidence, ambiguous recognitions and capability mismatches fail.

use crate::model::{Kind, Maturity, Registry};
use crate::policy::Policy;
use regex::Regex;
use std::collections::{BTreeMap, BTreeSet};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Evidence {
    pub kind: String,
    pub id: String,
    pub polarity: Option<String>,
    pub independent: bool,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Recognition {
    pub produces: String,
    pub signature: Vec<String>,
    pub excludes: Vec<String>,
    pub precedence: u32,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Registration {
    pub id: String,
    pub maturity: Maturity,
    pub execution: Vec<String>,
    pub needs_capabilities: Vec<String>,
    pub recognition: Option<Recognition>,
    pub evidence: Vec<Evidence>,
    pub implementation: Option<String>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum RegisterError {
    DuplicateId(String),
    UnknownPrimitive(String),
    UnresolvedKind(String),
    InvalidRecognition {
        id: String,
        reason: &'static str,
    },
    InvalidEvidence {
        id: String,
        reason: &'static str,
    },
    AmbiguousRecognition {
        a: String,
        b: String,
        produces: String,
    },
    UnknownMode {
        id: String,
        mode: String,
    },
    CapabilityMismatch {
        id: String,
        mode: String,
        capability: String,
    },
}

impl RegisterError {
    pub fn code(&self) -> &'static str {
        match self {
            RegisterError::DuplicateId(_) => "register.duplicate_id",
            RegisterError::UnknownPrimitive(_) => "register.unknown_primitive",
            RegisterError::UnresolvedKind(_) => "register.unresolved_kind",
            RegisterError::InvalidRecognition { .. } => "register.invalid_recognition",
            RegisterError::InvalidEvidence { .. } => "register.invalid_evidence",
            RegisterError::AmbiguousRecognition { .. } => "register.ambiguous_recognition",
            RegisterError::UnknownMode { .. } => "register.unknown_mode",
            RegisterError::CapabilityMismatch { .. } => "register.capability_mismatch",
        }
    }
}

pub struct Registrar<'a> {
    registry: &'a Registry,
    policy: &'a Policy,
    fixtures: BTreeSet<String>,
    registered: BTreeMap<String, Registration>,
    predicate: Regex,
}

fn matches_together(a: &Recognition, b: &Recognition) -> bool {
    let union: BTreeSet<&String> = a.signature.iter().chain(b.signature.iter()).collect();
    !a.excludes.iter().any(|x| union.contains(x)) && !b.excludes.iter().any(|x| union.contains(x))
}

impl<'a> Registrar<'a> {
    pub fn new(registry: &'a Registry, policy: &'a Policy, fixtures: BTreeSet<String>) -> Self {
        Registrar {
            registry,
            policy,
            fixtures,
            registered: BTreeMap::new(),
            predicate: Regex::new(r"^[a-z][a-z0-9_.-]*:[A-Za-z0-9_.*=\[\]-]+$")
                .unwrap_or_else(|_| unreachable!()),
        }
    }

    pub fn registered(&self) -> &BTreeMap<String, Registration> {
        &self.registered
    }

    fn check_recognition(&self, id: &str, r: &Recognition) -> Result<(), RegisterError> {
        let bad = |reason| {
            Err(RegisterError::InvalidRecognition {
                id: id.to_string(),
                reason,
            })
        };
        if r.produces.is_empty() {
            return bad("produces is empty");
        }
        if r.signature.is_empty() {
            return bad("signature is empty");
        }
        if r.signature
            .iter()
            .chain(r.excludes.iter())
            .any(|p| !self.predicate.is_match(p))
        {
            return bad("predicate is not namespace:value");
        }
        if r.signature.iter().any(|p| r.excludes.contains(p)) {
            return bad("a predicate is both required and excluded");
        }
        Ok(())
    }

    fn check_evidence(&self, reg: &Registration, kind: Kind) -> Result<(), RegisterError> {
        let bad = |reason| {
            Err(RegisterError::InvalidEvidence {
                id: reg.id.clone(),
                reason,
            })
        };
        let has = |k: &str| reg.evidence.iter().any(|e| e.kind == k);
        for e in &reg.evidence {
            if e.id.is_empty() {
                return bad("evidence id is empty");
            }
            if e.kind == "fixture" {
                if !self.fixtures.contains(&e.id) {
                    return bad("fixture evidence names no known fixture");
                }
                if !matches!(
                    e.polarity.as_deref(),
                    Some("positive" | "negative" | "adversarial")
                ) {
                    return bad("fixture evidence needs a polarity");
                }
            } else if !matches!(e.kind.as_str(), "spec-document" | "review" | "field") {
                return bad("unknown evidence kind");
            }
        }
        let fixture = |pol: &str| {
            reg.evidence
                .iter()
                .any(|e| e.kind == "fixture" && e.polarity.as_deref() == Some(pol))
        };
        if reg.maturity >= Maturity::Specified && !has("spec-document") {
            return bad("specified needs a spec-document");
        }
        if reg.maturity >= Maturity::Implemented {
            if reg.implementation.as_deref().is_none_or(str::is_empty) {
                return bad("implemented needs an implementation artifact");
            }
            if !fixture("positive") || !fixture("negative") {
                return bad("implemented needs a passing positive and negative fixture");
            }
        }
        if reg.maturity >= Maturity::Conformant {
            if !reg
                .evidence
                .iter()
                .any(|e| e.kind == "fixture" && e.independent)
            {
                return bad("conformant needs an independently sourced fixture");
            }
            if !fixture("adversarial") || !has("review") {
                return bad("conformant needs an adversarial fixture and a review record");
            }
            if kind == Kind::Composite && reg.maturity >= Maturity::Conformant {
                let deps_ok = self
                    .registry
                    .entries
                    .iter()
                    .find(|e| e.id == reg.id)
                    .is_some_and(|e| {
                        e.requires.iter().all(|d| {
                            self.registered
                                .get(d)
                                .is_some_and(|r| r.maturity >= Maturity::Implemented)
                        })
                    });
                if !deps_ok {
                    return bad("a conformant composite needs its dependencies implemented");
                }
            }
        }
        if reg.maturity >= Maturity::ProductionProven && !has("field") {
            return bad("production_proven needs field evidence");
        }
        Ok(())
    }

    pub fn register(&mut self, reg: Registration) -> Result<(), RegisterError> {
        if self.registered.contains_key(&reg.id) {
            return Err(RegisterError::DuplicateId(reg.id));
        }
        let entry = self
            .registry
            .entries
            .iter()
            .find(|e| e.id == reg.id)
            .ok_or_else(|| RegisterError::UnknownPrimitive(reg.id.clone()))?;
        if entry.kind == Kind::Unresolved && reg.maturity > Maturity::Candidate {
            return Err(RegisterError::UnresolvedKind(reg.id));
        }
        for mode in &reg.execution {
            let Some(caps) = self.policy.modes.get(mode) else {
                return Err(RegisterError::UnknownMode {
                    id: reg.id.clone(),
                    mode: mode.clone(),
                });
            };
            if let Some(missing) = reg.needs_capabilities.iter().find(|c| !caps.contains(c)) {
                return Err(RegisterError::CapabilityMismatch {
                    id: reg.id.clone(),
                    mode: mode.clone(),
                    capability: missing.clone(),
                });
            }
        }
        self.check_evidence(&reg, entry.kind)?;
        if let Some(r) = &reg.recognition {
            self.check_recognition(&reg.id, r)?;
            for other in self.registered.values() {
                let Some(o) = &other.recognition else {
                    continue;
                };
                if o.produces == r.produces
                    && o.precedence == r.precedence
                    && matches_together(o, r)
                {
                    return Err(RegisterError::AmbiguousRecognition {
                        a: other.id.clone(),
                        b: reg.id.clone(),
                        produces: r.produces.clone(),
                    });
                }
            }
        }
        self.registered.insert(reg.id.clone(), reg);
        Ok(())
    }
}

/// Primitives whose recognition accepts this observed predicate set, highest precedence first.
pub fn recognize<'a>(
    registered: &'a BTreeMap<String, Registration>,
    observed: &BTreeSet<String>,
) -> Vec<&'a Registration> {
    let mut hits: Vec<&Registration> = registered
        .values()
        .filter(|r| {
            r.recognition.as_ref().is_some_and(|x| {
                x.signature.iter().all(|p| observed.contains(p))
                    && !x.excludes.iter().any(|p| observed.contains(p))
            })
        })
        .collect();
    hits.sort_by(|a, b| {
        let (pa, pb) = (
            a.recognition.as_ref().map_or(0, |r| r.precedence),
            b.recognition.as_ref().map_or(0, |r| r.precedence),
        );
        pb.cmp(&pa).then_with(|| a.id.cmp(&b.id))
    });
    hits
}
