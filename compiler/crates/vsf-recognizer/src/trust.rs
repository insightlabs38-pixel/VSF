//! Who and what an observation, protocol document or witness was made under; any change in it ends reuse.

use crate::val::{obj, s};
use std::collections::BTreeSet;
use vsf_canon::{Value, sha256_hex, to_canonical};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct TrustContext {
    pub app_id: String,
    /// Keyed fingerprint of tenant, roles, locale and flags; a tenant or role change is a variant change.
    pub variant_id: String,
    pub origin: String,
    pub principal: String,
    pub runtime_id: String,
    pub mode: String,
    pub capability_digest: String,
    /// Fingerprint of the source application state/version the evidence was gathered against.
    pub source_revision: String,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord)]
pub enum ContextChange {
    App,
    Origin,
    Principal,
    Variant,
    Runtime,
    Capability,
    SourceRevision,
}

impl ContextChange {
    pub fn as_str(self) -> &'static str {
        match self {
            ContextChange::App => "app",
            ContextChange::Origin => "origin",
            ContextChange::Principal => "principal",
            ContextChange::Variant => "variant",
            ContextChange::Runtime => "runtime",
            ContextChange::Capability => "capability",
            ContextChange::SourceRevision => "source_revision",
        }
    }

    /// Behavioural evidence is only as good as the context it was demonstrated in: every change ends it.
    pub fn invalidates_behaviour(self) -> bool {
        true
    }

    /// User-specific evidence (labels, values, decisions) survives only a pure runtime or capability change.
    pub fn invalidates_private_evidence(self) -> bool {
        !matches!(
            self,
            ContextChange::Runtime | ContextChange::Capability | ContextChange::SourceRevision
        )
    }

    /// Structure reusable across principals survives principal change, but not a different app, origin or variant.
    pub fn invalidates_shared_structure(self) -> bool {
        matches!(
            self,
            ContextChange::App | ContextChange::Origin | ContextChange::Variant
        )
    }
}

fn hex_after(v: &str, prefix: &str, min: usize, max: usize) -> bool {
    v.strip_prefix(prefix).is_some_and(|h| {
        (min..=max).contains(&h.len())
            && h.bytes()
                .all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(&b))
    })
}

pub fn principal_ok(p: &str) -> bool {
    hex_after(p, "prin_", 32, 32)
}

pub fn variant_ok(v: &str) -> bool {
    hex_after(v, "var_", 32, 32)
}

pub fn digest_ok(d: &str) -> bool {
    hex_after(d, "sha256:", 64, 64)
}

pub fn app_id_ok(a: &str) -> bool {
    let Some((provider, install)) = a.split_once(':') else {
        return false;
    };
    let lead = |x: &str| {
        x.bytes()
            .next()
            .is_some_and(|b| b.is_ascii_lowercase() || b.is_ascii_digit())
    };
    provider.len() <= 63
        && install.len() <= 128
        && lead(provider)
        && lead(install)
        && provider
            .bytes()
            .all(|b| b.is_ascii_lowercase() || b.is_ascii_digit() || b == b'-')
        && install
            .bytes()
            .all(|b| b.is_ascii_lowercase() || b.is_ascii_digit() || b == b'-' || b == b'.')
}

pub fn runtime_ok(r: &str) -> bool {
    r.strip_prefix("rt_").is_some_and(|x| {
        (1..=48).contains(&x.len())
            && x.bytes()
                .all(|b| b.is_ascii_lowercase() || b.is_ascii_digit())
    })
}

pub fn mode_ok(m: &str) -> bool {
    matches!(
        m,
        "extension"
            | "desktop"
            | "localhost"
            | "hosted_sandbox"
            | "headless_worker"
            | "private_vpc"
    )
}

/// `scheme://host[:port]`: lowercase, no default port, no empty or hyphen-edged label, no path.
pub fn origin_ok(o: &str) -> bool {
    let Some((scheme, rest)) = o.split_once("://") else {
        return false;
    };
    let default = match scheme {
        "http" => "80",
        "https" => "443",
        _ => return false,
    };
    let (host, port) = match rest.split_once(':') {
        Some((h, p)) => (h, Some(p)),
        None => (rest, None),
    };
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
        && host.split('.').all(|label| {
            !label.is_empty()
                && label.len() <= 63
                && !label.starts_with('-')
                && !label.ends_with('-')
                && label
                    .bytes()
                    .all(|b| b.is_ascii_lowercase() || b.is_ascii_digit() || b == b'-')
        })
}

