use crate::frame::Params;
use crate::ids::{Ids, slug_of};
use crate::infer::column_type;
use crate::model::{ActionOut, Avail, EntityOut, FieldOut, RowOut, SrcRef, Withheld};
use crate::{MAX_ACTIONS, MAX_ENTITIES, MAX_FIELDS, MAX_ROWS, SCHEMA};
use std::collections::{BTreeMap, BTreeSet};
use vsf_canon::{Value, sha256_hex};
use vsf_ir::doc::{document_digest, encode_document};
use vsf_ir::semantics::validate_semantic;
use vsf_ir::{
    ActionDef, Body, Certainty, CompilerVersion, Digest, EffectAssertion, EffectAssessment,
    EffectClass, Entity, EvidenceDef, EvidenceType, FieldDef, Ident, Instant, Label,
    ObservationEpoch, SemanticIR, SourceAppId, SourceVariantId, Tristate, ValueType,
};
use vsf_recognizer::contract::{Availability, Contract};
use vsf_recognizer::engine::Recognition;
use vsf_recognizer::obs::{F_CREDENTIAL, F_OPAQUE, Observation};
use vsf_recognizer::semantics::{
    Binding, Collection, FieldKind, Reader, Verb, is_action, is_control, region_name, table_rows,
};
use vsf_recognizer::val::{count, list, n, obj, s};

type Fail = &'static str;

fn clean(raw: &str) -> String {
    let kept: String = raw
        .chars()
        .filter(|&c| {
            let u = c as u32;
            !(u < 0x20
                || (0x7f..=0x9f).contains(&u)
                || (0x2028..=0x202e).contains(&u)
                || (0x2066..=0x2069).contains(&u))
        })
        .collect();
    kept.split_whitespace()
        .collect::<Vec<_>>()
        .join(" ")
        .chars()
        .take(200)
        .collect()
}

fn label(raw: &str, fallback: &str) -> Label {
    let c = clean(raw);
    let text = if c.is_empty() { fallback } else { c.as_str() };
    Label::new(text)
        .or_else(|_| Label::new(fallback))
        .unwrap_or_else(|_| unreachable_label())
}

fn unreachable_label() -> Label {
    match Label::new("Item") {
        Ok(l) => l,
        Err(_) => unreachable!(),
    }
}

fn ident(raw: &str) -> Result<Ident, Fail> {
    Ident::new(raw).map_err(|_| "assemble.bad_identifier")
}

fn hint(v: Verb) -> EffectClass {
    match v {
        Verb::Query | Verb::Export | Verb::Navigate | Verb::Dismiss => EffectClass::ReadOnly,
        Verb::Create | Verb::Commit => EffectClass::NonIdempotentWrite,
        Verb::Destroy => EffectClass::Destructive,
    }
}

fn value_type(kind: FieldKind) -> ValueType {
    match kind {
        FieldKind::Text => ValueType::Text,
        FieldKind::Choice => ValueType::Choice,
        FieldKind::Toggle => ValueType::Boolean,
        FieldKind::Range => ValueType::Decimal,
    }
}

fn app_id(origin: &str) -> String {
    let (scheme, rest) = origin.split_once("://").unwrap_or(("http", origin));
    let mapped: String = rest
        .chars()
        .map(|c| {
            if c.is_ascii_alphanumeric() || c == '.' {
                c.to_ascii_lowercase()
            } else {
                '-'
            }
        })
        .collect();
    format!("{scheme}:{mapped}")
}

fn avail_of(a: Availability) -> Avail {
    Avail {
        state: a.as_str(),
        reason: a.reason(),
    }
}

/// The words a cell shows, leaving out controls and actions, which are recognized separately.
fn cell_text(r: &Reader, i: usize) -> String {
    let n = r.node(i);
    if n.has(F_OPAQUE) || n.has(F_CREDENTIAL) || is_action(n) || is_control(n) {
        return String::new();
    }
    if let Some(t) = n.text.as_deref() {
        let t = clean(t);
        if !t.is_empty() {
            return t;
        }
    }
    let mut parts = Vec::new();
    let mut stack: Vec<usize> = r.children(i).iter().rev().copied().collect();
    while let Some(c) = stack.pop() {
        let cn = r.node(c);
        if !cn.visible() || cn.has(F_OPAQUE) || is_action(cn) || is_control(cn) {
            continue;
        }
        if let Some(t) = cn.text.as_deref() {
            parts.push(clean(t));
        }
        stack.extend(r.children(c).iter().rev().copied());
        if parts.len() > 20 {
            break;
        }
    }
    clean(&parts.join(" "))
}

