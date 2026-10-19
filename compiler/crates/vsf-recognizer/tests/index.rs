#![allow(clippy::unwrap_used, clippy::expect_used, clippy::panic)]

use std::path::PathBuf;
use vsf_recognizer::index::{FeatureIndex, Query, features_of, node_has, scan};
use vsf_recognizer::obs::{Node, Observation, Scope};

struct Rng(u64);
impl Rng {
    fn next(&mut self) -> u64 {
        self.0 ^= self.0 << 13;
        self.0 ^= self.0 >> 7;
        self.0 ^= self.0 << 17;
        self.0
    }
    fn below(&mut self, n: usize) -> usize {
        (self.next() % n as u64) as usize
    }
}

const TAGS: [&str; 8] = ["div", "span", "input", "button", "a", "td", "li", "label"];
const ROLES: [&str; 6] = ["textbox", "button", "link", "cell", "checkbox", "heading"];
const WORDS: [&str; 10] = [
    "name", "email", "save", "cancel", "order", "total", "customer", "Délete", "ÉDIT", "x1",
];

fn random_observation(seed: u64, n: usize) -> Observation {
    let mut r = Rng(seed | 1);
    let mut nodes = Vec::with_capacity(n);
    for i in 0..n {
        let tail = r.below(n / 3 + 1);
        let name = (r.below(3) > 0).then(|| {
            format!(
                "{} {} item{tail}",
                WORDS[r.below(WORDS.len())],
                WORDS[r.below(WORDS.len())]
            )
        });
        nodes.push(Node {
            id: i as u32 + 1,
            parent: (i > 0).then(|| r.below(i)),
            tag: TAGS[r.below(TAGS.len())].to_string(),
            role: (r.below(2) == 0).then(|| ROLES[r.below(ROLES.len())].to_string()),
            name,
            text: (r.below(4) == 0).then(|| WORDS[r.below(WORDS.len())].to_string()),
            flags: (r.next() % 512) as u32,
            rels: Vec::new(),
            scope: (r.below(20) == 0).then(|| Scope {
                kind: "shadow".into(),
                origin: None,
                mode: None,
                opaque: None,
            }),
        });
    }
    Observation::from_nodes(nodes)
}

fn random_query(r: &mut Rng, depth: usize) -> Query {
    const FEATS: [&str; 16] = [
        "tag:input",
        "tag:button",
        "tag:div",
        "role:textbox",
        "role:button",
        "flag:visible",
        "flag:disabled",
        "flag:editable",
        "named",
        "unnamed",
        "tok:save",
        "tok:délete",
        "scope:shadow",
        "tok:nothing",
        "tok:item3",
        "tok:item1",
    ];
    if depth == 0 || r.below(3) == 0 {
        return Query::feat(FEATS[r.below(FEATS.len())]);
    }
    match r.below(3) {
        0 => Query::And(
            (0..1 + r.below(3))
                .map(|_| random_query(r, depth - 1))
                .collect(),
        ),
        1 => Query::Or(
            (0..1 + r.below(3))
                .map(|_| random_query(r, depth - 1))
                .collect(),
        ),
        _ => Query::Not(Box::new(random_query(r, depth - 1))),
    }
}

#[test]
fn the_index_returns_exactly_what_a_full_scan_returns_over_seeded_cases() {
    for (seed, n) in [
        (1u64, 1usize),
        (2, 63),
        (3, 64),
        (4, 65),
        (5, 1000),
        (6, 5000),
        (7, 20_000),
    ] {
        let obs = random_observation(seed * 7919, n);
        let index = FeatureIndex::build(&obs);
        let mut r = Rng(seed * 104_729 + 3);
        for _ in 0..150 {
            let q = random_query(&mut r, 3);
            let (got, st) = index.query(&q);
            assert_eq!(got, scan(&obs, &q), "seed {seed} n {n} query {q:?}");
            assert_eq!(st.results, got.len());
        }
    }
}

#[test]
fn feature_extraction_and_direct_checks_agree_for_every_node() {
    let obs = random_observation(99, 3000);
    for n in &obs.nodes {
        for f in features_of(n) {
            assert!(node_has(n, &f), "{f}");
        }
    }
    let mut checked = 0;
    for n in obs.nodes.iter().take(300) {
        let have = features_of(n);
        for probe in [
            "tag:input",
            "role:textbox",
            "flag:visible",
            "flag:account",
            "tok:save",
            "named",
            "scope:shadow",
        ] {
            assert_eq!(
                have.iter().any(|f| f == probe),
                node_has(n, probe),
                "{probe}"
            );
            checked += 1;
        }
    }
    assert!(checked > 1000);
}

#[test]
fn boundaries_empty_observation_unknown_features_and_negation_tails() {
    let empty = Observation::from_nodes(Vec::new());
    let idx = FeatureIndex::build(&empty);
    assert!(
        idx.query(&Query::Not(Box::new(Query::feat("tag:div"))))
            .0
            .is_empty()
    );
    assert!(idx.query(&Query::And(vec![])).0.is_empty());
    let obs = random_observation(5, 70);
    let idx = FeatureIndex::build(&obs);
    assert!(idx.query(&Query::feat("tag:nonexistent")).0.is_empty());
    assert!(idx.query(&Query::feat("malformed")).0.is_empty());
    let all = idx
        .query(&Query::Not(Box::new(Query::feat("tag:nonexistent"))))
        .0;
    assert_eq!(all, (0..70).collect::<Vec<u32>>());
    assert_eq!(
        idx.query(&Query::And(vec![])).0,
        (0..70).collect::<Vec<u32>>()
    );
    assert_eq!(idx.query(&Query::Or(vec![])).0.len(), 0);
}

#[test]
fn a_selective_query_examines_a_small_fraction_of_the_nodes_and_the_index_stays_small() {
    let obs = random_observation(42, 20_000);
    let idx = FeatureIndex::build(&obs);
    let q = Query::And(vec![
        Query::feat("role:textbox"),
        Query::feat("tok:save"),
        Query::feat("flag:editable"),
    ]);
    let (ids, st) = idx.query(&q);
    assert_eq!(ids, scan(&obs, &q));
    assert!(st.candidates < obs.len() * 2, "{st:?}");
    let narrow = idx.query(&Query::And(vec![
        Query::feat("tok:item7"),
        Query::feat("role:textbox"),
    ]));
    assert_eq!(
        narrow.0,
        scan(
            &obs,
            &Query::And(vec![Query::feat("tok:item7"), Query::feat("role:textbox")])
        )
    );
    assert!(narrow.1.candidates * 10 < obs.len(), "{:?}", narrow.1);
    let s = idx.stats();
    assert!(s.sparse > 0 && s.dense > 0, "{s:?}");
    assert!(s.bytes() < obs.len() * 400, "{s:?}");
}

#[test]
fn real_snapshots_load_and_are_searchable() {
    let dir =
        PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../../../fixtures/recognition/snapshots");
    let mut seen = 0;
    for entry in std::fs::read_dir(dir).unwrap() {
        let path = entry.unwrap().path();
        let obs = Observation::from_json(&std::fs::read(&path).unwrap()).unwrap();
        assert!(obs.digest.starts_with("sha256:"));
        let idx = FeatureIndex::build(&obs);
        let q = Query::feat("flag:interactive");
        assert_eq!(idx.query(&q).0, scan(&obs, &q), "{}", path.display());
        seen += 1;
    }
    assert!(seen >= 6);
}