fn field<'a>(v: &'a Value, key: &str) -> Result<&'a str, String> {
    v.get(key)
        .and_then(Value::as_str)
        .ok_or_else(|| format!("context.missing:{key}"))
}

impl TrustContext {
    pub const KEYS: [&'static str; 8] = [
        "app_id",
        "capability_digest",
        "mode",
        "origin",
        "principal",
        "runtime_id",
        "source_revision",
        "variant_id",
    ];

    pub fn validate(&self) -> Result<(), &'static str> {
        if !app_id_ok(&self.app_id) {
            return Err("context.app_id");
        }
        if !variant_ok(&self.variant_id) {
            return Err("context.variant_id");
        }
        if !origin_ok(&self.origin) {
            return Err("context.origin");
        }
        if !principal_ok(&self.principal) {
            return Err("context.principal");
        }
        if !runtime_ok(&self.runtime_id) {
            return Err("context.runtime_id");
        }
        if !mode_ok(&self.mode) {
            return Err("context.mode");
        }
        if !digest_ok(&self.capability_digest) {
            return Err("context.capability_digest");
        }
        if !digest_ok(&self.source_revision) {
            return Err("context.source_revision");
        }
        Ok(())
    }

    pub fn from_value(v: &Value) -> Result<TrustContext, String> {
        let Value::Object(entries) = v else {
            return Err("context.not_object".into());
        };
        let keys: BTreeSet<&str> = entries.iter().map(|(k, _)| k.as_str()).collect();
        if keys != Self::KEYS.iter().copied().collect() || entries.len() != Self::KEYS.len() {
            return Err("context.keys".into());
        }
        let c = TrustContext {
            app_id: field(v, "app_id")?.into(),
            variant_id: field(v, "variant_id")?.into(),
            origin: field(v, "origin")?.into(),
            principal: field(v, "principal")?.into(),
            runtime_id: field(v, "runtime_id")?.into(),
            mode: field(v, "mode")?.into(),
            capability_digest: field(v, "capability_digest")?.into(),
            source_revision: field(v, "source_revision")?.into(),
        };
        c.validate().map_err(String::from)?;
        Ok(c)
    }

    pub fn to_value(&self) -> Value {
        obj(vec![
            ("app_id", s(&self.app_id)),
            ("capability_digest", s(&self.capability_digest)),
            ("mode", s(&self.mode)),
            ("origin", s(&self.origin)),
            ("principal", s(&self.principal)),
            ("runtime_id", s(&self.runtime_id)),
            ("source_revision", s(&self.source_revision)),
            ("variant_id", s(&self.variant_id)),
        ])
    }

    /// Identity of the whole context; any field change gives a different digest.
    pub fn digest(&self) -> String {
        let v = obj(vec![
            ("context", self.to_value()),
            ("domain", s("vsf.trust-context.v1")),
        ]);
        format!("sha256:{}", sha256_hex(&to_canonical(&v)))
    }

    fn keyed(&self, domain: &str, parts: &[&str]) -> String {
        let v = obj(vec![
            ("domain", s(domain)),
            ("parts", Value::Array(parts.iter().map(|p| s(p)).collect())),
        ]);
        format!("sha256:{}", sha256_hex(&to_canonical(&v)))
    }

    /// Key for user-specific evidence: bound to app, variant, origin and principal.
    pub fn private_key(&self) -> String {
        self.keyed(
            "vsf.cache.private.v1",
            &[
                &self.app_id,
                &self.variant_id,
                &self.origin,
                &self.principal,
            ],
        )
    }

    /// Key for structure shared across principals: bound to app, variant and origin only.
    pub fn shared_key(&self) -> String {
        self.keyed(
            "vsf.cache.shared.v1",
            &[&self.app_id, &self.variant_id, &self.origin],
        )
    }

    /// Every dimension in which `other` differs, in a fixed order.
    pub fn diff(&self, other: &TrustContext) -> Vec<ContextChange> {
        let mut out = Vec::new();
        if self.app_id != other.app_id {
            out.push(ContextChange::App);
        }
        if self.origin != other.origin {
            out.push(ContextChange::Origin);
        }
        if self.principal != other.principal {
            out.push(ContextChange::Principal);
        }
        if self.variant_id != other.variant_id {
            out.push(ContextChange::Variant);
        }
        if self.runtime_id != other.runtime_id || self.mode != other.mode {
            out.push(ContextChange::Runtime);
        }
        if self.capability_digest != other.capability_digest {
            out.push(ContextChange::Capability);
        }
        if self.source_revision != other.source_revision {
            out.push(ContextChange::SourceRevision);
        }
        out
    }
}
