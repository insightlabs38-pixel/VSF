//! Inverted feature index with sparse and bitset postings, plus the full-scan baseline it must agree with.

use crate::obs::{
    F_ACCOUNT, F_CREDENTIAL, F_DISABLED, F_EDITABLE, F_INTERACTIVE, F_OPAQUE, F_REDACTED, F_RTL,
    F_VISIBLE, Node, Observation,
};
use std::collections::HashMap;

const FLAGS: [(&str, u32); 9] = [
    ("visible", F_VISIBLE),
    ("interactive", F_INTERACTIVE),
    ("editable", F_EDITABLE),
    ("redacted", F_REDACTED),
    ("disabled", F_DISABLED),
    ("opaque", F_OPAQUE),
    ("rtl", F_RTL),
    ("account", F_ACCOUNT),
    ("credential", F_CREDENTIAL),
];

/// Lowercase alphanumeric words of at most 32 characters; the lexical features of a name or text.
pub fn tokens(s: &str) -> impl Iterator<Item = String> + '_ {
    s.split(|c: char| !c.is_alphanumeric())
        .filter(|w| !w.is_empty())
        .map(|w| w.chars().take(32).flat_map(char::to_lowercase).collect())
}

/// Features by which a node can be found. Kept apart from `node_has` so the index and the scan are independent.
pub fn features_of(n: &Node) -> Vec<String> {
    let mut f = vec![format!("tag:{}", n.tag)];
    if let Some(r) = &n.role {
        f.push(format!("role:{r}"));
    }
    for (name, bit) in FLAGS {
        if n.flags & bit != 0 {
            f.push(format!("flag:{name}"));
        }
    }
    f.push(if n.name.is_some() { "named" } else { "unnamed" }.to_string());
    if let Some(s) = &n.scope {
        f.push(format!("scope:{}", s.kind));
    }
    for t in n.name.iter().chain(n.text.iter()).flat_map(|s| tokens(s)) {
        f.push(format!("tok:{t}"));
    }
    f.sort();
    f.dedup();
    f
}

