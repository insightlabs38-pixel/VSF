//! Fixture descriptors: parseable, seeded, polarity-tagged inputs for the conformance runner.

use crate::json::{Obj, Res, array, err, list, object, s};
use std::collections::BTreeMap;
use vsf_canon::Value;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Node {
    pub id: u32,
    pub parent: u32,
    pub tag: String,
    pub attrs: BTreeMap<String, String>,
    pub text: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Expected {
    pub root: u32,
    pub primitive: String,
    pub columns: Vec<String>,
    pub rows: Vec<Vec<String>>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Fixture {
    pub id: String,
    pub primitive: String,
    pub polarity: String,
    pub app: String,
    pub origin: String,
    pub seed: Option<u64>,
    pub operations: Vec<String>,
    pub nodes: Vec<Node>,
    pub expect: Vec<Expected>,
}

fn strings_map(v: &Value, path: &str) -> Res<BTreeMap<String, String>> {
    let Value::Object(entries) = v else {
        return Err(err("registry.not_object", path));
    };
    entries
        .iter()
        .map(|(k, x)| Ok((k.clone(), crate::json::text(x, &format!("{path}/{k}"))?)))
        .collect()
}

impl Fixture {
    pub fn decode(v: &Value) -> Res<Fixture> {
        let mut o = Obj::new(v, "")?;
        if o.str("schema")? != "vsf-primitive-fixture-v1" {
            return Err(err("registry.bad_schema", "fixture"));
        }
        let polarity = o.str("polarity")?;
        if !matches!(polarity.as_str(), "positive" | "negative" | "adversarial") {
            return Err(err("fixture.bad_polarity", polarity));
        }
        let seed = match o.opt("seed") {
            Some(x) => Some(
                u64::try_from(
                    x.as_int()
                        .ok_or_else(|| err("registry.wrong_type", "/seed"))?,
                )
                .map_err(|_| err("registry.wrong_type", "/seed"))?,
            ),
            None => None,
        };
        let mut nodes = Vec::new();
        for (i, x) in array(o.req("nodes")?, "/nodes")?.iter().enumerate() {
            let p = format!("/nodes/{i}");
            let mut n = Obj::new(x, &p)?;
            let id = u32::try_from(n.int("id")?)
                .map_err(|_| err("registry.wrong_type", format!("{p}/id")))?;
            let parent = u32::try_from(n.int("parent")?)
                .map_err(|_| err("registry.wrong_type", format!("{p}/parent")))?;
            let node = Node {
                id,
                parent,
                tag: n.str("tag")?,
                attrs: strings_map(n.req("attrs")?, &format!("{p}/attrs"))?,
                text: n.str("text")?,
            };
            n.finish()?;
            nodes.push(node);
        }
        let mut expect = Vec::new();
        for (i, x) in array(o.req("expect")?, "/expect")?.iter().enumerate() {
            let p = format!("/expect/{i}");
            let mut e = Obj::new(x, &p)?;
            let columns = e.strs("columns")?;
            let mut rows = Vec::new();
            for (j, r) in array(e.req("rows")?, &format!("{p}/rows"))?
                .iter()
                .enumerate()
            {
                rows.push(crate::json::texts(r, &format!("{p}/rows/{j}"))?);
            }
            let root = u32::try_from(e.int("root")?)
                .map_err(|_| err("registry.wrong_type", format!("{p}/root")))?;
            let item = Expected {
                root,
                primitive: e.str("primitive")?,
                columns,
                rows,
            };
            e.finish()?;
            expect.push(item);
        }
        let mut src = Obj::new(o.req("source")?, "/source")?;
        let (app, origin) = (src.str("app")?, src.str("origin")?);
        src.finish()?;
        let fixture = Fixture {
            id: o.str("id")?,
            primitive: o.str("primitive")?,
            polarity,
            app,
            origin,
            seed,
            operations: o.strs("operations")?,
            nodes,
            expect,
        };
        o.finish()?;
        fixture.check()?;
        Ok(fixture)
    }

    /// Structural sanity: unique ids, parents precede children, positive fixtures expect something.
    pub fn check(&self) -> Res<()> {
        let mut seen = std::collections::BTreeSet::new();
        for n in &self.nodes {
            if n.id == 0 || !seen.insert(n.id) {
                return Err(err("fixture.bad_node_id", format!("{}:{}", self.id, n.id)));
            }
            if n.parent != 0 && !seen.contains(&n.parent) {
                return Err(err(
                    "fixture.parent_after_child",
                    format!("{}:{}", self.id, n.id),
                ));
            }
        }
        if self.polarity == "positive" && self.expect.is_empty() {
            return Err(err("fixture.positive_without_expectation", self.id.clone()));
        }
        if self.polarity == "negative" && !self.expect.is_empty() {
            return Err(err("fixture.negative_with_expectation", self.id.clone()));
        }
        Ok(())
    }

    pub fn encode(&self) -> Value {
        let nodes = self
            .nodes
            .iter()
            .map(|n| {
                object(vec![
                    (
                        "attrs",
                        Value::Object(n.attrs.iter().map(|(k, v)| (k.clone(), s(v))).collect()),
                    ),
                    ("id", Value::Int(i64::from(n.id))),
                    ("parent", Value::Int(i64::from(n.parent))),
                    ("tag", s(&n.tag)),
                    ("text", s(&n.text)),
                ])
            })
            .collect();
        let expect = self
            .expect
            .iter()
            .map(|e| {
                object(vec![
                    ("columns", list(&e.columns)),
                    ("primitive", s(&e.primitive)),
                    ("root", Value::Int(i64::from(e.root))),
                    (
                        "rows",
                        Value::Array(e.rows.iter().map(|r| list(r)).collect()),
                    ),
                ])
            })
            .collect();
        let mut fields = vec![
            ("expect", Value::Array(expect)),
            ("id", s(&self.id)),
            ("nodes", Value::Array(nodes)),
            ("operations", list(&self.operations)),
            ("polarity", s(&self.polarity)),
            ("primitive", s(&self.primitive)),
            ("schema", s("vsf-primitive-fixture-v1")),
            (
                "source",
                object(vec![("app", s(&self.app)), ("origin", s(&self.origin))]),
            ),
        ];
        if let Some(seed) = self.seed {
            fields.push(("seed", Value::Int(seed as i64)));
        }
        object(fields)
    }
}

// ---- seeded generation -----------------------------------------------------------------------------

pub struct Rng(u64);

impl Rng {
    pub fn new(seed: u64) -> Rng {
        Rng(seed.wrapping_mul(0x9E37_79B9_7F4A_7C15) ^ 0xD1B5_4A32_D192_ED03)
    }

    pub fn step(&mut self) -> u64 {
        self.0 ^= self.0 >> 12;
        self.0 ^= self.0 << 25;
        self.0 ^= self.0 >> 27;
        self.0.wrapping_mul(0x2545_F491_4F6C_DD1D)
    }

    pub fn below(&mut self, n: u64) -> u64 {
        self.step() % n
    }
}

const HEADERS: [&str; 10] = [
    "Order",
    "Customer",
    "Total",
    "Placed on",
    "Stage",
    "Região",
    "הערות",
    "Café",
    "金額",
    "Owner",
];
const WORDS: [&str; 12] = [
    "alpha",
    "beta",
    "gamma",
    "1,204.50",
    "2024-02-29",
    "won",
    "Zoë",
    "שלום",
    "東京",
    "x y",
    "n/a",
    "0",
];

/// The logical content both renderings must agree on.
pub fn logical(seed: u64) -> (Vec<String>, Vec<Vec<String>>) {
    let mut r = Rng::new(seed);
    let cols = 1 + r.below(6) as usize;
    let start = r.below(HEADERS.len() as u64) as usize;
    let columns: Vec<String> = (0..cols)
        .map(|i| HEADERS[(start + i) % HEADERS.len()].to_string())
        .collect();
    let rows = (0..r.below(9) as usize)
        .map(|_| {
            (0..cols)
                .map(|_| WORDS[r.below(WORDS.len() as u64) as usize].to_string())
                .collect()
        })
        .collect();
    (columns, rows)
}

struct Builder {
    nodes: Vec<Node>,
}

impl Builder {
    fn add(&mut self, parent: u32, tag: &str, attrs: &[(&str, &str)], text: &str) -> u32 {
        let id = self.nodes.len() as u32 + 1;
        self.nodes.push(Node {
            id,
            parent,
            tag: tag.into(),
            attrs: attrs
                .iter()
                .map(|(k, v)| ((*k).into(), (*v).into()))
                .collect(),
            text: text.into(),
        });
        id
    }
}

pub fn render_html_table(seed: u64) -> Fixture {
    let (columns, rows) = logical(seed);
    let mut r = Rng::new(seed ^ 0xA5A5);
    let mut b = Builder { nodes: Vec::new() };
    let root = b.add(0, "body", &[], "");
    let table = b.add(root, "table", &[("class", "data")], "");
    if r.below(2) == 0 {
        b.add(table, "caption", &[], "Caption text is not data");
    }
    let thead = b.add(table, "thead", &[], "");
    let hr = b.add(thead, "tr", &[], "");
    for c in &columns {
        let th = b.add(hr, "th", &[("scope", "col")], "");
        b.add(th, "span", &[], c);
    }
    let tbody = b.add(table, "tbody", &[], "");
    for row in &rows {
        let tr = b.add(tbody, "tr", &[], "");
        for cell in row {
            let td = b.add(tr, "td", &[], "");
            if r.below(3) == 0 {
                let em = b.add(td, "em", &[], "");
                b.add(em, "span", &[], cell);
            } else {
                b.add(td, "span", &[], cell);
            }
        }
    }
    finish(
        format!("table.html.seed{seed}"),
        "TABLE-001",
        "synthetic-html-tables",
        seed,
        b,
        table,
        "TABLE-001",
        columns,
        rows,
    )
}

pub fn render_aria_grid(seed: u64) -> Fixture {
    let (columns, rows) = logical(seed);
    let mut r = Rng::new(seed ^ 0x5A5A);
    let mut b = Builder { nodes: Vec::new() };
    let root = b.add(0, "div", &[("id", "app")], "");
    let grid = b.add(
        root,
        "div",
        &[("role", "grid"), ("style", "display:grid")],
        "",
    );
    let hr = b.add(grid, "div", &[("role", "row")], "");
    for c in &columns {
        let h = b.add(
            hr,
            "div",
            &[("role", "columnheader"), ("style", "font-weight:bold")],
            "",
        );
        b.add(h, "p", &[], c);
    }
    for row in &rows {
        let tr = b.add(grid, "div", &[("role", "row")], "");
        for cell in row {
            let td = b.add(tr, "div", &[("role", "gridcell")], "");
            if r.below(3) == 0 {
                let w = b.add(td, "div", &[("class", "wrap")], "");
                b.add(w, "b", &[], cell);
            } else {
                b.add(td, "p", &[], cell);
            }
        }
    }
    finish(
        format!("table.aria.seed{seed}"),
        "TABLE-002",
        "synthetic-aria-grids",
        seed,
        b,
        grid,
        "TABLE-002",
        columns,
        rows,
    )
}

#[allow(clippy::too_many_arguments)]
fn finish(
    id: String,
    primitive: &str,
    app: &str,
    seed: u64,
    b: Builder,
    root: u32,
    expected: &str,
    columns: Vec<String>,
    rows: Vec<Vec<String>>,
) -> Fixture {
    Fixture {
        id,
        primitive: primitive.into(),
        polarity: "positive".into(),
        app: app.into(),
        origin: "seeded".into(),
        seed: Some(seed),
        operations: vec!["collection.read".into()],
        nodes: b.nodes,
        expect: vec![Expected {
            root,
            primitive: expected.into(),
            columns,
            rows,
        }],
    }
}

/// A look-alike: laid out as a grid and filled with the same data, but with no semantics at all.
pub fn render_visual_only(seed: u64) -> Fixture {
    let (columns, rows) = logical(seed);
    let mut b = Builder { nodes: Vec::new() };
    let root = b.add(
        0,
        "div",
        &[("style", "display:grid;grid-template-columns:repeat(3,1fr)")],
        "",
    );
    for c in &columns {
        b.add(root, "div", &[("class", "hd")], c);
    }
    for row in &rows {
        for cell in row {
            b.add(root, "div", &[("class", "c")], cell);
        }
    }
    Fixture {
        id: format!("table.visual-only.seed{seed}"),
        primitive: "TABLE-002".into(),
        polarity: "negative".into(),
        app: "synthetic-visual-lookalikes".into(),
        origin: "seeded".into(),
        seed: Some(seed),
        operations: vec!["collection.read".into()],
        nodes: b.nodes,
        expect: Vec::new(),
    }
}

pub fn load_dir(dir: &std::path::Path) -> Result<Vec<Fixture>, crate::json::Error> {
    let mut paths = Vec::new();
    let mut stack = vec![dir.to_path_buf()];
    while let Some(d) = stack.pop() {
        let entries = std::fs::read_dir(&d)
            .map_err(|e| err("fixture.io", format!("{}: {e}", d.display())))?;
        for e in entries {
            let p = e.map_err(|e| err("fixture.io", e.to_string()))?.path();
            if p.is_dir() {
                stack.push(p);
            } else if p.extension().is_some_and(|x| x == "json") {
                paths.push(p);
            }
        }
    }
    paths.sort();
    let mut out: Vec<Fixture> = Vec::new();
    for p in paths {
        let bytes =
            std::fs::read(&p).map_err(|e| err("fixture.io", format!("{}: {e}", p.display())))?;
        let f = Fixture::decode(&crate::model::load(&bytes)?)?;
        if out.iter().any(|x| x.id == f.id) {
            return Err(err("fixture.duplicate_id", f.id));
        }
        out.push(f);
    }
    Ok(out)
}

pub const SEEDS: [u64; 3] = [1, 2, 3];

/// Seeded fixtures as (relative path, file text); the committed files must equal these.
pub fn seeded_files() -> Vec<(String, String)> {
    SEEDS
        .iter()
        .flat_map(|&s| {
            [
                ("html-table", render_html_table(s)),
                ("aria-grid", render_aria_grid(s)),
                ("lookalike", render_visual_only(s)),
            ]
        })
        .map(|(dir, f)| {
            (
                format!("seeded/{dir}/{}.json", f.id),
                crate::json::pretty(&f.encode()),
            )
        })
        .collect()
}