struct Owners<'a> {
    obs: &'a Observation,
    collections: BTreeMap<usize, usize>,
}

impl Owners<'_> {
    /// The collection (by entity index) whose subtree holds `i`, if any.
    fn within_collection(&self, i: usize) -> Option<usize> {
        if let Some(&e) = self.collections.get(&i) {
            return Some(e);
        }
        self.obs
            .ancestors(i)
            .find_map(|a| self.collections.get(&a).copied())
    }
}

struct Built {
    entities: Vec<EntityOut>,
    actions: Vec<ActionOut>,
    withheld: Vec<Withheld>,
    truncated: bool,
}

fn collection_entity(
    r: &Reader,
    ids: &mut Ids,
    c: &Collection,
    ordinal: usize,
    truncated: &mut bool,
) -> Option<(EntityOut, usize, Vec<usize>)> {
    let idx = r.obs.index_of(c.node)?;
    let (header, mut body) = table_rows(r, idx);
    if body.len() > MAX_ROWS {
        body.truncate(MAX_ROWS);
        *truncated = true;
    }
    let width = header
        .map(|h| r.children(h).len())
        .into_iter()
        .chain(body.iter().map(|&row| r.children(row).len()))
        .max()
        .unwrap_or(0);
    let cells: Vec<Vec<String>> = body
        .iter()
        .map(|&row| {
            (0..width)
                .map(|k| {
                    r.children(row)
                        .get(k)
                        .map(|&cell| cell_text(r, cell))
                        .unwrap_or_default()
                })
                .collect()
        })
        .collect();
    let has_action = |col: usize| {
        body.iter().any(|&row| {
            r.children(row).get(col).is_some_and(|&cell| {
                std::iter::once(cell)
                    .chain(r.obs.descendants(cell))
                    .any(|d| is_action(r.node(d)))
            })
        })
    };
    let kept: Vec<usize> = (0..width)
        .filter(|&k| {
            let any_text = cells
                .iter()
                .any(|row| row.get(k).is_some_and(|t| !t.is_empty()));
            any_text || (body.is_empty() && !has_action(k))
        })
        .collect();
    if kept.is_empty() {
        return None;
    }
    let entity_label = c
        .label
        .clone()
        .unwrap_or_else(|| format!("Table {}", ordinal + 1));
    let entity_id = ids.alloc("t", &entity_label);
    let mut fields = Vec::new();
    for &k in kept.iter().take(MAX_FIELDS) {
        let name = c
            .columns
            .get(k)
            .cloned()
            .flatten()
            .map(|t| clean(&t))
            .filter(|t| !t.is_empty());
        let column: Vec<&str> = cells
            .iter()
            .map(|row| row.get(k).map_or("", String::as_str))
            .collect();
        let vt = column_type(&column);
        fields.push(FieldOut {
            id: ids.alloc(
                "f",
                &format!(
                    "{entity_label} {}",
                    name.as_deref().unwrap_or(&format!("column {}", k + 1))
                ),
            ),
            name: name.unwrap_or_else(|| format!("Column {}", k + 1)),
            value_type: vt,
            certainty: if c.columns.get(k).is_some_and(Option::is_some) {
                Certainty::Inferred
            } else {
                Certainty::Unknown
            },
            shape_inferred: vt != ValueType::Text,
        });
    }
    let first: Vec<String> = kept
        .first()
        .map(|&k| {
            cells
                .iter()
                .map(|row| slug_of(row.get(k).map_or("", String::as_str)))
                .collect()
        })
        .unwrap_or_default();
    let natural = !first.is_empty()
        && first.iter().all(|k| !k.is_empty())
        && first.iter().collect::<BTreeSet<_>>().len() == first.len();
    let rows = cells
        .iter()
        .enumerate()
        .map(|(i, row)| RowOut {
            key: if natural {
                format!("k_{}", first.get(i).map_or("", String::as_str))
            } else {
                format!("r{}", i + 1)
            },
            values: kept
                .iter()
                .zip(&fields)
                .map(|(&k, f)| (f.id.clone(), row.get(k).cloned().unwrap_or_default()))
                .collect(),
        })
        .collect();
    let entity = EntityOut {
        id: entity_id,
        kind: "record",
        label: entity_label,
        layout: "grid",
        fields,
        rows,
    };
    Some((entity, idx, body))
}

