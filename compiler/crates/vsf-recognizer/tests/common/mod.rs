#![allow(dead_code, clippy::unwrap_used, clippy::expect_used, clippy::panic)]

use std::path::PathBuf;
use vsf_recognizer::obs::{F_EDITABLE, F_INTERACTIVE, F_VISIBLE, Node, Observation, RelKind};

pub const V: u32 = F_VISIBLE;
pub const CTL: u32 = F_VISIBLE | F_INTERACTIVE | F_EDITABLE;
pub const BTN: u32 = F_VISIBLE | F_INTERACTIVE;

pub struct B {
    nodes: Vec<Node>,
}

impl B {
    pub fn new() -> B {
        let mut b = B { nodes: Vec::new() };
        b.nodes.push(Node {
            id: 1,
            parent: None,
            tag: "#document".into(),
            role: None,
            name: None,
            text: None,
            flags: 0,
            rels: Vec::new(),
            states: Vec::new(),
            scope: None,
        });
        b
    }

    pub fn add(
        &mut self,
        parent: u32,
        tag: &str,
        role: Option<&str>,
        name: Option<&str>,
        text: Option<&str>,
        flags: u32,
    ) -> u32 {
        let id = self.nodes.len() as u32 + 1;
        let p = self.nodes.iter().position(|n| n.id == parent).unwrap();
        self.nodes.push(Node {
            id,
            parent: Some(p),
            tag: tag.into(),
            role: role.map(String::from),
            name: name.map(String::from),
            text: text.map(String::from),
            flags,
            rels: Vec::new(),
            states: Vec::new(),
            scope: None,
        });
        id
    }

    pub fn div(&mut self, parent: u32) -> u32 {
        self.add(parent, "div", None, None, None, V)
    }

    pub fn text(&mut self, parent: u32, tag: &str, text: &str) -> u32 {
        self.add(parent, tag, None, None, Some(text), V)
    }

    pub fn input(&mut self, parent: u32) -> u32 {
        self.add(parent, "input", Some("textbox"), None, None, CTL)
    }

    pub fn button(&mut self, parent: u32, name: &str) -> u32 {
        self.add(
            parent,
            "button",
            Some("button"),
            Some(name),
            Some(name),
            BTN,
        )
    }

    pub fn state(&mut self, id: u32, token: &str) {
        let i = self.nodes.iter().position(|n| n.id == id).unwrap();
        self.nodes[i].states.push(token.into());
    }

    pub fn rel(&mut self, from: u32, kind: RelKind, to: u32) {
        let t = self.nodes.iter().position(|n| n.id == to).unwrap();
        let f = self.nodes.iter().position(|n| n.id == from).unwrap();
        self.nodes[f].rels.push((kind, t));
    }

    pub fn build(self) -> Observation {
        Observation::from_nodes(self.nodes)
    }
}

pub fn snapshot(name: &str) -> Observation {
    let dir =
        PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../../../fixtures/recognition/snapshots");
    Observation::from_json(&std::fs::read(dir.join(format!("{name}.json"))).unwrap()).unwrap()
}

pub const REGRESSION: [&str; 4] = ["plain-form", "table-orders", "aria-grid", "adversarial"];
pub const HELD_OUT: [&str; 2] = ["heldout-shadow-ds", "heldout-datagrid-edit"];

use std::collections::HashMap;

/// A mutable copy of an observation that can be edited by id and flattened back into preorder.
pub struct Edit {
    nodes: HashMap<u32, Node>,
    parent: HashMap<u32, Option<u32>>,
    kids: HashMap<u32, Vec<u32>>,
    relids: HashMap<u32, Vec<(RelKind, u32)>>,
    root: u32,
    next: u32,
}

