//! Per-axis version compatibility; the overall status is the most severe axis.

use crate::generated::*;
use crate::rt::{Version, parse_version};
use std::collections::BTreeMap;
use vsf_canon::Value;

pub const AXES: [&str; 12] = [
    "source_app",
    "source_variant",
    "compiler",
    "primitive_set",
    "observation_schema",
    "semantic_schema",
    "operation_schema",
    "operations",
    "presentation",
    "cache_format",
    "runtime",
    "policy",
];

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AxisResult {
    pub axis: &'static str,
    pub status: CompatStatus,
    pub reason: &'static str,
}

#[derive(Debug, Clone)]
pub struct SchemaSupport {
    pub current: Version,
    pub migratable_majors: Vec<u32>,
}

/// What the evaluating host can read, translate and trust.
#[derive(Debug, Clone)]
pub struct Support {
    pub observation: SchemaSupport,
    pub semantic: SchemaSupport,
    pub operation: SchemaSupport,
    pub compiler_min: Version,
    pub compiler_max: Version,
    pub primitive_sets: Vec<String>,
    pub primitive_translatable: Vec<String>,
    pub presentation_max: u64,
    pub cache_format: i64,
    pub runtime: Version,
    pub min_policy: i64,
    pub revoked_policies: Vec<i64>,
    pub operations: BTreeMap<String, Version>,
    pub compatible_fingerprints: Vec<String>,
    pub observed: Option<(String, String)>,
}

/// Axis name to the exact textual value it must hold.
pub type Pins = BTreeMap<String, String>;

pub fn severity(s: CompatStatus) -> u8 {
    match s {
        CompatStatus::Supported => 0,
        CompatStatus::SupportedWithTranslation => 1,
        CompatStatus::Degraded => 2,
        CompatStatus::Unknown => 3,
        CompatStatus::NeedsReauth => 4,
        CompatStatus::SourceIncompatible => 5,
        CompatStatus::UnsupportedRuntime => 6,
        CompatStatus::SecurityRevoked => 7,
    }
}

pub fn overall(results: &[AxisResult]) -> CompatStatus {
    results
        .iter()
        .map(|r| r.status)
        .max_by_key(|s| severity(*s))
        .unwrap_or(CompatStatus::Supported)
}

fn schema(artifact: &SemVer, sup: &SchemaSupport) -> (CompatStatus, &'static str) {
    let Some(a) = parse_version(artifact.as_str()) else {
        return (CompatStatus::UnsupportedRuntime, "unparseable");
    };
    if a.0 == sup.current.0 {
        if a <= sup.current {
            (CompatStatus::Supported, "ok")
        } else {
            (CompatStatus::UnsupportedRuntime, "newer_than_supported")
        }
    } else if sup.migratable_majors.contains(&a.0) {
        (
            CompatStatus::SupportedWithTranslation,
            "migration_available",
        )
    } else {
        (CompatStatus::UnsupportedRuntime, "major_not_supported")
    }
}

fn compiler_core(c: &CompilerVersion) -> Option<Version> {
    parse_version(c.as_str().split('+').next().unwrap_or(""))
}

fn pv_number(p: &PresentationVersion) -> u64 {
    p.as_str()
        .trim_start_matches("pv")
        .parse()
        .unwrap_or(u64::MAX)
}

fn ops_text(m: &BTreeMap<OperationId, SemVer>) -> String {
    m.iter()
        .map(|(k, v)| format!("{}={}", k.as_str(), v.as_str()))
        .collect::<Vec<_>>()
        .join(",")
}

/// The exact text a pin on `axis` is compared against.
pub fn axis_text(a: &VersionAxes, axis: &str) -> String {
    match axis {
        "source_app" => a.source_app.fingerprint.as_str().to_string(),
        "source_variant" => a.source_variant.as_str().to_string(),
        "compiler" => a.compiler.as_str().to_string(),
        "primitive_set" => a.primitive_set.as_str().to_string(),
        "observation_schema" => a.observation_schema.as_str().to_string(),
        "semantic_schema" => a.semantic_schema.as_str().to_string(),
        "operation_schema" => a.operation_schema.as_str().to_string(),
        "operations" => ops_text(&a.operations),
        "presentation" => a.presentation.as_str().to_string(),
        "cache_format" => a.cache_format.0.to_string(),
        "runtime" => a.runtime.as_str().to_string(),
        "policy" => a.policy.0.to_string(),
        _ => String::new(),
    }
}