fn build_entities(
    obs: &Observation,
    rec: &Recognition,
    contract: &Contract,
    ids: &mut Ids,
) -> Built {
    let r = Reader::new(obs);
    let mut entities: Vec<EntityOut> = Vec::new();
    let mut withheld = Vec::new();
    let mut truncated = false;
    let mut owners = Owners {
        obs,
        collections: BTreeMap::new(),
    };
    let mut row_of: BTreeMap<usize, (usize, String)> = BTreeMap::new();

    for (ordinal, c) in rec.collections().enumerate() {
        if entities.len() >= MAX_ENTITIES {
            truncated = true;
            break;
        }
        let Some((entity, idx, rows)) = collection_entity(&r, ids, c, ordinal, &mut truncated)
        else {
            continue;
        };
        let e = entities.len();
        owners.collections.insert(idx, e);
        for (row_node, out) in rows.iter().zip(&entity.rows) {
            row_of.insert(*row_node, (e, out.key.clone()));
        }
        entities.push(entity);
    }

    let mut forms: BTreeMap<Option<String>, usize> = BTreeMap::new();
    for f in rec.fields() {
        let Some(i) = obs.index_of(f.node) else {
            continue;
        };
        if owners.within_collection(i).is_some() {
            continue;
        }
        let node = &obs.nodes[i];
        if node.has(F_CREDENTIAL) {
            withheld.push(Withheld {
                node: f.node,
                reason: "credential_field",
            });
            continue;
        }
        if f.why_unusable == Some("not_visible") {
            withheld.push(Withheld {
                node: f.node,
                reason: "not_visible",
            });
            continue;
        }
        if !f.action_roles.is_empty() {
            withheld.push(Withheld {
                node: f.node,
                reason: "kind_conflict",
            });
            continue;
        }
        let slot = match forms.get(&f.scope) {
            Some(&e) => e,
            None if entities.len() < MAX_ENTITIES => {
                let name = f.scope.clone().unwrap_or_else(|| "Form".to_string());
                entities.push(EntityOut {
                    id: ids.alloc("form", &name),
                    kind: "form",
                    label: name,
                    layout: "form",
                    fields: Vec::new(),
                    rows: Vec::new(),
                });
                forms.insert(f.scope.clone(), entities.len() - 1);
                entities.len() - 1
            }
            None => {
                truncated = true;
                continue;
            }
        };
        let Some(entity) = entities.get_mut(slot) else {
            continue;
        };
        if entity.fields.len() >= MAX_FIELDS {
            truncated = true;
            continue;
        }
        let (name, certainty) = match &f.binding {
            Binding::Bound(c) => (clean(&c.text), Certainty::Inferred),
            Binding::Ambiguous(_) | Binding::Unbound => {
                ("Unlabeled field".to_string(), Certainty::Unknown)
            }
        };
        entity.fields.push(FieldOut {
            id: ids.alloc("f", &format!("{} {name}", entity.label)),
            name,
            value_type: value_type(f.kind),
            certainty,
            shape_inferred: false,
        });
    }

    let mut page: Option<usize> = None;
    let mut actions: Vec<ActionOut> = Vec::new();
    let mut group: BTreeMap<(usize, String), usize> = BTreeMap::new();
    for a in rec.actions() {
        let Some(i) = obs.index_of(a.node) else {
            continue;
        };
        if a.why_unusable == Some("not_visible") {
            withheld.push(Withheld {
                node: a.node,
                reason: "not_visible",
            });
            continue;
        }
        let (owner, row) = match owners.within_collection(i) {
            Some(e) => {
                let key = std::iter::once(i)
                    .chain(obs.ancestors(i))
                    .find_map(|x| row_of.get(&x).map(|(_, k)| k.clone()));
                (e, key)
            }
            None => {
                let scope = region_name(&r, i);
                let found = forms.get(&scope).copied();
                match found {
                    Some(e) => (e, None),
                    None => {
                        let e = match page {
                            Some(e) => e,
                            None if entities.len() < MAX_ENTITIES => {
                                entities.push(EntityOut {
                                    id: ids.alloc("page", "actions"),
                                    kind: "page",
                                    label: "Page actions".to_string(),
                                    layout: "form",
                                    fields: Vec::new(),
                                    rows: Vec::new(),
                                });
                                page = Some(entities.len() - 1);
                                entities.len() - 1
                            }
                            None => {
                                truncated = true;
                                continue;
                            }
                        };
                        (e, None)
                    }
                }
            }
        };
        let name = a.name.as_deref().map(clean).filter(|t| !t.is_empty());
        let avail = contract.for_node(a.node).next().map_or(
            Avail {
                state: "unknown",
                reason: Some("no_offer"),
            },
            |o| avail_of(o.availability),
        );
        let key = (owner, name.clone().unwrap_or_default());
        if let Some(&g) = group.get(&key) {
            if let Some(out) = actions.get_mut(g) {
                if out.avail != avail {
                    out.avail = Avail {
                        state: "unknown",
                        reason: Some("mixed_availability"),
                    };
                }
                out.sources.push(SrcRef { row, node: a.node });
            }
            continue;
        }
        if actions.len() >= MAX_ACTIONS {
            truncated = true;
            continue;
        }
        let entity_id = entities
            .get(owner)
            .map_or_else(String::new, |e| e.id.clone());
        let (declared, from_verb) = match a.verbs.as_slice() {
            [v] => (hint(*v), true),
            _ => (EffectClass::Indeterminate, false),
        };
        group.insert(key, actions.len());
        actions.push(ActionOut {
            id: ids.alloc(
                "act",
                &format!("{entity_id} {}", name.as_deref().unwrap_or("unnamed")),
            ),
            entity: entity_id,
            label: name.unwrap_or_else(|| "Unnamed action".to_string()),
            declared,
            from_verb,
            certainty: if from_verb {
                Certainty::Inferred
            } else {
                Certainty::Unknown
            },
            avail,
            sources: vec![SrcRef { row, node: a.node }],
        });
    }
    Built {
        entities,
        actions,
        withheld,
        truncated,
    }
}

