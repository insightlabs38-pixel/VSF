//! Ownership, dependency anchors, classification rules and evidence semantics.

use crate::json::{Obj, Res, array, err, texts};
use crate::model::Kind;
use regex::Regex;
use std::collections::BTreeMap;
use vsf_canon::Value;

#[derive(Debug, Clone)]
pub struct FamilyPolicy {
    pub code: String,
    pub owner: String,
    pub default_kind: Kind,
    pub requires: Vec<String>,
}

#[derive(Debug, Clone)]
pub struct KeywordRule {
    pub id: String,
    pub kind: Kind,
    pub families: Vec<String>,
    pub pattern: Regex,
}

#[derive(Debug, Clone)]
pub struct Policy {
    pub root: String,
    pub families: Vec<FamilyPolicy>,
    pub keyword_rules: Vec<KeywordRule>,
    pub advancement: BTreeMap<String, BTreeMap<String, Vec<String>>>,
    pub modes: BTreeMap<String, Vec<String>>,
}

impl Policy {
    pub fn decode(v: &Value) -> Res<Policy> {
        let mut o = Obj::new(v, "")?;
        if o.str("schema")? != "vsf-primitive-policy-v1" {
            return Err(err("registry.bad_schema", "policy"));
        }
        let root = o.str("root")?;
        let mut families = Vec::new();
        for (i, x) in array(o.req("families")?, "/families")?.iter().enumerate() {
            let mut f = Obj::new(x, &format!("/families/{i}"))?;
            families.push(FamilyPolicy {
                code: f.str("code")?,
                owner: f.str("owner")?,
                default_kind: Kind::parse(&f.str("default_kind")?)?,
                requires: f.strs("requires")?,
            });
            f.finish()?;
        }
        let mut keyword_rules = Vec::new();
        for (i, x) in array(o.req("keyword_rules")?, "/keyword_rules")?
            .iter()
            .enumerate()
        {
            let mut r = Obj::new(x, &format!("/keyword_rules/{i}"))?;
            let pattern = r.str("pattern")?;
            keyword_rules.push(KeywordRule {
                id: r.str("id")?,
                kind: Kind::parse(&r.str("kind")?)?,
                families: r.strs("families")?,
                pattern: Regex::new(&format!("(?i){pattern}"))
                    .map_err(|_| err("registry.bad_pattern", pattern.clone()))?,
            });
            r.finish()?;
        }
        let mut advancement = BTreeMap::new();
        let Value::Object(kinds) = o.req("advancement")? else {
            return Err(err("registry.not_object", "/advancement"));
        };
        for (kind, levels) in kinds {
            let Value::Object(levels) = levels else {
                return Err(err("registry.not_object", format!("/advancement/{kind}")));
            };
            let mut m = BTreeMap::new();
            for (level, items) in levels {
                m.insert(
                    level.clone(),
                    texts(items, &format!("/advancement/{kind}/{level}"))?,
                );
            }
            advancement.insert(kind.clone(), m);
        }
        let mut modes = BTreeMap::new();
        for (i, x) in array(o.req("modes")?, "/modes")?.iter().enumerate() {
            let mut m = Obj::new(x, &format!("/modes/{i}"))?;
            modes.insert(m.str("mode")?, m.strs("capabilities")?);
            m.finish()?;
        }
        o.finish()?;
        Ok(Policy {
            root,
            families,
            keyword_rules,
            advancement,
            modes,
        })
    }

    pub fn family(&self, code: &str) -> Option<&FamilyPolicy> {
        self.families.iter().find(|f| f.code == code)
    }
}
