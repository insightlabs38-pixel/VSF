//! Relationships, label binding and operation hypotheses; every node read is logged for exact dependencies.

use crate::index::tokens;
use crate::obs::{F_DISABLED, F_INTERACTIVE, F_OPAQUE, Node, Observation, RelKind};
use std::cell::RefCell;
use std::collections::BTreeSet;

/// Which recognition rules are in force. The baseline binds only direct evidence; v2 adds delegated labels.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct Rules {
    pub delegation: bool,
}

impl Rules {
    pub const V1: Rules = Rules { delegation: false };
    pub const V2: Rules = Rules { delegation: true };

    pub fn version(self) -> &'static str {
        if self.delegation { "2" } else { "1" }
    }
}

/// Read-logging view of an observation.
pub struct Reader<'a> {
    pub obs: &'a Observation,
    log: RefCell<BTreeSet<u32>>,
}

impl<'a> Reader<'a> {
    pub fn new(obs: &'a Observation) -> Reader<'a> {
        Reader {
            obs,
            log: RefCell::new(BTreeSet::new()),
        }
    }

    fn touch(&self, i: usize) {
        self.log.borrow_mut().insert(self.obs.nodes[i].id);
    }

    pub fn node(&self, i: usize) -> &'a Node {
        self.touch(i);
        &self.obs.nodes[i]
    }

    pub fn children(&self, i: usize) -> &'a [usize] {
        self.touch(i);
        &self.obs.children[i]
    }

    pub fn parent(&self, i: usize) -> Option<usize> {
        self.touch(i);
        self.obs.nodes[i].parent
    }

    pub fn take(&self) -> BTreeSet<u32> {
        std::mem::take(&mut *self.log.borrow_mut())
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord)]
pub enum Strength {
    Explicit,
    Structural,
    Delegated,
    Lexical,
}

impl Strength {
    pub fn parse(x: &str) -> Option<Strength> {
        [
            Strength::Explicit,
            Strength::Structural,
            Strength::Delegated,
            Strength::Lexical,
        ]
        .into_iter()
        .find(|s| s.as_str() == x)
    }

