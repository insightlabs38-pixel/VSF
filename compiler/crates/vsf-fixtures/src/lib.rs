#![cfg_attr(test, allow(clippy::unwrap_used, clippy::expect_used, clippy::panic))]

use vsf_canon::Value;
use vsf_observation::{Builder, NONE};

pub const GOLDEN_SEEDS: [u64; 3] = [1, 42, 20_261_018];

const ROLES: [&str; 6] = ["button", "textbox", "row", "link", "checkbox", "heading"];
const WORDS: [&str; 8] = [
    "invoice", "customer", "status", "export", "filter", "total", "owner", "region",
];

pub struct SplitMix64(u64);

impl SplitMix64 {
    pub fn new(seed: u64) -> Self {
        Self(seed)
    }

    pub fn next_u64(&mut self) -> u64 {
        self.0 = self.0.wrapping_add(0x9E37_79B9_7F4A_7C15);
        let mut z = self.0;
        z = (z ^ (z >> 30)).wrapping_mul(0xBF58_476D_1CE4_E5B9);
        z = (z ^ (z >> 27)).wrapping_mul(0x94D0_49BB_1331_11EB);
        z ^ (z >> 31)
    }

    pub fn below(&mut self, bound: u64) -> u64 {
        self.next_u64() % bound
    }
}

fn pick<'a>(rng: &mut SplitMix64, list: &[&'a str]) -> &'a str {
    list[rng.below(list.len() as u64) as usize]
}

fn text_for(rng: &mut SplitMix64, index: u64) -> String {
    let base = format!("{} {}", pick(rng, &WORDS), pick(rng, &WORDS));
    if index % 4 == 3 {
        // Secret-shaped values are assembled at run time so none is committed.
        let key = ["api", "_key"].concat();
        format!(
            "{base} {key}={:016x}{:016x}",
            rng.next_u64(),
            rng.next_u64()
        )
    } else {
        base
    }
}

pub fn generate_payload(seed: u64) -> Value {
    let mut rng = SplitMix64::new(seed);
    let count = 4 + rng.below(5);
    let nodes = (0..count)
        .map(|i| {
            let role = pick(&mut rng, &ROLES);
            let name = pick(&mut rng, &WORDS);
            let text = text_for(&mut rng, i);
            Value::Object(vec![
                ("id".to_string(), Value::Int(i as i64)),
                ("role".to_string(), Value::Str(role.to_string())),
                ("name".to_string(), Value::Str(name.to_string())),
                ("text".to_string(), Value::Str(text)),
            ])
        })
        .collect();
    Value::Object(vec![
        (
            "app".to_string(),
            Value::Str("synthetic-orders".to_string()),
        ),
        ("seed".to_string(), Value::Int(seed as i64)),
        ("nodes".to_string(), Value::Array(nodes)),
    ])
}

pub fn generate_envelope_bytes(seed: u64) -> Vec<u8> {
    let envelope = vsf_schema::make_envelope(seed as i64, generate_payload(seed));
    vsf_canon::to_canonical(&envelope)
}

const PACK_TAGS: [&str; 10] = [
    "div", "span", "button", "a", "input", "tr", "td", "li", "h2", "form",
];
const PACK_ROLES: [&str; 6] = ["button", "link", "textbox", "row", "cell", "heading"];

/// Deterministic synthetic DOM of `nodes` elements; the TypeScript generator mirrors it call for call.
pub fn generate_pack(seed: u64, nodes: u32) -> Vec<u8> {
    let mut rng = SplitMix64::new(seed);
    let mut b = Builder::new();
    if nodes > 0 {
        b.node(NONE, "html", None, None, None, 1);
    }
    for i in 1..u64::from(nodes) {
        let parent = if rng.below(4) == 0 {
            rng.below(i)
        } else {
            i - 1 - rng.below(i.min(8))
        };
        let tag = pick(&mut rng, &PACK_TAGS);
        let role = (rng.below(3) == 0).then(|| pick(&mut rng, &PACK_ROLES));
        let name = (rng.below(2) == 0).then(|| pick(&mut rng, &WORDS));
        let text = (rng.below(2) == 0).then(|| format!("{} {}", pick(&mut rng, &WORDS), i));
        let flags = rng.below(16) as u32;
        b.node(parent as u32, tag, role, name, text.as_deref(), flags);
    }
    vsf_observation::encode(&b.finish())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn splitmix_matches_reference_vector() {
        let mut rng = SplitMix64::new(0);
        assert_eq!(rng.next_u64(), 0xE220_A839_7B1D_CDAF);
        assert_eq!(rng.next_u64(), 0x6E78_9E6A_A1B9_65F4);
    }

    #[test]
    fn generation_is_deterministic_and_seed_sensitive() {
        assert_eq!(generate_envelope_bytes(42), generate_envelope_bytes(42));
        assert_ne!(generate_envelope_bytes(42), generate_envelope_bytes(43));
    }

    #[test]
    fn packs_are_deterministic_valid_and_scale() {
        assert_eq!(generate_pack(7, 1000), generate_pack(7, 1000));
        assert_ne!(generate_pack(7, 1000), generate_pack(8, 1000));
        for n in [0, 1, 2, 1000, 10_000] {
            let bytes = generate_pack(3, n);
            assert_eq!(
                vsf_observation::decode(&bytes).unwrap().nodes.len(),
                n as usize
            );
        }
    }

    #[test]
    fn envelopes_validate() {
        for seed in GOLDEN_SEEDS {
            assert!(vsf_schema::verify_envelope_bytes(&generate_envelope_bytes(seed)).is_ok());
        }
    }
}
