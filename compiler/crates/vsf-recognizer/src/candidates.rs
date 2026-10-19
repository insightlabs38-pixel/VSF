//! Role, tag and wording collisions stay as separate candidates; nothing is picked, no model is called.

use crate::engine::Recognition;
use crate::semantics::{Action, Binding, Field, FieldKind, Strength, Verb};
use std::collections::BTreeMap;

#[derive(Debug, Clone, PartialEq, Eq, PartialOrd, Ord)]
pub enum Hypothesis {
    Field(FieldKind),
    Action(String),
    Verb(Verb),
    Label(String),
}

impl Hypothesis {
    pub fn describe(&self) -> String {
        match self {
            Hypothesis::Field(k) => format!("field:{}", k.as_str()),
            Hypothesis::Action(r) => format!("action:{r}"),
            Hypothesis::Verb(v) => format!("verb:{}", v.as_str()),
            Hypothesis::Label(t) => format!("label:{t}"),
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Candidate {
    pub hypothesis: Hypothesis,
    pub strength: Strength,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord)]
pub enum Resolution {
    Single,
    Ambiguous,
    Conflicted,
}

impl Resolution {
    pub fn as_str(self) -> &'static str {
        match self {
            Resolution::Single => "single",
            Resolution::Ambiguous => "ambiguous",
            Resolution::Conflicted => "conflicted",
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Group {
    pub node: u32,
    pub topic: &'static str,
    pub resolution: Resolution,
    pub candidates: Vec<Candidate>,
}

/// The two readings of an element that is both a control and an action.
pub fn kind_group(f: &Field) -> Group {
    Group {
        node: f.node,
        topic: "kind",
        resolution: Resolution::Conflicted,
        candidates: vec![
            Candidate {
                hypothesis: Hypothesis::Field(f.kind),
                strength: Strength::Structural,
            },
            Candidate {
                hypothesis: Hypothesis::Action(f.action_roles.join("|")),
                strength: Strength::Structural,
            },
        ],
    }
}

/// One candidate per verb the wording could mean; none when the wording names at most one.
pub fn verb_group(a: &Action) -> Option<Group> {
    (a.verbs.len() > 1).then(|| Group {
        node: a.node,
        topic: "verb",
        resolution: Resolution::Ambiguous,
        candidates: a
            .verbs
            .iter()
            .map(|&v| Candidate {
                hypothesis: Hypothesis::Verb(v),
                strength: Strength::Lexical,
            })
            .collect(),
    })
}

pub fn label_group(f: &Field) -> Option<Group> {
    let Binding::Ambiguous(cs) = &f.binding else {
        return None;
    };
    let mut candidates: Vec<Candidate> = cs
        .iter()
        .map(|c| Candidate {
            hypothesis: Hypothesis::Label(c.text.clone()),
            strength: c.source.strength(),
        })
        .collect();
    candidates.sort_by(|a, b| a.hypothesis.cmp(&b.hypothesis));
    candidates.dedup();
    Some(Group {
        node: f.node,
        topic: "label",
        resolution: Resolution::Ambiguous,
        candidates,
    })
}

/// Every candidate group in the run, ordered by node then topic.
pub fn groups(rec: &Recognition) -> Vec<Group> {
    let mut out: BTreeMap<(u32, &'static str), Group> = BTreeMap::new();
    let mut add = |g: Group| {
        out.insert((g.node, g.topic), g);
    };
    for f in rec.fields() {
        if let Some(g) = label_group(f) {
            add(g);
        }
        if !f.action_roles.is_empty() {
            add(kind_group(f));
        }
    }
    for a in rec.actions() {
        if let Some(g) = verb_group(a) {
            add(g);
        }
    }
    out.into_values().collect()
}

/// Outcome for one node and topic: resolved only when exactly one defensible reading remains.
pub fn lookup<'a>(gs: &'a [Group], node: u32, topic: &str) -> Option<&'a Group> {
    gs.iter().find(|g| g.node == node && g.topic == topic)
}
