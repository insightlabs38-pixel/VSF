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
            ]),
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
            ]),
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
    obj(vec![
        ("observation", s(&rec.digest)),
        ("rules", s(rec.rules.version())),
        ("schema", s("vsf-recognition-v1")),
        ("units", list(units)),
    ])
}
