//! Cross-field invariants decoding cannot express; Rust is the single authority.

use crate::generated::*;
use crate::rt::parse_version;
use std::collections::{BTreeMap, BTreeSet};
use vsf_canon::Value;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Violation {
    pub code: &'static str,
    pub path: String,
    pub detail: String,
}

fn v(code: &'static str, path: &str, detail: impl Into<String>) -> Violation {
    Violation {
        code,
        path: path.to_string(),
        detail: detail.into(),
    }
}

/// `scheme://host[:port]` in lowercase with no default port, no empty or hyphen-edged label and no path.
pub fn origin_canonical(o: &str) -> bool {
    let Some((scheme, rest)) = o.split_once("://") else {
        return false;
    };
    let default = match scheme {
        "http" => "80",
        "https" => "443",
        _ => return false,
    };
    let (host, port) = rest
        .split_once(':')
        .map_or((rest, None), |(h, p)| (h, Some(p)));
    if let Some(p) = port {
        let digits = !p.is_empty() && p.len() <= 5 && p.bytes().all(|b| b.is_ascii_digit());
        if !digits
            || p.starts_with('0')
            || p == default
            || p.parse::<u32>().is_ok_and(|n| n > 65535)
        {
            return false;
        }
    }
    !host.is_empty()
        && host.len() <= 253
        && host.split('.').all(|l| {
            !l.is_empty()
                && l.len() <= 63
                && !l.starts_with('-')
                && !l.ends_with('-')
                && l.bytes()
                    .all(|b| b.is_ascii_lowercase() || b.is_ascii_digit() || b == b'-')
        })
}

// ---- effect and certainty algebra -------------------------------------------------------------

/// Danger rank of an effect class; `indeterminate` is the most conservative.
pub fn danger(e: EffectClass) -> u8 {
    match e {
        EffectClass::ReadOnly => 0,
        EffectClass::IdempotentWrite => 1,
        EffectClass::ConditionalWrite => 2,
        EffectClass::NonIdempotentWrite => 3,
        EffectClass::Destructive => 4,
        EffectClass::Indeterminate => 5,
    }
}

/// Combining two effects keeps the more dangerous one; the result is never less safe than either input.
pub fn join_effects(a: EffectClass, b: EffectClass) -> EffectClass {
    if danger(a) >= danger(b) { a } else { b }
}

/// Anything not proven is treated as the most dangerous effect.
pub const CONSERVATIVE_EFFECT: EffectClass = EffectClass::Indeterminate;

pub fn is_write(e: EffectClass) -> bool {
    e != EffectClass::ReadOnly
}

/// Strength of support: conflicted < unknown < inferred < verified.
pub fn strength(c: Certainty) -> u8 {
    match c {
        Certainty::Conflicted => 0,
        Certainty::Unknown => 1,
        Certainty::Inferred => 2,
        Certainty::Verified => 3,
    }
}

/// Conjunction of certainties: a conclusion is only as strong as its weakest premise.
pub fn combine_certainty(a: Certainty, b: Certainty) -> Certainty {
    if strength(a) <= strength(b) { a } else { b }
}

/// Only these evidence kinds can establish what an action does; structure or labels alone cannot.
pub fn proves_behavior(t: EvidenceType) -> bool {
    matches!(
        t,
        EvidenceType::RuntimeBehavior
            | EvidenceType::DocumentedSchema
            | EvidenceType::ProtocolTrace
    )
}

/// Heuristics and bare user statements can support `inferred` but never `verified`.
pub fn is_strong(t: EvidenceType) -> bool {
    !matches!(
        t,
        EvidenceType::HeuristicInference | EvidenceType::UserAssertion
    )
}

// ---- operation contracts ----------------------------------------------------------------------

fn range_contains(outer: &VersionRange, inner: &VersionRange) -> bool {
    let min = |r: &VersionRange| r.min.as_ref().and_then(|m| parse_version(m.as_str()));
    let max = |r: &VersionRange| r.max.as_ref().and_then(|m| parse_version(m.as_str()));
    let lower_ok = match (min(outer), min(inner)) {
        (None, _) => true,
        (Some(_), None) => false,
        (Some(o), Some(i)) => o <= i,
    };
    let upper_ok = match (max(outer), max(inner)) {
        (None, _) => true,
        (Some(_), None) => false,
        (Some(o), Some(i)) => o >= i,
    };
    lower_ok && upper_ok
}

/// A witness applies when it covers the contract's app, variant class and whole version range.
pub fn witness_applies(w: &Witness, source: &SourceBinding) -> bool {
    if w.applies_to.app_id != source.app_id || w.applies_to.variant_class != source.variant_class {
        return false;
    }
    let all = VersionRange {
        min: None,
        max: None,
    };
    let outer = w.applies_to.version_range.as_ref().unwrap_or(&all);
    let inner = source.version_range.as_ref().unwrap_or(&all);
    range_contains(outer, inner)
}

/// A witness that is synthetic or already revoked never counts towards an executable contract.
pub fn witness_disqualified(w: &Witness) -> bool {
    w.synthetic == Some(true) || w.revoked_at.is_some()
}

/// What a witness demonstrated: a user's statement never counts, and a write needs observed behaviour.
pub fn witness_proves(w: &Witness, write: bool) -> bool {
    match w.kind {
        WitnessKind::FixtureRun | WitnessKind::DifferentialTest | WitnessKind::ProtocolTrace => {
            true
        }
        WitnessKind::DocumentedSchema => !write,
        WitnessKind::UserAssertion => false,
    }
}

/// What a host knows about the moment and place of execution.
#[derive(Debug, Clone)]
pub struct ExecEnv {
    pub now: i64,
    pub runtime_id: String,
    pub source_state: String,
    pub capability_digest: String,
    pub principal_ref: String,
    pub variant_id: String,
}

/// Fail-closed execution check: one fresh, unrevoked, non-synthetic witness must match this exact context.
pub fn authorizes_execution(c: &OperationContract, env: &ExecEnv) -> Result<(), &'static str> {
    if !is_executable(c) {
        return Err("not_executable");
    }
    let mut last = "no_passing_witness";
    for w in &c.witnesses {
        if w.result != WitnessResult::Passed
            || !witness_applies(w, &c.source)
            || !witness_proves(w, is_write(c.effect.class))
        {
            continue;
        }
        let verdict = (|| {
            if w.synthetic == Some(true) {
                return Err("synthetic_witness");
            }
            if w.revoked_at.is_some_and(|r| r.0 <= env.now) {
                return Err("witness_revoked");
            }
            let (Some(rt), Some(eff), Some(state), Some(cap), Some(exp), Some(_)) = (
                &w.runtime_id,
                w.observed_effect,
                &w.source_state,
                &w.capability_digest,
                w.expires_at,
                &w.issuer,
            ) else {
                return Err("witness_context_missing");
            };
            if exp.0 <= env.now {
                return Err("witness_expired");
            }
            if w.authorized_context.principal_ref.as_str() != env.principal_ref {
                return Err("witness_other_principal");
            }
            if w.authorized_context.variant_id.as_str() != env.variant_id {
                return Err("witness_other_variant");
            }
            if rt.as_str() != env.runtime_id {
                return Err("witness_other_environment");
            }
            if cap.as_str() != env.capability_digest {
                return Err("witness_capability_changed");
            }
            if state.as_str() != env.source_state {
                return Err("witness_source_state_changed");
            }
            if danger(eff) > danger(c.effect.class) {
                return Err("witness_effect_understated");
            }
            Ok(())
        })();
        match verdict {
            Ok(()) => return Ok(()),
            Err(reason) => last = reason,
        }
    }
    Err(last)
}

