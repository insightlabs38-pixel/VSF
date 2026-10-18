//! Catalog import: every entry exactly once, classified by a recorded rule, never a maturity claim.

use crate::json::{Obj, Res, array, err};
use crate::model::{Entry, Kind, Maturity, Registry};
use crate::policy::Policy;
use std::collections::BTreeSet;
use vsf_canon::sha256_hex;

pub fn classify(policy: &Policy, family_code: &str, name: &str) -> Res<(Kind, String)> {
    let fam = policy
        .family(family_code)
        .ok_or_else(|| err("registry.unknown_family", family_code))?;
    let hits: Vec<&crate::policy::KeywordRule> = policy
        .keyword_rules
        .iter()
        .filter(|r| {
            (r.families.is_empty() || r.families.iter().any(|f| f == family_code))
                && r.pattern.is_match(name)
        })
        .collect();
    let kinds: BTreeSet<Kind> = hits.iter().map(|r| r.kind).collect();
    Ok(match kinds.len() {
        0 if fam.default_kind == Kind::Unresolved => {
            (Kind::Unresolved, format!("F-{family_code}-UNRESOLVED"))
        }
        0 => (fam.default_kind, format!("F-{family_code}")),
        1 => (hits[0].kind, hits[0].id.clone()),
        _ => (
            Kind::Unresolved,
            hits.iter()
                .map(|r| r.id.as_str())
                .collect::<Vec<_>>()
                .join("+"),
        ),
    })
}

pub fn import(catalog_bytes: &[u8], policy: &Policy) -> Res<Registry> {
    let v = crate::model::load(catalog_bytes)?;
    let mut c = Obj::new(&v, "")?;
    c.req("schema")?;
    c.req("note")?;
    c.req("source")?;
    c.req("maturity_levels")?;
    c.req("counts")?;
    c.req("families")?;
    let raw = array(c.req("entries")?, "/entries")?;
    let mut seen = BTreeSet::new();
    let mut entries = Vec::with_capacity(raw.len());
    for (i, x) in raw.iter().enumerate() {
        let mut e = Obj::new(x, &format!("/entries/{i}"))?;
        let id = e.str("id")?;
        if !seen.insert(id.clone()) {
            return Err(err("registry.duplicate_id", id));
        }
        let family_code = e.str("family_code")?;
        let name = e.str("name")?;
        let maturity = Maturity::parse(&e.str("maturity")?)?;
        if maturity != Maturity::Candidate {
            return Err(err("registry.import_claims_maturity", id));
        }
        let fam = policy
            .family(&family_code)
            .ok_or_else(|| err("registry.unknown_family", family_code.clone()))?;
        let (kind, rule) = classify(policy, &family_code, &name)?;
        let requires = if id == policy.root {
            Vec::new()
        } else if family_code == "OBS" {
            vec![policy.root.clone()]
        } else {
            fam.requires.clone()
        };
        entries.push(Entry {
            family: e.str("family")?,
            family_code,
            id,
            kind,
            launch_priority: e.str("launch_priority")?,
            layer: e.str("layer")?,
            maturity,
            name,
            owner: fam.owner.clone(),
            requires,
            rule,
        });
        e.finish()?;
    }
    c.finish()?;
    Ok(Registry {
        catalog_sha256: sha256_hex(catalog_bytes),
        entries,
    })
}

pub fn count_by_kind(r: &Registry) -> Vec<(Kind, usize)> {
    [
        Kind::Atomic,
        Kind::Composite,
        Kind::Adapter,
        Kind::Service,
        Kind::Unresolved,
    ]
    .into_iter()
    .map(|k| (k, r.entries.iter().filter(|e| e.kind == k).count()))
    .collect()
}