fn operations(a: &VersionAxes, s: &Support) -> (CompatStatus, &'static str) {
    let mut worst = (CompatStatus::Supported, "ok");
    for (id, ver) in &a.operations {
        let r = match (s.operations.get(id.as_str()), parse_version(ver.as_str())) {
            (None, _) => (CompatStatus::UnsupportedRuntime, "operation_unknown"),
            (_, None) => (CompatStatus::UnsupportedRuntime, "unparseable"),
            (Some(max), Some(v)) if v.0 != max.0 => {
                (CompatStatus::UnsupportedRuntime, "major_not_supported")
            }
            (Some(max), Some(v)) if v > *max => {
                (CompatStatus::UnsupportedRuntime, "newer_than_supported")
            }
            _ => (CompatStatus::Supported, "ok"),
        };
        if severity(r.0) > severity(worst.0) {
            worst = r;
        }
    }
    worst
}

pub fn evaluate(a: &VersionAxes, s: &Support, pins: &Pins) -> Vec<AxisResult> {
    AXES.iter()
        .map(|&axis| {
            if let Some(want) = pins.get(axis)
                && *want != axis_text(a, axis)
            {
                let status = if axis.starts_with("source") {
                    CompatStatus::SourceIncompatible
                } else {
                    CompatStatus::UnsupportedRuntime
                };
                return AxisResult {
                    axis,
                    status,
                    reason: "pin_mismatch",
                };
            }
            let (status, reason) = match axis {
                "source_app" => match &s.observed {
                    None => (CompatStatus::Unknown, "not_observed"),
                    Some((fp, _))
                        if fp == a.source_app.fingerprint.as_str()
                            || s.compatible_fingerprints.iter().any(|c| c == fp) =>
                    {
                        (CompatStatus::Supported, "ok")
                    }
                    Some(_) => (CompatStatus::SourceIncompatible, "fingerprint_changed"),
                },
                "source_variant" => match &s.observed {
                    Some((_, v)) if v != a.source_variant.as_str() => {
                        (CompatStatus::SourceIncompatible, "variant_changed")
                    }
                    None => (CompatStatus::Unknown, "not_observed"),
                    Some(_) => (CompatStatus::Supported, "ok"),
                },
                "compiler" => match compiler_core(&a.compiler) {
                    Some(c) if c < s.compiler_min => {
                        (CompatStatus::SupportedWithTranslation, "compiler_older")
                    }
                    Some(c) if c > s.compiler_max => {
                        (CompatStatus::UnsupportedRuntime, "compiler_newer")
                    }
                    Some(_) => (CompatStatus::Supported, "ok"),
                    None => (CompatStatus::UnsupportedRuntime, "unparseable"),
                },
                "primitive_set" => {
                    let d = a.primitive_set.as_str();
                    if s.primitive_sets.iter().any(|x| x == d) {
                        (CompatStatus::Supported, "ok")
                    } else if s.primitive_translatable.iter().any(|x| x == d) {
                        (
                            CompatStatus::SupportedWithTranslation,
                            "primitive_translation",
                        )
                    } else {
                        (CompatStatus::UnsupportedRuntime, "primitive_set_unknown")
                    }
                }
                "observation_schema" => schema(&a.observation_schema, &s.observation),
                "semantic_schema" => schema(&a.semantic_schema, &s.semantic),
                "operation_schema" => schema(&a.operation_schema, &s.operation),
                "operations" => operations(a, s),
                "presentation" => {
                    if pv_number(&a.presentation) <= s.presentation_max {
                        (CompatStatus::Supported, "ok")
                    } else {
                        (CompatStatus::Degraded, "newer_falls_back")
                    }
                }
                "cache_format" => match a.cache_format.0.cmp(&s.cache_format) {
                    std::cmp::Ordering::Equal => (CompatStatus::Supported, "ok"),
                    std::cmp::Ordering::Less => {
                        (CompatStatus::SupportedWithTranslation, "cache_rebuild")
                    }
                    std::cmp::Ordering::Greater => {
                        (CompatStatus::UnsupportedRuntime, "cache_newer")
                    }
                },
                "runtime" => match parse_version(a.runtime.as_str()) {
                    Some(r) if r.0 < s.runtime.0 => (
                        CompatStatus::SupportedWithTranslation,
                        "older_runtime_major",
                    ),
                    Some(r) if r.0 == s.runtime.0 && r <= s.runtime => {
                        (CompatStatus::Supported, "ok")
                    }
                    Some(_) => (
                        CompatStatus::UnsupportedRuntime,
                        "produced_by_newer_runtime",
                    ),
                    None => (CompatStatus::UnsupportedRuntime, "unparseable"),
                },
                "policy" => {
                    if s.revoked_policies.contains(&a.policy.0) {
                        (CompatStatus::SecurityRevoked, "policy_revoked")
                    } else if a.policy.0 < s.min_policy {
                        (CompatStatus::NeedsReauth, "policy_below_minimum")
                    } else {
                        (CompatStatus::Supported, "ok")
                    }
                }
                _ => (CompatStatus::UnsupportedRuntime, "unknown_axis"),
            };
            AxisResult {
                axis,
                status,
                reason,
            }
        })
        .collect()
}

