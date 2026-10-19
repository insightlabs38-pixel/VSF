//! The diff as a stable, machine-readable document for the CLI and the future Studio.

use crate::diff::{Class, Diff};
use crate::impact::{Mode, Selection};
use vsf_canon::Value;

pub const SCHEMA: &str = "vsf-diff-report-v1";

fn text(s: &str) -> Value {
    Value::Str(s.to_string())
}

fn strings(items: &[String]) -> Value {
    Value::Array(items.iter().map(|s| text(s)).collect())
}

fn opt(s: &Option<String>) -> Value {
    s.as_deref().map_or(Value::Null, text)
}

/// Classes that need a person to look before anything is promoted.
fn needs_review(c: Class) -> bool {
    c >= Class::Breaking
}

pub fn to_value(d: &Diff, selection: Option<&Selection>) -> Value {
    let count = |c: Class| Value::Int(d.changes.iter().filter(|x| x.class == c).count() as i64);
    let changes = d
        .changes
        .iter()
        .map(|c| {
            Value::Object(vec![
                ("after".into(), opt(&c.after)),
                ("before".into(), opt(&c.before)),
                ("class".into(), text(c.class.as_str())),
                ("op".into(), text(c.op.as_str())),
                ("path".into(), text(&c.path)),
                ("pattern".into(), text(&c.pattern)),
                ("review".into(), Value::Bool(needs_review(c.class))),
            ])
        })
        .collect();
    let mut fields: Vec<(String, Value)> = vec![
        ("changes".into(), Value::Array(changes)),
        (
            "class".into(),
            d.class.map_or(Value::Null, |c| text(c.as_str())),
        ),
        ("family".into(), text(d.family)),
        ("from".into(), text(&d.from)),
        (
            "review_required".into(),
            Value::Bool(d.class.is_some_and(needs_review)),
        ),
        ("schema".into(), text(SCHEMA)),
        (
            "summary".into(),
            Value::Object(vec![
                ("additive".into(), count(Class::Additive)),
                ("breaking".into(), count(Class::Breaking)),
                ("cosmetic".into(), count(Class::Cosmetic)),
                ("permission".into(), count(Class::Permission)),
                ("unknown".into(), count(Class::Unknown)),
            ]),
        ),
        ("to".into(), text(&d.to)),
    ];
    if let Some(s) = selection {
        let (mode, reason) = match s.mode {
            Mode::Targeted => ("targeted", Value::Null),
            Mode::Full(r) => ("full", text(r)),
        };
        fields.push((
            "selection".into(),
            Value::Object(vec![
                ("changed".into(), strings(&s.changed)),
                ("impacted".into(), strings(&s.impacted)),
                ("mode".into(), text(mode)),
                ("reason".into(), reason),
                ("tests".into(), strings(&s.tests)),
            ]),
        ));
    }
    fields.sort_by(|a, b| a.0.cmp(&b.0));
    Value::Object(fields)
}
