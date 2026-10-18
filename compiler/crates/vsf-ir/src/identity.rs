//! Source-variant identity and cache scoping; private state never crosses principals.

use crate::rt::{StrRule, TextMode, check_string};
use sha2::{Digest, Sha256};
use std::collections::BTreeMap;
use vsf_canon::{Value, to_canonical};

const BLOCK: usize = 64;

pub fn hmac_sha256(key: &[u8], msg: &[u8]) -> [u8; 32] {
    let mut k = [0u8; BLOCK];
    if key.len() > BLOCK {
        k[..32].copy_from_slice(&Sha256::digest(key));
    } else {
        k[..key.len()].copy_from_slice(key);
    }
    let mut inner = Sha256::new();
    inner.update(k.map(|b| b ^ 0x36));
    inner.update(msg);
    let mut outer = Sha256::new();
    outer.update(k.map(|b| b ^ 0x5c));
    outer.update(inner.finalize());
    outer.finalize().into()
}

fn hex(bytes: &[u8]) -> String {
    bytes.iter().map(|b| format!("{b:02x}")).collect()
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct IdentityError {
    pub code: &'static str,
    pub detail: String,
}

fn err(code: &'static str, detail: impl Into<String>) -> IdentityError {
    IdentityError {
        code,
        detail: detail.into(),
    }
}

/// What distinguishes one rendering of a source product from another.
#[derive(Debug, Clone, PartialEq, Eq, Default)]
pub struct VariantDescriptor {
    pub app_id: String,
    pub tenant: Option<String>,
    pub roles: Vec<String>,
    pub locale: String,
    pub flags: Vec<(String, String)>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Variant {
    pub id: String,
    pub class: String,
    pub canonical: Vec<u8>,
}

const TOKEN: StrRule = StrRule {
    pattern: None,
    max_len: 128,
    text: TextMode::Label,
};

fn token(what: &str, s: &str) -> Result<(), IdentityError> {
    if s.is_empty() {
        return Err(err("identity.empty", what));
    }
    check_string(s, what, &TOKEN).map_err(|e| err(e.code, what))
}

/// Case-normalizes a BCP 47 style tag (`en-us` -> `en-US`, `zh-hant-tw` -> `zh-Hant-TW`).
pub fn canonical_locale(tag: &str) -> Result<String, IdentityError> {
    let parts: Vec<&str> = tag.split('-').collect();
    let ok =
        |p: &str| !p.is_empty() && p.len() <= 8 && p.bytes().all(|b| b.is_ascii_alphanumeric());
    let lang = parts[0];
    if !(2..=3).contains(&lang.len())
        || !lang.bytes().all(|b| b.is_ascii_alphabetic())
        || !parts.iter().all(|p| ok(p))
        || parts.len() > 6
    {
        return Err(err("identity.bad_locale", tag));
    }
    let mut out = vec![lang.to_ascii_lowercase()];
    for p in &parts[1..] {
        let alpha = p.bytes().all(|b| b.is_ascii_alphabetic());
        out.push(match (p.len(), alpha) {
            (4, true) => format!(
                "{}{}",
                p[..1].to_ascii_uppercase(),
                p[1..].to_ascii_lowercase()
            ),
            (2, true) => p.to_ascii_uppercase(),
            _ => p.to_ascii_lowercase(),
        });
    }
    Ok(out.join("-"))
}

/// Order-independent: roles and flags are sorted, duplicates collapse, conflicting flags fail.
pub fn normalize(d: &VariantDescriptor) -> Result<VariantDescriptor, IdentityError> {
    let (provider, install) = d
        .app_id
        .split_once(':')
        .ok_or_else(|| err("identity.bad_app_id", &d.app_id))?;
    if provider.is_empty() || install.is_empty() {
        return Err(err("identity.bad_app_id", &d.app_id));
    }
    token("app_id", &d.app_id)?;
    if let Some(t) = &d.tenant {
        token("tenant", t)?;
    }
    let mut roles = d.roles.clone();
    for r in &roles {
        token("role", r)?;
    }
    roles.sort();
    roles.dedup();
    let mut flags: BTreeMap<String, String> = BTreeMap::new();
    for (k, v) in &d.flags {
        token("flag", k)?;
        token("flag_value", v)?;
        if let Some(prev) = flags.insert(k.clone(), v.clone())
            && prev != *v
        {
            return Err(err("identity.conflicting_flag", k));
        }
    }
    Ok(VariantDescriptor {
        app_id: d.app_id.clone(),
        tenant: d.tenant.clone(),
        roles,
        locale: canonical_locale(&d.locale)?,
        flags: flags.into_iter().collect(),
    })
}

fn canonical_descriptor(d: &VariantDescriptor) -> Vec<u8> {
    let s = |x: &str| Value::Str(x.to_string());
    to_canonical(&Value::Object(vec![
        ("app_id".into(), s(&d.app_id)),
        (
            "flags".into(),
            Value::Object(d.flags.iter().map(|(k, v)| (k.clone(), s(v))).collect()),
        ),
        ("locale".into(), s(&d.locale)),
        (
            "roles".into(),
            Value::Array(d.roles.iter().map(|r| s(r)).collect()),
        ),
        ("tenant".into(), d.tenant.as_deref().map_or(Value::Null, s)),
    ]))
}

/// Keyed so identifiers cannot be guessed or correlated across deployments from public inputs.
pub fn variant(key: &[u8], d: &VariantDescriptor) -> Result<Variant, IdentityError> {
    let n = normalize(d)?;
    let canonical = canonical_descriptor(&n);
    let mut msg = b"vsf.variant.v1\0".to_vec();
    msg.extend_from_slice(&canonical);
    let id = format!("var_{}", &hex(&hmac_sha256(key, &msg))[..32]);
    let class = if n.roles.is_empty() {
        "default".to_string()
    } else {
        class_of(&n.roles)
    };
    Ok(Variant {
        id,
        class,
        canonical,
    })
}

/// Coarse role label matching the `VariantClass` pattern; the keyed variant id carries the exact identity.
fn class_of(roles: &[String]) -> String {
    let joined = roles
        .iter()
        .map(|r| {
            r.chars()
                .map(|c| {
                    if c.is_ascii_alphanumeric() {
                        c.to_ascii_lowercase()
                    } else {
                        '-'
                    }
                })
                .collect::<String>()
        })
        .collect::<Vec<_>>()
        .join("_");
    let trimmed = joined.trim_start_matches(|c: char| !c.is_ascii_lowercase());
    if trimmed.is_empty() {
        "role".to_string()
    } else {
        trimmed.chars().take(64).collect()
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Visibility {
    /// Structure with no user data: shareable between principals of the same variant.
    Public,
    /// Anything derived from user or tenant values: bound to one principal and one variant.
    Private,
}

pub fn cache_key(
    key: &[u8],
    vis: Visibility,
    variant_id: &str,
    principal: &str,
    artifact_kind: &str,
    content: &str,
) -> String {
    let mut msg = b"vsf.cache.v1\0".to_vec();
    msg.extend_from_slice(if vis == Visibility::Public {
        b"public\0"
    } else {
        b"private\0"
    });
    for part in [
        variant_id,
        if vis == Visibility::Private {
            principal
        } else {
            ""
        },
        artifact_kind,
        content,
    ] {
        msg.extend_from_slice(part.as_bytes());
        msg.push(0);
    }
    format!("ck_{}", hex(&hmac_sha256(key, &msg)))
}