/// Who and where the artifacts are being used now; `None` means not observed, which is never compatible.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct ObservedContext {
    pub origin: Option<String>,
    pub principal: Option<String>,
}

fn worse(
    a: (CompatStatus, &'static str),
    b: (CompatStatus, &'static str),
) -> (CompatStatus, &'static str) {
    if severity(b.0) > severity(a.0) { b } else { a }
}

/// Origin, owner and revocation checks that the twelve axes cannot express; each unconfirmed input is `unknown`.
pub fn evaluate_context(m: &VersionManifest, o: &ObservedContext) -> Vec<AxisResult> {
    let origin = match (&m.origin, &o.origin) {
        (None, _) => (CompatStatus::Unknown, "origin_undeclared"),
        (_, None) => (CompatStatus::Unknown, "not_observed"),
        (Some(a), Some(b)) if a.as_str() == b => (CompatStatus::Supported, "ok"),
        _ => (CompatStatus::SourceIncompatible, "origin_changed"),
    };
    let mut principal = (CompatStatus::Supported, "shared_structure");
    for a in &m.artifacts {
        let r = match a.visibility.unwrap_or(Visibility::PrivatePrincipal) {
            Visibility::GlobalStructure | Visibility::PrivateVariant => {
                (CompatStatus::Supported, "ok")
            }
            Visibility::PrivatePrincipal => match (&a.principal_ref, &o.principal) {
                (None, _) => (CompatStatus::Unknown, "principal_unbound"),
                (_, None) => (CompatStatus::Unknown, "not_observed"),
                (Some(x), Some(y)) if x.as_str() == y => (CompatStatus::Supported, "ok"),
                _ => (CompatStatus::NeedsReauth, "principal_changed"),
            },
        };
        principal = worse(principal, r);
    }
    let mut revocation = (CompatStatus::Supported, "ok");
    for r in m.revocations.iter().flatten() {
        let hit = match r.kind {
            RevocationKind::Artifact => r
                .digest
                .as_ref()
                .is_some_and(|d| m.artifacts.iter().any(|a| a.digest.as_str() == d.as_str()))
                .then_some("artifact_revoked"),
            RevocationKind::Operation => r
                .operation
                .as_ref()
                .is_some_and(|op| m.axes.operations.keys().any(|k| k.as_str() == op.as_str()))
                .then_some("operation_revoked"),
            RevocationKind::PrincipalGrant => (r.principal.as_ref().map(|p| p.as_str())
                == o.principal.as_deref()
                && o.principal.is_some())
            .then_some("grant_revoked"),
            RevocationKind::Witness | RevocationKind::TrustAuthority => None,
        };
        if let Some(reason) = hit {
            revocation = worse(revocation, (CompatStatus::SecurityRevoked, reason));
        }
    }
    [
        ("origin", origin),
        ("principal", principal),
        ("revocation", revocation),
    ]
    .into_iter()
    .map(|(axis, (status, reason))| AxisResult {
        axis,
        status,
        reason,
    })
    .collect()
}

// ---- host description from canonical JSON (fixtures, WASM and native callers) ----------------------

fn field<'a>(v: &'a Value, k: &str) -> Result<&'a Value, String> {
    v.get(k).ok_or_else(|| format!("missing {k}"))
}

