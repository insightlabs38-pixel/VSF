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
        if !c
            .witnesses
            .iter()
            .any(|w| w.result == WitnessResult::Passed && witness_applies(w, &c.source))
        {
            out.push(v(
                "contract.witness_not_applicable",
                "/body/witnesses",
                "no passing witness covers the source binding",
            ));
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