fn semantic(p: &Params, built: &Built) -> Result<SemanticIR, Fail> {
    let ev_obs = ident("ev_obs")?;
    let ev_heur = ident("ev_heur")?;
    let at = Instant(p.observed_at);
    let evidence = vec![
        EvidenceDef {
            id: ev_obs.clone(),
            r#type: EvidenceType::ObservedStructure,
            witness: label("dom-recognizer", "recognizer"),
            observed_at: at,
            observation_epoch: Some(ObservationEpoch(p.epoch)),
            authorizing_context: None,
            depends_on: Vec::new(),
            detail: None,
        },
        EvidenceDef {
            id: ev_heur.clone(),
            r#type: EvidenceType::HeuristicInference,
            witness: label("label-lexicon-and-column-shape", "heuristic"),
            observed_at: at,
            observation_epoch: Some(ObservationEpoch(p.epoch)),
            authorizing_context: None,
            depends_on: vec![ev_obs.clone()],
            detail: None,
        },
    ];
    let mut entities = Vec::new();
    for e in &built.entities {
        let mut fields = Vec::new();
        for f in &e.fields {
            let mut refs = vec![ev_obs.clone()];
            if f.shape_inferred {
                refs.push(ev_heur.clone());
            }
            fields.push(FieldDef {
                id: ident(&f.id)?,
                name: label(&f.name, "Field"),
                value_type: f.value_type,
                unit: None,
                nullable: Tristate::Unknown,
                certainty: f.certainty,
                evidence: refs,
            });
        }
        entities.push(Entity {
            id: ident(&e.id)?,
            kind: ident(e.kind)?,
            label: Some(label(&e.label, "Entity")),
            fields,
            certainty: Certainty::Inferred,
            evidence: vec![ev_obs.clone()],
        });
    }
    let mut actions = Vec::new();
    for a in &built.actions {
        let mut refs = vec![ev_obs.clone()];
        let mut asserted = Vec::new();
        if a.from_verb {
            refs.push(ev_heur.clone());
            asserted.push(EffectAssertion {
                evidence: ev_heur.clone(),
                effect: a.declared,
            });
        }
        actions.push(ActionDef {
            id: ident(&a.id)?,
            entity: Some(ident(&a.entity)?),
            label: label(&a.label, "Action"),
            effect: EffectAssessment {
                declared: a.declared,
                asserted,
            },
            source_confirmation: Tristate::Unknown,
            certainty: a.certainty,
            evidence: refs,
        });
    }
    Ok(SemanticIR {
        source_app_id: SourceAppId::new(&app_id(&p.origin)).map_err(|_| "assemble.bad_origin")?,
        source_variant_id: SourceVariantId::new(&p.variant_id)
            .map_err(|_| "assemble.bad_variant")?,
        observations: vec![ObservationEpoch(p.epoch)],
        compiler_version: CompilerVersion::new(&format!(
            "0.1.0+{}",
            &sha256_hex(b"vsf-assemble-1/rules-v2")[..12]
        ))
        .map_err(|_| "assemble.bad_identifier")?,
        primitive_set: Digest::new(&format!(
            "sha256:{}",
            sha256_hex(b"vsf-recognizer-rules-v2")
        ))
        .map_err(|_| "assemble.bad_identifier")?,
        entities,
        relationships: Vec::new(),
        actions,
        state_machines: Vec::new(),
        evidence,
        claims: Vec::new(),
    })
}

