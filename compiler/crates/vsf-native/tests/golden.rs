#![allow(clippy::unwrap_used, clippy::expect_used, clippy::panic)]

use std::path::PathBuf;
use vsf_canon::Value;

fn golden_dir() -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../../../fixtures/golden")
}

#[test]
fn native_output_is_byte_identical_to_committed_golden() {
    let dir = golden_dir();
    let manifest = vsf_canon::parse(&std::fs::read(dir.join("MANIFEST.json")).unwrap()).unwrap();
    let Some(Value::Array(entries)) = manifest.get("entries") else {
        panic!("entries")
    };
    assert_eq!(entries.len(), vsf_fixtures::GOLDEN_SEEDS.len());
    for entry in entries {
        let seed = entry.get("seed").unwrap().as_int().unwrap() as u64;
        let input = vsf_fixtures::generate_envelope_bytes(seed);
        assert_eq!(
            vsf_canon::sha256_hex(&input),
            entry.get("input_sha256").unwrap().as_str().unwrap()
        );
        let compiled = vsf_fabric::compile_envelope(&input).unwrap();
        let golden =
            std::fs::read(dir.join(entry.get("compiled_file").unwrap().as_str().unwrap())).unwrap();
        assert_eq!(compiled, golden, "seed {seed}");
        assert_eq!(
            vsf_canon::sha256_hex(&golden),
            entry.get("compiled_sha256").unwrap().as_str().unwrap()
        );
    }
}

#[test]
fn golden_outputs_contain_no_unredacted_secret_shapes() {
    for seed in vsf_fixtures::GOLDEN_SEEDS {
        let golden =
            std::fs::read_to_string(golden_dir().join(format!("compiled-seed-{seed}.json")))
                .unwrap();
        assert_eq!(
            vsf_redact::redact(&golden),
            golden,
            "seed {seed} leaks a secret shape"
        );
        assert!(!golden.contains("api_key="));
    }
}
