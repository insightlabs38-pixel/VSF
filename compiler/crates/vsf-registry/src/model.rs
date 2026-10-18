//! Registry records and their strict canonical encoding.

use crate::json::{Error, Obj, Res, array, err, list, object, s};
use std::collections::BTreeMap;
use vsf_canon::Value;

#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord)]
pub enum Kind {
    Atomic,
    Composite,
    Adapter,
    Service,
    Unresolved,
}

impl Kind {
    pub fn as_str(self) -> &'static str {
        match self {
            Kind::Atomic => "atomic",
            Kind::Composite => "composite",
            Kind::Adapter => "adapter",
            Kind::Service => "service",
            Kind::Unresolved => "UNRESOLVED",
        }
    }

    pub fn parse(x: &str) -> Res<Kind> {
        Ok(match x {
            "atomic" => Kind::Atomic,
            "composite" => Kind::Composite,
            "adapter" => Kind::Adapter,
            "service" => Kind::Service,
            "UNRESOLVED" => Kind::Unresolved,
            _ => return Err(err("registry.bad_kind", x)),
        })
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord)]
pub enum Maturity {
    Candidate,
    Specified,
    Implemented,
    Conformant,
    ProductionProven,
}

impl Maturity {
    pub const ALL: [Maturity; 5] = [
        Maturity::Candidate,
        Maturity::Specified,
        Maturity::Implemented,
        Maturity::Conformant,
        Maturity::ProductionProven,
    ];

    pub fn as_str(self) -> &'static str {
        match self {
            Maturity::Candidate => "candidate",
            Maturity::Specified => "specified",
            Maturity::Implemented => "implemented",
            Maturity::Conformant => "conformant",
            Maturity::ProductionProven => "production_proven",
        }
    }

    pub fn parse(x: &str) -> Res<Maturity> {
        Maturity::ALL
            .into_iter()
            .find(|m| m.as_str() == x)
            .ok_or_else(|| err("registry.bad_maturity", x))
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Entry {
    pub id: String,
    pub name: String,
    pub family: String,
    pub family_code: String,
    pub layer: String,
    pub launch_priority: String,
    pub kind: Kind,
    pub rule: String,
    pub owner: String,
    pub requires: Vec<String>,
    pub maturity: Maturity,
}

impl Entry {
    pub fn encode(&self) -> Value {
        object(vec![
            ("family", s(&self.family)),
            ("family_code", s(&self.family_code)),
            ("id", s(&self.id)),
            ("kind", s(self.kind.as_str())),
            ("launch_priority", s(&self.launch_priority)),
            ("layer", s(&self.layer)),
            ("maturity", s(self.maturity.as_str())),
            ("name", s(&self.name)),
            ("owner", s(&self.owner)),
            ("requires", list(&self.requires)),
            ("rule", s(&self.rule)),
        ])
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Registry {
    pub catalog_sha256: String,
    pub entries: Vec<Entry>,
}

impl Registry {
    pub fn index(&self) -> BTreeMap<&str, &Entry> {
        self.entries.iter().map(|e| (e.id.as_str(), e)).collect()
    }

    pub fn encode(&self) -> Value {
        let entries = self.entries.iter().map(Entry::encode).collect();
        object(vec![
            ("catalog_sha256", s(&self.catalog_sha256)),
            ("entries", Value::Array(entries)),
            ("schema", s("vsf-primitive-registry-v1")),
        ])
    }

    pub fn decode(v: &Value) -> Res<Registry> {
        let mut o = Obj::new(v, "")?;
        if o.str("schema")? != "vsf-primitive-registry-v1" {
            return Err(err("registry.bad_schema", "schema"));
        }
        let catalog_sha256 = o.str("catalog_sha256")?;
        let mut entries = Vec::new();
        for (i, x) in array(o.req("entries")?, "/entries")?.iter().enumerate() {
            let p = format!("/entries/{i}");
            let mut e = Obj::new(x, &p)?;
            let entry = Entry {
                family: e.str("family")?,
                family_code: e.str("family_code")?,
                id: e.str("id")?,
                kind: Kind::parse(&e.str("kind")?)?,
                launch_priority: e.str("launch_priority")?,
                layer: e.str("layer")?,
                maturity: Maturity::parse(&e.str("maturity")?)?,
                name: e.str("name")?,
                owner: e.str("owner")?,
                requires: e.strs("requires")?,
                rule: e.str("rule")?,
            };
            e.finish()?;
            entries.push(entry);
        }
        o.finish()?;
        Ok(Registry {
            catalog_sha256,
            entries,
        })
    }
}

pub fn load(bytes: &[u8]) -> Result<Value, Error> {
    vsf_canon::parse(bytes).map_err(|e| err(e.code(), "parse"))
}