fn view_values(built: &Built) -> (Value, Value, Value, Value) {
    let views = list(
        built
            .entities
            .iter()
            .map(|e| obj(vec![("entity", s(&e.id)), ("layout", s(e.layout))]))
            .collect(),
    );
    let mut rows = Vec::new();
    for e in built.entities.iter().filter(|e| e.layout == "grid") {
        let items = e
            .rows
            .iter()
            .map(|row| {
                obj(vec![
                    ("key", s(&row.key)),
                    (
                        "values",
                        Value::Object(row.values.iter().map(|(k, v)| (k.clone(), s(v))).collect()),
                    ),
                ])
            })
            .collect();
        rows.push((e.id.clone(), list(items)));
    }
    let acts = Value::Object(
        built
            .actions
            .iter()
            .map(|a| {
                let sources = a
                    .sources
                    .iter()
                    .map(|x| {
                        let mut f = vec![("node", n(i64::from(x.node)))];
                        if let Some(row) = &x.row {
                            f.push(("row", s(row)));
                        }
                        obj(f)
                    })
                    .collect();
                (
                    a.id.clone(),
                    obj(vec![
                        ("reason", a.avail.reason.map_or(Value::Null, s)),
                        ("sources", list(sources)),
                        ("state", s(a.avail.state)),
                    ]),
                )
            })
            .collect(),
    );
    let withheld = list(
        built
            .withheld
            .iter()
            .map(|w| {
                obj(vec![
                    ("node", n(i64::from(w.node))),
                    ("reason", s(w.reason)),
                ])
            })
            .collect(),
    );
    (views, Value::Object(rows), acts, withheld)
}

pub fn run(
    p: &Params,
    obs: &Observation,
    rec: &Recognition,
    contract: &Contract,
) -> Result<Value, Fail> {
    let mut ids = Ids::default();
    let built = build_entities(obs, rec, contract, &mut ids);
    let sem = semantic(p, &built)?;
    if !validate_semantic(&sem).is_empty() {
        return Err("assemble.invalid_semantic");
    }
    let body = Body::Semantic(sem);
    let digest = document_digest(&body);
    let doc = vsf_canon::parse(&encode_document(&body)).map_err(|_| "assemble.invalid_semantic")?;
    let (views, rows, actions, withheld) = view_values(&built);
    Ok(obj(vec![
        ("actions", actions),
        (
            "limits",
            obj(vec![
                ("nodes", count(obs.len())),
                ("truncated", Value::Bool(built.truncated)),
            ]),
        ),
        ("observation", s(&obs.digest)),
        ("rows", rows),
        ("schema", s(SCHEMA)),
        ("semantic", doc),
        ("semantic_digest", s(&digest)),
        ("views", views),
        ("withheld", withheld),
    ]))
}