    pub fn as_str(self) -> &'static str {
        match self {
            Strength::Explicit => "explicit",
            Strength::Structural => "structural",
            Strength::Delegated => "delegated",
            Strength::Lexical => "lexical",
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum LabelSource {
    Relation(RelKind),
    SelfName,
    Proximity,
    WrapperSibling,
    EnclosingText,
    ColumnHeader,
}

impl LabelSource {
    pub fn as_str(&self) -> &'static str {
        match self {
            LabelSource::Relation(RelKind::Label) => "label_relation",
            LabelSource::Relation(RelKind::LabelledBy) => "labelledby_relation",
            LabelSource::Relation(RelKind::DescribedBy) => "describedby_relation",
            LabelSource::SelfName => "self_name",
            LabelSource::Proximity => "proximity",
            LabelSource::WrapperSibling => "wrapper_sibling",
            LabelSource::EnclosingText => "enclosing_text",
            LabelSource::ColumnHeader => "column_header",
        }
    }

    pub fn strength(&self) -> Strength {
        match self {
            LabelSource::Relation(_) | LabelSource::SelfName => Strength::Explicit,
            LabelSource::Proximity => Strength::Structural,
            _ => Strength::Delegated,
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct LabelCandidate {
    pub text: String,
    pub source: LabelSource,
    pub from: Vec<u32>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Binding {
    Bound(LabelCandidate),
    /// More than one defensible label (or one label shared by several controls); nothing is chosen.
    Ambiguous(Vec<LabelCandidate>),
    Unbound,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord)]
pub enum FieldKind {
    Text,
    Choice,
    Toggle,
    Range,
}

impl FieldKind {
    pub fn as_str(self) -> &'static str {
        match self {
            FieldKind::Text => "text",
            FieldKind::Choice => "choice",
            FieldKind::Toggle => "toggle",
            FieldKind::Range => "range",
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Field {
    pub node: u32,
    pub kind: FieldKind,
    pub binding: Binding,
    /// Nearest named region, group or section: what tells two identical labels apart.
    pub scope: Option<String>,
    /// Roles the same element also satisfies as an action; non-empty means the kind is contested.
    pub action_roles: Vec<String>,
    pub usable: bool,
    pub why_unusable: Option<&'static str>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord)]
pub enum Verb {
    Commit,
    Destroy,
    Create,
    Navigate,
    Query,
    Export,
    Dismiss,
}

impl Verb {
    pub fn as_str(self) -> &'static str {
        match self {
            Verb::Commit => "commit",
            Verb::Destroy => "destroy",
            Verb::Create => "create",
            Verb::Navigate => "navigate",
            Verb::Query => "query",
            Verb::Export => "export",
            Verb::Dismiss => "dismiss",
        }
    }

    /// A hint only: the effect class stays undemonstrated until a witness exists.
    pub fn effect_hint(self) -> &'static str {
        match self {
            Verb::Commit | Verb::Create => "state_change_candidate",
            Verb::Destroy => "destructive_candidate",
            Verb::Navigate | Verb::Dismiss => "navigation_candidate",
            Verb::Query | Verb::Export => "read_candidate",
        }
    }
}

const LEXICON: [(Verb, &[&str]); 7] = [
    (
        Verb::Commit,
        &[
            "save", "submit", "apply", "update", "confirm", "place", "send", "finish", "done",
            "ok", "publish", "pay",
        ],
    ),
    (
        Verb::Destroy,
        &[
            "delete", "remove", "discard", "destroy", "erase", "clear", "reset",
        ],
    ),
    (Verb::Create, &["add", "new", "create", "insert"]),
    (
        Verb::Navigate,
        &[
            "open", "view", "details", "edit", "next", "previous", "back", "terms", "more", "show",
        ],
    ),
    (Verb::Query, &["search", "find", "filter", "go"]),
    (Verb::Export, &["export", "download", "print"]),
    (Verb::Dismiss, &["cancel", "close", "dismiss"]),
];

pub fn verbs_of(name: &str) -> Vec<Verb> {
    let toks: Vec<String> = tokens(name).collect();
    let mut out: Vec<Verb> = LEXICON
        .iter()
        .filter(|(_, words)| toks.iter().any(|t| words.contains(&t.as_str())))
        .map(|(v, _)| *v)
        .collect();
    out.sort();
    out
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Action {
    pub node: u32,
    pub name: Option<String>,
    /// Every role the element can be read as; more than one means the candidates stay separate.
    pub roles: Vec<String>,
    pub verbs: Vec<Verb>,
    pub usable: bool,
    pub why_unusable: Option<&'static str>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Collection {
    pub node: u32,
    pub label: Option<String>,
    pub columns: Vec<Option<String>>,
    pub rows: usize,
    pub row_actions: Vec<(usize, u32)>,
}

fn collapse(s: &str) -> String {
    s.split_whitespace().collect::<Vec<_>>().join(" ")
}

fn clip(s: String) -> String {
    s.chars().take(200).collect()
}

/// The words a node (or the visible text beneath it) says, as one line.
pub fn text_of(r: &Reader, i: usize) -> Option<String> {
    let n = r.node(i);
    if n.has(F_OPAQUE) || n.has(crate::obs::F_CREDENTIAL) {
        return None;
    }
    if let Some(t) = n.text.as_deref().or(n.name.as_deref()) {
        let t = collapse(t);
        if !t.is_empty() {
            return Some(clip(t));
        }
    }
    let mut parts = Vec::new();
    let mut stack: Vec<usize> = r.children(i).iter().rev().copied().collect();
    while let Some(c) = stack.pop() {
        let cn = r.node(c);
        if !cn.visible() || cn.has(F_OPAQUE) || is_control(cn) {
            continue;
        }
        if let Some(t) = cn.text.as_deref().or(cn.name.as_deref()) {
            parts.push(collapse(t));
        }
        stack.extend(r.children(c).iter().rev().copied());
        if parts.len() > 20 {
            break;
        }
    }
    let joined = collapse(&parts.join(" "));
    (!joined.is_empty()).then(|| clip(joined))
}

fn role_of(n: &Node) -> &str {
    n.role.as_deref().unwrap_or("")
}

pub fn is_control(n: &Node) -> bool {
    matches!(
        role_of(n),
        "textbox"
            | "combobox"
            | "listbox"
            | "checkbox"
            | "radio"
            | "slider"
            | "switch"
            | "spinbutton"
            | "searchbox"
    ) || matches!(n.tag.as_str(), "input" | "select" | "textarea")
}

pub fn field_kind(n: &Node) -> Option<FieldKind> {
    if !is_control(n) {
        return None;
    }
    Some(match (role_of(n), n.tag.as_str()) {
        ("checkbox" | "radio" | "switch", _) => FieldKind::Toggle,
        ("combobox" | "listbox", _) | (_, "select") => FieldKind::Choice,
        ("slider" | "spinbutton", _) => FieldKind::Range,
        _ => FieldKind::Text,
    })
}

pub fn is_action(n: &Node) -> bool {
    matches!(role_of(n), "button" | "link" | "menuitem")
        || n.tag == "button"
        || (n.tag == "a" && n.role.as_deref() != Some("presentation"))
}

fn subtree_has_control(r: &Reader, i: usize) -> bool {
    let mut stack = vec![i];
    while let Some(n) = stack.pop() {
        if n != i && (is_control(r.node(n)) || is_action(r.node(n))) {
            return true;
        }
        stack.extend(r.children(n).iter().copied());
    }
    false
}

fn word_count(s: &str) -> usize {
    s.split_whitespace().count()
}

/// A node that plausibly names something next to it: a label element, or short control-free text.
fn label_like(r: &Reader, i: usize) -> Option<String> {
    let n = r.node(i);
    if !n.visible() || n.has(F_OPAQUE) || is_control(n) || is_action(n) {
        return None;
    }
    let explicit = matches!(n.tag.as_str(), "label" | "legend" | "th" | "dt" | "caption");
    if !explicit && subtree_has_control(r, i) {
        return None;
    }
    let t = text_of(r, i)?;
    (explicit || word_count(&t) <= 8).then_some(t)
}

pub fn region_name(r: &Reader, i: usize) -> Option<String> {
    for a in r.obs.ancestors(i) {
        let n = r.node(a);
        let named_region = matches!(
            role_of(n),
            "group" | "radiogroup" | "region" | "form" | "grid" | "table"
        ) || matches!(n.tag.as_str(), "fieldset" | "section" | "form" | "table");
        if named_region && let Some(name) = &n.name {
            let name = collapse(name);
            if !name.is_empty() {
                return Some(clip(name));
            }
        }
    }
    None
}

fn explicit_candidates(r: &Reader, i: usize) -> Vec<LabelCandidate> {
    let n = r.node(i);
    let mut out = Vec::new();
    for kind in [RelKind::Label, RelKind::LabelledBy] {
        let targets: Vec<usize> = n
            .rels
            .iter()
            .filter(|(k, _)| *k == kind)
            .map(|(_, t)| *t)
            .collect();
        if targets.is_empty() {
            continue;
        }
        let texts: Vec<String> = targets.iter().filter_map(|&t| text_of(r, t)).collect();
        if !texts.is_empty() {
            out.push(LabelCandidate {
                text: clip(texts.join(" ")),
                source: LabelSource::Relation(kind),
                from: targets.iter().map(|&t| r.obs.nodes[t].id).collect(),
            });
        }
    }
    out
}

/// Nearest preceding label-like sibling of `i`, skipping empty and hidden siblings.
fn preceding_label(r: &Reader, i: usize, budget: usize) -> Option<(usize, String)> {
    let p = r.parent(i)?;
    let sibs = r.children(p);
    let pos = sibs.iter().position(|&s| s == i)?;
    let mut seen = 0;
    for &s in sibs[..pos].iter().rev() {
        let n = r.node(s);
        if !n.visible() {
            continue;
        }
        if let Some(t) = label_like(r, s) {
            return Some((s, t));
        }
        seen += 1;
        if seen >= budget {
            break;
        }
    }
    None
}

/// Labels next to a control, and whether picking one is unsafe (shared by several controls or competing).
fn proximity(r: &Reader, i: usize) -> Option<(Vec<LabelCandidate>, bool)> {
    let p = r.parent(i)?;
    let sibs = r.children(p);
    let pos = sibs.iter().position(|&s| s == i)?;
    for k in (0..pos).rev() {
        let s = sibs[k];
        let n = r.node(s);
        if !n.visible() || is_control(n) {
            continue;
        }
        let t = label_like(r, s)?;
        let mut cands = vec![LabelCandidate {
            text: t,
            source: LabelSource::Proximity,
            from: vec![r.obs.nodes[s].id],
        }];
        let competing = sibs[..k]
            .iter()
            .rev()
            .find(|&&q| r.node(q).visible() && !is_control(r.node(q)));
        if let Some(&q) = competing
            && !matches!(role_of(r.node(q)), "heading")
            && !matches!(
                r.node(q).tag.as_str(),
                "legend" | "caption" | "h1" | "h2" | "h3" | "h4" | "h5" | "h6"
            )
            && let Some(qt) = label_like(r, q)
        {
            cands.push(LabelCandidate {
                text: qt,
                source: LabelSource::Proximity,
                from: vec![r.obs.nodes[q].id],
            });
        }
        let mut controls = 0;
        for &after in &sibs[k + 1..] {
            let an = r.node(after);
            if !an.visible() {
                continue;
            }
            if is_control(an) {
                controls += 1;
            } else if label_like(r, after).is_some() {
                break;
            }
        }
        let unsafe_pick = cands.len() > 1 || controls > 1;
        return Some((cands, unsafe_pick));
    }
    None
}

fn column_header(r: &Reader, i: usize) -> Option<LabelCandidate> {
    let cell = std::iter::once(i)
        .chain(r.obs.ancestors(i))
        .take(4)
        .find(|&a| matches!(role_of(r.node(a)), "gridcell" | "cell") || r.node(a).tag == "td")?;
    let row = r.parent(cell)?;
    let col = r.children(row).iter().position(|&c| c == cell)?;
    let table = r.obs.ancestors(row).find(|&a| {
        matches!(role_of(r.node(a)), "grid" | "table" | "treegrid") || r.node(a).tag == "table"
    })?;
    let mut stack = vec![table];
    while let Some(n) = stack.pop() {
        if n != row && (role_of(r.node(n)) == "row" || r.node(n).tag == "tr") {
            let cells = r.children(n);
            let headerish = !cells.is_empty()
                && cells.iter().all(|&c| {
                    matches!(role_of(r.node(c)), "columnheader") || r.node(c).tag == "th"
                });
            if headerish && let Some(&h) = cells.get(col) {
                let t = text_of(r, h)?;
                return Some(LabelCandidate {
                    text: t,
                    source: LabelSource::ColumnHeader,
                    from: vec![r.obs.nodes[h].id],
                });
            }
        }
        stack.extend(r.children(n).iter().rev().copied());
    }
    None
}

fn delegated(r: &Reader, i: usize) -> Option<LabelCandidate> {
    if let Some(c) = column_header(r, i) {
        return Some(c);
    }
    let mut cur = i;
    for depth in 0..4 {
        let p = r.parent(cur)?;
        let pn = r.node(p);
        if matches!(
            pn.tag.as_str(),
            "form" | "fieldset" | "table" | "body" | "html"
        ) || matches!(role_of(pn), "form" | "grid" | "table")
        {
            return None;
        }
        if (depth > 0 || r.children(p).len() > 1)
            && let Some((s, t)) = preceding_label(r, cur, 2)
        {
            return Some(LabelCandidate {
                text: t,
                source: LabelSource::WrapperSibling,
                from: vec![r.obs.nodes[s].id],
            });
        }
        if let Some(t) = pn.text.as_deref() {
            let t = collapse(t);
            let only_control = r
                .children(p)
                .iter()
                .filter(|&&c| is_control(r.node(c)))
                .count()
                == 1;
            if !t.is_empty() && only_control && word_count(&t) <= 8 {
                return Some(LabelCandidate {
                    text: t,
                    source: LabelSource::EnclosingText,
                    from: vec![pn.id],
                });
            }
        }
        cur = p;
    }
    None
}

pub fn bind_label(r: &Reader, i: usize, rules: Rules) -> Binding {
    let n = r.node(i);
    let explicit = explicit_candidates(r, i);
    if !explicit.is_empty() {
        let mut texts: Vec<&str> = explicit.iter().map(|c| c.text.as_str()).collect();
        texts.sort_unstable();
        texts.dedup();
        return if texts.len() == 1 {
            Binding::Bound(explicit[0].clone())
        } else {
            Binding::Ambiguous(explicit)
        };
    }
    if let Some(name) = n.name.as_deref().map(collapse).filter(|s| !s.is_empty()) {
        return Binding::Bound(LabelCandidate {
            text: clip(name),
            source: LabelSource::SelfName,
            from: vec![n.id],
        });
    }
    if let Some((mut cands, unsafe_pick)) = proximity(r, i) {
        return if unsafe_pick {
            Binding::Ambiguous(cands)
        } else {
            Binding::Bound(cands.remove(0))
        };
    }
    if rules.delegation
        && let Some(c) = delegated(r, i)
    {
        return Binding::Bound(c);
    }
    Binding::Unbound
}

pub fn recognize_field(r: &Reader, i: usize, rules: Rules) -> Option<Field> {
    let n = r.node(i);
    let kind = field_kind(n)?;
    let (usable, why) = if n.has(crate::obs::F_CREDENTIAL) {
        (false, Some("credential_field"))
    } else if !n.visible() {
        (false, Some("not_visible"))
    } else if n.has(F_DISABLED) {
        (false, Some("disabled"))
    } else {
        (true, None)
    };
    Some(Field {
        node: n.id,
        kind,
        binding: bind_label(r, i, rules),
        scope: region_name(r, i),
        action_roles: if is_action(n) {
            action_roles(n)
        } else {
            Vec::new()
        },
        usable,
        why_unusable: why,
    })
}

fn action_roles(n: &Node) -> Vec<String> {
    let mut roles = Vec::new();
    if let Some(explicit) = &n.role {
        roles.push(explicit.clone());
    }
    let native = match n.tag.as_str() {
        "button" => Some("button"),
        "a" => Some("link"),
        _ => None,
    };
    if let Some(nat) = native
        && !roles.iter().any(|x| x == nat)
    {
        roles.push(nat.to_string());
    }
    roles
}

pub fn recognize_action(r: &Reader, i: usize) -> Option<Action> {
    let n = r.node(i);
    if !is_action(n) {
        return None;
    }
    let roles = action_roles(n);
    let name = n
        .name
        .as_deref()
        .or(n.text.as_deref())
        .map(collapse)
        .filter(|s| !s.is_empty());
    let verbs = name.as_deref().map(verbs_of).unwrap_or_default();
    let (usable, why) = if !n.visible() {
        (false, Some("not_visible"))
    } else if n.has(F_DISABLED) {
        (false, Some("disabled"))
    } else if !n.has(F_INTERACTIVE) {
        (false, Some("not_interactive"))
    } else {
        (true, None)
    };
    Some(Action {
        node: n.id,
        name,
        roles,
        verbs,
        usable,
        why_unusable: why,
    })
}

/// The header row and the body rows of a table-like node, in document order.
pub fn table_rows(r: &Reader, i: usize) -> (Option<usize>, Vec<usize>) {
    let mut rows: Vec<usize> = Vec::new();
    let mut stack: Vec<usize> = r.children(i).iter().rev().copied().collect();
    while let Some(c) = stack.pop() {
        let cn = r.node(c);
        if cn.tag == "tr" || role_of(cn) == "row" {
            rows.push(c);
        } else if matches!(cn.tag.as_str(), "thead" | "tbody" | "tfoot")
            || matches!(role_of(cn), "rowgroup")
        {
            stack.extend(r.children(c).iter().rev().copied());
        }
    }
    let is_header = |row: usize| {
        let cells = r.children(row);
        !cells.is_empty()
            && cells
                .iter()
                .all(|&c| role_of(r.node(c)) == "columnheader" || r.node(c).tag == "th")
    };
    let header = rows.iter().copied().find(|&row| is_header(row));
    let body = rows
        .iter()
        .copied()
        .filter(|&row| Some(row) != header && !r.children(row).is_empty())
        .collect();
    (header, body)
}

pub fn recognize_collection(r: &Reader, i: usize) -> Option<Collection> {
    let n = r.node(i);
    let is_table = n.tag == "table" || matches!(role_of(n), "table" | "grid" | "treegrid");
    if !is_table || !n.visible() {
        return None;
    }
    let (header, body) = table_rows(r, i);
    let columns: Vec<Option<String>> = header
        .map(|h| r.children(h).iter().map(|&c| text_of(r, c)).collect())
        .unwrap_or_default();
    if columns.is_empty() && body.is_empty() {
        return None;
    }
    let mut row_actions = Vec::new();
    for (k, &row) in body.iter().enumerate() {
        for d in r.obs.descendants(row) {
            let dn = r.node(d);
            if is_action(dn) {
                row_actions.push((k, dn.id));
            }
        }
    }
    let caption = r
        .children(i)
        .iter()
        .find(|&&c| r.node(c).tag == "caption")
        .and_then(|&c| text_of(r, c));
    let label = n
        .name
        .as_deref()
        .map(collapse)
        .filter(|s| !s.is_empty())
        .or(caption);
    Some(Collection {
        node: n.id,
        label,
        columns,
        rows: body.len(),
        row_actions,
    })
}
