//! Resolves a pinned manifest against the host and the source it is meeting now, and says what it may do.

use std::collections::{BTreeMap, BTreeSet};
use vsf_ir::matrix::{
    AxisResult, ObservedContext, Pins, Support, axis_text, evaluate, evaluate_context, severity,
};
use vsf_ir::{CompatStatus, VersionManifest};

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum State {
    Compatible,
    Degraded,
    Blocked,
}

impl State {
    pub fn as_str(self) -> &'static str {
        match self {
            State::Compatible => "compatible",
            State::Degraded => "degraded",
            State::Blocked => "blocked",
        }
    }
}

/// Revocations published after a manifest was made; they outrank every pin.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct RevocationSet {
    pub manifests: BTreeSet<String>,
    pub artifacts: BTreeSet<String>,
    /// `operation@version`, or `operation@*` for every version.
    pub operations: BTreeSet<String>,
    pub policies: BTreeSet<i64>,
}

impl RevocationSet {
    pub fn hit(&self, id: &str, m: &VersionManifest) -> Option<&'static str> {
        if self.manifests.contains(id) {
            return Some("manifest_revoked");
        }
        if m.artifacts
            .iter()
            .any(|a| self.artifacts.contains(a.digest.as_str()))
        {
            return Some("artifact_revoked");
        }
        if m.axes.operations.iter().any(|(op, v)| {
            self.operations
                .contains(&format!("{}@{}", op.as_str(), v.as_str()))
                || self.operations.contains(&format!("{}@*", op.as_str()))
        }) {
            return Some("operation_revoked");
        }
        self.policies
            .contains(&m.axes.policy.0)
            .then_some("policy_revoked")
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Finding {
    pub axis: String,
    pub status: CompatStatus,
    pub reason: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Resolution {
    pub state: State,
    pub overall: CompatStatus,
    pub read: bool,
    pub write: bool,
    pub revoked: bool,
    /// The operations and versions the pin exposes; drift in the source never changes this.
    pub exposes: BTreeMap<String, String>,
    pub findings: Vec<Finding>,
}

/// Pins the public contract: operation versions and the operation schema.
pub fn contract_pin(m: &VersionManifest) -> Pins {
    ["operations", "operation_schema"]
        .into_iter()
        .map(|a| (a.to_string(), axis_text(&m.axes, a)))
        .collect()
}

fn state_of(s: CompatStatus) -> State {
    match s {
        CompatStatus::Supported | CompatStatus::SupportedWithTranslation => State::Compatible,
        CompatStatus::Degraded | CompatStatus::Unknown => State::Degraded,
        CompatStatus::NeedsReauth
        | CompatStatus::SourceIncompatible
        | CompatStatus::UnsupportedRuntime
        | CompatStatus::SecurityRevoked => State::Blocked,
    }
}

/// `id` is the manifest's own id. Unknown inputs lower the state; nothing is assumed compatible.
pub fn resolve(
    id: &str,
    m: &VersionManifest,
    support: &Support,
    observed: &ObservedContext,
    pins: &Pins,
    revocations: &RevocationSet,
) -> Resolution {
    let mut results: Vec<AxisResult> = evaluate(&m.axes, support, pins);
    results.extend(evaluate_context(m, observed));
    let mut findings: Vec<Finding> = results
        .iter()
        .map(|r| Finding {
            axis: r.axis.to_string(),
            status: r.status,
            reason: r.reason.to_string(),
        })
        .collect();
    let revoked_by_set = revocations.hit(id, m);
    if let Some(reason) = revoked_by_set {
        findings.push(Finding {
            axis: "revocation_set".into(),
            status: CompatStatus::SecurityRevoked,
            reason: reason.into(),
        });
    }
    findings.sort_by(|a, b| {
        severity(b.status)
            .cmp(&severity(a.status))
            .then_with(|| a.axis.cmp(&b.axis))
    });
    let overall = findings
        .first()
        .map_or(CompatStatus::Supported, |f| f.status);
    let state = findings
        .iter()
        .map(|f| state_of(f.status))
        .fold(State::Compatible, |a, b| match (a, b) {
            (State::Blocked, _) | (_, State::Blocked) => State::Blocked,
            (State::Degraded, _) | (_, State::Degraded) => State::Degraded,
            _ => State::Compatible,
        });
    let revoked = findings
        .iter()
        .any(|f| f.status == CompatStatus::SecurityRevoked);
    Resolution {
        state,
        overall,
        read: state != State::Blocked,
        write: state == State::Compatible,
        revoked,
        exposes: m
            .axes
            .operations
            .iter()
            .map(|(k, v)| (k.as_str().to_string(), v.as_str().to_string()))
            .collect(),
        findings,
    }
}