/// Direct field check of one feature: the reference semantics for the index.
pub fn node_has(n: &Node, feature: &str) -> bool {
    let Some((kind, v)) = feature.split_once(':') else {
        return match feature {
            "named" => n.name.is_some(),
            "unnamed" => n.name.is_none(),
            _ => false,
        };
    };
    match kind {
        "tag" => n.tag == v,
        "role" => n.role.as_deref() == Some(v),
        "flag" => FLAGS
            .iter()
            .any(|(name, bit)| *name == v && n.flags & bit != 0),
        "scope" => n.scope.as_ref().is_some_and(|s| s.kind == v),
        "tok" => n
            .name
            .iter()
            .chain(n.text.iter())
            .any(|s| tokens(s).any(|t| t == v)),
        _ => false,
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Query {
    Feat(String),
    And(Vec<Query>),
    Or(Vec<Query>),
    Not(Box<Query>),
}

impl Query {
    pub fn feat(s: &str) -> Query {
        Query::Feat(s.to_string())
    }
}

/// Reference evaluation: one full scan, features checked straight off the node.
pub fn scan(obs: &Observation, q: &Query) -> Vec<u32> {
    fn holds(n: &Node, q: &Query) -> bool {
        match q {
            Query::Feat(f) => node_has(n, f),
            Query::And(v) => v.iter().all(|x| holds(n, x)),
            Query::Or(v) => v.iter().any(|x| holds(n, x)),
            Query::Not(x) => !holds(n, x),
        }
    }
    (0..obs.len())
        .filter(|&i| holds(&obs.nodes[i], q))
        .map(|i| i as u32)
        .collect()
}

#[derive(Debug, Clone, PartialEq, Eq)]
enum Set {
    Sparse(Vec<u32>),
    Dense(Vec<u64>),
}

fn words(n: usize) -> usize {
    n.div_ceil(64)
}

impl Set {
    fn ids(&self) -> Vec<u32> {
        match self {
            Set::Sparse(v) => v.clone(),
            Set::Dense(w) => {
                let mut out = Vec::new();
                for (k, &word) in w.iter().enumerate() {
                    let mut x = word;
                    while x != 0 {
                        out.push((k * 64) as u32 + x.trailing_zeros());
                        x &= x - 1;
                    }
                }
                out
            }
        }
    }

    fn dense(&self, n: usize) -> Vec<u64> {
        match self {
            Set::Dense(w) => w.clone(),
            Set::Sparse(v) => {
                let mut w = vec![0u64; words(n)];
                for &i in v {
                    w[(i / 64) as usize] |= 1 << (i % 64);
                }
                w
            }
        }
    }

    fn size(&self) -> usize {
        match self {
            Set::Sparse(v) => v.len(),
            Set::Dense(w) => w.iter().map(|x| x.count_ones() as usize).sum(),
        }
    }

    fn and(&self, o: &Set) -> Set {
        match (self, o) {
            (Set::Sparse(a), Set::Sparse(b)) => {
                let (mut i, mut j, mut out) = (0, 0, Vec::new());
                while i < a.len() && j < b.len() {
                    match a[i].cmp(&b[j]) {
                        std::cmp::Ordering::Less => i += 1,
                        std::cmp::Ordering::Greater => j += 1,
                        std::cmp::Ordering::Equal => {
                            out.push(a[i]);
                            i += 1;
                            j += 1;
                        }
                    }
                }
                Set::Sparse(out)
            }
            (Set::Sparse(a), Set::Dense(w)) | (Set::Dense(w), Set::Sparse(a)) => Set::Sparse(
                a.iter()
                    .copied()
                    .filter(|&i| w[(i / 64) as usize] >> (i % 64) & 1 == 1)
                    .collect(),
            ),
            (Set::Dense(a), Set::Dense(b)) => {
                Set::Dense(a.iter().zip(b).map(|(x, y)| x & y).collect())
            }
        }
    }

    fn or(&self, o: &Set, n: usize) -> Set {
        match (self, o) {
            (Set::Sparse(a), Set::Sparse(b)) if a.len() + b.len() <= n / 32 => {
                let mut out: Vec<u32> = a.iter().chain(b).copied().collect();
                out.sort_unstable();
                out.dedup();
                Set::Sparse(out)
            }
            _ => Set::Dense(
                self.dense(n)
                    .iter()
                    .zip(o.dense(n))
                    .map(|(x, y)| x | y)
                    .collect(),
            ),
        }
    }

    fn not(&self, n: usize) -> Set {
        let mut w: Vec<u64> = self.dense(n).iter().map(|x| !x).collect();
        if !n.is_multiple_of(64)
            && let Some(last) = w.last_mut()
        {
            *last &= (1u64 << (n % 64)) - 1;
        }
        Set::Dense(w)
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub struct IndexStats {
    pub features: usize,
    pub sparse: usize,
    pub dense: usize,
    pub posting_bytes: usize,
    pub key_bytes: usize,
}

impl IndexStats {
    pub fn bytes(&self) -> usize {
        self.posting_bytes + self.key_bytes
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub struct QueryStats {
    /// Sum of the sizes of the postings read: the candidates actually examined.
    pub candidates: usize,
    pub results: usize,
}

pub struct FeatureIndex {
    n: usize,
    postings: HashMap<String, Set>,
}

impl FeatureIndex {
    pub fn build(obs: &Observation) -> FeatureIndex {
        let n = obs.len();
        let mut lists: HashMap<String, Vec<u32>> = HashMap::new();
        for (i, node) in obs.nodes.iter().enumerate() {
            for f in features_of(node) {
                lists.entry(f).or_default().push(i as u32);
            }
        }
        let postings = lists
            .into_iter()
            .map(|(k, v)| {
                let set = if v.len() * 32 > n {
                    Set::Sparse(v).to_dense(n)
                } else {
                    Set::Sparse(v)
                };
                (k, set)
            })
            .collect();
        FeatureIndex { n, postings }
    }

    pub fn stats(&self) -> IndexStats {
        let mut s = IndexStats {
            features: self.postings.len(),
            ..IndexStats::default()
        };
        for (k, p) in &self.postings {
            s.key_bytes += k.len();
            match p {
                Set::Sparse(v) => {
                    s.sparse += 1;
                    s.posting_bytes += v.len() * 4;
                }
                Set::Dense(w) => {
                    s.dense += 1;
                    s.posting_bytes += w.len() * 8;
                }
            }
        }
        s
    }

    fn eval(&self, q: &Query, st: &mut QueryStats) -> Set {
        match q {
            Query::Feat(f) => match self.postings.get(f) {
                Some(p) => {
                    st.candidates += p.size();
                    p.clone()
                }
                None => Set::Sparse(Vec::new()),
            },
            Query::And(v) => {
                let mut parts: Vec<Set> = v.iter().map(|x| self.eval(x, st)).collect();
                parts.sort_by_key(Set::size);
                let mut it = parts.into_iter();
                let first = it
                    .next()
                    .unwrap_or_else(|| Set::Dense(vec![u64::MAX; words(self.n)]).clip(self.n));
                it.fold(first, |acc, s| acc.and(&s))
            }
            Query::Or(v) => v
                .iter()
                .map(|x| self.eval(x, st))
                .reduce(|a, b| a.or(&b, self.n))
                .unwrap_or(Set::Sparse(Vec::new())),
            Query::Not(x) => self.eval(x, st).not(self.n),
        }
    }

    pub fn query(&self, q: &Query) -> (Vec<u32>, QueryStats) {
        let mut st = QueryStats::default();
        let ids = self.eval(q, &mut st).ids();
        st.results = ids.len();
        (ids, st)
    }
}

impl Set {
    fn to_dense(&self, n: usize) -> Set {
        Set::Dense(self.dense(n))
    }

    fn clip(self, n: usize) -> Set {
        let mut w = self.dense(n);
        if !n.is_multiple_of(64)
            && let Some(last) = w.last_mut()
        {
            *last &= (1u64 << (n % 64)) - 1;
        }
        w.truncate(words(n));
        Set::Dense(w)
    }
}

impl Query {
    /// Whether one node satisfies the query, by direct field checks.
    pub fn holds(&self, n: &Node) -> bool {
        match self {
            Query::Feat(f) => node_has(n, f),
            Query::And(v) => v.iter().all(|x| x.holds(n)),
            Query::Or(v) => v.iter().any(|x| x.holds(n)),
            Query::Not(x) => !x.holds(n),
        }
    }
}
