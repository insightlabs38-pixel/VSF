//! Whole-observation recognition: index-driven discovery of unit roots, then one logged computation per unit.

use crate::index::{FeatureIndex, Query};
use crate::obs::Observation;
use crate::semantics::{
    Action, Collection, Field, Reader, Rules, recognize_action, recognize_collection,
    recognize_field,
};
use std::collections::{BTreeMap, BTreeSet};

#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub enum UnitKind {
    Field,
    Action,
    Collection,
}

impl UnitKind {
    pub fn as_str(self) -> &'static str {
        match self {
            UnitKind::Field => "field",
            UnitKind::Action => "action",
            UnitKind::Collection => "collection",
        }
    }
}

pub type UnitKey = (UnitKind, u32);

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Output {
    Field(Field),
    Action(Action),
    Collection(Collection),
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Unit {
    pub key: UnitKey,
    pub output: Output,
    /// Ids of every node whose facts the computation looked at.
    pub reads: BTreeSet<u32>,
}

/// Nodes that could root a unit; everything else is never examined for discovery.
pub fn discovery_queries() -> Vec<(UnitKind, Query)> {
    let any = |fs: &[&str]| Query::Or(fs.iter().map(|f| Query::feat(f)).collect());
    vec![
        (
            UnitKind::Field,
            any(&[
                "tag:input",
                "tag:select",
                "tag:textarea",
                "role:textbox",
                "role:combobox",
                "role:listbox",
                "role:checkbox",
                "role:radio",
                "role:slider",
                "role:switch",
                "role:spinbutton",
                "role:searchbox",
            ]),
        ),
        (
            UnitKind::Action,
            any(&[
                "tag:button",
                "tag:a",
                "role:button",
                "role:link",
                "role:menuitem",
            ]),
        ),
        (
            UnitKind::Collection,
            any(&["tag:table", "role:table", "role:grid", "role:treegrid"]),
        ),
    ]
}

pub fn compute_unit(obs: &Observation, kind: UnitKind, i: usize, rules: Rules) -> Option<Unit> {
    let r = Reader::new(obs);
    let output = match kind {
        UnitKind::Field => recognize_field(&r, i, rules).map(Output::Field),
        UnitKind::Action => recognize_action(&r, i).map(Output::Action),
        UnitKind::Collection => recognize_collection(&r, i).map(Output::Collection),
    }?;
    Some(Unit {
        key: (kind, obs.nodes[i].id),
        output,
        reads: r.take(),
    })
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct RunStats {
    pub candidates_examined: usize,
    pub nodes: usize,
    pub units_computed: usize,
}

#[derive(Debug, Clone)]
pub struct Recognition {
    pub rules: Rules,
    pub digest: String,
    pub units: BTreeMap<UnitKey, Unit>,
    pub stats: RunStats,
}

pub fn recognize(obs: &Observation, rules: Rules) -> Recognition {
    let index = FeatureIndex::build(obs);
    let mut units = BTreeMap::new();
    let mut stats = RunStats {
        nodes: obs.len(),
        ..RunStats::default()
    };
    for (kind, q) in discovery_queries() {
        let (ids, st) = index.query(&q);
        stats.candidates_examined += st.candidates;
        for i in ids {
            if let Some(u) = compute_unit(obs, kind, i as usize, rules) {
                stats.units_computed += 1;
                units.insert(u.key, u);
            }
        }
    }
    Recognition {
        rules,
        digest: obs.digest.clone(),
        units,
        stats,
    }
}

impl Recognition {
    pub fn fields(&self) -> impl Iterator<Item = &Field> {
        self.units.values().filter_map(|u| match &u.output {
            Output::Field(f) => Some(f),
            _ => None,
        })
    }

    pub fn actions(&self) -> impl Iterator<Item = &Action> {
        self.units.values().filter_map(|u| match &u.output {
            Output::Action(a) => Some(a),
            _ => None,
        })
    }

    pub fn collections(&self) -> impl Iterator<Item = &Collection> {
        self.units.values().filter_map(|u| match &u.output {
            Output::Collection(c) => Some(c),
            _ => None,
        })
    }

    pub fn field(&self, id: u32) -> Option<&Field> {
        match &self.units.get(&(UnitKind::Field, id))?.output {
            Output::Field(f) => Some(f),
            _ => None,
        }
    }

    pub fn action(&self, id: u32) -> Option<&Action> {
        match &self.units.get(&(UnitKind::Action, id))?.output {
            Output::Action(a) => Some(a),
            _ => None,
        }
    }
}
