//! Only units that read a changed node are recomputed; the result equals a from-scratch run.

use crate::engine::{
    Recognition, RunStats, Unit, UnitKey, compute_unit, discovery_queries, recognize,
};
use crate::ledger::{Ctx, Ledger, NewClaim, build_ledger_in, claims_for_unit, producer_for};
use crate::obs::Observation;
use crate::semantics::Rules;
use crate::trust::TrustContext;
use std::collections::{BTreeMap, BTreeSet};

#[derive(Debug, Clone, PartialEq, Eq)]
struct Sig {
    parent: Option<u32>,
    children: Vec<u32>,
    rels: Vec<(String, u32)>,
    fields: (String, Option<String>, Option<String>, Option<String>, u32),
}

fn sig(obs: &Observation, i: usize) -> Sig {
    let n = &obs.nodes[i];
    Sig {
        parent: n.parent.map(|p| obs.nodes[p].id),
        children: obs.children[i].iter().map(|&c| obs.nodes[c].id).collect(),
        rels: n
            .rels
            .iter()
            .map(|(k, t)| (format!("{k:?}"), obs.nodes[*t].id))
            .collect(),
        fields: (
            n.tag.clone(),
            n.role.clone(),
            n.name.clone(),
            n.text.clone(),
            n.flags,
        ),
    }
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct Diff {
    pub added: BTreeSet<u32>,
    pub removed: BTreeSet<u32>,
    pub changed: BTreeSet<u32>,
}

impl Diff {
    pub fn touched(&self) -> BTreeSet<u32> {
        self.added
            .iter()
            .chain(&self.removed)
            .chain(&self.changed)
            .copied()
            .collect()
    }
}

pub fn diff(old: &Observation, new: &Observation) -> Diff {
    let mut d = Diff::default();
    for (i, n) in new.nodes.iter().enumerate() {
        match old.index_of(n.id) {
            None => {
                d.added.insert(n.id);
            }
            Some(j) => {
                if sig(old, j) != sig(new, i) {
                    d.changed.insert(n.id);
                }
            }
        }
    }
    for n in &old.nodes {
        if new.index_of(n.id).is_none() {
            d.removed.insert(n.id);
        }
    }
    d
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct Update {
    pub diff: Diff,
    pub recomputed: Vec<UnitKey>,
    pub created: Vec<UnitKey>,
    pub dropped: Vec<UnitKey>,
    pub reused: usize,
}

pub struct Session {
    rules: Rules,
    captured_at: i64,
    pub obs: Observation,
    pub rec: Recognition,
    pub ledger: Ledger,
    source: String,
    context: Option<TrustContext>,
    readers: BTreeMap<u32, BTreeSet<UnitKey>>,
}

fn reader_index(rec: &Recognition) -> BTreeMap<u32, BTreeSet<UnitKey>> {
    let mut m: BTreeMap<u32, BTreeSet<UnitKey>> = BTreeMap::new();
    for u in rec.units.values() {
        for &id in &u.reads {
            m.entry(id).or_default().insert(u.key);
        }
    }
    m
}

impl Session {
    pub fn new(obs: Observation, rules: Rules, captured_at: i64) -> Session {
        Session::in_context(obs, rules, captured_at, None)
    }

    pub fn in_context(
        obs: Observation,
        rules: Rules,
        captured_at: i64,
        context: Option<TrustContext>,
    ) -> Session {
        let rec = recognize(&obs, rules);
        let ledger = build_ledger_in(&obs, &rec, captured_at, context.as_ref());
        let source = crate::ledger::observation_source(&obs, context.as_ref());
        let readers = reader_index(&rec);
        Session {
            rules,
            captured_at,
            obs,
            rec,
            ledger,
            source,
            context,
            readers,
        }
    }

    fn unlink(&mut self, key: UnitKey) {
        if let Some(u) = self.rec.units.remove(&key) {
            for id in &u.reads {
                if let Some(set) = self.readers.get_mut(id) {
                    set.remove(&key);
                }
            }
        }
        self.ledger.claims.retain(|_, c| c.unit != Some(key));
    }

    fn link(&mut self, u: Unit, new: &Observation) {
        for &id in &u.reads {
            self.readers.entry(id).or_default().insert(u.key);
        }
        let producer = producer_for(self.rules);
        let cx = Ctx {
            obs: new,
            source: &self.source,
            producer: &producer,
        };
        claims_for_unit(&mut self.ledger, &cx, &u);
        self.rec.units.insert(u.key, u);
    }

    /// Unit claims whose nodes did not change are re-cited against the new observation, as a fresh run would.
    fn resource(&mut self, old: &str) {
        let moved: Vec<String> = self
            .ledger
            .claims
            .values()
            .filter(|c| c.unit.is_some() && c.evidence.iter().any(|e| e.source == old))
            .map(|c| c.id.clone())
            .collect();
        for id in moved {
            if let Some(c) = self.ledger.claims.remove(&id) {
                let evidence = c
                    .evidence
                    .into_iter()
                    .map(|mut e| {
                        if e.source == old {
                            e.source = self.source.clone();
                        }
                        e
                    })
                    .collect();
                self.ledger.push(NewClaim {
                    subject: c.subject,
                    predicate: c.predicate,
                    object: c.object,
                    certainty: c.certainty,
                    confidence: c.confidence,
                    evidence,
                    unit: c.unit,
                    producer: c.producer,
                });
            }
        }
    }

    /// Moves to `new`, recomputing only what the change can reach.
    pub fn update(&mut self, new: Observation) -> Update {
        let d = diff(&self.obs, &new);
        let touched = d.touched();
        let mut affected: BTreeSet<UnitKey> = BTreeSet::new();
        for id in &touched {
            if let Some(set) = self.readers.get(id) {
                affected.extend(set.iter().copied());
            }
        }
        let mut up = Update::default();
        let old_source = std::mem::replace(
            &mut self.source,
            self.ledger
                .record_observation(&new, self.captured_at, self.context.as_ref())
                .unwrap_or_default(),
        );
        let before = self.rec.units.len();
        for key in &affected {
            self.unlink(*key);
        }
        let mut candidates: BTreeSet<UnitKey> = affected.clone();
        let queries = discovery_queries();
        for id in d.added.iter().chain(&d.changed) {
            if let Some(i) = new.index_of(*id) {
                for (kind, q) in &queries {
                    if q.holds(&new.nodes[i]) {
                        candidates.insert((*kind, *id));
                    }
                }
            }
        }
        for key in candidates {
            let had = affected.contains(&key);
            match new
                .index_of(key.1)
                .and_then(|i| compute_unit(&new, key.0, i, self.rules))
            {
                Some(u) => {
                    if had {
                        up.recomputed.push(key);
                    } else {
                        up.created.push(key);
                    }
                    self.link(u, &new);
                }
                None => {
                    if had {
                        up.dropped.push(key);
                    }
                }
            }
        }
        up.reused = before.saturating_sub(affected.len());
        if old_source != self.source {
            self.resource(&old_source);
        }
        self.rec.digest = new.digest.clone();
        self.rec.stats = RunStats {
            nodes: new.len(),
            units_computed: up.recomputed.len() + up.created.len(),
            candidates_examined: 0,
        };
        self.obs = new;
        up.diff = d;
        up
    }
}