impl Edit {
    pub fn of(obs: &Observation) -> Edit {
        let mut e = Edit {
            nodes: HashMap::new(),
            parent: HashMap::new(),
            kids: HashMap::new(),
            relids: HashMap::new(),
            root: obs.nodes[0].id,
            next: 1,
        };
        for (i, n) in obs.nodes.iter().enumerate() {
            e.nodes.insert(n.id, n.clone());
            e.relids.insert(
                n.id,
                n.rels.iter().map(|(k, t)| (*k, obs.nodes[*t].id)).collect(),
            );
            e.parent.insert(n.id, n.parent.map(|p| obs.nodes[p].id));
            e.kids.insert(
                n.id,
                obs.children[i].iter().map(|&c| obs.nodes[c].id).collect(),
            );
            e.next = e.next.max(n.id + 1);
        }
        e
    }

    pub fn node(&mut self, id: u32) -> &mut Node {
        self.nodes.get_mut(&id).unwrap()
    }

    pub fn remove(&mut self, id: u32) {
        let mut stack = vec![id];
        let p = self.parent[&id];
        while let Some(n) = stack.pop() {
            stack.extend(self.kids.remove(&n).unwrap_or_default());
            self.nodes.remove(&n);
            self.parent.remove(&n);
        }
        if let Some(p) = p {
            self.kids.get_mut(&p).unwrap().retain(|&c| c != id);
        }
    }

    #[allow(clippy::too_many_arguments)]
    pub fn insert(
        &mut self,
        parent: u32,
        at: usize,
        tag: &str,
        role: Option<&str>,
        name: Option<&str>,
        text: Option<&str>,
        flags: u32,
    ) -> u32 {
        let id = self.next;
        self.next += 1;
        self.nodes.insert(
            id,
            Node {
                id,
                parent: None,
                tag: tag.into(),
                role: role.map(String::from),
                name: name.map(String::from),
                text: text.map(String::from),
                flags,
                rels: Vec::new(),
                states: Vec::new(),
                scope: None,
            },
        );
        self.parent.insert(id, Some(parent));
        self.kids.insert(id, Vec::new());
        let list = self.kids.get_mut(&parent).unwrap();
        let at = at.min(list.len());
        list.insert(at, id);
        id
    }

    pub fn all_ids(&self) -> Vec<u32> {
        let mut v: Vec<u32> = self.nodes.keys().copied().collect();
        v.sort_unstable();
        v
    }

    pub fn kids_of(&self, id: u32) -> Vec<u32> {
        self.kids[&id].clone()
    }

    pub fn build(&self) -> Observation {
        let mut order = Vec::new();
        let mut stack = vec![self.root];
        while let Some(n) = stack.pop() {
            order.push(n);
            stack.extend(self.kids[&n].iter().rev().copied());
        }
        let pos: HashMap<u32, usize> = order.iter().enumerate().map(|(i, &id)| (id, i)).collect();
        let nodes = order
            .iter()
            .map(|id| {
                let mut n = self.nodes[id].clone();
                n.parent = self.parent[id].map(|p| pos[&p]);
                n.rels = self
                    .relids
                    .get(id)
                    .map(|v| {
                        v.iter()
                            .filter_map(|(k, t)| pos.get(t).map(|&p| (*k, p)))
                            .collect()
                    })
                    .unwrap_or_default();
                n
            })
            .collect();
        Observation::from_nodes(nodes)
    }
}

pub const OWNER: &str = "prin_0123456789abcdef0123456789abcdef";
pub const REVIEWER: &str = "prin_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";
pub const NOW: i64 = 1_790_000_100_000;

pub fn trust() -> vsf_recognizer::trust::TrustContext {
    vsf_recognizer::trust::TrustContext {
        app_id: "acme:crm-eu1".into(),
        variant_id: "var_0123456789abcdef0123456789abcdef".into(),
        origin: "https://crm.example.com".into(),
        principal: OWNER.into(),
        runtime_id: "rt_ext1".into(),
        mode: "extension".into(),
        capability_digest: format!("sha256:{}", "c".repeat(64)),
        source_revision: format!("sha256:{}", "d".repeat(64)),
    }
}

