//! Composite structures: several controls that behave as one widget, recognized from roles, states and relations.

use crate::obs::{Node, Observation, RelKind};
use crate::semantics::{field_kind, is_action, is_control};
use std::collections::BTreeSet;

#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord)]
pub enum CompositeKind {
    Tabs,
    Disclosure,
    Menu,
    Dialog,
    ComboboxPopup,
    VirtualList,
    Tree,
    Pagination,
    BulkSelection,
    SortableColumns,
    FilterBar,
    FileInput,
    DatePicker,
    EditableCell,
}

impl CompositeKind {
    pub fn as_str(self) -> &'static str {
        match self {
            CompositeKind::Tabs => "tabs",
            CompositeKind::Disclosure => "disclosure",
            CompositeKind::Menu => "menu",
            CompositeKind::Dialog => "dialog",
            CompositeKind::ComboboxPopup => "combobox_popup",
            CompositeKind::VirtualList => "virtual_list",
            CompositeKind::Tree => "tree",
            CompositeKind::Pagination => "pagination",
            CompositeKind::BulkSelection => "bulk_selection",
            CompositeKind::SortableColumns => "sortable_columns",
            CompositeKind::FilterBar => "filter_bar",
            CompositeKind::FileInput => "file_input",
            CompositeKind::DatePicker => "date_picker",
            CompositeKind::EditableCell => "editable_cell",
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Member {
    pub node: u32,
    pub name: Option<String>,
    /// Which part of the structure the member plays when that matters: `header` selects everything, `row` one record.
    pub part: Option<&'static str>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Composite {
    pub kind: CompositeKind,
    pub node: u32,
    pub label: Option<String>,
    /// The control a person operates to reach the members: a combobox, a menu button, a table.
    pub owner: Option<u32>,
    pub members: Vec<Member>,
    /// Controls inside the owner that are parts of the same widget, such as the select it enhances.
    pub covers: Vec<u32>,
    /// Actions whose effect class the structure settles: members of tabs and pages only change what is shown.
    pub classifies: Vec<(u32, &'static str)>,
    pub evidence: Vec<&'static str>,
}

fn role(n: &Node) -> &str {
    n.role.as_deref().unwrap_or("")
}

fn clean(s: &str) -> Option<String> {
    let t = s.split_whitespace().collect::<Vec<_>>().join(" ");
    (!t.is_empty()).then(|| t.chars().take(120).collect())
}

struct View<'a> {
    obs: &'a Observation,
}

impl<'a> View<'a> {
    fn n(&self, i: usize) -> &'a Node {
        &self.obs.nodes[i]
    }

    fn text(&self, i: usize) -> Option<String> {
        let n = self.n(i);
        n.name
            .as_deref()
            .and_then(clean)
            .or_else(|| n.text.as_deref().and_then(clean))
            .or_else(|| {
                let mut parts = Vec::new();
                for d in self.obs.descendants(i) {
                    let dn = self.n(d);
                    if is_control(dn) {
                        continue;
                    }
                    if let Some(t) = dn.text.as_deref().and_then(clean) {
                        parts.push(t);
                    }
                    if parts.len() > 6 {
                        break;
                    }
                }
                clean(&parts.join(" "))
            })
    }

    /// The name a person reads for the node, following a labelling relation when it has no name of its own.
    fn label(&self, i: usize) -> Option<String> {
        let n = self.n(i);
        n.name.as_deref().and_then(clean).or_else(|| {
            n.rels
                .iter()
                .filter(|(k, _)| matches!(k, RelKind::LabelledBy | RelKind::Label))
                .find_map(|(_, t)| self.text(*t))
        })
    }

    fn member(&self, i: usize) -> Member {
        Member {
            node: self.n(i).id,
            name: self.text(i),
            part: None,
        }
    }

    fn controlled(&self, i: usize) -> Vec<usize> {
        self.n(i)
            .rels
            .iter()
            .filter(|(k, _)| *k == RelKind::Controls)
            .map(|(_, t)| *t)
            .collect()
    }

    fn within(&self, i: usize, root: usize) -> bool {
        i == root || self.obs.ancestors(i).any(|a| a == root)
    }

    fn nearest_with_role(&self, i: usize, roles: &[&str]) -> Option<usize> {
        self.obs
            .ancestors(i)
            .find(|&a| roles.contains(&role(self.n(a))))
    }
}

fn mk(
    v: &View,
    kind: CompositeKind,
    root: usize,
    owner: Option<usize>,
    members: &[usize],
    evidence: &[&'static str],
) -> Composite {
    Composite {
        kind,
        node: v.n(root).id,
        label: v.label(root),
        owner: owner.map(|o| v.n(o).id),
        members: members.iter().map(|&m| v.member(m)).collect(),
        covers: Vec::new(),
        classifies: Vec::new(),
        evidence: evidence.to_vec(),
    }
}

/// The actions at or under the given nodes, each tagged with the class the structure gives them.
fn classify(v: &View, c: &mut Composite, roots: &[usize], class: &'static str) {
    for &r in roots {
        for d in std::iter::once(r).chain(v.obs.descendants(r)) {
            if is_action(v.n(d)) || role(v.n(d)) == "tab" {
                c.classifies.push((v.n(d).id, class));
            }
        }
    }
}

fn is_header_cell(n: &Node) -> bool {
    role(n) == "columnheader" || n.tag == "th" && role(n) != "rowheader"
}

fn is_body_row(v: &View, r: usize) -> bool {
    let n = v.n(r);
    (role(n) == "row" || n.tag == "tr")
        && !v
            .obs
            .ancestors(r)
            .any(|a| matches!(v.n(a).tag.as_str(), "thead" | "tfoot"))
        && !v.obs.children[r].iter().all(|&c| is_header_cell(v.n(c)))
}

fn collections(v: &View) -> Vec<usize> {
    (0..v.obs.len())
        .filter(|&i| {
            let n = v.n(i);
            matches!(role(n), "table" | "grid" | "treegrid")
                || (n.tag == "table" && role(n).is_empty())
        })
        .collect()
}

fn header_cells(v: &View, table: usize) -> Vec<usize> {
    v.obs
        .descendants(table)
        .into_iter()
        .filter(|&d| {
            is_header_cell(v.n(d))
                && v.nearest_with_role(d, &["table", "grid", "treegrid"])
                    .is_none_or(|t| t == table)
        })
        .collect()
}

fn tabs(v: &View, out: &mut Vec<Composite>) {
    for i in 0..v.obs.len() {
        if role(v.n(i)) != "tablist" {
            continue;
        }
        let tabs: Vec<usize> = v
            .obs
            .descendants(i)
            .into_iter()
            .filter(|&d| role(v.n(d)) == "tab")
            .collect();
        if !tabs.is_empty() {
            let mut c = mk(v, CompositeKind::Tabs, i, None, &tabs, &["role:tablist"]);
            classify(v, &mut c, &tabs, "navigation");
            out.push(c);
        }
    }
}

/// Buttons that show or hide the content they control, grouped by the smallest container holding at least two.
fn disclosures(v: &View, out: &mut Vec<Composite>) {
    let buttons: Vec<usize> = (0..v.obs.len())
        .filter(|&i| {
            let n = v.n(i);
            (role(n) == "button" || n.tag == "button" || n.tag == "summary")
                && n.state("expanded").is_some()
                && n.state("haspopup").is_none()
                && !v.controlled(i).is_empty()
                && !v.obs.ancestors(i).any(|a| role(v.n(a)) == "tablist")
        })
        .collect();
    let mut roots: BTreeSet<usize> = BTreeSet::new();
    for &b in &buttons {
        let root = v.obs.ancestors(b).skip(1).find(|&a| {
            let inside = buttons.iter().filter(|&&o| v.within(o, a)).count();
            inside >= 2 && !matches!(v.n(a).tag.as_str(), "body" | "html" | "#document")
        });
        if let Some(r) = root {
            roots.insert(r);
        }
    }
    let kept: Vec<usize> = roots
        .iter()
        .copied()
        .filter(|&r| !roots.iter().any(|&o| o != r && v.within(o, r)))
        .collect();
    for r in kept {
        let members: Vec<usize> = buttons
            .iter()
            .copied()
            .filter(|&b| v.within(b, r))
            .collect();
        let mut c = mk(
            v,
            CompositeKind::Disclosure,
            r,
            None,
            &members,
            &["state:expanded", "relation:controls"],
        );
        classify(v, &mut c, &members, "navigation");
        out.push(c);
    }
}

fn menus(v: &View, out: &mut Vec<Composite>) {
    for i in 0..v.obs.len() {
        if !matches!(role(v.n(i)), "menu" | "menubar") {
            continue;
        }
        let items: Vec<usize> = v
            .obs
            .descendants(i)
            .into_iter()
            .filter(|&d| {
                matches!(
                    role(v.n(d)),
                    "menuitem" | "menuitemcheckbox" | "menuitemradio"
                )
            })
            .filter(|&d| v.nearest_with_role(d, &["menu", "menubar"]) == Some(i))
            .collect();
        if items.is_empty() {
            continue;
        }
        let owner = (0..v.obs.len()).find(|&o| {
            o != i && v.controlled(o).contains(&i) && v.n(o).state("expanded").is_some()
                || v.n(i)
                    .rels
                    .iter()
                    .any(|(k, t)| *k == RelKind::LabelledBy && *t == o)
        });
        out.push(mk(v, CompositeKind::Menu, i, owner, &items, &["role:menu"]));
    }
}

fn dialogs(v: &View, out: &mut Vec<Composite>) {
    for i in 0..v.obs.len() {
        let n = v.n(i);
        if !(matches!(role(n), "dialog" | "alertdialog") || n.tag == "dialog") {
            continue;
        }
        let parts: Vec<usize> = v
            .obs
            .descendants(i)
            .into_iter()
            .filter(|&d| {
                (is_control(v.n(d)) || is_action(v.n(d))) && v.n(d).name.is_some()
                    || is_control(v.n(d))
            })
            .collect();
        let ev: &[&'static str] = if n.state("modal") == Some("true") {
            &["role:dialog", "state:modal"]
        } else {
            &["role:dialog"]
        };
        out.push(mk(v, CompositeKind::Dialog, i, None, &parts, ev));
    }
}

const POPUP_ROLES: [&str; 4] = ["listbox", "tree", "grid", "menu"];

/// A combobox-like control and the list of choices it opens, whether or not the list is in the page yet.
fn comboboxes(v: &View, out: &mut Vec<Composite>) {
    for i in 0..v.obs.len() {
        let n = v.n(i);
        if n.tag == "select" || n.tag == "option" {
            continue;
        }
        let popup_hint = matches!(
            n.state("haspopup"),
            Some("true" | "listbox" | "grid" | "tree")
        );
        let is_combo = role(n) == "combobox" || (popup_hint && n.state("expanded").is_some());
        if !is_combo || !popup_hint && v.controlled(i).is_empty() && n.state("expanded").is_none() {
            continue;
        }
        let targets: Vec<usize> = v.controlled(i);
        if targets.iter().any(|&t| role(v.n(t)) == "menu") {
            continue;
        }
        let in_subtree = || {
            v.obs
                .descendants(i)
                .into_iter()
                .filter(|&d| role(v.n(d)) == "listbox" && v.n(d).tag != "select")
                .collect::<Vec<_>>()
        };
        let mut popups: Vec<usize> = targets
            .iter()
            .copied()
            .filter(|&t| POPUP_ROLES.contains(&role(v.n(t))))
            .collect();
        if popups.is_empty() {
            let cand = in_subtree();
            popups = cand
                .iter()
                .copied()
                .filter(|&c| {
                    v.n(c).state("expanded").is_some()
                        || v.n(c)
                            .parent
                            .is_some_and(|p| v.n(p).state("expanded").is_some())
                })
                .collect();
            if popups.is_empty() {
                popups = cand.into_iter().filter(|&c| !v.n(c).visible()).collect();
            }
        }
        let mut members = Vec::new();
        for &p in &popups {
            let below = v.obs.descendants(p);
            let roled: Vec<usize> = below
                .iter()
                .copied()
                .filter(|&d| matches!(role(v.n(d)), "option" | "treeitem" | "menuitem"))
                .collect();
            // A list with no option roles still lists its items.
            let items = if roled.is_empty() {
                below
                    .iter()
                    .copied()
                    .filter(|&d| v.n(d).tag == "li" || role(v.n(d)) == "listitem")
                    .collect()
            } else {
                roled
            };
            members.extend(items);
        }
        let ev: &[&'static str] = if popups.is_empty() {
            &["state:haspopup"]
        } else {
            &["state:haspopup", "popup:listbox"]
        };
        let root = popups.first().copied().unwrap_or(i);
        let mut c = mk(v, CompositeKind::ComboboxPopup, root, Some(i), &members, ev);
        c.label = v.label(i).or(c.label);
        c.covers = v
            .obs
            .descendants(i)
            .into_iter()
            .filter(|&d| is_control(v.n(d)))
            .map(|d| v.n(d).id)
            .collect();
        out.push(c);
    }
}

fn trees(v: &View, out: &mut Vec<Composite>) {
    for i in 0..v.obs.len() {
        if !matches!(role(v.n(i)), "tree" | "treegrid") {
            continue;
        }
        let items: Vec<usize> = v
            .obs
            .descendants(i)
            .into_iter()
            .filter(|&d| {
                role(v.n(d)) == "treeitem"
                    || (role(v.n(d)) == "row" || v.n(d).tag == "tr")
                        && v.n(d).state("expanded").is_some()
            })
            .collect();
        if !items.is_empty() {
            out.push(mk(
                v,
                CompositeKind::Tree,
                i,
                None,
                &items,
                &["role:tree", "state:expanded"],
            ));
        }
    }
}

fn page_word(name: &str) -> Option<&'static str> {
    let t = name.to_lowercase();
    let t = t.trim();
    let bare = t.trim_matches(|c: char| !c.is_alphanumeric());
    if bare.chars().all(|c| c.is_ascii_digit()) && !bare.is_empty() {
        return Some("number");
    }
    for (w, k) in [
        ("next", "step"),
        ("previous", "step"),
        ("prev", "step"),
        ("first", "step"),
        ("last", "step"),
    ] {
        if bare == w
            || bare.starts_with(&format!("{w} page"))
            || bare.ends_with(&format!("{w} page"))
            || bare == format!("{w}…")
        {
            return Some(k);
        }
    }
    if matches!(t, "›" | "»" | "‹" | "«" | "→" | "←") {
        return Some("step");
    }
    None
}

fn calendar_grids(v: &View) -> Vec<usize> {
    collections(v)
        .into_iter()
        .filter(|&t| {
            let heads = header_cells(v, t)
                .into_iter()
                .filter_map(|h| v.text(h))
                .map(|s| s.to_lowercase())
                .filter(|s| {
                    WEEKDAYS.contains(&s.as_str()) || WEEKDAYS.contains(&s.trim_end_matches('.'))
                })
                .collect::<BTreeSet<_>>();
            heads.len() >= 5
        })
        .collect()
}

fn paginations(v: &View, out: &mut Vec<Composite>) {
    let calendars = calendar_grids(v);
    let items: Vec<(usize, &'static str)> = (0..v.obs.len())
        .filter(|&i| is_action(v.n(i)) && !calendars.iter().any(|&c| v.within(i, c)))
        .filter_map(|i| v.text(i).and_then(|t| page_word(&t)).map(|k| (i, k)))
        .collect();
    let mut roots: BTreeSet<usize> = BTreeSet::new();
    for &(i, _) in &items {
        let root = v.obs.ancestors(i).find(|&a| {
            let inside: Vec<&(usize, &str)> =
                items.iter().filter(|(o, _)| v.within(*o, a)).collect();
            let steps = inside.iter().filter(|(_, k)| *k == "step").count();
            let numbers = inside
                .iter()
                .filter(|(o, k)| {
                    *k == "number"
                        && inside.iter().any(|(p, pk)| {
                            p != o && *pk == "number" && v.n(*p).parent == v.n(*o).parent
                        })
                })
                .count();
            (steps >= 1 && steps + numbers >= 2
                || numbers >= 2
                    && v.n(a)
                        .name
                        .as_deref()
                        .is_some_and(|nm| nm.to_lowercase().contains("page"))
                || numbers >= 3)
                && !matches!(v.n(a).tag.as_str(), "body" | "html" | "#document")
        });
        if let Some(r) = root {
            roots.insert(r);
        }
    }
    let kept: Vec<usize> = roots
        .iter()
        .copied()
        .filter(|&r| !roots.iter().any(|&o| o != r && v.within(o, r)))
        .collect();
    for r in kept {
        let members: Vec<usize> = items
            .iter()
            .filter(|(o, _)| v.within(*o, r))
            .map(|(o, _)| *o)
            .collect();
        let mut c = mk(
            v,
            CompositeKind::Pagination,
            r,
            None,
            &members,
            &["lexical:page_controls"],
        );
        classify(v, &mut c, &members, "navigation");
        out.push(c);
    }
}

fn is_checkbox(n: &Node) -> bool {
    role(n) == "checkbox" || n.state("type") == Some("checkbox")
}

fn bulk_selections(v: &View, out: &mut Vec<Composite>) {
    for t in collections(v) {
        let body: Vec<usize> = v
            .obs
            .descendants(t)
            .into_iter()
            .filter(|&r| is_body_row(v, r))
            .collect();
        let row_boxes: Vec<usize> = v
            .obs
            .descendants(t)
            .into_iter()
            .filter(|&d| is_checkbox(v.n(d)) && body.iter().any(|&r| v.within(d, r)))
            .collect();
        let head_boxes: Vec<usize> = v
            .obs
            .descendants(t)
            .into_iter()
            .filter(|&d| is_checkbox(v.n(d)) && !body.iter().any(|&r| v.within(d, r)))
            .collect();
        if row_boxes.len() >= 2 && body.len() >= 2 {
            let mut m = head_boxes.clone();
            m.extend(&row_boxes);
            let ev: &[&'static str] = if head_boxes.is_empty() {
                &["row:checkbox"]
            } else {
                &["row:checkbox", "header:checkbox"]
            };
            let mut c = mk(v, CompositeKind::BulkSelection, t, Some(t), &m, ev);
            for (k, member) in c.members.iter_mut().enumerate() {
                member.part = Some(if k < head_boxes.len() {
                    "header"
                } else {
                    "row"
                });
            }
            out.push(c);
        }
    }
    // A list whose items each hold a checkbox is a selectable set; a lone checkbox beside it selects them all.
    for i in 0..v.obs.len() {
        let n = v.n(i);
        if !(matches!(n.tag.as_str(), "ul" | "ol") || matches!(role(n), "list" | "feed"))
            || !n.visible()
        {
            continue;
        }
        if v.obs.ancestors(i).take(8).any(|a| {
            matches!(
                role(v.n(a)),
                "navigation" | "menu" | "menubar" | "tablist" | "toolbar"
            ) || matches!(v.n(a).tag.as_str(), "nav" | "menu")
        }) {
            continue;
        }
        let items: Vec<usize> = v
            .obs
            .descendants(i)
            .into_iter()
            .filter(|&d| v.n(d).tag == "li" || role(v.n(d)) == "listitem")
            .collect();
        let rows: Vec<usize> = v
            .obs
            .descendants(i)
            .into_iter()
            .filter(|&d| is_checkbox(v.n(d)))
            .collect();
        if items.len() < 2 || rows.len() < items.len() {
            continue;
        }
        let head: Vec<usize> = v.n(i).parent.map_or_else(Vec::new, |p| {
            v.obs
                .descendants(p)
                .into_iter()
                .filter(|&d| is_checkbox(v.n(d)) && !v.within(d, i))
                .take(1)
                .collect()
        });
        let mut m = head.clone();
        m.extend(&rows);
        let ev: &[&'static str] = if head.is_empty() {
            &["row:checkbox"]
        } else {
            &["row:checkbox", "header:checkbox"]
        };
        let mut c = mk(v, CompositeKind::BulkSelection, i, Some(i), &m, ev);
        for (k, member) in c.members.iter_mut().enumerate() {
            member.part = Some(if k < head.len() { "header" } else { "row" });
        }
        out.push(c);
    }
    // A mixed-state checkbox that controls other checkboxes is a select-all over a group.
    for i in 0..v.obs.len() {
        if is_checkbox(v.n(i))
            && (v.n(i).state("checked") == Some("mixed") || v.controlled(i).len() >= 2)
        {
            let group: Vec<usize> = v
                .controlled(i)
                .into_iter()
                .filter(|&t| is_checkbox(v.n(t)))
                .collect();
            if group.len() >= 2 {
                let mut m = vec![i];
                m.extend(group);
                let mut c = mk(
                    v,
                    CompositeKind::BulkSelection,
                    i,
                    Some(i),
                    &m,
                    &["checkbox:mixed", "relation:controls"],
                );
                for (k, member) in c.members.iter_mut().enumerate() {
                    member.part = Some(if k == 0 { "header" } else { "row" });
                }
                out.push(c);
            }
        }
    }
}

fn sortable_columns(v: &View, out: &mut Vec<Composite>) {
    for t in collections(v) {
        let interactive = |h: usize| {
            v.obs
                .descendants(h)
                .into_iter()
                .any(|d| is_action(v.n(d)) && v.text(d).is_some())
        };
        let by_state: Vec<usize> = header_cells(v, t)
            .into_iter()
            .filter(|&h| v.n(h).state("sort").is_some())
            .collect();
        let by_control: Vec<usize> = header_cells(v, t)
            .into_iter()
            .filter(|&h| interactive(h))
            .collect();
        // One linked header is a table title; several are columns a person can order by.
        let sortable = if !by_state.is_empty() {
            by_state
        } else if by_control.len() >= 2 {
            by_control
        } else {
            Vec::new()
        };
        if !sortable.is_empty() {
            let mut c = mk(
                v,
                CompositeKind::SortableColumns,
                t,
                Some(t),
                &sortable,
                &["header:sortable"],
            );
            classify(v, &mut c, &sortable, "read");
            out.push(c);
        }
    }
}

fn search_like(v: &View, i: usize) -> bool {
    let n = v.n(i);
    if !is_control(n)
        || field_kind(n) != Some(crate::semantics::FieldKind::Text)
            && field_kind(n) != Some(crate::semantics::FieldKind::Range)
    {
        return false;
    }
    let name = v.label(i).unwrap_or_default().to_lowercase();
    role(n) == "searchbox"
        || n.state("type") == Some("search")
        || name.contains("search")
        || name.contains("filter")
}

fn filter_bars(v: &View, out: &mut Vec<Composite>) {
    let mut taken: BTreeSet<usize> = BTreeSet::new();
    // Inputs inside a collection's header cells narrow that collection.
    for t in collections(v) {
        let fields: Vec<usize> = header_cells(v, t)
            .into_iter()
            .flat_map(|h| v.obs.descendants(h))
            .filter(|&d| {
                is_control(v.n(d)) && !is_checkbox(v.n(d)) && v.n(d).tag != "select"
                    || search_like(v, d)
            })
            .collect();
        if !fields.is_empty() {
            taken.extend(&fields);
            out.push(mk(
                v,
                CompositeKind::FilterBar,
                t,
                Some(t),
                &fields,
                &["header:inputs"],
            ));
        }
    }
    for i in 0..v.obs.len() {
        let in_popup = v
            .obs
            .ancestors(i)
            .any(|a| role(v.n(a)) == "combobox" || v.n(a).state("haspopup").is_some());
        if taken.contains(&i) || in_popup || !search_like(v, i) {
            continue;
        }
        let root = v
            .nearest_with_role(i, &["search", "form"])
            .or_else(|| v.obs.ancestors(i).find(|&a| v.n(a).tag == "form"))
            .unwrap_or(i);
        let members: Vec<usize> = std::iter::once(i)
            .chain(v.obs.descendants(root).into_iter().filter(|&d| {
                d != i && (is_control(v.n(d)) || is_action(v.n(d))) && !is_checkbox(v.n(d))
            }))
            .collect();
        taken.insert(i);
        let ev: &[&'static str] = if root == i {
            &["field:search"]
        } else {
            &["field:search", "landmark:search"]
        };
        let mut c = mk(v, CompositeKind::FilterBar, root, None, &members, ev);
        // Only a search landmark settles that its buttons read; a plain form's submit may write.
        if v.nearest_with_role(i, &["search"]).is_some() {
            classify(v, &mut c, &members, "read");
        }
        out.push(c);
    }
    // A named group of options (links or choices) headed "Filter".
    for i in 0..v.obs.len() {
        let n = v.n(i);
        let head = matches!(n.tag.as_str(), "h1" | "h2" | "h3" | "h4" | "h5" | "h6")
            || role(n) == "heading";
        if !head || v.text(i).map(|t| t.to_lowercase()).as_deref() != Some("filter") {
            continue;
        }
        let Some(p) = n.parent else { continue };
        let acts: Vec<usize> = v
            .obs
            .descendants(p)
            .into_iter()
            .filter(|&d| is_action(v.n(d)))
            .collect();
        if acts.len() >= 2 {
            let mut c = mk(
                v,
                CompositeKind::FilterBar,
                p,
                None,
                &acts,
                &["heading:filter"],
            );
            classify(v, &mut c, &acts, "read");
            out.push(c);
        }
    }
}

fn file_inputs(v: &View, out: &mut Vec<Composite>) {
    for i in 0..v.obs.len() {
        if v.n(i).state("type") == Some("file") {
            out.push(mk(
                v,
                CompositeKind::FileInput,
                i,
                Some(i),
                &[i],
                &["input:file"],
            ));
        }
    }
}

const WEEKDAYS: [&str; 14] = [
    "su", "mo", "tu", "we", "th", "fr", "sa", "sun", "mon", "tue", "wed", "thu", "fri", "sat",
];

fn date_pickers(v: &View, out: &mut Vec<Composite>) {
    for i in 0..v.obs.len() {
        if matches!(
            v.n(i).state("type"),
            Some("date" | "datetime-local" | "month" | "week" | "time")
        ) {
            out.push(mk(
                v,
                CompositeKind::DatePicker,
                i,
                Some(i),
                &[i],
                &["input:date"],
            ));
        }
    }
    for t in collections(v) {
        let heads: BTreeSet<String> = header_cells(v, t)
            .into_iter()
            .filter_map(|h| v.text(h))
            .map(|s| s.to_lowercase())
            .filter(|s| {
                WEEKDAYS.contains(&s.as_str()) || WEEKDAYS.contains(&s.trim_end_matches('.'))
            })
            .collect();
        let days: BTreeSet<u32> = v
            .obs
            .descendants(t)
            .into_iter()
            .filter(|&d| {
                is_action(v.n(d))
                    || matches!(role(v.n(d)), "gridcell" | "cell")
                    || v.n(d).tag == "td"
            })
            .filter_map(|d| v.text(d)?.parse::<u32>().ok())
            .filter(|d| (1..=31).contains(d))
            .collect();
        if heads.len() >= 5 && days.len() >= 28 {
            let day_items: Vec<usize> = v
                .obs
                .descendants(t)
                .into_iter()
                .filter(|&d| {
                    is_action(v.n(d)) && v.text(d).is_some_and(|s| s.parse::<u32>().is_ok())
                })
                .collect();
            out.push(mk(
                v,
                CompositeKind::DatePicker,
                t,
                None,
                &day_items,
                &["grid:month_days"],
            ));
        }
    }
}

fn editable_cells(v: &View, out: &mut Vec<Composite>) {
    for t in collections(v) {
        let cells: Vec<usize> = v
            .obs
            .descendants(t)
            .into_iter()
            .filter(|&d| {
                let dn = v.n(d);
                matches!(role(dn), "gridcell" | "cell") || dn.tag == "td"
            })
            .filter(|&c| {
                v.n(c).state("readonly") == Some("false")
                    || v.obs.descendants(c).into_iter().any(|d| {
                        let dn = v.n(d);
                        is_control(dn)
                            && !is_checkbox(dn)
                            && dn.tag != "select"
                            && dn.has(crate::obs::F_EDITABLE)
                            && dn.visible()
                    })
            })
            .collect();
        if !cells.is_empty() {
            out.push(mk(
                v,
                CompositeKind::EditableCell,
                t,
                Some(t),
                &cells,
                &["cell:editable"],
            ));
        }
    }
}

/// Every composite the observation supports, in document order of the root and then by kind.
pub fn recognize_composites(obs: &Observation) -> Vec<Composite> {
    let v = View { obs };
    let mut out = Vec::new();
    tabs(&v, &mut out);
    disclosures(&v, &mut out);
    menus(&v, &mut out);
    dialogs(&v, &mut out);
    comboboxes(&v, &mut out);
    trees(&v, &mut out);
    paginations(&v, &mut out);
    bulk_selections(&v, &mut out);
    sortable_columns(&v, &mut out);
    filter_bars(&v, &mut out);
    file_inputs(&v, &mut out);
    date_pickers(&v, &mut out);
    editable_cells(&v, &mut out);
    out.sort_by_key(|c| (c.node, c.kind));
    out
}

fn row_signatures(v: &View, table: usize) -> BTreeSet<String> {
    v.obs
        .descendants(table)
        .into_iter()
        .filter(|&r| is_body_row(v, r))
        .map(|r| {
            let cells: Vec<String> = v
                .obs
                .descendants(r)
                .into_iter()
                .filter_map(|d| v.n(d).text.as_deref().and_then(clean))
                .take(6)
                .collect();
            cells.join("|")
        })
        .filter(|s| !s.is_empty())
        .collect()
}

/// Collections whose rows change when their region is scrolled hold more records than the page renders.
/// Both observations must come from the same page state, the second after a scroll probe in disposable state.
pub fn virtual_lists(before: &Observation, after: &Observation) -> Vec<Composite> {
    let (b, a) = (View { obs: before }, View { obs: after });
    let mut out = Vec::new();
    for t in collections(&b) {
        let id = b.n(t).id;
        let Some(t2) = after.index_of(id) else {
            continue;
        };
        let (sb, sa) = (row_signatures(&b, t), row_signatures(&a, t2));
        if sb.is_empty() || sa.is_empty() || sb == sa {
            continue;
        }
        let replaced = sb.difference(&sa).next().is_some();
        let ev: &[&'static str] = if replaced {
            &["scroll:rows_replaced"]
        } else {
            &["scroll:rows_added"]
        };
        out.push(mk(&b, CompositeKind::VirtualList, t, Some(t), &[], ev));
    }
    out
}