fn text<'a>(v: &'a Value, k: &str) -> Result<&'a str, String> {
    field(v, k)?
        .as_str()
        .ok_or_else(|| format!("{k} must be text"))
}

fn num(v: &Value, k: &str) -> Result<i64, String> {
    field(v, k)?
        .as_int()
        .ok_or_else(|| format!("{k} must be an integer"))
}

fn ver(s: &str) -> Result<Version, String> {
    parse_version(s).ok_or_else(|| format!("bad version {s}"))
}

fn strings(v: &Value, k: &str) -> Result<Vec<String>, String> {
    match field(v, k)? {
        Value::Array(items) => items
            .iter()
            .map(|i| {
                i.as_str()
                    .map(str::to_string)
                    .ok_or_else(|| format!("{k} items must be text"))
            })
            .collect(),
        _ => Err(format!("{k} must be a list")),
    }
}

fn schema_support(v: &Value, k: &str) -> Result<SchemaSupport, String> {
    let s = field(v, k)?;
    let Value::Array(majors) = field(s, "migratable_majors")? else {
        return Err("migratable_majors must be a list".into());
    };
    Ok(SchemaSupport {
        current: ver(text(s, "current")?)?,
        migratable_majors: majors
            .iter()
            .map(|m| {
                m.as_int()
                    .and_then(|n| u32::try_from(n).ok())
                    .ok_or("bad major")
            })
            .collect::<Result<_, _>>()?,
    })
}

impl Support {
    pub fn from_value(v: &Value) -> Result<Self, String> {
        let compiler = field(v, "compiler")?;
        let Value::Object(ops) = field(v, "operations")? else {
            return Err("operations must be an object".into());
        };
        let observed = match v.get("observed") {
            Some(o) => Some((
                text(o, "fingerprint")?.to_string(),
                text(o, "variant")?.to_string(),
            )),
            None => None,
        };
        Ok(Self {
            observation: schema_support(v, "observation_schema")?,
            semantic: schema_support(v, "semantic_schema")?,
            operation: schema_support(v, "operation_schema")?,
            compiler_min: ver(text(compiler, "min")?)?,
            compiler_max: ver(text(compiler, "max")?)?,
            primitive_sets: strings(v, "primitive_sets")?,
            primitive_translatable: strings(v, "primitive_translatable")?,
            presentation_max: u64::try_from(num(v, "presentation_max")?)
                .map_err(|_| "presentation_max")?,
            cache_format: num(v, "cache_format")?,
            runtime: ver(text(v, "runtime")?)?,
            min_policy: num(v, "min_policy")?,
            revoked_policies: match field(v, "revoked_policies")? {
                Value::Array(items) => items.iter().filter_map(Value::as_int).collect(),
                _ => return Err("revoked_policies must be a list".into()),
            },
            operations: ops
                .iter()
                .map(|(k, x)| Ok((k.clone(), ver(x.as_str().ok_or("operation version")?)?)))
                .collect::<Result<_, String>>()?,
            compatible_fingerprints: strings(v, "compatible_fingerprints")?,
            observed,
        })
    }
}

/// Canonical JSON for an evaluation: per-axis results plus the overall status.
pub fn results_json(results: &[AxisResult]) -> Vec<u8> {
    let s = |x: &str| Value::Str(x.to_string());
    let axes = results
        .iter()
        .map(|r| {
            Value::Object(vec![
                ("axis".into(), s(r.axis)),
                ("reason".into(), s(r.reason)),
                ("status".into(), s(r.status.as_str())),
            ])
        })
        .collect();
    vsf_canon::to_canonical(&Value::Object(vec![
        ("axes".into(), Value::Array(axes)),
        ("overall".into(), s(overall(results).as_str())),
    ]))
}