/// True only for a contract that passed every invariant and is in the `active` lifecycle.
pub fn is_executable(c: &OperationContract) -> bool {
    c.lifecycle == Lifecycle::Active && validate_operation(c).is_empty()
}

pub fn validate_operation(c: &OperationContract) -> Vec<Violation> {
    let mut out = Vec::new();
    let class = c.effect.class;
    let write = is_write(class);

    let asserted = c
        .evidence_effects
        .iter()
        .fold(EffectClass::ReadOnly, |acc, a| join_effects(acc, a.effect));
    if danger(asserted) > danger(class) {
        out.push(v(
            "contract.effect_understated",
            "/body/effect/class",
            format!(
                "evidence asserts {}, declared {}",
                asserted.as_str(),
                class.as_str()
            ),
        ));
    }
    let provenance: BTreeSet<&str> = c.provenance.evidence.iter().map(|e| e.as_str()).collect();
    for (i, a) in c.evidence_effects.iter().enumerate() {
        if !provenance.contains(a.evidence.as_str()) {
            out.push(v(
                "contract.evidence_not_in_provenance",
                &format!("/body/evidence_effects/{i}/evidence"),
                a.evidence.as_str(),
            ));
        }
    }

    let idem_ok = match class {
        EffectClass::ReadOnly | EffectClass::IdempotentWrite => {
            c.effect.idempotency == Idempotency::Idempotent
        }
        EffectClass::ConditionalWrite => c.effect.idempotency == Idempotency::Conditional,
        EffectClass::NonIdempotentWrite => c.effect.idempotency == Idempotency::NonIdempotent,
        EffectClass::Destructive => matches!(
            c.effect.idempotency,
            Idempotency::NonIdempotent | Idempotency::Unknown
        ),
        EffectClass::Indeterminate => c.effect.idempotency == Idempotency::Unknown,
    };
    if !idem_ok {
        out.push(v(
            "contract.idempotency_mismatch",
            "/body/effect/idempotency",
            format!("{} with {}", class.as_str(), c.effect.idempotency.as_str()),
        ));
    }
    if danger(class) >= danger(EffectClass::NonIdempotentWrite)
        && c.effect.confirmation == ConfirmationPolicy::None
    {
        out.push(v(
            "contract.confirmation_missing",
            "/body/effect/confirmation",
            class.as_str(),
        ));
    }
    if write {
        if c.authorization.required_scopes.is_empty() {
            out.push(v(
                "contract.write_without_scopes",
                "/body/authorization/required_scopes",
                class.as_str(),
            ));
        }
        if !c.verification.required || c.verification.strategy == VerificationStrategy::None {
            out.push(v(
                "contract.write_without_verification",
                "/body/verification",
                class.as_str(),
            ));
        }
        if c.freshness.before_write == FreshnessRequirement::None {
            out.push(v(
                "contract.write_without_freshness",
                "/body/freshness/before_write",
                class.as_str(),
            ));
        }
        if !c.authorization.requires_source_session {
            out.push(v(
                "contract.write_without_source_session",
                "/body/authorization/requires_source_session",
                class.as_str(),
            ));
        }
    }
    if !c.authorization.requires_vsf_caller {
        out.push(v(
            "contract.caller_auth_disabled",
            "/body/authorization/requires_vsf_caller",
            "",
        ));
    }
    match (c.freshness.before_write, c.freshness.max_age_ms) {
        (FreshnessRequirement::ObservationWithin, None) => out.push(v(
            "contract.freshness_inconsistent",
            "/body/freshness/max_age_ms",
            "missing",
        )),
        (FreshnessRequirement::None | FreshnessRequirement::SourceReadRequired, Some(_)) => {
            out.push(v(
                "contract.freshness_inconsistent",
                "/body/freshness/max_age_ms",
                "unexpected",
            ));
        }
        _ => {}
    }
    for (side, params) in [("inputs", &c.inputs), ("outputs", &c.outputs)] {
        for (name, p) in params {
            let path = format!("/body/{side}/{}", name.as_str());
            match (&p.choices, p.r#type) {
                (Some(_), t) if t != ValueType::Choice => {
                    out.push(v("contract.choices_on_non_choice", &path, t.as_str()))
                }
                (None, ValueType::Choice) => {
                    out.push(v("contract.choice_without_choices", &path, ""))
                }
                (Some(ch), ValueType::Choice) if ch.is_empty() => {
                    out.push(v("contract.choice_without_choices", &path, "empty"))
                }
                _ => {}
            }
            if p.unit.is_some() && !matches!(p.r#type, ValueType::Decimal | ValueType::Integer) {
                out.push(v("contract.unit_on_non_numeric", &path, p.r#type.as_str()));
            }
        }
    }

    for (i, w) in c.witnesses.iter().enumerate() {
        let path = format!("/body/witnesses/{i}");
        let bundle = [
            w.runtime_id.is_some(),
            w.observed_effect.is_some(),
            w.source_state.is_some(),
            w.capability_digest.is_some(),
            w.expires_at.is_some(),
            w.issuer.is_some(),
        ];
        if bundle.iter().any(|b| *b) && !bundle.iter().all(|b| *b) {
            out.push(v("contract.witness_context_incomplete", &path, ""));
        }
        if w.expires_at.is_some_and(|e| e.0 <= w.observed_at.0) {
            out.push(v(
                "contract.witness_expiry_order",
                &format!("{path}/expires_at"),
                "",
            ));
        }
        if w.revoked_at.is_some_and(|r| r.0 < w.observed_at.0) {
            out.push(v(
                "contract.witness_revoked_before_observed",
                &format!("{path}/revoked_at"),
                "",
            ));
        }
        if let Some(e) = w.observed_effect
            && danger(e) > danger(class)
        {
            out.push(v(
                "contract.witness_effect_understated",
                &format!("{path}/observed_effect"),
                format!(
                    "witness observed {}, declared {}",
                    e.as_str(),
                    class.as_str()
                ),
            ));
        }
        if w.synthetic == Some(true)
            && c.lifecycle == Lifecycle::Active
            && w.result == WitnessResult::Passed
        {
            out.push(v("contract.synthetic_witness_on_active", &path, ""));
        }
    }
    if c.lifecycle == Lifecycle::Active {
        if class == EffectClass::Indeterminate {
            out.push(v("contract.active_indeterminate", "/body/effect/class", ""));
        }
        if c.strategies.is_empty() {
            out.push(v("contract.no_strategy", "/body/strategies", ""));
        }
        if c.provenance.evidence.is_empty() {
            out.push(v("contract.no_evidence", "/body/provenance/evidence", ""));
        }
        if c.uncertainty != Certainty::Verified {
            out.push(v(
                "contract.uncertain_active",
                "/body/uncertainty",
                c.uncertainty.as_str(),
            ));
        }
        if c.witnesses
            .iter()
            .any(|w| w.result == WitnessResult::Failed)
        {
            out.push(v("contract.failed_witness", "/body/witnesses", ""));
        }
        let usable = |w: &&Witness| {
            w.result == WitnessResult::Passed
                && witness_applies(w, &c.source)
                && !witness_disqualified(w)
        };
        if !c
            .witnesses
            .iter()
            .filter(usable)
            .any(|w| witness_proves(w, write))
        {
            if c.witnesses.iter().any(|w| usable(&w)) {
                out.push(v(
                    "contract.witness_kind_insufficient",
                    "/body/witnesses",
                    "a user assertion, or a document alone for a write, does not demonstrate behaviour",
                ));
            } else {
                out.push(v(
                    "contract.witness_not_applicable",
                    "/body/witnesses",
                    "no passing witness covers the source binding",
                ));
            }
        }
    }
    out
}

// ---- semantic IR ------------------------------------------------------------------------------

pub fn validate_semantic(s: &SemanticIR) -> Vec<Violation> {
    let mut out = Vec::new();
    let evidence: BTreeMap<&str, &EvidenceDef> =
        s.evidence.iter().map(|e| (e.id.as_str(), e)).collect();
    if evidence.len() != s.evidence.len() {
        out.push(v(
            "ir.duplicate_id",
            "/body/evidence",
            "evidence ids must be unique",
        ));
    }

    // Entities, fields and actions share one namespace because claims address them by bare id.
    let mut subjects: BTreeSet<&str> = BTreeSet::new();
    let note =
        |id: &'_ str, path: String, out: &mut Vec<Violation>, seen: &mut BTreeSet<String>| {
            if !seen.insert(id.to_string()) {
                out.push(v("ir.duplicate_id", &path, id));
            }
        };
    let mut seen: BTreeSet<String> = BTreeSet::new();
    for (i, e) in s.entities.iter().enumerate() {
        note(
            e.id.as_str(),
            format!("/body/entities/{i}/id"),
            &mut out,
            &mut seen,
        );
        subjects.insert(e.id.as_str());
        for (j, f) in e.fields.iter().enumerate() {
            note(
                f.id.as_str(),
                format!("/body/entities/{i}/fields/{j}/id"),
                &mut out,
                &mut seen,
            );
            subjects.insert(f.id.as_str());
        }
    }
    for (i, a) in s.actions.iter().enumerate() {
        note(
            a.id.as_str(),
            format!("/body/actions/{i}/id"),
            &mut out,
            &mut seen,
        );
        subjects.insert(a.id.as_str());
    }
    let entity_ids: BTreeSet<&str> = s.entities.iter().map(|e| e.id.as_str()).collect();

    let check_support =
        |kind: &str, path: &str, certainty: Certainty, refs: &[Ident], out: &mut Vec<Violation>| {
            let mut strong = false;
            for (k, r) in refs.iter().enumerate() {
                match evidence.get(r.as_str()) {
                    None => out.push(v(
                        "semantic.unknown_reference",
                        &format!("{path}/evidence/{k}"),
                        r.as_str(),
                    )),
                    Some(e) => strong |= is_strong(e.r#type),
                }
            }
            match certainty {
                Certainty::Verified if refs.is_empty() => out.push(v(
                    "semantic.unsupported_certainty",
                    path,
                    format!("{kind} is verified without evidence"),
                )),
                Certainty::Verified if !strong => out.push(v(
                    "semantic.overclaimed_certainty",
                    path,
                    format!("{kind} is verified by heuristic or user evidence only"),
                )),
                Certainty::Inferred if refs.is_empty() => out.push(v(
                    "semantic.unsupported_certainty",
                    path,
                    format!("{kind} is inferred without evidence"),
                )),
                _ => {}
            }
        };

    for (i, e) in s.entities.iter().enumerate() {
        check_support(
            "entity",
            &format!("/body/entities/{i}"),
            e.certainty,
            &e.evidence,
            &mut out,
        );
        for (j, f) in e.fields.iter().enumerate() {
            check_support(
                "field",
                &format!("/body/entities/{i}/fields/{j}"),
                f.certainty,
                &f.evidence,
                &mut out,
            );
        }
    }
    for (i, r) in s.relationships.iter().enumerate() {
        check_support(
            "relationship",
            &format!("/body/relationships/{i}"),
            r.certainty,
            &r.evidence,
            &mut out,
        );
        for (end, id) in [("from", &r.from), ("to", &r.to)] {
            if !entity_ids.contains(id.as_str()) {
                out.push(v(
                    "semantic.unknown_reference",
                    &format!("/body/relationships/{i}/{end}"),
                    id.as_str(),
                ));
            }
        }
    }

    let mut declared_effect: BTreeMap<&str, EffectClass> = BTreeMap::new();
    for (i, a) in s.actions.iter().enumerate() {
        let path = format!("/body/actions/{i}");
        check_support("action", &path, a.certainty, &a.evidence, &mut out);
        if let Some(en) = &a.entity
            && !entity_ids.contains(en.as_str())
        {
            out.push(v(
                "semantic.unknown_reference",
                &format!("{path}/entity"),
                en.as_str(),
            ));
        }
        let mut asserted = EffectClass::ReadOnly;
        for (k, x) in a.effect.asserted.iter().enumerate() {
            match evidence.get(x.evidence.as_str()) {
                None => out.push(v(
                    "semantic.unknown_reference",
                    &format!("{path}/effect/asserted/{k}/evidence"),
                    x.evidence.as_str(),
                )),
                Some(_) => asserted = join_effects(asserted, x.effect),
            }
        }
        if danger(asserted) > danger(a.effect.declared) {
            out.push(v(
                "semantic.effect_understated",
                &format!("{path}/effect/declared"),
                format!("evidence asserts {}", asserted.as_str()),
            ));
        }
        if a.certainty == Certainty::Verified {
            if a.effect.declared == EffectClass::Indeterminate {
                out.push(v(
                    "semantic.verified_indeterminate",
                    &format!("{path}/effect/declared"),
                    "",
                ));
            }
            if is_write(a.effect.declared)
                && !a.evidence.iter().any(|r| {
                    evidence
                        .get(r.as_str())
                        .is_some_and(|e| proves_behavior(e.r#type))
                })
            {
                out.push(v(
                    "semantic.effect_without_behavioral_evidence",
                    &path,
                    a.effect.declared.as_str(),
                ));
            }
        }
        declared_effect.insert(a.id.as_str(), a.effect.declared);
    }

    // Evidence dependencies must resolve and form a DAG.
    for (i, e) in s.evidence.iter().enumerate() {
        for (k, d) in e.depends_on.iter().enumerate() {
            if !evidence.contains_key(d.as_str()) {
                out.push(v(
                    "semantic.unknown_reference",
                    &format!("/body/evidence/{i}/depends_on/{k}"),
                    d.as_str(),
                ));
            }
        }
    }
    let mut state: BTreeMap<&str, u8> = BTreeMap::new();
    fn visit<'a>(
        id: &'a str,
        evidence: &BTreeMap<&'a str, &'a EvidenceDef>,
        state: &mut BTreeMap<&'a str, u8>,
    ) -> bool {
        match state.get(id) {
            Some(2) => return false,
            Some(1) => return true,
            _ => {}
        }
        state.insert(id, 1);
        let mut cyclic = false;
        if let Some(e) = evidence.get(id) {
            for d in &e.depends_on {
                cyclic |= visit(d.as_str(), evidence, state);
            }
        }
        state.insert(id, 2);
        cyclic
    }
    for id in evidence.keys() {
        if visit(id, &evidence, &mut state) {
            out.push(v("semantic.evidence_cycle", "/body/evidence", *id));
            break;
        }
    }

    for (i, m) in s.state_machines.iter().enumerate() {
        let path = format!("/body/state_machines/{i}");
        if !entity_ids.contains(m.entity.as_str()) {
            out.push(v(
                "semantic.unknown_reference",
                &format!("{path}/entity"),
                m.entity.as_str(),
            ));
        }
        let states: BTreeSet<&str> = m.states.iter().map(|x| x.id.as_str()).collect();
        if states.len() != m.states.len() {
            out.push(v("ir.duplicate_id", &format!("{path}/states"), ""));
        }
        if let Some(init) = &m.initial
            && !states.contains(init.as_str())
        {
            out.push(v(
                "semantic.unknown_reference",
                &format!("{path}/initial"),
                init.as_str(),
            ));
        }
        for (k, t) in m.transitions.iter().enumerate() {
            for (end, id) in [("from", &t.from), ("to", &t.to)] {
                if !states.contains(id.as_str()) {
                    out.push(v(
                        "semantic.unknown_reference",
                        &format!("{path}/transitions/{k}/{end}"),
                        id.as_str(),
                    ));
                }
            }
            if let Some(a) = &t.action
                && !s.actions.iter().any(|x| x.id == *a)
            {
                out.push(v(
                    "semantic.unknown_reference",
                    &format!("{path}/transitions/{k}/action"),
                    a.as_str(),
                ));
            }
        }
    }

    let mut claim_ids: BTreeSet<&str> = BTreeSet::new();
    let mut by_key: BTreeMap<(&str, &str), Vec<(&Claim, usize)>> = BTreeMap::new();
    for (i, c) in s.claims.iter().enumerate() {
        let path = format!("/body/claims/{i}");
        if !claim_ids.insert(c.id.as_str()) {
            out.push(v("ir.duplicate_id", &format!("{path}/id"), c.id.as_str()));
        }
        if !subjects.contains(c.subject.as_str()) {
            out.push(v(
                "semantic.unknown_reference",
                &format!("{path}/subject"),
                c.subject.as_str(),
            ));
        }
        for (side, refs) in [("supports", &c.supports), ("contradicts", &c.contradicts)] {
            for (k, r) in refs.iter().enumerate() {
                if !evidence.contains_key(r.as_str()) {
                    out.push(v(
                        "semantic.unknown_reference",
                        &format!("{path}/{side}/{k}"),
                        r.as_str(),
                    ));
                }
            }
        }
        if c.supports.iter().any(|x| c.contradicts.contains(x)) {
            out.push(v(
                "semantic.claim_overlap",
                &path,
                "the same evidence both supports and contradicts",
            ));
        }
        let strong_support = c.supports.iter().any(|r| {
            evidence
                .get(r.as_str())
                .is_some_and(|e| is_strong(e.r#type))
        });
        match c.certainty {
            Certainty::Verified => {
                if !c.contradicts.is_empty() {
                    out.push(v(
                        "semantic.contradiction_hidden",
                        &path,
                        "verified despite contradicting evidence",
                    ));
                }
                if c.supports.is_empty() {
                    out.push(v(
                        "semantic.unsupported_certainty",
                        &path,
                        "verified without support",
                    ));
                } else if !strong_support {
                    out.push(v(
                        "semantic.overclaimed_certainty",
                        &path,
                        "verified by heuristic or user evidence only",
                    ));
                }
            }
            Certainty::Inferred => {
                if !c.contradicts.is_empty() {
                    out.push(v(
                        "semantic.contradiction_hidden",
                        &path,
                        "inferred despite contradicting evidence",
                    ));
                }
                if c.supports.is_empty() {
                    out.push(v(
                        "semantic.unsupported_certainty",
                        &path,
                        "inferred without support",
                    ));
                }
            }
            Certainty::Conflicted if c.supports.is_empty() || c.contradicts.is_empty() => {
                out.push(v("semantic.conflict_without_contradiction", &path, ""));
            }
            _ => {}
        }
        let ok = match (c.predicate, &c.value) {
            (
                ClaimPredicate::Exists | ClaimPredicate::IsRequired | ClaimPredicate::IsEditable,
                ClaimValue::Bool(_),
            ) => true,
            (ClaimPredicate::HasType, ClaimValue::ValueType(_)) => true,
            (ClaimPredicate::HasEffect, ClaimValue::Effect(_)) => true,
            (ClaimPredicate::HasUnit | ClaimPredicate::HasLabel, ClaimValue::Text(_)) => true,
            (ClaimPredicate::HasValue | ClaimPredicate::HasDefault, x) => {
                !matches!(x, ClaimValue::Effect(_) | ClaimValue::ValueType(_))
            }
            _ => false,
        };
        if !ok {
            out.push(v(
                "semantic.claim_type_mismatch",
                &format!("{path}/value"),
                format!("{} with {}", c.predicate.as_str(), c.value.tag()),
            ));
        }
        if let (ClaimPredicate::HasEffect, ClaimValue::Effect(e)) = (c.predicate, &c.value) {
            if c.certainty == Certainty::Verified
                && !c.supports.iter().any(|r| {
                    evidence
                        .get(r.as_str())
                        .is_some_and(|ev| proves_behavior(ev.r#type))
                })
            {
                out.push(v(
                    "semantic.effect_without_behavioral_evidence",
                    &path,
                    e.value.as_str(),
                ));
            }
            if let Some(declared) = declared_effect.get(c.subject.as_str())
                && danger(e.value) > danger(*declared)
            {
                out.push(v(
                    "semantic.effect_understated",
                    &path,
                    format!(
                        "claim says {}, action declares {}",
                        e.value.as_str(),
                        declared.as_str()
                    ),
                ));
            }
        }
        by_key
            .entry((c.subject.as_str(), predicate_name(c.predicate)))
            .or_default()
            .push((c, i));
    }
    for ((subject, pred), claims) in &by_key {
        let live: Vec<_> = claims
            .iter()
            .filter(|(c, _)| matches!(c.certainty, Certainty::Verified | Certainty::Inferred))
            .collect();
        if let Some(first) = live.first()
            && live.iter().any(|(c, _)| c.value != first.0.value)
        {
            out.push(v(
                "semantic.unresolved_conflict",
                &format!("/body/claims/{}", first.1),
                format!("{subject} {pred}"),
            ));
        }
    }
    out
}

fn predicate_name(p: ClaimPredicate) -> &'static str {
    p.as_str()
}

// ---- remaining families -----------------------------------------------------------------------

pub fn validate_environment(m: &EnvironmentCapabilityManifest) -> Vec<Violation> {
    let mut out = Vec::new();
    for (side, list) in [("observation", &m.observation), ("execution", &m.execution)] {
        let mut seen = BTreeSet::new();
        for (i, c) in list.iter().enumerate() {
            if !seen.insert(c.id.as_str()) {
                out.push(v(
                    "ir.duplicate_id",
                    &format!("/body/{side}/{i}/id"),
                    c.id.as_str(),
                ));
            }
        }
    }
    for (i, g) in m.permissions.iter().enumerate() {
        if !origin_canonical(g.origin.as_str()) {
            out.push(v(
                "ir.origin_not_canonical",
                &format!("/body/permissions/{i}/origin"),
                g.origin.as_str(),
            ));
        }
    }
    if m.variant_id.is_some() && m.app_id.is_none() {
        out.push(v(
            "environment.variant_without_app",
            "/body/variant_id",
            "a variant is only meaningful for a named application",
        ));
    }
    let hosted = matches!(
        m.mode,
        ExecutionMode::HostedSandbox | ExecutionMode::HeadlessWorker | ExecutionMode::PrivateVpc
    );
    if hosted && m.credential_isolation == CredentialIsolation::BrowserProfileBound {
        out.push(v(
            "environment.portable_credentials",
            "/body/credential_isolation",
            "browser profile credentials do not move between environments",
        ));
    }
    if m.mode == ExecutionMode::Extension && m.persistence == PersistenceLevel::Managed {
        out.push(v(
            "environment.persistence_unsupported",
            "/body/persistence",
            "extensions have no managed persistence",
        ));
    }
    if m.mode == ExecutionMode::PrivateVpc && m.egress == EgressPolicy::Unrestricted {
        out.push(v(
            "environment.private_egress_unrestricted",
            "/body/egress",
            "",
        ));
    }
    for (name, n) in [
        ("max_payload_bytes", m.limits.max_payload_bytes.0),
        ("max_nodes", m.limits.max_nodes.0),
        ("max_concurrency", m.limits.max_concurrency.0),
        ("max_wallclock_ms", m.limits.max_wallclock_ms.0),
    ] {
        if n == 0 {
            out.push(v(
                "environment.zero_limit",
                &format!("/body/limits/{name}"),
                "",
            ));
        }
    }
    if let Some(a) = &m.attestation {
        let has = a.digest.is_some();
        if (a.kind == AttestationKind::None) == has {
            out.push(v(
                "environment.attestation_inconsistent",
                "/body/attestation",
                "",
            ));
        }
    }
    out
}

pub fn validate_observation(o: &ObservationIR) -> Vec<Violation> {
    let mut out = Vec::new();
    if !origin_canonical(o.origin.as_str()) {
        out.push(v(
            "ir.origin_not_canonical",
            "/body/origin",
            o.origin.as_str(),
        ));
    }
    match (o.mode, o.base_epoch) {
        (ObservationMode::Delta, None) => {
            out.push(v("observation.delta_without_base", "/body/base_epoch", ""))
        }
        (ObservationMode::Delta, Some(b)) if b.0 >= o.observation_epoch.0 => {
            out.push(v("observation.base_not_earlier", "/body/base_epoch", ""))
        }
        (ObservationMode::Full, Some(_)) => {
            out.push(v("observation.full_with_base", "/body/base_epoch", ""))
        }
        _ => {}
    }
    let has_variant = o.source_variant_id.is_some();
    match (o.variant_resolution, has_variant) {
        (Certainty::Verified | Certainty::Inferred, false) => out.push(v(
            "observation.variant_missing",
            "/body/source_variant_id",
            "",
        )),
        (Certainty::Unknown | Certainty::Conflicted, true) => out.push(v(
            "observation.variant_unexpected",
            "/body/source_variant_id",
            "",
        )),
        _ => {}
    }
    if (o.payload.bytes.0) < 24 * o.payload.nodes.0 {
        out.push(v(
            "observation.payload_inconsistent",
            "/body/payload",
            "fewer bytes than 24 per node",
        ));
    }
    let mut seen = BTreeSet::new();
    for (i, m) in o.missing_signals.iter().enumerate() {
        if !seen.insert(m.signal.as_str()) {
            out.push(v(
                "ir.duplicate_id",
                &format!("/body/missing_signals/{i}"),
                m.signal.as_str(),
            ));
        }
    }
    out
}

fn is_interactive(k: ComponentKind) -> bool {
    matches!(
        k,
        ComponentKind::Button
            | ComponentKind::Link
            | ComponentKind::TextInput
            | ComponentKind::Choice
            | ComponentKind::Checkbox
    )
}

pub fn validate_presentation(p: &PresentationIR, semantic: Option<&SemanticIR>) -> Vec<Violation> {
    let mut out = Vec::new();
    let ids: BTreeSet<&str> = p.components.iter().map(|c| c.id.as_str()).collect();
    if ids.len() != p.components.len() {
        out.push(v("ir.duplicate_id", "/body/components", ""));
    }
    let mut parent: BTreeMap<&str, &str> = BTreeMap::new();
    for (i, c) in p.components.iter().enumerate() {
        let path = format!("/body/components/{i}");
        for (k, ch) in c.children.iter().enumerate() {
            if !ids.contains(ch.as_str()) {
                out.push(v(
                    "presentation.unknown_child",
                    &format!("{path}/children/{k}"),
                    ch.as_str(),
                ));
            } else if parent.insert(ch.as_str(), c.id.as_str()).is_some() {
                out.push(v(
                    "presentation.multiple_parents",
                    &format!("{path}/children/{k}"),
                    ch.as_str(),
                ));
            }
        }
        if is_interactive(c.kind)
            && c.accessibility.name_source == NameSource::Explicit
            && c.accessibility.name.is_none()
        {
            out.push(v(
                "presentation.unnamed_interactive",
                &format!("{path}/accessibility/name"),
                c.id.as_str(),
            ));
        }
        if c.kind == ComponentKind::PreservedSurface && c.accessibility.name.is_none() {
            out.push(v(
                "presentation.unnamed_interactive",
                &format!("{path}/accessibility/name"),
                "preserved surface needs a name",
            ));
        }
        if let Some(b) = &c.binding
            && b.action.is_some()
            && !matches!(
                c.kind,
                ComponentKind::Button | ComponentKind::Link | ComponentKind::Form
            )
        {
            out.push(v(
                "presentation.action_binding_kind",
                &format!("{path}/binding/action"),
                c.id.as_str(),
            ));
        }
    }
    // Every component must hang under a region or another component, and the child graph must be acyclic.
    let mut in_region: BTreeSet<&str> = BTreeSet::new();
    for (i, r) in p.regions.iter().enumerate() {
        for (k, c) in r.components.iter().enumerate() {
            if !ids.contains(c.as_str()) {
                out.push(v(
                    "presentation.unknown_child",
                    &format!("/body/regions/{i}/components/{k}"),
                    c.as_str(),
                ));
            }
            in_region.insert(c.as_str());
        }
    }
    for c in &p.components {
        let id = c.id.as_str();
        let mut cur = id;
        let mut hops = 0;
        while let Some(par) = parent.get(cur) {
            cur = par;
            hops += 1;
            if hops > p.components.len() {
                out.push(v("presentation.cycle", "/body/components", id));
                break;
            }
        }
        if hops <= p.components.len() && !in_region.contains(cur) {
            out.push(v("presentation.orphan_component", "/body/components", id));
        }
    }
    for (i, c) in p.constraints.iter().enumerate() {
        if matches!(
            c.kind,
            ConstraintKind::MinContrast | ConstraintKind::MinTargetSize
        ) && c.value.is_none()
        {
            out.push(v(
                "presentation.constraint_without_value",
                &format!("/body/constraints/{i}"),
                c.id.as_str(),
            ));
        }
        for (k, s) in c.subjects.iter().enumerate() {
            if !ids.contains(s.as_str()) {
                out.push(v(
                    "presentation.unknown_child",
                    &format!("/body/constraints/{i}/subjects/{k}"),
                    s.as_str(),
                ));
            }
        }
    }
    validate_layout(p, semantic, &mut out);
    if let Some(sem) = semantic {
        for (i, c) in p.components.iter().enumerate() {
            let Some(b) = &c.binding else { continue };
            let path = format!("/body/components/{i}/binding");
            let entity = b
                .entity
                .as_ref()
                .and_then(|e| sem.entities.iter().find(|x| x.id == *e));
            if b.entity.is_some() && entity.is_none() {
                out.push(v(
                    "presentation.binding_unresolved",
                    &format!("{path}/entity"),
                    "",
                ));
            }
            if let (Some(f), Some(en)) = (&b.field, entity)
                && !en.fields.iter().any(|x| x.id == *f)
            {
                out.push(v(
                    "presentation.binding_unresolved",
                    &format!("{path}/field"),
                    f.as_str(),
                ));
            }
            if let Some(a) = &b.action
                && !sem.actions.iter().any(|x| x.id == *a)
            {
                out.push(v(
                    "presentation.binding_unresolved",
                    &format!("{path}/action"),
                    a.as_str(),
                ));
            }
        }
    }
    out
}

/// Region layout: structure only, so a layout can never move, hide or merge an operation out of its confirmation context.
fn validate_layout(p: &PresentationIR, semantic: Option<&SemanticIR>, out: &mut Vec<Violation>) {
    let index: BTreeMap<&str, usize> = p
        .regions
        .iter()
        .enumerate()
        .map(|(i, r)| (r.id.as_str(), i))
        .collect();
    if index.len() != p.regions.len() {
        out.push(v("ir.duplicate_id", "/body/regions", ""));
    }
    let mut kids: BTreeMap<&str, usize> = BTreeMap::new();
    let mut mains: BTreeMap<&str, usize> = BTreeMap::new();
    for (i, r) in p.regions.iter().enumerate() {
        let path = format!("/body/regions/{i}");
        let parent = r.parent.as_ref().map(|x| x.as_str());
        match parent {
            Some(par) if par == r.id.as_str() => {
                out.push(v(
                    "presentation.region_cycle",
                    &format!("{path}/parent"),
                    par,
                ));
            }
            Some(par) if !index.contains_key(par) => {
                out.push(v(
                    "presentation.region_unknown_parent",
                    &format!("{path}/parent"),
                    par,
                ));
            }
            Some(par) => *kids.entry(par).or_default() += 1,
            None => {}
        }
        if r.role == Some(RegionRole::Main) {
            *mains.entry(parent.unwrap_or("")).or_default() += 1;
        }
        if let Some(sz) = &r.size {
            let bad = sz.max.as_ref().is_some_and(|m| m.0 < sz.min.0)
                || sz
                    .basis
                    .as_ref()
                    .is_some_and(|b| b.0 < sz.min.0 || sz.max.as_ref().is_some_and(|m| b.0 > m.0));
            if bad {
                out.push(v(
                    "presentation.region_size",
                    &format!("{path}/size"),
                    r.id.as_str(),
                ));
            }
        }
        if r.dock.is_some()
            && !matches!(
                r.role,
                Some(
                    RegionRole::Navigation
                        | RegionRole::Aside
                        | RegionRole::Toolbar
                        | RegionRole::Header
                        | RegionRole::Footer
                )
            )
        {
            out.push(v(
                "presentation.region_dock_role",
                &format!("{path}/dock"),
                r.id.as_str(),
            ));
        }
        let mut last = u32::MAX as i64 + 1;
        for (k, rs) in r.responsive.iter().flatten().enumerate() {
            if rs.below.0 >= last {
                out.push(v(
                    "presentation.responsive_order",
                    &format!("{path}/responsive/{k}"),
                    r.id.as_str(),
                ));
            }
            last = rs.below.0;
        }
    }
    // The parent chain of every region must end at a root.
    for (i, r) in p.regions.iter().enumerate() {
        let mut cur = r.parent.as_ref().map(|x| x.as_str());
        let mut hops = 0;
        while let Some(par) = cur {
            hops += 1;
            if hops > p.regions.len() {
                out.push(v(
                    "presentation.region_cycle",
                    &format!("/body/regions/{i}/parent"),
                    r.id.as_str(),
                ));
                break;
            }
            cur = index
                .get(par)
                .and_then(|j| p.regions[*j].parent.as_ref().map(|x| x.as_str()));
        }
    }
    if mains.values().any(|n| *n > 1) {
        out.push(v(
            "presentation.region_main_count",
            "/body/regions",
            "one main region per level",
        ));
    }
    for (i, r) in p.regions.iter().enumerate() {
        let needs_two = |a: Arrange| matches!(a, Arrange::Split | Arrange::Tabs);
        let parts = kids.get(r.id.as_str()).copied().unwrap_or(0);
        if r.arrange.is_some_and(needs_two) && parts < 2 {
            out.push(v(
                "presentation.region_arrange_empty",
                &format!("/body/regions/{i}/arrange"),
                r.id.as_str(),
            ));
        }
        if r.responsive
            .iter()
            .flatten()
            .any(|rs| needs_two(rs.arrange))
            && parts < 2
        {
            out.push(v(
                "presentation.region_arrange_empty",
                &format!("/body/regions/{i}/responsive"),
                r.id.as_str(),
            ));
        }
    }
    let Some(sem) = semantic else { return };
    // A region a person or a narrow screen can hide must not hold an operation that needs to stay in view.
    let comp_index: BTreeMap<&str, &Component> =
        p.components.iter().map(|c| (c.id.as_str(), c)).collect();
    for (i, r) in p.regions.iter().enumerate() {
        let hideable = r.dock.as_ref().is_some_and(|d| d.collapsible)
            || r.responsive.iter().flatten().any(|rs| rs.collapse);
        if !hideable {
            continue;
        }
        let mut stack: Vec<&str> = r.components.iter().map(|x| x.as_str()).collect();
        let mut seen = BTreeSet::new();
        while let Some(id) = stack.pop() {
            if !seen.insert(id) {
                continue;
            }
            let Some(c) = comp_index.get(id) else {
                continue;
            };
            stack.extend(c.children.iter().map(|x| x.as_str()));
            let Some(a) = c.binding.as_ref().and_then(|b| b.action.as_ref()) else {
                continue;
            };
            let risky = sem.actions.iter().find(|x| x.id == *a).is_none_or(|d| {
                !matches!(
                    d.effect.declared,
                    EffectClass::ReadOnly | EffectClass::IdempotentWrite
                )
            });
            if risky {
                out.push(v(
                    "presentation.hideable_holds_risky_action",
                    &format!("/body/regions/{i}"),
                    a.as_str(),
                ));
            }
        }
    }
}

/// Private evidence stays private, revocations stay effective, and each kind of rollback restores only what it can.
fn validate_isolation(m: &VersionManifest, lineage: &BTreeSet<&str>, out: &mut Vec<Violation>) {
    let revoked: BTreeSet<&str> = m
        .revocations
        .iter()
        .flatten()
        .filter(|r| r.kind == RevocationKind::Artifact)
        .filter_map(|r| r.digest.as_ref().map(|d| d.as_str()))
        .collect();
    for (i, a) in m.artifacts.iter().enumerate() {
        let path = format!("/body/artifacts/{i}");
        match (a.visibility, &a.principal_ref) {
            (Some(Visibility::GlobalStructure), Some(_)) => {
                out.push(v(
                    "version.principal_on_shared",
                    &format!("{path}/principal_ref"),
                    "",
                ));
            }
            (Some(Visibility::PrivatePrincipal), None) => {
                out.push(v(
                    "version.private_without_owner",
                    &format!("{path}/principal_ref"),
                    "",
                ));
            }
            (Some(Visibility::PrivateVariant), Some(_)) => {
                out.push(v(
                    "version.principal_on_shared",
                    &format!("{path}/principal_ref"),
                    "",
                ));
            }
            (None, Some(_)) => {
                out.push(v(
                    "version.owner_without_visibility",
                    &format!("{path}/visibility"),
                    "",
                ));
            }
            _ => {}
        }
        if a.visibility == Some(Visibility::GlobalStructure)
            && matches!(
                a.role,
                ArtifactRole::Observation | ArtifactRole::OperationContract
            )
        {
            out.push(v(
                "version.private_artifact_marked_global",
                &format!("{path}/visibility"),
                format!("{:?}", a.role),
            ));
        }
        if revoked.contains(a.digest.as_str()) {
            out.push(v(
                "version.revoked_artifact_present",
                &path,
                a.digest.as_str(),
            ));
        }
    }
    for (i, r) in m.revocations.iter().flatten().enumerate() {
        let path = format!("/body/revocations/{i}");
        let (op, digest, authority, principal) = (
            r.operation.is_some(),
            r.digest.is_some(),
            r.authority.is_some(),
            r.principal.is_some(),
        );
        let ok = match r.kind {
            RevocationKind::Operation => {
                (op, digest, authority, principal) == (true, false, false, false)
            }
            RevocationKind::Artifact | RevocationKind::Witness => {
                (op, digest, authority, principal) == (false, true, false, false)
            }
            RevocationKind::TrustAuthority => {
                (op, digest, authority, principal) == (false, false, true, false)
            }
            RevocationKind::PrincipalGrant => {
                (op, digest, authority, principal) == (false, false, false, true)
            }
        };
        if !ok {
            out.push(v("version.revocation_malformed", &path, r.kind.as_str()));
        }
    }
    if let Some(rb) = &m.rollback {
        let path = "/body/rollback";
        let t = rb.target.as_str();
        if revoked.contains(t) {
            out.push(v(
                "version.rollback_to_revoked",
                &format!("{path}/target"),
                t,
            ));
        }
        let roles: BTreeSet<ArtifactRole> = rb.roles.iter().copied().collect();
        if roles.len() != rb.roles.len() {
            out.push(v("ir.duplicate_id", &format!("{path}/roles"), ""));
        }
        match rb.kind {
            RollbackKind::ArtifactVersion => {
                if roles.is_empty() {
                    out.push(v(
                        "version.rollback_scope",
                        &format!("{path}/roles"),
                        "artifact_version needs roles",
                    ));
                }
                if !lineage.contains(t) {
                    out.push(v(
                        "version.rollback_target_not_in_lineage",
                        &format!("{path}/target"),
                        t,
                    ));
                }
            }
            RollbackKind::Interface => {
                if roles.iter().any(|r| *r != ArtifactRole::Presentation) || roles.is_empty() {
                    out.push(v(
                        "version.rollback_scope",
                        &format!("{path}/roles"),
                        "an interface rollback touches the presentation only",
                    ));
                }
                if !lineage.contains(t) {
                    out.push(v(
                        "version.rollback_target_not_in_lineage",
                        &format!("{path}/target"),
                        t,
                    ));
                }
            }
            RollbackKind::SourceData => {
                if !roles.is_empty() {
                    out.push(v(
                        "version.rollback_scope",
                        &format!("{path}/roles"),
                        "a source-data rollback changes no VSF artifact",
                    ));
                }
            }
        }
    }
}

pub fn validate_version(m: &VersionManifest) -> Vec<Violation> {
    let mut out = Vec::new();
    let mut lineage = BTreeSet::new();
    for (i, d) in m.lineage.iter().enumerate() {
        if !lineage.insert(d.as_str()) {
            out.push(v(
                "ir.duplicate_id",
                &format!("/body/lineage/{i}"),
                d.as_str(),
            ));
        }
    }
    let mut artifacts = BTreeSet::new();
    for (i, a) in m.artifacts.iter().enumerate() {
        if !artifacts.insert((a.role, a.digest.as_str())) {
            out.push(v(
                "ir.duplicate_id",
                &format!("/body/artifacts/{i}"),
                a.digest.as_str(),
            ));
        }
    }
    if m.retention.legal_hold && m.retention.expires_at.is_some() {
        out.push(v(
            "version.legal_hold_expiry",
            "/body/retention/expires_at",
            "a legal hold cannot expire",
        ));
    }
    if m.retention.pinned && !m.retention.immutable {
        out.push(v("version.pinned_mutable", "/body/retention/immutable", ""));
    }
    if matches!(m.channel, Channel::Pinned) && !m.retention.pinned {
        out.push(v(
            "version.channel_pin_mismatch",
            "/body/retention/pinned",
            "",
        ));
    }
    if matches!(m.channel, Channel::Stable | Channel::Pinned) {
        match &m.promotion {
            None => out.push(v(
                "version.unpromoted",
                "/body/promotion",
                m.channel.as_str(),
            )),
            Some(p) => {
                if p.gates.is_empty() || p.gates.iter().any(|g| !g.passed) {
                    out.push(v("version.failed_gate", "/body/promotion/gates", ""));
                }
                if p.sequence.is_some() != p.not_after.is_some()
                    || (p.sequence.is_some() && p.attestation.is_none())
                {
                    out.push(v(
                        "version.freshness_incomplete",
                        "/body/promotion/sequence",
                        "sequence and not_after come together and only on a signed promotion",
                    ));
                }
                if let Some(end) = &p.not_after
                    && end.0 <= p.promoted_at.0
                {
                    out.push(v(
                        "version.freshness_window",
                        "/body/promotion/not_after",
                        "",
                    ));
                }
                if let Some(prev) = &p.previous
                    && !lineage.contains(prev.as_str())
                {
                    out.push(v(
                        "version.previous_not_in_lineage",
                        "/body/promotion/previous",
                        "",
                    ));
                }
            }
        }
    }
    validate_isolation(m, &lineage, &mut out);
    if let Some(o) = &m.origin
        && !origin_canonical(o.as_str())
    {
        out.push(v("ir.origin_not_canonical", "/body/origin", o.as_str()));
    }
    let trust_fields = m.origin.is_some()
        || m.rollback.is_some()
        || m.revocations.is_some()
        || m.artifacts
            .iter()
            .any(|a| a.visibility.is_some() || a.principal_ref.is_some());
    if trust_fields
        && matches!(m.channel, Channel::Stable | Channel::Pinned)
        && m.promotion
            .as_ref()
            .is_some_and(|p| p.attestation.is_none())
    {
        out.push(v(
            "version.unattested_promotion",
            "/body/promotion/attestation",
            "a promoted manifest that carries trust fields must name its signature",
        ));
    }
    let mut edges = BTreeSet::new();
    for (i, e) in m.migrations.iter().enumerate() {
        let path = format!("/body/migrations/{i}");
        let (Some(from), Some(to)) = (parse_version(e.from.as_str()), parse_version(e.to.as_str()))
        else {
            out.push(v("version.bad_migration", &path, ""));
            continue;
        };
        let bad = if e.kind == MigrationKind::Identity {
            from != to
        } else {
            from >= to
        };
        if bad {
            out.push(v(
                "version.bad_migration",
                &path,
                "migrations must move forward; identity edges stay put",
            ));
        }
        if !edges.insert((from, to)) {
            out.push(v("ir.duplicate_id", &path, ""));
        }
    }
    out
}

/// Runs the invariants for whichever family the document holds.
pub fn validate(body: &Body) -> Vec<Violation> {
    match body {
        Body::Environment(x) => validate_environment(x),
        Body::Observation(x) => validate_observation(x),
        Body::Semantic(x) => validate_semantic(x),
        Body::Operation(x) => validate_operation(x),
        Body::Presentation(x) => validate_presentation(x, None),
        Body::Version(x) => validate_version(x),
    }
}

pub fn violation_values(list: &[Violation]) -> Vec<Value> {
    list.iter()
        .map(|x| {
            Value::Object(vec![
                ("code".to_string(), Value::Str(x.code.to_string())),
                ("detail".to_string(), Value::Str(x.detail.clone())),
                ("path".to_string(), Value::Str(x.path.clone())),
            ])
        })
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    const EFFECTS: [EffectClass; 6] = [
        EffectClass::ReadOnly,
        EffectClass::IdempotentWrite,
        EffectClass::ConditionalWrite,
        EffectClass::NonIdempotentWrite,
        EffectClass::Destructive,
        EffectClass::Indeterminate,
    ];
    const CERTAINTIES: [Certainty; 4] = [
        Certainty::Conflicted,
        Certainty::Unknown,
        Certainty::Inferred,
        Certainty::Verified,
    ];

    #[test]
    fn effect_join_is_a_commutative_associative_idempotent_maximum() {
        for a in EFFECTS {
            assert_eq!(join_effects(a, a), a);
            for b in EFFECTS {
                assert_eq!(join_effects(a, b), join_effects(b, a));
                assert!(danger(join_effects(a, b)) >= danger(a).max(danger(b)));
                for c in EFFECTS {
                    assert_eq!(
                        join_effects(join_effects(a, b), c),
                        join_effects(a, join_effects(b, c))
                    );
                }
            }
        }
    }

    #[test]
    fn indeterminate_absorbs_and_is_the_default_for_the_unproven() {
        assert_eq!(CONSERVATIVE_EFFECT, EffectClass::Indeterminate);
        for e in EFFECTS {
            assert_eq!(
                join_effects(e, EffectClass::Indeterminate),
                EffectClass::Indeterminate
            );
            assert_eq!(join_effects(EffectClass::ReadOnly, e), e);
        }
        let ranks: Vec<u8> = EFFECTS.iter().map(|e| danger(*e)).collect();
        assert_eq!(ranks, [0, 1, 2, 3, 4, 5]);
        assert!(!is_write(EffectClass::ReadOnly) && EFFECTS[1..].iter().all(|e| is_write(*e)));
    }

    #[test]
    fn certainty_combines_to_the_weakest_premise() {
        for a in CERTAINTIES {
            assert_eq!(combine_certainty(a, a), a);
            for b in CERTAINTIES {
                assert_eq!(combine_certainty(a, b), combine_certainty(b, a));
                assert!(strength(combine_certainty(a, b)) <= strength(a).min(strength(b)));
            }
            assert_eq!(combine_certainty(a, Certainty::Verified), a);
        }
        assert_eq!(
            combine_certainty(Certainty::Inferred, Certainty::Unknown),
            Certainty::Unknown
        );
    }

    #[test]
    fn only_behavioral_evidence_proves_what_an_action_does() {
        assert!(proves_behavior(EvidenceType::RuntimeBehavior));
        assert!(!proves_behavior(EvidenceType::ObservedStructure));
        assert!(
            !is_strong(EvidenceType::HeuristicInference) && !is_strong(EvidenceType::UserAssertion)
        );
        assert!(is_strong(EvidenceType::ObservedStructure));
    }
}
