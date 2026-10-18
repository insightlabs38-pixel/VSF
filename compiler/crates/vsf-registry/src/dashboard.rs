//! Coverage dashboard: every ratio is over the full catalog, never over what happens to be registered.

use crate::harness::Package;
use crate::json::{object, s};
use crate::model::{Kind, Maturity};
use std::collections::{BTreeMap, BTreeSet};
use vsf_canon::Value;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct FamilyRow {
    pub code: String,
    pub total: usize,
    pub implemented: usize,
    pub validated: usize,
    pub proven: usize,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AppRow {
    pub app: String,
    pub witness_fixtures: usize,
    pub unbound_fixtures: usize,
    pub operations: usize,
    pub primitives: usize,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Dashboard {
    pub total: usize,
    pub catalog_sha256: String,
    pub by_maturity: Vec<(Maturity, usize)>,
    pub by_kind: Vec<(Kind, usize)>,
    pub families: Vec<FamilyRow>,
    pub apps: Vec<AppRow>,
    pub source_operations: usize,
}

#[derive(Default)]
struct Tally<'a> {
    witnesses: usize,
    unbound: usize,
    primitives: BTreeSet<&'a str>,
    operations: BTreeSet<&'a str>,
}

impl Dashboard {
    pub fn build(pkg: &Package) -> Dashboard {
        let mut by_maturity: BTreeMap<Maturity, usize> =
            Maturity::ALL.into_iter().map(|m| (m, 0)).collect();
        let mut by_kind: BTreeMap<Kind, usize> = BTreeMap::new();
        let mut families: BTreeMap<&str, FamilyRow> = BTreeMap::new();
        for e in &pkg.registry.entries {
            let m = pkg.effective_maturity(&e.id);
            *by_maturity.entry(m).or_default() += 1;
            *by_kind.entry(e.kind).or_default() += 1;
            let row = families
                .entry(e.family_code.as_str())
                .or_insert_with(|| FamilyRow {
                    code: e.family_code.clone(),
                    total: 0,
                    implemented: 0,
                    validated: 0,
                    proven: 0,
                });
            row.total += 1;
            row.implemented += usize::from(m >= Maturity::Implemented);
            row.validated += usize::from(m >= Maturity::Conformant);
            row.proven += usize::from(m >= Maturity::ProductionProven);
        }
        let witnesses: BTreeSet<&str> = pkg
            .registered
            .values()
            .flat_map(|r| r.evidence.iter())
            .filter(|e| e.kind == "fixture")
            .map(|e| e.id.as_str())
            .collect();
        let mut apps: BTreeMap<&str, Tally> = BTreeMap::new();
        let mut pairs: BTreeSet<(&str, &str)> = BTreeSet::new();
        for f in &pkg.fixtures {
            let row = apps.entry(f.app.as_str()).or_default();
            if witnesses.contains(f.id.as_str()) && pkg.registered.contains_key(&f.primitive) {
                row.witnesses += 1;
                row.primitives.insert(f.primitive.as_str());
                for op in &f.operations {
                    row.operations.insert(op.as_str());
                    pairs.insert((f.app.as_str(), op.as_str()));
                }
            } else {
                row.unbound += 1;
            }
        }
        Dashboard {
            total: pkg.registry.entries.len(),
            catalog_sha256: pkg.registry.catalog_sha256.clone(),
            by_maturity: by_maturity.into_iter().collect(),
            by_kind: by_kind.into_iter().collect(),
            families: families.into_values().collect(),
            apps: apps
                .into_iter()
                .map(|(app, t)| AppRow {
                    app: app.to_string(),
                    witness_fixtures: t.witnesses,
                    unbound_fixtures: t.unbound,
                    operations: t.operations.len(),
                    primitives: t.primitives.len(),
                })
                .collect(),
            source_operations: pairs.len(),
        }
    }

    pub fn at_least(&self, level: Maturity) -> usize {
        self.by_maturity
            .iter()
            .filter(|(m, _)| *m >= level)
            .map(|(_, n)| n)
            .sum()
    }

    pub fn encode(&self) -> Value {
        let n = |x: usize| Value::Int(i64::try_from(x).unwrap_or(i64::MAX));
        let maturity = self
            .by_maturity
            .iter()
            .map(|(m, c)| (m.as_str(), n(*c)))
            .collect();
        let kinds = self
            .by_kind
            .iter()
            .map(|(k, c)| (k.as_str(), n(*c)))
            .collect();
        let tiers = vec![
            ("listed", n(self.total)),
            ("implemented", n(self.at_least(Maturity::Implemented))),
            (
                "independently_validated",
                n(self.at_least(Maturity::Conformant)),
            ),
            (
                "production_proven",
                n(self.at_least(Maturity::ProductionProven)),
            ),
        ];
        let families = self
            .families
            .iter()
            .map(|f| {
                object(vec![
                    ("code", s(&f.code)),
                    ("implemented", n(f.implemented)),
                    ("independently_validated", n(f.validated)),
                    ("production_proven", n(f.proven)),
                    ("total", n(f.total)),
                ])
            })
            .collect();
        let apps = self
            .apps
            .iter()
            .map(|a| {
                object(vec![
                    ("app", s(&a.app)),
                    ("operations", n(a.operations)),
                    ("primitives", n(a.primitives)),
                    ("unbound_fixtures", n(a.unbound_fixtures)),
                    ("witness_fixtures", n(a.witness_fixtures)),
                ])
            })
            .collect();
        let obj = |v: Vec<(&str, Value)>| object(v);
        object(vec![
            ("apps", Value::Array(apps)),
            ("catalog_sha256", s(&self.catalog_sha256)),
            ("families", Value::Array(families)),
            ("kinds", obj(kinds)),
            ("maturity", obj(maturity)),
            ("schema", s("vsf-primitive-dashboard-v1")),
            ("source_operations", n(self.source_operations)),
            ("tiers", obj(tiers)),
            ("total", n(self.total)),
        ])
    }

    pub fn render(&self) -> String {
        let pct = |x: usize| {
            let tenths = x * 1000 / self.total.max(1);
            format!("{}.{}%", tenths / 10, tenths % 10)
        };
        let mut out = format!("catalog entries (fixed denominator): {}\n", self.total);
        for (name, level) in [
            ("implemented", Maturity::Implemented),
            ("independently validated", Maturity::Conformant),
            ("production proven", Maturity::ProductionProven),
        ] {
            let c = self.at_least(level);
            out += &format!("  {name}: {c}/{} ({})\n", self.total, pct(c));
        }
        out += "by maturity:";
        for (m, c) in &self.by_maturity {
            out += &format!(" {}={c}", m.as_str());
        }
        out += "\nsource apps (witness-bound fixtures only):\n";
        for a in &self.apps {
            out += &format!(
                "  {}: {} witness, {} unbound, {} operations, {} primitives\n",
                a.app, a.witness_fixtures, a.unbound_fixtures, a.operations, a.primitives
            );
        }
        out += &format!(
            "distinct (app, operation) pairs: {}\n",
            self.source_operations
        );
        out
    }
}
