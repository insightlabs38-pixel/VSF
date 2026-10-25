//! Canonical documents for recognition output, used by the command line and by evidence logs.

use crate::engine::{Output, Recognition};
use crate::semantics::Binding;
use crate::val::{count, list, n, obj, s};
use vsf_canon::Value;

fn binding(b: &Binding) -> Value {
    match b {
        Binding::Bound(c) => obj(vec![
            ("source", s(c.source.as_str())),
            ("state", s("bound")),
            ("strength", s(c.source.strength().as_str())),
            ("text", s(&c.text)),
        ]),
        Binding::Ambiguous(cs) => obj(vec![
            ("candidates", list(cs.iter().map(|c| s(&c.text)).collect())),
            ("state", s("ambiguous")),
        ]),
        Binding::Unbound => obj(vec![("state", s("unbound"))]),
    }
}

pub fn recognition_value(rec: &Recognition) -> Value {
    let units = rec
        .units
        .values()
        .map(|u| match &u.output {
            Output::Field(f) => obj(vec![
                ("binding", binding(&f.binding)),
                ("kind", s(f.kind.as_str())),
                ("node", n(i64::from(f.node))),
                ("scope", f.scope.as_deref().map_or(Value::Null, s)),
                ("type", s("field")),
                ("usable", Value::Bool(f.usable)),
            ]
            .into_iter()
            .chain(
                rec.rules
                    .scoped
                    .then_some(("editable", Value::Bool(f.editable))),
            )
            .collect()),
            Output::Action(a) => obj(vec![
                ("name", a.name.as_deref().map_or(Value::Null, s)),
                ("node", n(i64::from(a.node))),
                ("roles", list(a.roles.iter().map(|r| s(r)).collect())),
                ("type", s("action")),
                ("usable", Value::Bool(a.usable)),
                (
                    "verbs",
                    list(a.verbs.iter().map(|v| s(v.as_str())).collect()),
                ),
            ]
            .into_iter()
            .chain(
                rec.rules
                    .scoped
                    .then_some(("shown", a.shown.as_deref().map_or(Value::Null, s))),
            )
            .collect()),
            Output::Collection(c) => obj(vec![
                (
                    "columns",
                    list(
                        c.columns
                            .iter()
                            .map(|x| x.as_deref().map_or(Value::Null, s))
                            .collect(),
                    ),
                ),
                ("label", c.label.as_deref().map_or(Value::Null, s)),
                ("node", n(i64::from(c.node))),
                ("rows", count(c.rows)),
                ("type", s("collection")),
            ]),
        })
        .collect();
    let composites = rec
        .composites
        .iter()
        .map(|c| {
            obj(vec![
                (
                    "covers",
                    list(c.covers.iter().map(|x| n(i64::from(*x))).collect()),
                ),
                ("evidence", list(c.evidence.iter().map(|e| s(e)).collect())),
                ("kind", s(c.kind.as_str())),
                ("label", c.label.as_deref().map_or(Value::Null, s)),
                (
                    "members",
                    list(
                        c.members
                            .iter()
                            .map(|m| {
                                obj(vec![
                                    ("name", m.name.as_deref().map_or(Value::Null, s)),
                                    ("node", n(i64::from(m.node))),
                                    ("part", m.part.map_or(Value::Null, s)),
                                ])
                            })
                            .collect(),
                    ),
                ),
                ("node", n(i64::from(c.node))),
                ("owner", c.owner.map_or(Value::Null, |o| n(i64::from(o)))),
            ])
        })
        .collect();
    let mut doc = vec![
        ("observation", s(&rec.digest)),
        ("rules", s(rec.rules.version())),
        ("schema", s("vsf-recognition-v1")),
        ("units", list(units)),
    ];
    if rec.rules.scoped {
        doc.push(("composites", list(composites)));
    }
    obj(doc)
}