pub fn policy() -> vsf_recognizer::protocol::ProtocolPolicy {
    use vsf_recognizer::protocol::{Grant, ProtocolPolicy, Revocations};
    ProtocolPolicy {
        trust: trust(),
        now: NOW,
        max_age_ms: 30 * 24 * 3600 * 1000,
        required_scope: "observe.protocol".into(),
        reviewers: [REVIEWER.to_string()].into_iter().collect(),
        grants: vec![Grant {
            principal: OWNER.into(),
            scope: "observe.protocol".into(),
            granted_at: 1_789_999_999_000,
            revoked_at: None,
        }],
        known_witnesses: ["profile-w1".to_string(), "checkout-w1".to_string()]
            .into_iter()
            .collect(),
        revoked: Revocations::default(),
    }
}

pub struct Wit {
    pub id: String,
    pub status: &'static str,
    pub issuer: &'static str,
    pub kind: &'static str,
    pub node: u32,
    pub digest: String,
    pub class: &'static str,
    pub verified_at: i64,
    pub expires_at: i64,
    pub revoked_at: Option<i64>,
    pub ctx: vsf_recognizer::trust::TrustContext,
}

impl Wit {
    pub fn of(obs: &Observation, node: u32, kind: &'static str, class: &'static str) -> Wit {
        let i = obs.index_of(node).unwrap();
        Wit {
            id: format!(
                "wit_{:0>16x}",
                u64::from(node) * 7 + if kind == "action" { 1 } else { 2 }
            ),
            status: "verified",
            issuer: "fixture-verifier",
            kind,
            node,
            digest: vsf_recognizer::ledger::node_digest(obs, i),
            class,
            verified_at: NOW - 60_000,
            expires_at: NOW + 3_600_000,
            revoked_at: None,
            ctx: trust(),
        }
    }

    pub fn json(&self) -> String {
        let c = &self.ctx;
        let revoked = self
            .revoked_at
            .map_or(String::new(), |t| format!(r#","revoked_at":{t}"#));
        format!(
            r#"{{"schema":"vsf-behavioral-witness-v1","id":"{}","status":"{}","issuer":"{}","context":{{"app_id":"{}","variant_id":"{}","origin":"{}","principal":"{}","runtime_id":"{}","mode":"{}","capability_digest":"{}","source_revision":"{}"}},"operation":{{"kind":"{}","node":{},"node_digest":"{}"}},"effect":{{"class":"{}","evidence_digest":"sha256:{}"}},"verified_at":{},"expires_at":{}{revoked}}}"#,
            self.id,
            self.status,
            self.issuer,
            c.app_id,
            c.variant_id,
            c.origin,
            c.principal,
            c.runtime_id,
            c.mode,
            c.capability_digest,
            c.source_revision,
            self.kind,
            self.node,
            self.digest,
            self.class,
            "e".repeat(64),
            self.verified_at,
            self.expires_at
        )
    }

    pub fn value(&self) -> vsf_canon::Value {
        vsf_canon::parse(self.json().as_bytes()).unwrap()
    }
}

pub fn wpolicy(
    emission: vsf_recognizer::witness::Emission,
) -> vsf_recognizer::witness::WitnessPolicy {
    vsf_recognizer::witness::WitnessPolicy {
        trust: trust(),
        now: NOW,
        max_age_ms: 24 * 3600 * 1000,
        trusted_issuers: ["fixture-verifier".to_string(), "synthetic-test".to_string()]
            .into_iter()
            .collect(),
        revoked: vsf_recognizer::protocol::Revocations::default(),
        emission,
    }
}

pub fn bare_contract(
    rec: &vsf_recognizer::engine::Recognition,
    ledger: &vsf_recognizer::ledger::Ledger,
) -> vsf_recognizer::contract::Contract {
    vsf_recognizer::contract::derive(
        rec,
        ledger,
        &vsf_recognizer::witness::AcceptedWitnesses::none(),
        &std::collections::BTreeMap::new(),
    )
}
