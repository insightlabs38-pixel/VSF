use vsf_ir::{Certainty, EffectClass, ValueType};

pub struct FieldOut {
    pub id: String,
    pub name: String,
    pub value_type: ValueType,
    pub certainty: Certainty,
    pub shape_inferred: bool,
}

pub struct RowOut {
    pub key: String,
    pub values: Vec<(String, String)>,
}

pub struct EntityOut {
    pub id: String,
    pub kind: &'static str,
    pub label: String,
    pub layout: &'static str,
    pub fields: Vec<FieldOut>,
    pub rows: Vec<RowOut>,
}

pub struct SrcRef {
    pub row: Option<String>,
    pub node: u32,
}

#[derive(Clone, PartialEq, Eq)]
pub struct Avail {
    pub state: &'static str,
    pub reason: Option<&'static str>,
}

pub struct ActionOut {
    pub id: String,
    pub entity: String,
    pub label: String,
    pub declared: EffectClass,
    pub from_verb: bool,
    pub certainty: Certainty,
    pub avail: Avail,
    pub sources: Vec<SrcRef>,
}

pub struct Withheld {
    pub node: u32,
    pub reason: &'static str,
}
